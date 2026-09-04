package opencodebridge

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

//go:embed assets/server-plugin.mjs
var serverPlugin []byte

//go:embed assets/tui-plugin.tsx
var tuiPlugin []byte

//go:embed assets/ivoai-theme.json
var ivoaiTheme []byte

type ManagedOptions struct {
	OpenCodePath string
	Version      string
	RuntimeDir   string
	StateDir     string
	Directory    string
	Environment  []string
	Bridge       *Bridge
	Instructions string
	// ResumeSessionID is a non-sensitive OpenCode conversation identifier. It
	// is only supplied after IVOAI has matched the working directory and
	// knowledge scope of a completed managed session.
	ResumeSessionID string
}

type Managed struct {
	URL             string
	Environment     []string
	AttachArgs      []string
	password        string
	command         *exec.Cmd
	done            chan struct{}
	doneMu          sync.Mutex
	doneErr         error
	expectedVersion string
	expectedModels  []string
	lease           *os.File
	closeOnce       sync.Once
}

func (m *Managed) Args() []string        { return append([]string(nil), m.AttachArgs...) }
func (m *Managed) Env() []string         { return append([]string(nil), m.Environment...) }
func (m *Managed) BackendURL() string    { return m.URL }
func (m *Managed) BackendLoopback() bool { return strings.HasPrefix(m.URL, "http://127.0.0.1:") }

func StartManaged(ctx context.Context, options ManagedOptions) (*Managed, error) {
	if options.OpenCodePath == "" || options.RuntimeDir == "" || options.StateDir == "" || options.Directory == "" || options.Bridge == nil {
		return nil, errors.New("incomplete managed OpenCode options")
	}
	if err := platform.EnsurePrivateDir(options.RuntimeDir); err != nil {
		return nil, err
	}
	lease, err := acquireManagedLease(options.StateDir)
	if err != nil {
		return nil, err
	}
	releaseLease := true
	defer func() {
		if releaseLease {
			releaseManagedLease(lease)
		}
	}()
	paths, err := writeManagedAssets(options)
	if err != nil {
		return nil, err
	}
	password, err := randomSecret()
	if err != nil {
		return nil, err
	}
	environment, err := managedEnvironment(options.Environment, options.StateDir, paths, password)
	if err != nil {
		return nil, err
	}
	command := exec.Command(options.OpenCodePath, "serve", "--hostname", "127.0.0.1", "--port", "0")
	command.Dir = options.Directory
	command.Env = environment
	command.SysProcAttr = managedProcessAttributes()
	var processStdout, processStderr strings.Builder
	address := &listenAddressWriter{writer: &boundedWriter{writer: &processStdout, remaining: 64 << 10}, found: make(chan string, 1)}
	command.Stdout = address
	command.Stderr = &boundedWriter{writer: &processStderr, remaining: 64 << 10}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start managed OpenCode backend: %w", err)
	}
	models := make([]string, 0, len(options.Bridge.Catalog().Entries()))
	for _, entry := range options.Bridge.Catalog().Entries() {
		models = append(models, entry.ID)
	}
	managed := &Managed{Environment: environment, password: password, command: command, done: make(chan struct{}), expectedVersion: options.Version, expectedModels: models, lease: lease}
	go func() {
		managed.doneMu.Lock()
		managed.doneErr = command.Wait()
		managed.doneMu.Unlock()
		close(managed.done)
	}()
	select {
	case managed.URL = <-address.found:
		managed.AttachArgs = []string{"attach", managed.URL, "--dir", options.Directory}
		if options.ResumeSessionID != "" {
			if !safeID(options.ResumeSessionID) {
				_ = managed.Close(context.Background())
				return nil, errors.New("invalid managed OpenCode resume session identifier")
			}
			managed.AttachArgs = append(managed.AttachArgs, "--session", options.ResumeSessionID)
		}
	case <-managed.done:
		_ = managed.Close(context.Background())
		return nil, errors.New("managed OpenCode backend exited before publishing its loopback listener")
	case <-ctx.Done():
		_ = managed.Close(context.Background())
		return nil, ctx.Err()
	case <-time.After(20 * time.Second):
		_ = managed.Close(context.Background())
		return nil, errors.New("managed OpenCode backend did not publish its loopback listener")
	}
	if err := managed.waitReady(ctx); err != nil {
		_ = managed.Close(context.Background())
		return nil, fmt.Errorf("managed OpenCode backend readiness: %w", err)
	}
	if err := managed.waitProviderReady(ctx, options.Directory); err != nil {
		_ = managed.Close(context.Background())
		return nil, fmt.Errorf("managed OpenCode provider readiness: %w", err)
	}
	releaseLease = false
	return managed, nil
}

func acquireManagedLease(stateDir string) (*os.File, error) {
	if err := platform.EnsurePrivateDir(stateDir); err != nil {
		return nil, err
	}
	path := filepath.Join(stateDir, "opencode-managed.lock")
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open managed OpenCode lease: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("another managed OpenCode frontend is already active")
	}
	return file, nil
}

func releaseManagedLease(file *os.File) {
	if file == nil {
		return
	}
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	_ = file.Close()
}

func managedProcessAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}

type managedPaths struct {
	config string
	tui    string
	theme  string
}

func writeManagedAssets(options ManagedOptions) (managedPaths, error) {
	assets := filepath.Join(options.RuntimeDir, "opencode-managed")
	if err := platform.EnsurePrivateDir(assets); err != nil {
		return managedPaths{}, err
	}
	paths := managedPaths{
		config: filepath.Join(assets, "opencode.json"),
		tui:    filepath.Join(assets, "tui.json"),
		theme:  filepath.Join(assets, "ivoai-theme.json"),
	}
	serverPath := filepath.Join(assets, "server-plugin.mjs")
	tuiPath := filepath.Join(assets, "tui-plugin.tsx")
	instructionsPath := filepath.Join(assets, "instructions.md")
	for path, body := range map[string][]byte{serverPath: serverPlugin, tuiPath: tuiPlugin, paths.theme: ivoaiTheme, instructionsPath: []byte(options.Instructions)} {
		if err := platform.AtomicWritePrivate(body, path); err != nil {
			return managedPaths{}, err
		}
	}
	serverURI := (&url.URL{Scheme: "file", Path: serverPath}).String()
	tuiURI := (&url.URL{Scheme: "file", Path: tuiPath}).String()
	configuration := map[string]any{
		"$schema":           "https://opencode.ai/config.json",
		"autoupdate":        false,
		"share":             "disabled",
		"model":             "ivoai/auto",
		"enabled_providers": []string{"ivoai"},
		"instructions":      []string{instructionsPath},
		"plugin":            []any{serverURI},
		"agent":             map[string]any{"title": map[string]any{"disable": true}},
		"compaction":        map[string]any{"auto": false, "prune": false},
		"provider": map[string]any{"ivoai": map[string]any{
			"npm": "@ai-sdk/openai-compatible", "name": "IVOAI",
			"options": map[string]any{"baseURL": options.Bridge.URL() + "/v1", "apiKey": options.Bridge.Token()},
			"models":  options.Bridge.Catalog().OpenCodeModels(),
		}},
	}
	tuiConfiguration := map[string]any{
		"$schema": "https://opencode.ai/tui.json", "theme": "ivoai",
		"plugin": []any{[]any{tuiURI, map[string]string{"bridge": options.Bridge.URL(), "token": options.Bridge.Token(), "theme": paths.theme}}},
	}
	for path, value := range map[string]any{paths.config: configuration, paths.tui: tuiConfiguration} {
		body, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return managedPaths{}, err
		}
		if err := platform.AtomicWritePrivate(append(body, '\n'), path); err != nil {
			return managedPaths{}, err
		}
	}
	return paths, nil
}

func managedEnvironment(existing []string, stateDir string, paths managedPaths, password string) ([]string, error) {
	if existing == nil {
		existing = os.Environ()
	}
	root := filepath.Join(stateDir, "opencode-managed")
	values := map[string]string{
		"OPENCODE_CONFIG":                  paths.config,
		"OPENCODE_TUI_CONFIG":              paths.tui,
		"OPENCODE_DISABLE_PROJECT_CONFIG":  "1",
		"OPENCODE_DISABLE_AUTOUPDATE":      "1",
		"OPENCODE_DISABLE_MODELS_FETCH":    "1",
		"OPENCODE_DISABLE_LSP_DOWNLOAD":    "1",
		"OPENCODE_DISABLE_EXTERNAL_SKILLS": "1",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS": "1",
		"OPENCODE_SERVER_USERNAME":         "ivoai",
		"OPENCODE_SERVER_PASSWORD":         password,
		"XDG_CONFIG_HOME":                  filepath.Join(root, "config"),
		"XDG_DATA_HOME":                    filepath.Join(root, "data"),
		"XDG_STATE_HOME":                   filepath.Join(root, "state"),
		"XDG_CACHE_HOME":                   filepath.Join(root, "cache"),
		"HOME":                             filepath.Join(root, "home"),
	}
	for _, directory := range []string{values["XDG_CONFIG_HOME"], values["XDG_DATA_HOME"], values["XDG_STATE_HOME"], values["XDG_CACHE_HOME"], values["HOME"]} {
		if err := platform.EnsurePrivateDir(directory); err != nil {
			return nil, fmt.Errorf("prepare managed OpenCode directory: %w", err)
		}
	}
	allowed := map[string]bool{
		"PATH": true, "TERM": true, "COLORTERM": true, "TERM_PROGRAM": true,
		"TERM_PROGRAM_VERSION": true, "LANG": true, "LANGUAGE": true, "TZ": true,
		"TMPDIR": true, "NO_COLOR": true, "IVOAI_ASCII": true,
	}
	result := make([]string, 0, len(existing)+len(values))
	for _, entry := range existing {
		key, _, found := strings.Cut(entry, "=")
		if !found || !allowed[key] && !strings.HasPrefix(key, "LC_") {
			continue
		}
		result = append(result, entry)
	}
	for key, value := range values {
		result = setEnv(result, key, value)
	}
	return result, nil
}

type listenAddressWriter struct {
	mu     sync.Mutex
	writer io.Writer
	buffer strings.Builder
	found  chan string
	once   sync.Once
}

func (w *listenAddressWriter) Write(body []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	original := len(body)
	_, _ = w.writer.Write(body)
	w.buffer.Write(body)
	if w.buffer.Len() > 4096 {
		return original, errors.New("OpenCode listener announcement exceeded limit")
	}
	for {
		line, rest, found := strings.Cut(w.buffer.String(), "\n")
		if !found {
			break
		}
		w.buffer.Reset()
		w.buffer.WriteString(rest)
		const prefix = "opencode server listening on "
		if strings.HasPrefix(line, prefix) {
			candidate := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			parsed, err := url.Parse(candidate)
			if err == nil && parsed.Scheme == "http" && parsed.User == nil && parsed.Hostname() == "127.0.0.1" && parsed.Port() != "" && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" {
				w.once.Do(func() { w.found <- candidate })
			}
		}
	}
	return original, nil
}

func (m *Managed) waitReady(ctx context.Context) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			m.doneMu.Lock()
			err := m.doneErr
			m.doneMu.Unlock()
			if err == nil {
				err = errors.New("backend exited before readiness")
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("backend readiness timeout")
		case <-ticker.C:
			request, _ := http.NewRequestWithContext(ctx, http.MethodGet, m.URL+"/global/health", nil)
			request.SetBasicAuth("ivoai", m.password)
			response, err := client.Do(request)
			if err == nil {
				body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
				_ = response.Body.Close()
				var health struct {
					Healthy bool   `json:"healthy"`
					Version string `json:"version"`
				}
				if response.StatusCode == http.StatusOK && json.Unmarshal(body, &health) == nil && health.Healthy && (m.expectedVersion == "" || health.Version == m.expectedVersion) {
					return nil
				}
			}
		}
	}
}

func (m *Managed) waitProviderReady(ctx context.Context, directory string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	target, err := url.Parse(m.URL + "/config/providers")
	if err != nil {
		return err
	}
	query := target.Query()
	query.Set("directory", directory)
	target.RawQuery = query.Encode()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			return errors.New("backend exited before provider readiness")
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("IVOAI provider catalog readiness timeout")
		case <-ticker.C:
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
			if err != nil {
				return err
			}
			request.SetBasicAuth("ivoai", m.password)
			response, err := client.Do(request)
			if err != nil {
				continue
			}
			body, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK || readErr != nil || len(body) > 64<<10 {
				continue
			}
			var registry struct {
				Providers []struct {
					ID     string         `json:"id"`
					Models map[string]any `json:"models"`
				} `json:"providers"`
			}
			if json.Unmarshal(body, &registry) != nil {
				continue
			}
			for _, provider := range registry.Providers {
				if provider.ID != "ivoai" {
					continue
				}
				ready := true
				for _, model := range m.expectedModels {
					if _, ok := provider.Models[model]; !ok {
						ready = false
						break
					}
				}
				if ready && len(m.expectedModels) > 0 {
					return nil
				}
			}
		}
	}
}

func (m *Managed) Close(ctx context.Context) error {
	var result error
	m.closeOnce.Do(func() {
		defer func() {
			releaseManagedLease(m.lease)
			m.lease = nil
		}()
		if m.command == nil || m.command.Process == nil {
			return
		}
		_ = syscall.Kill(-m.command.Process.Pid, syscall.SIGTERM)
		select {
		case <-m.done:
			m.doneMu.Lock()
			err := m.doneErr
			m.doneMu.Unlock()
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					result = err
				}
			}
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(-m.command.Process.Pid, syscall.SIGKILL)
			select {
			case <-m.done:
			case <-ctx.Done():
				result = ctx.Err()
			}
		}
	})
	return result
}

func randomSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func setEnv(environment []string, key, value string) []string {
	prefix := key + "="
	result := append([]string(nil), environment...)
	for index, entry := range result {
		if strings.HasPrefix(entry, prefix) {
			result[index] = prefix + value
			return result
		}
	}
	return append(result, prefix+value)
}
