package connections

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/externalmcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type probeTransport struct {
	token  string
	mu     sync.Mutex
	status int
	scheme string
}

func (t *probeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+t.token)
	response, err := http.DefaultTransport.RoundTrip(request)
	if response != nil {
		t.mu.Lock()
		t.status = response.StatusCode
		t.scheme = "none"
		fields := strings.Fields(response.Header.Get("WWW-Authenticate"))
		if len(fields) > 0 {
			switch strings.ToLower(fields[0]) {
			case "bearer", "basic", "digest":
				t.scheme = strings.ToLower(fields[0])
			default:
				t.scheme = "other"
			}
		}
		t.mu.Unlock()
	}
	return response, err
}

func (t *probeTransport) diagnostic() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return fmt.Sprintf("HTTP_STATUS=%d AUTH_CHALLENGE=%s", t.status, t.scheme)
}

func (r Registry) Test(ctx context.Context, entry config.MCPServer) (int, error) {
	headers, err := r.Headers(entry)
	if err != nil {
		return 0, err
	}
	gateway, err := externalmcp.Start([]externalmcp.Target{{Name: "probe", URL: entry.URL, Headers: headers}}, "full")
	if err != nil {
		return 0, err
	}
	defer gateway.Close()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "ivoai-mcp-test", Version: "1"}, nil)
	transport := &probeTransport{token: gateway.Token(), scheme: "none"}
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: gateway.URL(0), DisableStandaloneSSE: true, HTTPClient: &http.Client{Transport: transport}}, nil)
	if err != nil {
		return 0, fmt.Errorf("external MCP initialize failed (%s); check endpoint, TLS, authentication and required headers", transport.diagnostic())
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("external MCP tools/list failed (%s)", transport.diagnostic())
	}
	return len(tools.Tools), nil
}
