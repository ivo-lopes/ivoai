package app

import (
	"os"
	"os/exec"
	"testing"
)

// The actual CLI owns the session, router, scheduler, official clients and
// native OpenCode attach. This must not intercept Auto before frontend launch.
func TestLiveP0VoicehubThroughManagedOpenCode(t *testing.T) {
	if os.Getenv("IVOAI_LIVE_P0") != "1" {
		t.Skip("set IVOAI_LIVE_P0=1 with official Codex login and Memory/Context; Claude live is optional")
	}
	command := exec.Command("bash", "../../scripts/test-p0-voicehub.sh")
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
}
