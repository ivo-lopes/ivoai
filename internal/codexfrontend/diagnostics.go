package codexfrontend

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

// startupTail is never persisted or rendered. Only fixed operational categories
// leave this boundary; redaction alone cannot identify every prompt or secret.
type startupTail struct {
	mu      sync.Mutex
	value   []byte
	stopped bool
}

func (s *startupTail) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return len(p), nil
	}
	const limit = 4096
	if len(p) >= limit {
		s.value = append(s.value[:0], p[len(p)-limit:]...)
	} else {
		s.value = append(s.value, p...)
		if len(s.value) > limit {
			s.value = append([]byte(nil), s.value[len(s.value)-limit:]...)
		}
	}
	return len(p), nil
}

func (s *startupTail) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.value)
	s.value = nil
	s.stopped = true
}

func (s *startupTail) category() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := strings.ToLower(platform.Redact(string(s.value)))
	for _, entry := range []struct{ match, category string }{
		{"error loading config", "configuration_load_failed"},
		{"permission denied", "filesystem_permission_denied"},
		{"database is locked", "native_database_locked"},
		{"no such file or directory", "required_path_unavailable"},
		{"unexpected argument", "unsupported_runtime_argument"},
		{"invalid value", "invalid_runtime_configuration"},
	} {
		if strings.Contains(value, entry.match) {
			return entry.category
		}
	}
	return "upstream_process_exit"
}

func (f *Facade) recordProcessExit(exitCode int, started time.Time, tail *startupTail) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.initializedResult) != 0 || f.lastTurnError != nil || (f.ctx.Err() != nil && exitCode <= 0) {
		return
	}
	f.lastTurnError = fmt.Errorf("CODEX_APP_SERVER_STARTUP_FAILED: phase=initialize exit_code=%d reason=%s elapsed_ms=%d", exitCode, tail.category(), time.Since(started).Milliseconds())
}
