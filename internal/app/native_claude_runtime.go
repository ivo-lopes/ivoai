package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ivo-lopes/ivoai/internal/config"
)

const claudeNativeFlag = "IVOAI_EXPERIMENTAL_CLAUDE_HEADLESS"
const claudeNativeSHA = "708c77b204db30086ff6f1ed34577bda03e1be74ced80557cbf7b54249ebf0bd"

type managedNativeRuntime struct {
	Binary, SHA256, Version string
	Info                    os.FileInfo
}

func nativeRuntimeDigest(executor string) string {
	if executor == "claude" {
		return claudeNativeSHA
	}
	return antigravitySHA
}

func reviewedManagedNative(ctx context.Context, executor string, state config.State) (managedNativeRuntime, error) {
	if executor == "antigravity" {
		path, info, err := reviewedAntigravity(ctx)
		return managedNativeRuntime{path, antigravitySHA, "1.5.2", info}, err
	}
	if executor != "claude" || runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return managedNativeRuntime{}, errors.New("NATIVE_MANAGED_PLATFORM_UNCERTIFIED")
	}
	c := state.Components["claude-code"]
	if !c.Managed || !c.Installed || c.Version != "2.1.318" || !filepath.IsAbs(c.Path) {
		return managedNativeRuntime{}, errors.New("CLAUDE_NATIVE_MANAGED_RUNTIME_UNAVAILABLE")
	}
	path, err := filepath.EvalSymlinks(c.Path)
	if err != nil {
		return managedNativeRuntime{}, err
	}
	before, err := adaptiveFileInfo(path)
	if err != nil {
		return managedNativeRuntime{}, err
	}
	sum, err := adaptiveBinarySHA(ctx, path)
	after, statErr := adaptiveFileInfo(path)
	if err != nil || statErr != nil || sum != claudeNativeSHA || !sameAdaptiveFile(before, after) {
		return managedNativeRuntime{}, errors.New("CLAUDE_NATIVE_RUNTIME_UNREVIEWED")
	}
	return managedNativeRuntime{path, claudeNativeSHA, "2.1.318", after}, nil
}
