package admin

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

// Dispatch is the existing CLI application dispatcher, injected to avoid an
// import cycle. Arguments are constructed exclusively from the typed catalog,
// not a shell, executable name, browser-supplied argv, or an agent instruction.
type Dispatch func(context.Context, []string, io.Reader, io.Writer, io.Writer) error

type Field struct {
	Name     string   `json:"name"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
	Help     string   `json:"help,omitempty"`
}
type Action struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Group           string  `json:"group"`
	Description     string  `json:"description"`
	Kind            string  `json:"kind"`
	Fields          []Field `json:"fields"`
	Confirmation    bool    `json:"confirmation"`
	Available       bool    `json:"available"`
	Reason          string  `json:"reason,omitempty"`
	command         []string
	optionalFlags   map[string]string
	stdin           string
	sensitiveResult bool
	requiresRoot    bool
}

type confirmation struct {
	digest, actor, fingerprint string
	expires                    time.Time
}
type actionRequest struct {
	Input        map[string]string `json:"input"`
	Phase        string            `json:"phase"`
	Confirmation string            `json:"confirmation,omitempty"`
	RequestID    string            `json:"request_id,omitempty"`
}

func (s *Server) actions(r *http.Request) (any, int) {
	if r.URL.RawQuery != "" {
		return nil, 400
	}
	rows := ActionCatalog()
	for i := range rows {
		if rows[i].ID == "project.init" {
			if cwd, err := os.Getwd(); err == nil {
				rows[i].Fields[0].Help = "Only this console working directory is eligible: " + cwd
			}
		}
		if rows[i].requiresRoot && os.Geteuid() != 0 {
			rows[i].Available = false
			rows[i].Reason = "Requires a server administrator process started with existing OS privileges; HTTP cannot elevate privileges."
		}
		if rows[i].Kind != "information" && rows[i].Kind != "native_handoff" && s.dispatch == nil {
			rows[i].Available = false
			rows[i].Reason = "Administrative dispatcher unavailable in this host."
		}
	}
	return map[string]any{"schema_version": 1, "actions": rows}, 200
}
func (s *Server) tokenHash(r *http.Request) string {
	c, err := r.Cookie(s.cookie)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(c.Value))
	return hex.EncodeToString(sum[:])
}
func (s *Server) csrfValue(r *http.Request) string {
	h := hmac.New(sha256.New, []byte(s.csrfSecret))
	h.Write([]byte(s.tokenHash(r)))
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Server) canManage(r *http.Request) bool {
	c, err := r.Cookie(s.cookie)
	if err != nil {
		return false
	}
	_, err = s.auth.Authenticate(c.Value, s.Resource(), ScopeRead, ScopeManage)
	return err == nil
}
func (s *Server) csrf(r *http.Request) (any, int) {
	access := "read"
	if s.canManage(r) {
		access = "manage"
	}
	return map[string]string{"csrf_token": s.csrfValue(r), "access": access}, 200
}
func (s *Server) configurationFingerprint() (string, error) {
	// A configuration edit, permission/policy change or root replacement between
	// preview and execution invalidates the authorization. No secrets are logged.
	h := sha256.New()
	for _, p := range []string{s.paths.Config, s.paths.State, s.paths.Ownership} {
		fd, err := unix.Open(p, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		var f *os.File
		if err == nil {
			f = os.NewFile(uintptr(fd), p)
		}
		if errors.Is(err, os.ErrNotExist) {
			h.Write([]byte("missing\x00"))
			continue
		}
		if err != nil {
			return "", err
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 8<<20 {
			f.Close()
			return "", errors.New("administrative state unavailable")
		}
		_, err = io.Copy(h, io.LimitReader(f, (8<<20)+1))
		f.Close()
		if err != nil {
			return "", err
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func actionDigest(id string, input map[string]string) string {
	b, _ := json.Marshal(struct {
		ID    string
		Input map[string]string
	}{id, input})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s *Server) action(w http.ResponseWriter, r *http.Request) {
	p, err := s.principal(r)
	if err != nil {
		s.emit("action", "deny", "")
		errorJSON(w, 401, "unauthorized")
		return
	}
	if r.Header.Get("Origin") != s.origin || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-IVOAI-CSRF")), []byte(s.csrfValue(r))) != 1 {
		s.emit("action", "deny", p.ClientID)
		errorJSON(w, 403, "csrf_required")
		return
	}
	content, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || content != "application/json" || r.URL.RawQuery != "" {
		errorJSON(w, 400, "invalid_request")
		return
	}
	var in actionRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || dec.Decode(new(any)) != io.EOF || len(in.RequestID) > 96 || len(in.Confirmation) > 128 || len(in.Input) > 16 {
		errorJSON(w, 400, "invalid_request")
		return
	}
	var a Action
	found := false
	for _, candidate := range ActionCatalog() {
		if candidate.ID == r.PathValue("id") {
			a = candidate
			found = true
			break
		}
	}
	if !found {
		errorJSON(w, 404, "unknown_action")
		return
	}
	if a.requiresRoot && os.Geteuid() != 0 {
		errorJSON(w, 403, "existing_os_privileges_required")
		return
	}
	if err := validateActionInput(a, in.Input); err != nil {
		errorJSON(w, 400, "invalid_input")
		return
	}
	if a.ID == "project.init" {
		cwd, err := os.Getwd()
		if err != nil || !filepath.IsAbs(in.Input["project_root"]) || filepath.Clean(in.Input["project_root"]) != cwd {
			errorJSON(w, 409, "project_scope_mismatch")
			return
		}
	}
	if in.Phase != "preview" && in.Phase != "execute" {
		errorJSON(w, 400, "invalid_phase")
		return
	}
	if a.Kind == "mutation" && !s.canManage(r) {
		s.emit(a.ID, "deny", p.ClientID)
		errorJSON(w, 403, "manage_scope_required")
		return
	}
	digest := actionDigest(a.ID, in.Input)
	fingerprint, err := s.configurationFingerprint()
	if err != nil {
		errorJSON(w, 503, "state_unavailable")
		return
	}
	if a.Kind == "mutation" && in.Phase == "preview" {
		token, err := randomString()
		if err != nil {
			errorJSON(w, 503, "authorization_unavailable")
			return
		}
		expires := time.Now().Add(2 * time.Minute)
		s.mu.Lock()
		for key, c := range s.confirmations {
			if time.Now().After(c.expires) {
				delete(s.confirmations, key)
			}
		}
		if len(s.confirmations) >= 64 {
			s.mu.Unlock()
			errorJSON(w, 429, "capacity_exceeded")
			return
		}
		s.confirmations[token] = confirmation{digest, s.tokenHash(r), fingerprint, expires}
		s.mu.Unlock()
		s.emit(a.ID, "preview", p.ClientID)
		response(w, map[string]any{"action_id": a.ID, "summary": a.Description, "confirmation": token, "expires_at": expires})
		return
	}
	if in.Phase == "preview" {
		response(w, map[string]any{"action_id": a.ID, "summary": a.Description})
		return
	}
	// Serialize all administrative dispatch, including reads of changing state.
	// This lock is deliberately nonblocking: no hidden job/scheduler or retry.
	if !s.actionMu.TryLock() {
		errorJSON(w, 409, "administration_busy")
		return
	}
	defer s.actionMu.Unlock()
	unlock, lockErr := s.lockAdministration()
	if lockErr != nil {
		errorJSON(w, 409, "administration_busy")
		return
	}
	defer unlock()
	if _, err := s.principal(r); err != nil || !s.rootValid() {
		errorJSON(w, 401, "unauthorized")
		return
	}
	if a.Kind == "mutation" {
		if !requestIDPattern.MatchString(in.RequestID) {
			errorJSON(w, 400, "request_id_required")
			return
		}
		s.mu.Lock()
		receipt, ok := s.confirmations[in.Confirmation]
		delete(s.confirmations, in.Confirmation)
		s.mu.Unlock()
		if !ok || time.Now().After(receipt.expires) || receipt.digest != digest || receipt.actor != s.tokenHash(r) || receipt.fingerprint != fingerprint {
			errorJSON(w, 409, "confirmation_invalid_or_stale")
			return
		}
		// Re-check privilege and configuration after acquiring ownership. A consumed
		// confirmation is never replayed, even if cancellation/failure follows.
		latest, err := s.configurationFingerprint()
		if err != nil || latest != fingerprint || !s.canManage(r) {
			errorJSON(w, 409, "confirmation_invalid_or_stale")
			return
		}
	}
	if r.Context().Err() != nil {
		errorJSON(w, 408, "cancelled")
		return
	}
	var receiptPath string
	if a.Kind == "mutation" {
		receiptPath, err = s.claimRequest(r, a.ID, in.RequestID)
		if err != nil {
			errorJSON(w, 409, "request_already_claimed_or_unavailable")
			return
		}
	}
	args, stdin := actionArguments(a, in.Input)
	if a.Kind == "native_handoff" {
		s.emit(a.ID, "handoff", p.ClientID)
		response(w, map[string]any{"action_id": a.ID, "status": "handoff", "output": a.Description, "handoff": map[string]any{"executable": "ivoai", "args": args}, "request_id": in.RequestID})
		return
	}
	if a.Kind == "information" {
		response(w, map[string]any{"action_id": a.ID, "status": "information", "output": a.Description, "request_id": in.RequestID})
		return
	}
	if s.dispatch == nil {
		errorJSON(w, 503, "dispatcher_unavailable")
		return
	}
	// ConfigShow would dump private headers/custom fields. Its web equivalent is
	// the existing explicitly nonsecret management snapshot, never raw TOML.
	if a.ID == "config.show" {
		snapshot, err := s.app.MenuSnapshot()
		if err != nil {
			errorJSON(w, 503, "state_unavailable")
			return
		}
		response(w, map[string]any{"action_id": a.ID, "status": "completed", "output": snapshot, "request_id": in.RequestID})
		return
	}
	out := &boundedOutput{limit: maxAdministrativeOutputBytes}
	privateOutputValues, initialPrivateError := s.outputSecrets(r)
	s.emit(a.ID, "started", p.ClientID)
	operationContext, stopOperation := context.WithCancel(r.Context())
	watchDone := make(chan struct{})
	go func() {
		timer := time.NewTicker(100 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-operationContext.Done():
				return
			case <-timer.C:
				if _, err := s.principal(r); err != nil || !s.rootValid() || (a.Kind == "mutation" && !s.canManage(r)) {
					stopOperation()
					return
				}
			}
		}
	}()
	err = s.dispatch(operationContext, args, strings.NewReader(stdin), out, out)
	close(watchDone)
	operationCancelled := operationContext.Err() != nil
	stopOperation()
	// Never include error text: it can contain secret input or external output.
	status := "completed"
	if err != nil {
		status = "failed"
	}
	if r.Context().Err() != nil || operationCancelled {
		status = "cancelled"
	}
	currentPrivateValues, currentPrivateError := s.outputSecrets(r)
	privateOutputValues = append(privateOutputValues, currentPrivateValues...)
	rawOutput := out.String()
	sanitizationInput := rawOutput
	if out.truncated {
		// Incomplete captures are never archived. Inspect enough lookahead to redact
		// any known credential starting within the visible prefix, without spending
		// unbounded sanitization work on output that cannot be recovered completely.
		lookahead := 8192
		for _, value := range privateOutputValues {
			if len(value) > lookahead {
				lookahead = len(value)
			}
		}
		for _, value := range in.Input {
			if len(value) > lookahead {
				lookahead = len(value)
			}
		}
		if budget := maxResponseBytes/2 + lookahead; len(sanitizationInput) > budget {
			sanitizationInput = sanitizationInput[:budget]
		}
	}
	sanitized := sanitizedAdministrativeOutput(sanitizationInput, in.Input, privateOutputValues)
	redactionAvailable := initialPrivateError == nil && currentPrivateError == nil
	if !redactionAvailable {
		sanitized = "Administrative output withheld: private redaction context unavailable. Inspect current state without repeating the operation."
	}
	preview := administrativeOutputPreview(sanitized)
	truncated := out.truncated || len(preview) < len(sanitized)
	result := map[string]any{"action_id": a.ID, "status": status, "request_id": in.RequestID, "output": preview, "truncated": truncated}
	if truncated {
		if !out.truncated && redactionAvailable {
			if ref, retainErr := s.retainOutput(r.Context(), r, a.ID, sanitized); retainErr == nil {
				result["output_ref"] = ref
			} else {
				result["output_warning"] = "Sanitized output could not be retained (quota, storage or authorization); inspect current state without repeating the operation."
			}
		} else {
			result["output_warning"] = "Capture exceeded the 16 MiB safety limit; complete output was not retained. Inspect current state without repeating the operation."
		}
	}
	if a.sensitiveResult && err == nil && redactionAvailable { // Only explicitly requested one-time codes, separate from logs/output.
		if secret := oneTimeCode(rawOutput); secret != "" {
			result["one_time_code"] = secret
			result["one_time_code_warning"] = "Shown once; keep private. It is never written to the audit log."
		}
	}
	// Revocation during a command cannot undo an authorized mutation; withhold
	// output and record it rather than replaying or claiming rollback.
	if _, authErr := s.principal(r); authErr != nil || !s.rootValid() {
		s.emit(a.ID, "authority_changed", p.ClientID)
		errorJSON(w, 401, "authority_changed_result_withheld")
		return
	}
	if receiptPath != "" {
		if writeErr := finishRequest(receiptPath, status); writeErr != nil {
			s.emit(a.ID, "receipt_persistence_failed", p.ClientID)
			errorJSON(w, 503, "operation_outcome_requires_inspection")
			return
		}
	}
	s.emit(a.ID, status, p.ClientID)
	response(w, result)
}
func validateActionInput(a Action, input map[string]string) error {
	fields := map[string]Field{}
	for _, f := range a.Fields {
		fields[f.Name] = f
	}
	for k, v := range input {
		f, ok := fields[k]
		if !ok || len(v) > 4096 || strings.IndexFunc(v, func(c rune) bool { return unicode.IsControl(c) }) >= 0 {
			return errors.New("invalid input")
		}
		if f.Type != "password" && strings.HasPrefix(v, "-") {
			return errors.New("option injection")
		}
		if len(f.Options) > 0 {
			found := false
			for _, o := range f.Options {
				if v == o {
					found = true
				}
			}
			if !found {
				return errors.New("invalid option")
			}
		}
	}
	for _, f := range a.Fields {
		if f.Required && strings.TrimSpace(input[f.Name]) == "" {
			return errors.New("missing field")
		}
	}
	return nil
}
func actionArguments(a Action, input map[string]string) ([]string, string) {
	args := make([]string, 0, len(a.command)+len(a.optionalFlags)*2)
	for _, part := range a.command {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			part = input[strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")]
		}
		args = append(args, part)
	}
	names := make([]string, 0, len(a.optionalFlags))
	for name := range a.optionalFlags {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if value := input[name]; value != "" {
			if flag := a.optionalFlags[name]; flag != "" {
				args = append(args, flag)
			}
			args = append(args, value)
		}
	}
	stdin := ""
	if a.stdin != "" {
		stdin = input[a.stdin] + "\n"
	}
	return args, stdin
}

type boundedOutput struct {
	mu sync.Mutex
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) > b.limit-b.Len() {
		p = p[:b.limit-b.Len()]
		b.truncated = true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func (b *boundedOutput) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }

var oneTimePattern = regexp.MustCompile(`(?m)^(?:Activation|Enrollment) code \(shown once\): ([A-Za-z0-9_-]{16,256})$`)
var sensitiveLine = regexp.MustCompile(`(?im)^.*(?:activation code|enrollment code|access.token|refresh.token|authorization|cookie|client.secret|api.key|bearer).*?$`)

func oneTimeCode(out string) string {
	m := oneTimePattern.FindStringSubmatch(out)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}
func sanitizeOutput(out string, input map[string]string) string {
	if potentiallySensitiveOutput(out) {
		out = platform.Redact(out)
		out = sensitiveLine.ReplaceAllString(out, "[REDACTED]")
	}
	for name, value := range input {
		if len(value) > 0 && (name == "secret" || name == "code" || name == "token" || name == "value" && len(value) > 24) {
			out = strings.ReplaceAll(out, value, "[REDACTED]")
		}
	}
	return strings.Map(func(c rune) rune {
		if unicode.IsControl(c) && c != '\n' && c != '\t' {
			return -1
		}
		return c
	}, out)
}

func field(name, label string) Field {
	return Field{Name: name, Label: label, Type: "text", Required: true}
}
func choice(name, label string, options ...string) Field {
	return Field{Name: name, Label: label, Type: "select", Required: true, Options: options}
}
func password(name, label string) Field {
	return Field{Name: name, Label: label, Type: "password", Required: true}
}
func makeAction(id, kind string, fields []Field, args ...string) Action {
	group, _, _ := strings.Cut(id, ".")
	if group == "" {
		group = "maintenance"
	}
	return Action{ID: id, Title: strings.ReplaceAll(id, ".", " · "), Group: group, Description: fmt.Sprintf("%s through the existing IVOAI control plane; existing policy, validation and operating-system privileges apply.", id), Kind: kind, Fields: fields, Confirmation: kind == "mutation", Available: true, command: args}
}

// CatalogIDs includes every public TUI item plus web equivalents for profile
// apply/preview and native conversation inspection. Navigation and intentionally
// unavailable controls are classified, never counted as executed mutations.
func CatalogIDs() []string {
	rows := ActionCatalog()
	out := make([]string, 0, len(rows))
	for _, a := range rows {
		out = append(out, a.ID)
	}
	sort.Strings(out)
	return out
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// Claim-before-dispatch is durable across HTTP retries and process restart.
// Stored receipts contain action/status/time only, never inputs, output or tokens.
func (s *Server) claimRequest(r *http.Request, action, requestID string) (string, error) {
	dir := filepath.Join(filepath.Dir(s.auth.Path), "operations")
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil || !privateOwner(info) {
		return "", errors.New("invalid operation store")
	}
	h := sha256.Sum256([]byte(s.instance + "\x00" + requestID))
	entries, err := os.Open(dir)
	if err != nil {
		return "", err
	}
	names, readErr := entries.Readdirnames(10001)
	entries.Close()
	if readErr != nil && readErr != io.EOF {
		return "", readErr
	}
	if len(names) >= 10000 {
		return "", errors.New("operation receipt capacity exceeded")
	}
	path := filepath.Join(dir, hex.EncodeToString(h[:])+".json")
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(fd), path)
	b, _ := json.Marshal(map[string]any{"schema_version": 1, "action": action, "status": "claimed", "claimed_at": time.Now().UTC()})
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	parent, err := os.Open(dir)
	if err != nil {
		return "", err
	}
	err = parent.Sync()
	parent.Close()
	return path, err
}
func finishRequest(path, status string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var record map[string]any
	if json.Unmarshal(body, &record) != nil {
		return errors.New("invalid request receipt")
	}
	record["status"] = status
	record["completed_at"] = time.Now().UTC()
	body, err = json.Marshal(record)
	if err != nil {
		return err
	}
	return platform.AtomicWritePrivate(append(body, '\n'), path)
}

// Concurrent console processes for the same owner/configuration share this lock.
// Acquiring an administrative lease never elevates process privileges.
func (s *Server) lockAdministration() (func(), error) {
	path := filepath.Join(filepath.Dir(s.auth.Path), "administration.lock")
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || int(stat.Uid) != os.Geteuid() || stat.Mode&0077 != 0 {
		unix.Close(fd)
		return nil, errors.New("invalid administrative lock")
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, err
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = unix.Close(fd) }, nil
}

// All current redaction patterns contain at least one of these ASCII markers.
// Avoid expensive case-folding regex machines for long, plain compiler lines.
// Non-ASCII always takes the original regex path, preserving Unicode folding.
func potentiallySensitiveOutput(out string) bool {
	for i := 0; i < len(out); i++ {
		if out[i] >= 0x80 {
			return true
		}
	}
	lower := strings.ToLower(out)
	for _, marker := range []string{"authorization", "cookie", "api", "secret", "access", "refresh", "oauth", "id", "client", "enrollment", "activation", "bearer", "sk-", "ivo", "plane_api_"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
