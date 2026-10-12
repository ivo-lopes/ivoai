package memory

import (
	"os"
	"path/filepath"
)

// NewHookSession supplies a private failure latch shared by the launched agent's
// lifecycle events. A failed preflight disables only receipt-owned wrappers.
func NewHookSession(root string) ([]string, func(), []string) {
	healthy, issues := InspectHooks(root)
	_, err := readHookReceipt(root)
	if err != nil {
		return nil, func() {}, issues
	}
	if err := safeHookPath(root); err != nil {
		return nil, func() {}, []string{"ai-memory hook session path is unsafe"}
	}
	dir, err := os.MkdirTemp(root, ".ivoai-session-")
	if err != nil {
		return nil, func() {}, []string{"ai-memory hook failure latch is unavailable"}
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if !healthy {
		if err := os.Mkdir(filepath.Join(dir, "disabled"), 0700); err != nil {
			cleanup()
			return nil, func() {}, []string{"ai-memory hook preflight could not disable unhealthy wrappers"}
		}
	}

	return []string{"IVOAI_HOOK_SESSION_DIR=" + dir}, cleanup, issues
}
