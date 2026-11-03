package codexfrontend

import (
	"os"
	"sync"
)

// ownedFrontendSignals serializes raw process-group signals with retirement.
// On Linux the owner retires while WNOWAIT still reserves the leader PID,
// before Cmd.Wait can reap it. The context watcher may finish after that reap.
type ownedFrontendSignals struct {
	mu      sync.Mutex
	retired bool
}

func (s *ownedFrontendSignals) signal(send func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired {
		return os.ErrProcessDone
	}
	return send()
}

func (s *ownedFrontendSignals) retire() {
	s.mu.Lock()
	s.retired = true
	s.mu.Unlock()
}
