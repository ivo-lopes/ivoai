package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// This is a transient Core transport capability, never a subscription token.
// It stays in an owned 0600 runtime file and is removed with the managed turn.
type nativeMCPProjection struct {
	Identity core.PrimaryTurnIdentity `json:"identity"`
	Server   string                   `json:"server"`
	Endpoint string                   `json:"endpoint"`
	Token    string                   `json:"token"`
}

var nativeMCPName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

func nativeProjectionPath(runtimeDir, turn, server string) (string, error) {
	if session.ValidateID(turn) != nil || !nativeMCPName.MatchString(server) {
		return "", errors.New("NATIVE_MCP_PROJECTION_INVALID")
	}
	return filepath.Join(runtimeDir, "native-mcp-projections", turn, server+".json"), nil
}

func writeNativeMCPProjection(runtimeDir string, p nativeMCPProjection) (string, error) {
	path, err := nativeProjectionPath(runtimeDir, p.Identity.ID, p.Server)
	if err != nil {
		return "", err
	}
	if err = platform.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return "", err
	}
	body, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	if err = platform.AtomicWritePrivate(body, path); err != nil {
		return "", err
	}
	return path, nil
}

type nativeCapabilityTransport struct {
	base  http.RoundTripper
	token string
}

func (t nativeCapabilityTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	copy.Header = r.Header.Clone()
	copy.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(copy)
}

// NativeMCPProxy exposes an already-authorized Core HTTP projection over stdio
// for official clients that cannot attach it to a custom primary over HTTP.
// Original tool schemas, annotations, results and Gateway checks remain intact.
// This adapter has no scheduler, cache, credential reader or policy engine.
func (a *App) NativeMCPProxy(ctx context.Context, id, turn, serverName string) error {
	store := session.Store{Root: a.Store.Paths.SessionsDir}
	v, err := store.Get(id)
	if err != nil || v.PrimaryTurn == nil || v.PrimaryTurn.ID != turn {
		return errors.New("NATIVE_MCP_PROJECTION_DENIED")
	}
	runtimeDir, err := store.RuntimeDir(id)
	if err != nil {
		return err
	}
	path, err := nativeProjectionPath(runtimeDir, turn, serverName)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errors.New("NATIVE_MCP_PROJECTION_UNAVAILABLE")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Geteuid() {
		return errors.New("NATIVE_MCP_PROJECTION_UNAVAILABLE")
	}
	body, err := platform.ReadRegularFile(path, 64<<10)
	if err != nil {
		return errors.New("NATIVE_MCP_PROJECTION_UNAVAILABLE")
	}
	var projection nativeMCPProjection
	if json.Unmarshal(body, &projection) != nil || projection.Server != serverName || projection.Identity != v.PrimaryTurn.Identity() || (len(projection.Token) != 48 && len(projection.Token) != 64) {
		return errors.New("NATIVE_MCP_PROJECTION_INVALID")
	}
	if _, err = hex.DecodeString(projection.Token); err != nil {
		return errors.New("NATIVE_MCP_PROJECTION_INVALID")
	}
	endpoint, err := url.Parse(projection.Endpoint)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" || endpoint.Port() == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.RawQuery != "" {
		return errors.New("NATIVE_MCP_PROJECTION_INVALID")
	}
	guard, err := a.bindNativeACPServer(ctx, store, v)
	if err != nil {
		return err
	}
	authorize := func(callCtx context.Context) error {
		value, err := guard.check(callCtx)
		if err != nil || value.PrimaryTurn.Identity() != projection.Identity {
			return errors.New("NATIVE_MCP_PROJECTION_DENIED")
		}
		return nil
	}
	if err = authorize(ctx); err != nil {
		return err
	}
	server, closeProxy, err := newNativeMCPProxy(ctx, projection, authorize)
	if err != nil {
		return err
	}
	defer closeProxy()
	return server.Run(ctx, &mcp.StdioTransport{})
}

func newNativeMCPProxy(ctx context.Context, projection nativeMCPProjection, authorize func(context.Context) error) (*mcp.Server, func(), error) {
	if authorize == nil {
		return nil, nil, errors.New("NATIVE_MCP_PROJECTION_DENIED")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	client := &http.Client{Transport: nativeCapabilityTransport{transport, projection.Token}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	connection, err := mcp.NewClient(&mcp.Implementation{Name: "ivoai-native-transport", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: projection.Endpoint, HTTPClient: client, DisableStandaloneSSE: true}, nil)
	if err != nil {
		transport.CloseIdleConnections()
		return nil, nil, errors.New("NATIVE_MCP_TRANSPORT_UNAVAILABLE")
	}
	cleanup := func() { _ = connection.Close(); transport.CloseIdleConnections() }
	ready := false
	defer func() {
		if !ready {
			cleanup()
		}
	}()
	upstream, err := connection.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil || len(upstream.Tools) > 256 || upstream.NextCursor != "" {
		return nil, nil, errors.New("NATIVE_MCP_CATALOG_UNAVAILABLE")
	}
	options := &mcp.ServerOptions{}
	if initialized := connection.InitializeResult(); initialized != nil {
		options.Instructions = initialized.Instructions
	}
	server := mcp.NewServer(&mcp.Implementation{Name: projection.Server, Version: "1"}, options)
	for _, tool := range upstream.Tools {
		if !nativeMCPName.MatchString(tool.Name) {
			return nil, nil, errors.New("NATIVE_MCP_CATALOG_INVALID")
		}
		encoded, e := json.Marshal(tool.InputSchema)
		if e != nil || len(encoded) > 128<<10 {
			return nil, nil, errors.New("NATIVE_MCP_CATALOG_INVALID")
		}
		var schema struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(encoded, &schema) != nil || schema.Type != "object" {
			return nil, nil, errors.New("NATIVE_MCP_CATALOG_INVALID")
		}
		name := tool.Name
		server.AddTool(tool, func(callCtx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := authorize(callCtx); err != nil {
				return nil, err
			}
			result, err := connection.CallTool(callCtx, &mcp.CallToolParams{Name: name, Arguments: request.Params.Arguments})
			if err != nil {
				return nil, errors.New("NATIVE_MCP_UPSTREAM_FAILED")
			}
			if err = authorize(callCtx); err != nil {
				return nil, err
			}
			return result, nil
		})
	}

	ready = true
	return server, cleanup, nil
}
