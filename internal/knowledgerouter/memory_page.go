package knowledgerouter

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ivo-lopes/ivoai/internal/serverpool"
)

const maxMemoryPageBytes = 1 << 20

type memoryReadRedirectKey struct{}

var (
	ErrMemoryPageArguments = errors.New("invalid memory page read arguments")
	ErrMemoryPageDenied    = errors.New("memory page read denied")
	ErrMemoryPageSource    = errors.New("memory page read requires one exact source")
	ErrMemoryPageResponse  = errors.New("memory page response rejected")
	ErrMemoryPageBusy      = errors.New("memory page read capacity reached")
)

// Only these exact tool arguments can reach the upstream. No name, endpoint,
// project, credential or retention decision comes from model/response data.
type MemoryPageArguments struct {
	Query string `json:"query,omitempty"`
	Path  string `json:"path,omitempty"`
}

func (MemoryPageArguments) String() string   { return "[private memory page arguments]" }
func (MemoryPageArguments) GoString() string { return "[private memory page arguments]" }

func (a *MemoryPageArguments) UnmarshalJSON(body []byte) error {
	if len(body) > 4096 {
		return ErrMemoryPageArguments
	}
	value, err := uniqueJSON(body)
	object, ok := value.(map[string]any)
	if err != nil || !ok || len(object) != 1 {
		return ErrMemoryPageArguments
	}
	var result MemoryPageArguments
	for name, value := range object {
		text, ok := value.(string)
		if !ok {
			return ErrMemoryPageArguments
		}
		switch name {
		case "query":
			result.Query = text
		case "path":
			result.Path = text
		default:
			return ErrMemoryPageArguments
		}
	}
	if result.validate() != nil {
		return ErrMemoryPageArguments
	}
	*a = result
	return nil
}

func (a MemoryPageArguments) validate() error {
	if (a.Query == "") == (a.Path == "") {
		return ErrMemoryPageArguments
	}
	value := a.Query
	if a.Path != "" {
		value = a.Path
	}
	if len(value) > 512 || strings.TrimSpace(value) == "" || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n\x1b") {
		return ErrMemoryPageArguments
	}
	if a.Path != "" && (path.IsAbs(value) || path.Clean(value) != value || value == "." || value == ".." || strings.HasPrefix(value, "../") || strings.ContainsAny(value, "\\:")) {
		return ErrMemoryPageArguments
	}
	return nil
}

// ScopeKey and Authorize are supplied by Core, never serialized into RPC args.
// ScopeKey binds the immutable response to its original local retention domain.
type MemoryPageAuthority struct {
	ScopeKey  string                      `json:"-"`
	Authorize func(context.Context) error `json:"-"`
}

type MemoryPageSource struct{ ID, Alias, Purpose, RedundancyGroup, EndpointSHA256 string }

type MemoryPageMetadata struct {
	ID              string
	SHA256          string
	ResultSHA256    string
	ArgumentsSHA256 string
	ScopeKey        string
	Source          MemoryPageSource
	CapturedAt      time.Time
	Transport       string
	Bytes           int
	// Complete response framing is not proof of a complete remote page/corpus.
	ResponseComplete bool
}

// An opaque, process-local response. Only a fresh admitted Router read creates
// it. Bytes returns a copy. Zero values or JSON-decoded lookalikes are invalid.
type MemoryPageSnapshot struct {
	metadata MemoryPageMetadata
	raw      []byte
}

func (MemoryPageSnapshot) String() string                 { return "[private immutable memory response snapshot]" }
func (MemoryPageSnapshot) GoString() string               { return "[private immutable memory response snapshot]" }
func (s MemoryPageSnapshot) Metadata() MemoryPageMetadata { return s.metadata }
func (s MemoryPageSnapshot) Bytes() []byte                { return bytes.Clone(s.raw) }
func (s MemoryPageSnapshot) Validate() error {
	m := s.metadata
	sum := sha256.Sum256(s.raw)
	if !strings.HasPrefix(m.ID, "memory_snapshot_") || len(m.ID) != len("memory_snapshot_")+32 || !hexString(strings.TrimPrefix(m.ID, "memory_snapshot_"), 16) || !hexString(m.ScopeKey, 32) || !hexString(m.ResultSHA256, 32) || !hexString(m.ArgumentsSHA256, 32) || !hexString(m.Source.EndpointSHA256, 32) || m.SHA256 != hex.EncodeToString(sum[:]) || len(s.raw) == 0 || len(s.raw) > maxMemoryPageBytes || m.Bytes != len(s.raw) || m.CapturedAt.IsZero() || !m.ResponseComplete || (m.Transport != "application/json" && m.Transport != "text/event-stream") || serverpool.ValidateID(m.Source.ID) != nil || serverpool.ValidateAlias(m.Source.Alias) != nil || serverpool.ValidateLabel("purpose", m.Source.Purpose) != nil {
		return ErrMemoryPageResponse
	}
	return nil
}

func hexString(s string, n int) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == n && s == strings.ToLower(s)
}

func (r *Router) admitMemoryRead(ctx context.Context, authority MemoryPageAuthority) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	active := r.token != ""
	r.mu.Unlock()
	if !active || !hexString(authority.ScopeKey, 32) || authority.Authorize == nil || authority.Authorize(ctx) != nil {
		return ErrMemoryPageDenied
	}
	return ctx.Err()
}

// ReadMemoryPageExact reads one fresh, stateless memory_read_page response using
// this Router's existing source selection, credentials, HTTP client and circuit.
// It does not cache LIVE reads, classify retention, persist or execute content.
// Sessionful, federated and unrecognized responses fail closed in this slice.
func (r *Router) ReadMemoryPageExact(ctx context.Context, args MemoryPageArguments, authority MemoryPageAuthority) (MemoryPageSnapshot, error) {
	if args.validate() != nil {
		return MemoryPageSnapshot{}, ErrMemoryPageArguments
	}
	if err := r.admitMemoryRead(ctx, authority); err != nil {
		return MemoryPageSnapshot{}, err
	}
	groups := r.groupsFor("memory")
	if len(r.selection.Groups) != 1 || len(r.selection.Groups[0].Profiles) != 1 || len(groups) != 1 || len(groups[0].Profiles) != 1 {
		return MemoryPageSnapshot{}, ErrMemoryPageSource
	}
	r.mu.Lock()
	if r.memoryReads == nil {
		r.memoryReads = make(chan struct{}, 8)
	}
	capacity := r.memoryReads
	r.mu.Unlock()
	select {
	case capacity <- struct{}{}:
		defer func() { <-capacity }()
	default:
		return MemoryPageSnapshot{}, ErrMemoryPageBusy
	}
	id, err := randomToken()
	if err != nil {
		return MemoryPageSnapshot{}, ErrMemoryPageDenied
	}
	requestID := "ivoai_memory_read_" + id
	argBytes, _ := json.Marshal(args)
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": requestID, "method": "tools/call", "params": map[string]any{"name": "memory_read_page", "arguments": json.RawMessage(argBytes)}})
	var resultDigest, transport string
	callContext := context.WithValue(ctx, memoryReadRedirectKey{}, true)
	response, profile, failover, err := r.callGroupValidated(callContext, groups[0], "memory", body, false, http.Header{}, maxMemoryPageBytes, func(response upstreamResponse) error {
		var validationErr error
		resultDigest, transport, validationErr = strictMemoryPageResponse(response, requestID)
		return validationErr
	})
	// This is upstream transport health, not an assertion of snapshot retention.
	r.emit(Event{Operation: "memory", SourceID: profile.ID, SourceAlias: profile.Alias, Purpose: profile.Purpose, SelectedCount: 1, Failover: failover, Duration: response.duration, State: state(err), Reason: boundedReason(err)})
	if err != nil {
		if errors.Is(err, ErrMemoryPageResponse) {
			return MemoryPageSnapshot{}, ErrMemoryPageResponse
		}
		return MemoryPageSnapshot{}, errors.New(boundedReason(err))
	}
	if err := r.admitMemoryRead(ctx, authority); err != nil {
		return MemoryPageSnapshot{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return MemoryPageSnapshot{}, ErrMemoryPageDenied
	}
	sum := sha256.Sum256(response.body)
	argSum := sha256.Sum256(argBytes)
	endpointSum := sha256.Sum256([]byte(profile.MemoryMCPURL))
	snapshot := MemoryPageSnapshot{raw: bytes.Clone(response.body), metadata: MemoryPageMetadata{ID: "memory_snapshot_" + hex.EncodeToString(nonce[:]), SHA256: hex.EncodeToString(sum[:]), ResultSHA256: resultDigest, ArgumentsSHA256: hex.EncodeToString(argSum[:]), ScopeKey: authority.ScopeKey, Source: MemoryPageSource{ID: profile.ID, Alias: profile.Alias, Purpose: profile.Purpose, RedundancyGroup: profile.RedundancyGroup, EndpointSHA256: hex.EncodeToString(endpointSum[:])}, CapturedAt: r.now().UTC(), Transport: transport, Bytes: len(response.body), ResponseComplete: true}}
	if err := snapshot.Validate(); err != nil {
		return MemoryPageSnapshot{}, err
	}
	return snapshot, nil
}
