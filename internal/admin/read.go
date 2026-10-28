package admin

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
)

const maxSessions = 32
const maxSessionBytes = 256 << 10

var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,95}$`)
var versionName = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:\.[0-9]+)?$`)

func model(value session.ModelInfo) session.ModelInfo {
	if !modelName.MatchString(value.Name) {
		return session.ModelInfo{Name: "unknown", Source: session.ModelUnknown}
	}
	switch value.Source {
	case session.ModelRuntimeVerified, session.ModelCapabilityRegistry, session.ModelArgument, session.ModelConfigured, session.ModelDefault, session.ModelUnsupported, session.ModelUnknown:
	default:
		value.Source = session.ModelUnknown
	}
	return value
}
func executor(value string) string {
	switch value {
	case "codex", "claude", "opencode":
		return value
	}
	return "unknown"
}

type Usage struct {
	InputTokens                int64  `json:"input_tokens"`
	OutputTokens               int64  `json:"output_tokens"`
	CachedInputTokens          *int64 `json:"cached_input_tokens"`
	Basis                      string `json:"basis"`
	Source                     string `json:"source"`
	Scope                      string `json:"scope"`
	CachedInputIncludedInInput bool   `json:"cached_input_included_in_input"`
}

// Only the already persisted, bounded invocation report is projected. Never
// fetch a live provider or sum reports/events into a fabricated session total.
func usage(value session.Session) *Usage {
	if len(value.ExecutorTrace) == 0 || len(value.ExecutorTrace) > maxSessionBytes {
		return nil
	}
	var trace struct {
		Usage  *core.ReportedTokenUsage `json:"usage"`
		Status string                   `json:"usage_status"`
	}
	if json.Unmarshal(value.ExecutorTrace, &trace) != nil || trace.Status != "reported" || trace.Usage == nil {
		return nil
	}
	u := trace.Usage
	if u.Basis != "client_reported" || u.Source != "codex_exec_turn_completed" || u.Scope != "cli_invocation" || u.InputTokens < 0 || u.OutputTokens < 0 || u.InputTokens > 1<<53-1 || u.OutputTokens > 1<<53-1 || u.CachedInputTokens != nil && (*u.CachedInputTokens < 0 || *u.CachedInputTokens > u.InputTokens) {
		return nil
	}
	return &Usage{u.InputTokens, u.OutputTokens, u.CachedInputTokens, u.Basis, u.Source, u.Scope, true}
}

type Quota struct {
	Provider         string    `json:"provider"`
	Kind             string    `json:"kind"`
	State            string    `json:"state"`
	RemainingPercent *float64  `json:"remaining_percent"`
	ObservedAt       time.Time `json:"observed_at"`
}
type Session struct {
	ID                    string            `json:"id"`
	State                 session.State     `json:"state"`
	Mode                  session.Mode      `json:"mode"`
	Executor              string            `json:"executor"`
	Model                 session.ModelInfo `json:"model"`
	StartedAt             time.Time         `json:"started_at"`
	UpdatedAt             time.Time         `json:"updated_at"`
	WorkerCount           int               `json:"worker_count"`
	TaskCount             int               `json:"task_count"`
	LatestInvocationUsage *Usage            `json:"latest_invocation_usage"`
	Quotas                []Quota           `json:"quotas"`
}

func projectSession(value session.Session) Session {
	out := Session{ID: value.SessionID, State: value.State, Mode: value.Mode, Executor: executor(value.PrimaryExecutor), Model: model(value.PrimaryModel), StartedAt: value.StartedAt, UpdatedAt: value.UpdatedAt, WorkerCount: len(value.Workers), TaskCount: len(value.Tasks), LatestInvocationUsage: usage(value), Quotas: []Quota{}}
	for _, provider := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
		q := value.Quota[provider]
		for i, w := range q.Windows {
			if i >= 8 {
				break
			}
			kind := string(w.Kind)
			switch w.Kind {
			case quota.KindContext, quota.KindSession, quota.KindRolling, quota.KindWeekly, quota.KindMonthly, quota.KindIndividual, quota.KindModelWeekly, quota.KindCredits:
			default:
				continue
			}
			state := w.TelemetryState()
			switch state {
			case quota.TelemetryPending, quota.TelemetryAvailable, quota.TelemetryNotExposed, quota.TelemetryStale, quota.TelemetryExhausted:
			default:
				state = quota.TelemetryNotExposed
			}
			var remaining *float64
			if w.Available && w.Authoritative && (state == quota.TelemetryAvailable || state == quota.TelemetryExhausted) && !math.IsNaN(w.RemainingPercent) && !math.IsInf(w.RemainingPercent, 0) && w.RemainingPercent >= 0 && w.RemainingPercent <= 100 {
				n := w.RemainingPercent
				remaining = &n
			}
			out.Quotas = append(out.Quotas, Quota{string(provider), kind, string(state), remaining, w.ObservedAt})
		}
	}
	return out
}
func (s *Server) readSession(id string) (session.Session, error) {
	return (session.Store{Root: s.paths.SessionsDir}).GetBounded(id, maxSessionBytes)
}

type sessionList struct {
	Sessions  []Session `json:"sessions"`
	Truncated bool      `json:"truncated"`
	Partial   bool      `json:"partial"`
}

func (s *Server) list(r *http.Request) (sessionList, error) {
	snapshot, err := (session.Store{Root: s.paths.SessionsDir}).ListBounded(r.Context(), maxSessions, maxSessionBytes)
	out := sessionList{Sessions: []Session{}, Truncated: snapshot.Truncated, Partial: snapshot.Partial}
	for _, value := range snapshot.Sessions {
		out.Sessions = append(out.Sessions, projectSession(value))
	}
	return out, err
}

func (s *Server) sessions(r *http.Request) (any, int) {
	if r.URL.RawQuery != "" {
		return nil, 400
	}
	out, err := s.list(r)
	if err != nil {
		return nil, 503
	}
	return out, 200
}
func (s *Server) session(r *http.Request) (any, int) {
	if r.URL.RawQuery != "" {
		return nil, 400
	}
	value, err := s.readSession(r.PathValue("id"))
	if err != nil {
		return nil, 404
	}
	return projectSession(value), 200
}
func (s *Server) overview(r *http.Request) (any, int) {
	if r.URL.RawQuery != "" {
		return nil, 400
	}
	list, err := s.list(r)
	if err != nil {
		return nil, 503
	}
	active := 0
	for _, v := range list.Sessions {
		switch v.State {
		case session.StateStarting, session.StateRunning, session.StateDegraded, session.StateStopping, session.StateWaiting:
			active++
		}
	}
	version := s.app.Version
	if !versionName.MatchString(version) {
		version = "unknown"
	}
	var latest *Usage
	if len(list.Sessions) > 0 {
		latest = list.Sessions[0].LatestInvocationUsage
	}
	return map[string]any{"schema_version": 1, "version": version, "read_only": true, "instance_id": s.instance, "generated_at": time.Now().UTC(), "session_count": len(list.Sessions), "active_sessions": active, "truncated": list.Truncated, "partial": list.Partial, "latest_invocation_usage": latest}, 200
}

type Component struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
	Managed   bool   `json:"managed"`
}

func (s *Server) components(r *http.Request) (any, int) {
	if r.URL.RawQuery != "" {
		return nil, 400
	}
	state, err := s.app.Store.LoadState()
	if err != nil {
		return nil, 503
	}
	rows := []Component{}
	for _, name := range []string{"ivoai", "codex", "claude-code", "opencode", "ai-memory", "graft", "caveman", "headroom", "ruflo", "node"} {
		c, ok := state.Components[name]
		if !ok {
			continue
		}
		version := c.Version
		if !versionName.MatchString(version) {
			version = "unknown"
		}
		rows = append(rows, Component{name, version, c.Installed, c.Managed})
	}
	return map[string]any{"components": rows, "partial": false}, 200
}
func (s *Server) connections(r *http.Request) (any, int) {
	if r.URL.RawQuery != "" {
		return nil, 400
	}
	cfg, err := s.app.Store.Load()
	if err != nil {
		return nil, 503
	}
	type connection struct {
		Name       string `json:"name"`
		Configured bool   `json:"configured"`
		Enabled    bool   `json:"enabled"`
		Count      int    `json:"count"`
	}
	serverEnabled := false
	for _, v := range cfg.Connections.Servers {
		serverEnabled = serverEnabled || v.Enabled
	}
	external, enabled := 0, false
	for _, v := range cfg.MCP.Servers {
		if v.Kind == "external" {
			external++
			enabled = enabled || v.Enabled
		}
	}
	return map[string]any{"connections": []connection{{"codex", cfg.Connections.ChatGPT.Status == "connected", cfg.Connections.ChatGPT.Status == "connected", boolCount(cfg.Connections.ChatGPT.Status == "connected")}, {"claude", cfg.Connections.Claude.Status == "connected", cfg.Connections.Claude.Status == "connected", boolCount(cfg.Connections.Claude.Status == "connected")}, {"server", len(cfg.Connections.Servers) > 0, serverEnabled, len(cfg.Connections.Servers)}, {"external_mcp", external > 0, enabled, external}}, "partial": false}, 200
}
func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}

type Event struct {
	Sequence    uint64    `json:"sequence"`
	ObservedAt  time.Time `json:"observed_at"`
	Category    string    `json:"category"`
	Operation   string    `json:"operation"`
	State       string    `json:"state"`
	DurationMS  int64     `json:"duration_ms"`
	CacheHit    *bool     `json:"cache_hit"`
	BytesBefore int64     `json:"bytes_before"`
	BytesAfter  int64     `json:"bytes_after"`
	TokenBasis  string    `json:"token_basis"`
}

func (s *Server) events(r *http.Request) (any, int) {
	q := r.URL.Query()
	if len(q) > 1 || len(q["after"]) > 1 || len(q) == 1 && q["after"] == nil {
		return nil, 400
	}
	var after uint64
	if raw := q.Get("after"); raw != "" {
		var err error
		after, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return nil, 400
		}
	}
	value, err := s.readSession(r.PathValue("id"))
	if err != nil {
		return nil, 404
	}
	page := session.ReplayObservations(value, after)
	rows := []Event{}
	for _, event := range page.Events {
		if event.Validate() != nil {
			return nil, 503
		}
		rows = append(rows, Event{event.Sequence, event.ObservedAt, string(event.Category), string(event.Operation), string(event.State), event.DurationMilliseconds, event.CacheHit, event.BytesBefore, event.BytesAfter, event.TokenBasis})
	}
	return map[string]any{"schema_version": 1, "session_id": value.SessionID, "latest_sequence": page.LatestSequence, "oldest_sequence": page.OldestSequence, "gap": page.Gap, "events": rows, "partial": false}, 200
}
