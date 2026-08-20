package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/headroom"
	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type Component struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Path      string `json:"path,omitempty"`
	Managed   bool   `json:"managed"`
	Fixture   bool   `json:"fixture,omitempty"`
	Hooks     bool   `json:"hooks,omitempty"`
}
type Auth struct {
	Installed     bool   `json:"installed"`
	Version       string `json:"version,omitempty"`
	Authenticated bool   `json:"authenticated"`
}
type Server struct {
	Configured         bool   `json:"configured"`
	Reachable          bool   `json:"reachable"`
	TLS                bool   `json:"tls"`
	ProtocolCompatible bool   `json:"protocol_compatible"`
	URL                string `json:"url,omitempty"`
}
type Report struct {
	Overall           string               `json:"overall"`
	OS                string               `json:"os"`
	Architecture      string               `json:"architecture"`
	Version           string               `json:"ivoai_version"`
	TestMode          bool                 `json:"test_mode"`
	ConfigPath        string               `json:"config_path"`
	StatePath         string               `json:"state_path"`
	SecretPath        string               `json:"secret_path"`
	SecretPermissions string               `json:"secret_permissions"`
	Codex             Auth                 `json:"codex"`
	Claude            Auth                 `json:"claude"`
	Headroom          headroom.Status      `json:"headroom"`
	Memory            Component            `json:"ai_memory"`
	Ruflo             orchestration.Status `json:"ruflo"`
	Server            Server               `json:"server"`
	Issues            []string             `json:"issues"`
}
type Doctor struct {
	Store      *config.Store
	Runner     platform.Runner
	Version    string
	HTTPClient *http.Client
}

func (d Doctor) Run(ctx context.Context) Report {
	cfg, cfgErr := d.Store.Load()
	state, stateErr := d.Store.LoadState()
	r := Report{Overall: "READY", OS: runtime.GOOS, Architecture: runtime.GOARCH, Version: d.Version, TestMode: os.Getenv("IVOAI_TEST_MODE") == "1", ConfigPath: d.Store.Paths.Config, StatePath: d.Store.Paths.State, SecretPath: d.Store.Paths.Secrets}
	if cfgErr != nil {
		r.Issues = append(r.Issues, cfgErr.Error())
	}
	if stateErr != nil {
		r.Issues = append(r.Issues, stateErr.Error())
	}
	r.SecretPermissions = permissions(d.Store.Paths.Secrets)
	r.Codex = d.agent(ctx, "codex", []string{"login", "status"}, state.Components["codex"])
	r.Claude = d.agent(ctx, "claude", []string{"auth", "status"}, state.Components["claude-code"])
	r.Headroom = (headroom.Manager{Runner: d.Runner, Binary: state.Components["headroom"].Path}).Inspect(ctx, cfg.Headroom.Enabled)
	if fixture := state.Components["headroom"]; !r.Headroom.Installed && strings.HasSuffix(fixture.Version, "-fixture") {
		r.Headroom.Installed, r.Headroom.Healthy, r.Headroom.CodexCompatible, r.Headroom.ClaudeCompatible, r.Headroom.Version = true, true, true, true, fixture.Version
	}
	r.Memory = componentFromState(state.Components["ai-memory"])
	r.Memory.Hooks = hooksInstalled(d.Store.Paths.HooksDir)
	if r.Memory.Fixture {
		r.Memory.Hooks = true
	}
	r.Ruflo = (orchestration.Manager{Runner: d.Runner, Binary: state.Components["ruflo"].Path, ProfileDir: d.Store.Paths.DataDir}).Inspect(ctx)
	if fixture := state.Components["ruflo"]; !r.Ruflo.Installed && strings.HasSuffix(fixture.Version, "-fixture") {
		r.Ruflo.Installed, r.Ruflo.Version = true, fixture.Version
	}
	r.Server = d.server(ctx, cfg.Connections.Server)
	for name, component := range map[string]Component{"Codex": componentFromAuth(r.Codex), "Claude Code": componentFromAuth(r.Claude), "Headroom": {Installed: r.Headroom.Installed}, "ai-memory": r.Memory, "Ruflo": {Installed: r.Ruflo.Installed}} {
		if !component.Installed {
			r.Issues = append(r.Issues, name+" is not installed")
		}
	}
	if cfg.Memory.Enabled && r.Memory.Installed && !r.Memory.Hooks {
		r.Issues = append(r.Issues, "ai-memory hooks are not installed")
	}
	if cfg.Orchestration.Enabled && r.Ruflo.Installed && !r.Ruflo.SafeMode {
		r.Issues = append(r.Issues, "Ruflo safe profile is not active")
	}
	if r.SecretPermissions != "not-created" && r.SecretPermissions != "0600" {
		r.Issues = append(r.Issues, "secret file permissions must be 0600")
	}
	for _, directory := range []string{d.Store.Paths.ConfigDir, d.Store.Paths.DataDir, d.Store.Paths.StateDir} {
		if mode := permissions(directory); mode != "0700" {
			r.Issues = append(r.Issues, fmt.Sprintf("private directory %s must be 0700 (got %s)", directory, mode))
		}
	}
	if cfg.Connections.ChatGPT.Status == "connected" && !r.Codex.Authenticated {
		r.Issues = append(r.Issues, "ChatGPT connection authentication is not valid")
	}
	if cfg.Connections.Claude.Status == "connected" && !r.Claude.Authenticated {
		r.Issues = append(r.Issues, "Claude connection authentication is not valid")
	}
	if r.Server.Configured && (!r.Server.Reachable || !r.Server.ProtocolCompatible || (!r.Server.TLS && !loopbackServer(r.Server.URL))) {
		r.Issues = append(r.Issues, "configured server is unreachable, incompatible, or not protected by TLS")
	}
	if len(r.Issues) > 0 {
		r.Overall = "DEGRADED"
	}
	return r
}

func loopbackServer(raw string) bool {
	base, err := connections.ValidateBaseURL(raw)
	if err != nil {
		return false
	}
	host := base.Hostname()
	return strings.EqualFold(host, "localhost") || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
}

func (d Doctor) agent(ctx context.Context, name string, statusArgs []string, state config.ComponentState) Auth {
	a := Auth{Installed: state.Installed, Version: state.Version}
	path := state.Path
	var err error
	if path != "" {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
			if !(state.Installed && strings.HasSuffix(state.Version, "-fixture")) {
				return a
			}
		}
	} else {
		path, err = d.Runner.LookPath(name)
	}
	if err != nil || path == "" {
		if state.Installed && strings.HasSuffix(state.Version, "-fixture") {
			a.Installed = true
		}
		return a
	}
	a.Installed = true
	if version, versionErr := d.Runner.Run(ctx, path, []string{"--version"}, platform.RunOptions{Timeout: 10 * time.Second}); versionErr == nil {
		a.Version = strings.TrimSpace(version.Stdout)
	}
	status, err := d.Runner.Run(ctx, path, statusArgs, platform.RunOptions{Timeout: 15 * time.Second})
	a.Authenticated = connections.AuthenticationStatus(status, err)
	return a
}
func (d Doctor) server(ctx context.Context, connection config.Connection) Server {
	s := Server{Configured: connection.Status == "connected", URL: connection.URL}
	if !s.Configured {
		return s
	}
	base, err := connections.ValidateBaseURL(connection.URL)
	if err != nil {
		return s
	}
	s.TLS = base.Scheme == "https"
	client := d.HTTPClient
	if client == nil {
		client = connections.SecureHTTPClient()
		client.Timeout = 5 * time.Second
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base.String(), "/")+"/.well-known/ivoai", nil)
	if err != nil {
		return s
	}
	resp, err := client.Do(req)
	if err != nil {
		return s
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return s
	}
	var discovery connections.Discovery
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&discovery); err != nil {
		return s
	}
	s.Reachable = true
	s.ProtocolCompatible = discovery.ProtocolVersion == connections.ProtocolVersion
	if !doctorProbe(ctx, client, base.String(), discovery.HealthEndpoint) || !doctorProbe(ctx, client, base.String(), discovery.ReadyEndpoint) {
		s.Reachable = false
	}
	return s
}

func doctorProbe(ctx context.Context, client *http.Client, base, endpoint string) bool {
	if !strings.HasPrefix(endpoint, "/") || strings.HasPrefix(endpoint, "//") || strings.ContainsAny(endpoint, "?#\\") {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
func componentFromState(s config.ComponentState) Component {
	fixture := strings.HasSuffix(s.Version, "-fixture")
	installed := s.Installed && fixture
	if s.Installed && !fixture && s.Path != "" {
		info, err := os.Stat(s.Path)
		installed = err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
	}
	return Component{Installed: installed, Version: s.Version, Path: s.Path, Managed: s.Managed, Fixture: fixture}
}
func componentFromAuth(a Auth) Component {
	return Component{Installed: a.Installed, Version: a.Version}
}
func hooksInstalled(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}
func permissions(path string) string {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "not-created"
	}
	if err != nil {
		return "unreadable"
	}
	return fmt.Sprintf("%04o", info.Mode().Perm())
}
func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }
