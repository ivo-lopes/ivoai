package app

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/session"
)

func TestAdaptiveReviewedRuntimeVersionsRemainExact(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("production adaptive runtime platform is intentionally restricted")
	}
	for _, version := range []string{"0.162.1", "0.166.0"} {
		t.Run(version, func(t *testing.T) {
			sha, companion, ok := reviewedAdaptiveDigests(version, "linux/amd64")
			if !ok || len(sha) != 64 || len(companion) != 64 {
				t.Fatal("reviewed identity absent")
			}
			root := t.TempDir()
			state := config.State{Components: map[string]config.ComponentState{
				"codex":                {Path: filepath.Join(root, "codex"), Version: version, Installed: true, Managed: true},
				"codex-code-mode-host": {Path: filepath.Join(root, "codex-code-mode-host"), Version: version, Installed: true, Managed: true},
			}}
			v := session.Session{CodexPath: state.Components["codex"].Path, CodexVersion: version, CodexSHA256: sha, CodexManaged: true}
			pair, err := pinnedAdaptiveRuntime(state, v)
			if err != nil || pair.sha != sha || pair.companionSHA != companion {
				t.Fatal("reviewed runtime rejected", err)
			}
			for _, mismatch := range []string{"version", "digest", "unmanaged", "companion-version", "companion-path"} {
				t.Run(mismatch, func(t *testing.T) {
					candidate := v
					copy := config.State{Components: map[string]config.ComponentState{}}
					for k, component := range state.Components {
						copy.Components[k] = component
					}
					switch mismatch {
					case "version":
						candidate.CodexVersion = "0.167.0"
					case "digest":
						candidate.CodexSHA256 = companion
					case "unmanaged":
						candidate.CodexManaged = false
					case "companion-version":
						c := copy.Components["codex-code-mode-host"]
						c.Version = "0.167.0"
						copy.Components["codex-code-mode-host"] = c
					case "companion-path":
						c := copy.Components["codex-code-mode-host"]
						c.Path = filepath.Join(root, "other", "codex-code-mode-host")
						copy.Components["codex-code-mode-host"] = c
					}
					if _, err := pinnedAdaptiveRuntime(copy, candidate); err == nil {
						t.Fatal("mismatched runtime admitted")
					}
				})
			}
		})
	}
	for _, version := range []string{"0.166", "0.166.1", "0.167.0", "latest", ""} {
		if _, _, ok := reviewedAdaptiveDigests(version, "linux/amd64"); ok {
			t.Fatal("unreviewed version admitted")
		}
	}
	if _, _, ok := reviewedAdaptiveDigests("0.166.0", "linux/arm64"); ok {
		t.Fatal("ARM compilation is not adaptive runtime certification")
	}
}

func TestAdaptiveRuntimePairFingerprintStillRejectsAReviewedBannerWithFixtureBytes(t *testing.T) {
	f := newAdaptiveFixture(t)
	state := f.state
	c := state.Components["codex"]
	c.Managed, c.Version = true, "0.166.0"
	state.Components["codex"] = c
	state.Components["codex-code-mode-host"] = config.ComponentState{Path: filepath.Join(filepath.Dir(c.Path), "codex-code-mode-host"), Version: c.Version, Installed: true, Managed: true}
	sha, _, _ := reviewedAdaptiveDigests(c.Version, "linux/amd64")
	v := session.Session{CodexPath: c.Path, CodexVersion: c.Version, CodexManaged: true, CodexSHA256: sha}
	pair, err := pinnedAdaptiveRuntime(state, v)
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fingerprintAdaptivePair(context.Background(), pair); err == nil {
		t.Fatal("version metadata replaced executable verification")
	}
}
