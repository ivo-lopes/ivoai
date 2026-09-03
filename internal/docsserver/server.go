// Package docsserver serves the immutable Docusaurus production build embedded
// in the IVOAI binary. It deliberately has no Node.js or writable runtime state.
package docsserver

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed site
var embedded embed.FS

func Site() (fs.FS, error) { return fs.Sub(embedded, "site") }

// Handler returns the production documentation handler. Docusaurus generates
// extensionless routes as .html files, so the handler resolves both forms.
func Handler() (http.Handler, error) {
	site, err := Site()
	if err != nil {
		return nil, err
	}
	files := http.FileServer(http.FS(site))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		if r.URL.Path == "/healthz" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
			return
		}
		clean := path.Clean("/" + r.URL.Path)
		name := strings.TrimPrefix(clean, "/")
		if name == "" || strings.HasSuffix(clean, "/") {
			name = path.Join(name, "index.html")
		}
		if info, statErr := fs.Stat(site, name); statErr == nil && !info.IsDir() {
			serveFile(w, r, site, name, files)
			return
		}
		if path.Ext(name) == "" {
			if _, statErr := fs.Stat(site, name+".html"); statErr == nil {
				serveFile(w, r, site, name+".html", files)
				return
			}
		}
		http.NotFound(w, r)
	}), nil
}

func serveFile(w http.ResponseWriter, r *http.Request, site fs.FS, name string, fallback http.Handler) {
	data, err := fs.ReadFile(site, name)
	if err != nil {
		fallback.ServeHTTP(w, r)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", cachePolicy(name))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func cachePolicy(name string) string {
	if strings.HasPrefix(name, "assets/") || strings.Contains(name, "/assets/") {
		return "public, max-age=31536000, immutable"
	}
	return "public, max-age=300"
}

// Serve binds exactly the configured address and shuts down cleanly with ctx.
func Serve(ctx context.Context, address string) error {
	listener, err := net.Listen(networkForAddress(address), address)
	if err != nil {
		return fmt.Errorf("listen for documentation: %w", err)
	}
	return ServeListener(ctx, listener)
}

// networkForAddress preserves the operator's explicit address-family choice.
// In particular, 0.0.0.0 must create an IPv4 wildcard socket rather than an
// implementation-dependent IPv6 dual-stack socket that is absent from
// `ss -lnt4` and may be restricted differently by a host firewall.
func networkForAddress(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return "tcp"
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return "tcp"
	}
	if ip.To4() != nil {
		return "tcp4"
	}
	return "tcp6"
}

func ServeListener(ctx context.Context, listener net.Listener) error {
	handler, err := Handler()
	if err != nil {
		_ = listener.Close()
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			return err
		}
		return ctx.Err()
	}
}
