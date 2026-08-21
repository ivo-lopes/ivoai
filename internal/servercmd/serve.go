package servercmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	contextsvc "github.com/ivo-lopes/ivoai/internal/context"
	"github.com/ivo-lopes/ivoai/internal/gateway"
	"github.com/ivo-lopes/ivoai/internal/server"
)

const (
	qdrantURL    = "http://127.0.0.1:6333"
	embeddingURL = "http://127.0.0.1:8080"
	memoryURL    = "http://127.0.0.1:49374"
)

func contextService(layout server.Layout) (*contextsvc.Service, error) {
	qdrantKey := strings.TrimSpace(os.Getenv("QDRANT__SERVICE__API_KEY"))
	embeddingKey := strings.TrimSpace(os.Getenv("API_KEY"))
	var err error
	if qdrantKey == "" {
		qdrantKey, err = server.LoadBackendSecret(layout, "qdrant.env", "QDRANT__SERVICE__API_KEY")
		if err != nil {
			return nil, err
		}
	}
	if embeddingKey == "" {
		embeddingKey, err = server.LoadBackendSecret(layout, "embeddings.env", "API_KEY")
		if err != nil {
			return nil, err
		}
	}
	if qdrantKey == "" || embeddingKey == "" {
		return nil, errors.New("private backend credentials are not loaded")
	}
	service, err := contextsvc.NewService(
		contextsvc.HTTPEmbedder{BaseURL: embeddingURL, DimensionsN: 384, APIKey: embeddingKey},
		contextsvc.QdrantStore{BaseURL: qdrantURL, Collection: "ivoai-context-v1-d384", APIKey: qdrantKey},
		&contextsvc.FileCatalog{Path: filepath.Join(layout.ContextDir, "catalog.json")},
	)
	if err != nil {
		return nil, err
	}
	return service, nil
}

func serveContext(ctx context.Context, layout server.Layout, errOut io.Writer) error {
	service, err := contextService(layout)
	if err != nil {
		return err
	}
	if err := service.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize context dependencies: %w", err)
	}
	if err := addConfiguredConnectors(service, layout); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	ingest := func() {
		for _, name := range service.ConnectorNames() {
			if _, err := service.Ingest(ctx, name); err != nil {
				fmt.Fprintf(errOut, "context connector %s: %v\n", name, err)
			}
		}
	}
	ingest()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			ingest()
		}
	}
}

func serveGateway(ctx context.Context, layout server.Layout, version string, errOut io.Writer) error {
	gatewayConfig, err := server.LoadGatewayConfig(layout)
	if err != nil {
		return err
	}
	service, err := contextService(layout)
	if err != nil {
		return err
	}
	if err := service.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize gateway context: %w", err)
	}
	if err := addConfiguredConnectors(service, layout); err != nil {
		return err
	}
	memoryToken := strings.TrimSpace(os.Getenv("AI_MEMORY_AUTH_TOKEN"))
	if memoryToken == "" {
		memoryToken, err = server.LoadBackendSecret(layout, "memory.env", "AI_MEMORY_AUTH_TOKEN")
		if err != nil {
			return err
		}
	}
	memoryHandler, err := memoryProxyWithToken(memoryToken, memoryURL)
	if err != nil {
		return err
	}
	g, err := gateway.New(gateway.Config{
		ServerVersion: version,
		PublicBaseURL: gatewayConfig.PublicURL,
		Context:       service,
		Enrollments:   enrollmentStore(layout),
		Memory:        memoryHandler,
		MemoryHealth: func(checkCtx context.Context) error {
			if probeMemoryMCP(checkCtx, memoryURL+"/mcp", memoryToken) != "healthy" {
				return errors.New("ai-memory unavailable")
			}
			return nil
		},
		EnrollmentAudit: func(event gateway.EnrollmentAudit) {
			peer := event.Peer
			if host, _, splitErr := net.SplitHostPort(event.Peer); splitErr == nil {
				peer = host
			}
			fmt.Fprintf(errOut, "ivoai gateway: enrollment audit id=%s length=%d format_valid=%t accepted=%t reason=%s peer=%s\n",
				event.ID, event.CodeLength, event.FormatValid, event.Accepted, event.Reason, peer)
		},
	})
	if err != nil {
		return err
	}
	handler := g.Handler()
	if len(gatewayConfig.TrustedProxyCIDRs) > 0 {
		networks, err := server.ParseTrustedProxyCIDRs(gatewayConfig.TrustedProxyCIDRs)
		if err != nil {
			return err
		}
		handler = trustedHTTPSProxyOnly(handler, networks)
	}
	httpServer := &http.Server{
		Addr:              gatewayConfig.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() {
		if gatewayConfig.TLSCertFile != "" {
			done <- httpServer.ListenAndServeTLS(gatewayConfig.TLSCertFile, gatewayConfig.TLSKeyFile)
			return
		}
		done <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(errOut, "gateway shutdown: %v\n", err)
		}
		return nil
	}
}

func trustedHTTPSProxyOnly(next http.Handler, networks []*net.IPNet) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		peer := net.ParseIP(host)
		trusted := err == nil && peer != nil
		if trusted {
			trusted = false
			for _, network := range networks {
				if network.Contains(peer) {
					trusted = true
					break
				}
			}
		}
		if !trusted || !strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "HTTPS reverse proxy required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func memoryProxy(targetURLs ...string) (http.Handler, error) {
	if len(targetURLs) > 1 {
		return nil, errors.New("memory proxy accepts at most one target")
	}
	targetURL := memoryURL
	if len(targetURLs) == 1 {
		targetURL = targetURLs[0]
	}
	upstreamToken := strings.TrimSpace(os.Getenv("AI_MEMORY_AUTH_TOKEN"))
	return memoryProxyWithToken(upstreamToken, targetURL)
}

func memoryProxyWithToken(upstreamToken, targetURL string) (http.Handler, error) {
	if upstreamToken == "" {
		return nil, errors.New("private ai-memory credential is not loaded")
	}
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	original := proxy.Director
	proxy.Director = func(request *http.Request) {
		original(request)
		// The private backend enforces a local Host allowlist. Never forward the
		// public gateway Host into the loopback-only ai-memory service.
		request.Host = target.Host
		switch request.URL.Path {
		case "/v1/memory/mcp":
			request.URL.Path = "/mcp"
		case "/v1/memory/hook":
			request.URL.Path = "/hook"
		case "/v1/memory/hook/batch":
			request.URL.Path = "/hook/batch"
		case "/v1/memory/handoff":
			request.URL.Path = "/handoff"
			// Native ai-memory lifecycle hooks append these stable paths to the
			// configured gateway origin. Only this explicit allowlist is proxied.
		default:
			request.URL.Path = "/invalid-ivoai-memory-route"
		}
		request.URL.RawPath = ""
		request.Header.Del("Authorization")
		request.Header.Del("Cookie")
		if upstreamToken != "" {
			request.Header.Set("Authorization", "Bearer "+upstreamToken)
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":"ai-memory is temporarily unavailable"}`+"\n")
	}
	return proxy, nil
}

func probeURL(ctx context.Context, endpoint string) string {
	return probeURLWithBearer(ctx, endpoint, "")
}

func probeURLWithBearer(ctx context.Context, endpoint, token string) string {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "unhealthy"
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "unhealthy"
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "healthy"
	}
	return "unhealthy"
}

func probeMemoryMCP(ctx context.Context, endpoint, token string) string {
	if strings.TrimSpace(token) == "" {
		return "unhealthy"
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	payload := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, endpoint, payload)
	if err != nil {
		return "unhealthy"
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return "unhealthy"
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "healthy"
	}
	return "unhealthy"
}
