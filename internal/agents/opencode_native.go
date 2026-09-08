package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
)

// NativeOpenCode is both an eligibility probe and an ExecutorRunner. Only the
// official subprocess accesses native authentication; no credential data is
// inspected, copied, fingerprinted, cached, or exposed by IVOAI.
type NativeOpenCode struct {
	Options opencodebridge.ManagedOptions
	// AuthMetadata invokes the official metadata command; injection is for
	// contract fixtures, not an alternative credential source.
	AuthMetadata func(context.Context, string, []string) (map[string]bool, error)
	mu           sync.Mutex
	probeMu      sync.Mutex
	capability   routing.ProviderCapability
	permissions  map[string]nativePermission
}

type nativePermission struct {
	View    opencodebridge.PermissionView
	Session *OpenCodeSession
}

func (n *NativeOpenCode) PendingPermissions() []opencodebridge.PermissionView {
	n.mu.Lock()
	defer n.mu.Unlock()
	result := []opencodebridge.PermissionView{}
	for _, pending := range n.permissions {
		result = append(result, pending.View)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
func (n *NativeOpenCode) ReplyPermission(ctx context.Context, id string, allow bool) error {
	n.mu.Lock()
	pending, ok := n.permissions[id]
	n.mu.Unlock()
	if !ok {
		return errors.New("native permission is no longer pending")
	}
	reply := "reject"
	if allow {
		reply = "once"
	}
	if err := pending.Session.ReplyPermission(ctx, id, reply); err != nil {
		return err
	}
	n.mu.Lock()
	delete(n.permissions, id)
	n.mu.Unlock()
	return nil
}
func (n *NativeOpenCode) observePermissions(ctx context.Context, s *OpenCodeSession) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	defer func() {
		n.mu.Lock()
		for id, value := range n.permissions {
			if value.Session == s {
				delete(n.permissions, id)
			}
		}
		n.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			body, err := s.Request(ctx, "permissions", nil, nil)
			if err != nil {
				continue
			}
			var requests []struct {
				ID         string   `json:"id"`
				SessionID  string   `json:"sessionID"`
				Permission string   `json:"permission"`
				Patterns   []string `json:"patterns"`
			}
			if json.Unmarshal(body, &requests) != nil {
				continue
			}
			var rejected []string
			n.mu.Lock()
			if n.permissions == nil {
				n.permissions = map[string]nativePermission{}
			}
			for _, request := range requests {
				if request.SessionID != s.ID() || !nativeLabel(request.ID) || len(request.ID) > 128 {
					continue
				}
				description := platform.Redact(request.Permission + ": " + strings.Join(request.Patterns, ", "))
				if len(description) > 1024 || len(n.permissions) >= 64 {
					rejected = append(rejected, request.ID)
					continue
				}
				cleanDescription := strings.Map(func(r rune) rune {
					if r < 32 || r == 127 || r >= 128 && r <= 159 || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
						return -1
					}
					return r
				}, description)
				if cleanDescription != description {
					rejected = append(rejected, request.ID)
					continue
				}
				n.permissions[request.ID] = nativePermission{Session: s, View: opencodebridge.PermissionView{ID: request.ID, Description: description}}
			}
			n.mu.Unlock()
			for _, id := range rejected {
				_ = s.ReplyPermission(ctx, id, "reject")
			}
		}
	}
}

func (n *NativeOpenCode) Probe(ctx context.Context) (quota.ProviderQuota, error) {
	if n == nil {
		return quota.ProviderQuota{Provider: quota.ProviderOpenCode, TelemetryUnknown: true}, errors.New("native OpenCode unavailable")
	}
	n.probeMu.Lock()
	defer n.probeMu.Unlock()
	succeeded := false
	defer func() {
		if !succeeded {
			n.mu.Lock()
			n.capability = routing.ProviderCapability{}
			n.mu.Unlock()
		}
	}()
	capabilitySet := routing.ProviderCapability{Provider: "opencode", Source: routing.SourceRuntimeVerified}
	value := quota.ProviderQuota{Provider: quota.ProviderOpenCode, TelemetryUnknown: true, Source: "official OpenCode provider/auth metadata", ObservedAt: time.Now().UTC(), Reason: "native authentication/capability unavailable; quota telemetry unknown"}
	options := n.Options
	options.RuntimeDir = filepath.Join(options.RuntimeDir, "discovery")
	options.StateDir = filepath.Join(options.StateDir, "discovery")
	options.NativeExecutor, options.Bridge = true, nil
	options.NativeMCP = nil
	options.NativePermissions = map[string]any{"*": "deny"}
	backend, err := opencodebridge.StartManaged(ctx, options)
	if err != nil {
		return value, errors.New("native OpenCode discovery unavailable")
	}
	defer backend.Close(context.Background())
	body, err := backend.APIRequest(ctx, "providers", "", nil, nil)
	if err != nil {
		return value, err
	}
	var catalog opencodebridge.NativeCatalog
	if json.Unmarshal(body, &catalog) != nil {
		return value, errors.New("invalid native provider metadata")
	}
	auth := n.AuthMetadata
	if auth == nil {
		auth = officialOpenCodeAuthMetadata
	}
	authenticated, err := auth(ctx, options.OpenCodePath, backend.Env())
	if err != nil {
		return value, err
	}
	connected := map[string]bool{}
	for _, id := range catalog.Connected {
		connected[id] = true
	}
	for _, provider := range catalog.All {
		if !connected[provider.ID] || provider.ID == "ivoai" || !nativeLabel(provider.ID) || !(authenticated[provider.ID] || authenticated[provider.Name]) {
			continue
		}
		for id, model := range provider.Models {
			if !nativeLabel(id) || !model.Capabilities.ToolCall || model.Status == "deprecated" {
				continue
			}
			name := provider.ID + "/" + id
			capability := routing.ModelCapability{Name: name, DisplayName: name, Provider: "opencode", Availability: "authenticated", QuotaEligibility: "unknown", IsDefault: catalog.Default[provider.ID] == id, Source: routing.SourceRuntimeVerified}
			for variant := range model.Variants {
				if nativeLabel(variant) && len(variant) <= 16 {
					capability.SupportedEfforts = append(capability.SupportedEfforts, variant)
				}
			}
			sort.Strings(capability.SupportedEfforts)
			capabilitySet.SupportsEffort = capabilitySet.SupportsEffort || len(capability.SupportedEfforts) > 0
			capabilitySet.Models = append(capabilitySet.Models, capability)
		}
	}
	sort.Slice(capabilitySet.Models, func(i, j int) bool { return capabilitySet.Models[i].Name < capabilitySet.Models[j].Name })
	value.Authenticated = len(capabilitySet.Models) > 0
	value.Eligible = value.Authenticated
	capabilitySet.Authenticated = value.Authenticated
	capabilitySet.WorkerCapable = value.Authenticated
	n.mu.Lock()
	n.capability = capabilitySet
	n.mu.Unlock()
	if value.Authenticated {
		value.Reason = "native provider eligible; quota telemetry unknown"
	}
	succeeded = true
	return value, nil
}

func (n *NativeOpenCode) Capability() routing.ProviderCapability {
	if n == nil {
		return routing.ProviderCapability{}
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	value := n.capability
	value.Models = append([]routing.ModelCapability(nil), value.Models...)
	return value
}
func (n *NativeOpenCode) CanModel(model string) bool {
	value := n.Capability()
	for _, entry := range value.Models {
		if model == "" || entry.Name == model {
			return value.Authenticated
		}
	}
	return false
}

func (n *NativeOpenCode) Run(ctx context.Context, request opencodebridge.ExecutorRequest, emit func(string) error) (opencodebridge.ExecutorResult, error) {
	result := opencodebridge.ExecutorResult{RequestedModel: request.Model, SelectionMode: request.SelectionMode, ConfigurationSource: "official OpenCode HTTP", CompressionProvider: "direct"}
	if request.Executor != "opencode" {
		return result, errors.New("native executor routing mismatch")
	}
	observed, err := n.Probe(ctx)
	if err != nil || !observed.Eligible {
		return result, errors.New("native OpenCode authentication/capability unavailable")
	}
	capability := n.Capability()
	model := request.Model
	if model == "" {
		for _, entry := range capability.Models {
			if model == "" || entry.IsDefault {
				model = entry.Name
				if entry.IsDefault {
					break
				}
			}
		}
	}
	if !n.CanModel(model) {
		return result, errors.New("explicit native OpenCode model unavailable")
	}
	if request.Effort != "" {
		valid := false
		for _, entry := range capability.Models {
			if entry.Name == model {
				for _, variant := range entry.SupportedEfforts {
					valid = valid || variant == request.Effort
				}
			}
		}
		if !valid {
			return result, errors.New("native OpenCode variant unavailable")
		}
	}
	options := n.Options
	if err := os.MkdirAll(options.RuntimeDir, 0700); err != nil {
		return result, err
	}
	turnDir, err := os.MkdirTemp(options.RuntimeDir, "turn-")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(turnDir)
	options.RuntimeDir = filepath.Join(turnDir, "runtime")
	options.StateDir = filepath.Join(turnDir, "state")
	options.NativeExecutor, options.Bridge = true, nil
	// Official metadata exposes no stable account identity. Never send a prior
	// conversation ID across probes, even if the provider name is unchanged.
	options.ResumeSessionID = ""
	session, err := (OpenCodeExecutor{}).OpenSession(ctx, options)
	if err != nil {
		return result, err
	}
	defer session.Close(context.Background())
	permissionCtx, stopPermissions := context.WithCancel(ctx)
	permissionDone := make(chan struct{})
	go func() { defer close(permissionDone); n.observePermissions(permissionCtx, session) }()
	defer func() { stopPermissions(); <-permissionDone }()
	provider, id, _ := strings.Cut(model, "/")
	payload := map[string]any{"model": map[string]string{"providerID": provider, "modelID": id}, "parts": []map[string]string{{"type": "text", "text": request.Prompt}}}
	if request.Effort != "" {
		payload["variant"] = request.Effort
	}
	body, err := session.Prompt(ctx, payload, false)
	if err != nil {
		return result, err
	}
	var response struct {
		Info struct {
			Error      json.RawMessage `json:"error"`
			Finish     string          `json:"finish"`
			ModelID    string          `json:"modelID"`
			ProviderID string          `json:"providerID"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if json.Unmarshal(body, &response) != nil || len(response.Info.Error) > 0 && string(response.Info.Error) != "null" || response.Info.Finish == "" {
		var failure struct {
			Data struct {
				StatusCode int `json:"statusCode"`
			} `json:"data"`
		}
		if json.Unmarshal(response.Info.Error, &failure) == nil && failure.Data.StatusCode == 429 {
			return result, NativeProviderLimitError{}
		}
		return result, errors.New("native OpenCode executor failed; partial output not accepted")
	}
	if response.Info.ProviderID+"/"+response.Info.ModelID != model {
		return result, errors.New("native OpenCode effective model mismatch")
	}
	var output strings.Builder
	for _, part := range response.Parts {
		if part.Type == "text" {
			output.WriteString(part.Text)
		}
	}
	if output.Len() == 0 || output.Len() > 1<<20 {
		return result, errors.New("native OpenCode final response absent or oversized")
	}
	result.ExecutorSessionID, result.Model, result.Effort = session.ID(), model, request.Effort
	return result, emit(output.String())
}

type NativeProviderLimitError struct{}

func (NativeProviderLimitError) Error() string     { return "native OpenCode provider rate limit" }
func (NativeProviderLimitError) RateLimited() bool { return true }

// The command intentionally prints only names and authentication types. Its
// credential-path heading and environment section are discarded, never logged.
func officialOpenCodeAuthMetadata(ctx context.Context, path string, environment []string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "auth", "list")
	command.Env = append(append([]string(nil), environment...), "NO_COLOR=1", "TERM=dumb")
	pipe, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = io.Discard
	if command.Start() != nil {
		return nil, errors.New("native auth metadata unavailable")
	}
	body, readErr := io.ReadAll(io.LimitReader(pipe, 64<<10+1))
	if len(body) > 64<<10 {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if readErr != nil || waitErr != nil || len(body) > 64<<10 {
		return nil, errors.New("native auth metadata unavailable")
	}
	result := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kind := fields[len(fields)-1]
		if kind != "api" && kind != "oauth" {
			continue
		}
		name := strings.TrimSpace(strings.Join(fields[:len(fields)-1], " "))
		name = strings.TrimLeft(name, "│┃●○◆◇■□ ")
		if nativeLabel(name) {
			result[name] = true
		}
	}
	return result, nil
}
func nativeLabel(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 || r >= 128 && r <= 159 || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 {
			return false
		}
	}
	return true
}

// RoutedRunner keeps executor ownership in IVOAI; the native backend never
// points at the frontend bridge. Existing official CLI runners are unchanged.
type RoutedRunner struct {
	Official opencodebridge.ExecutorRunner
	Native   *NativeOpenCode
}

func (r RoutedRunner) Run(ctx context.Context, request opencodebridge.ExecutorRequest, emit func(string) error) (opencodebridge.ExecutorResult, error) {
	if request.Executor == "opencode" {
		if r.Native == nil {
			return opencodebridge.ExecutorResult{}, errors.New("native OpenCode unavailable")
		}
		return r.Native.Run(ctx, request, emit)
	}
	return r.Official.Run(ctx, request, emit)
}
