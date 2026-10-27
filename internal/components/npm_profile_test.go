package components

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type rufloProfileRunner struct {
	t            *testing.T
	node, root   string
	version      string
	npmCalls     atomic.Int32
	versionCalls atomic.Int32
	npmFailure   bool
	wrongVersion bool
	afterNPM     func(string) error
	started      chan struct{}
	resume       chan struct{}
}

func (r *rufloProfileRunner) LookPath(string) (string, error) { return "", os.ErrNotExist }

func (r *rufloProfileRunner) Run(ctx context.Context, command string, args []string, options platform.RunOptions) (platform.Result, error) {
	r.t.Helper()
	if command != r.node || !options.CleanEnv || options.Timeout <= 0 {
		r.t.Fatal("runtime did not use the pinned Node, clean environment and deadline")
	}
	for _, field := range options.Env {
		if !strings.HasPrefix(field, "HOME=") && !strings.HasPrefix(field, "PATH=") && !strings.HasPrefix(field, "NPM_CONFIG_CACHE=") && field != "RUFLO_PROVIDER=ivoai-disabled" && field != "CLAUDE_FLOW_MEMORY_BACKEND=memory" {
			r.t.Fatal("unexpected inherited installer environment")
		}
	}
	if len(args) == 1 && args[0] == "--version" {
		return platform.Result{Stdout: "v" + managedNodeVersion + "\n"}, nil
	}
	if len(args) == 2 && args[1] == "--version" {
		r.versionCalls.Add(1)
		if filepath.Dir(options.Dir) != r.root || !strings.HasPrefix(args[0], options.Dir+string(filepath.Separator)) {
			r.t.Fatal("version preflight escaped the private profile")
		}
		return (platform.ExecRunner{}).Run(ctx, args[0], args[1:], options)
	}
	if len(args) != 7 || args[1] != "ci" || args[2] != "--prefix" || args[3] != options.Dir || args[4] != "--ignore-scripts" || args[5] != "--no-audit" || args[6] != "--no-fund" {
		r.t.Fatalf("unsafe or unexpected npm invocation: %v", args)
	}
	if filepath.Dir(options.Dir) != r.root || !strings.HasPrefix(filepath.Base(options.Dir), ".ruflo-stage-") {
		r.t.Fatal("npm ran against an active/legacy prefix rather than a private stage")
	}
	if info, err := os.Stat(options.Dir); err != nil || info.Mode().Perm() != 0o700 {
		r.t.Fatal("npm stage is not private")
	}
	call := r.npmCalls.Add(1)
	if r.started != nil {
		close(r.started)
		<-r.resume
	}
	if r.npmFailure {
		return platform.Result{}, errors.New("synthetic npm failure")
	}
	launcher := filepath.Join(options.Dir, "node_modules", "ruflo", "bin", "ruflo.js")
	if err := os.MkdirAll(filepath.Dir(launcher), 0o700); err != nil {
		return platform.Result{}, err
	}
	version := r.version
	if r.wrongVersion {
		version = "0.0.0"
	}
	body := fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = '--version' ]; then printf 'ruflo %%s\\n' '%s'; else printf 'profile-%%s\\n' '%d'; fi\n", version, call)
	// Exercise the group-writable mode observed in the real npm graph. Only a
	// newly staged, contained leaf may be normalized before admission.
	if err := os.WriteFile(launcher, []byte(body), 0o775); err != nil {
		return platform.Result{}, err
	}
	if err := os.Chmod(launcher, 0o775); err != nil {
		return platform.Result{}, err
	}
	bin := filepath.Join(options.Dir, "node_modules", ".bin", "ruflo")
	if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
		return platform.Result{}, err
	}
	if err := os.Symlink("../ruflo/bin/ruflo.js", bin); err != nil {
		return platform.Result{}, err
	}
	if r.afterNPM != nil {
		if err := r.afterNPM(options.Dir); err != nil {
			return platform.Result{}, err
		}
	}
	return platform.Result{}, nil
}

func newRufloProfileFixture(t *testing.T) (managedInstallerFixture, Spec, *rufloProfileRunner) {
	t.Helper()
	t.Setenv("IVOAI_TEST_MODE", "")
	t.Setenv("OPENAI_API_KEY", "must-not-reach-npm-or-version")
	fixture := newRufloRuntimeFixture(t)
	var spec Spec
	for _, entry := range DefaultCatalog() {
		if entry.Name == "ruflo" {
			spec = entry
		}
	}
	if spec.Name == "" {
		t.Fatal("missing managed Ruflo pin")
	}
	runner := &rufloProfileRunner{t: t, node: ManagedNodePath(fixture.store.Paths.DataDir), root: filepath.Join(fixture.store.Paths.DataDir, "components"), version: spec.Version}
	fixture.installer.Runner = runner
	return fixture, spec, runner
}

func changeRufloProfileLock(t *testing.T) {
	t.Helper()
	previous := rufloPackageLock
	t.Cleanup(func() { rufloPackageLock = previous })
	// An embedded lock replacement must be detected independently of the same
	// top-level version; preserve its valid JSON and all reviewed integrity pins.
	rufloPackageLock = append(append([]byte(nil), previous...), '\n')
}

func runRufloWrapper(t *testing.T, wrapper, expected string) {
	t.Helper()
	result, err := (platform.ExecRunner{}).Run(context.Background(), wrapper, nil, platform.RunOptions{CleanEnv: true, Env: []string{"PATH=/usr/bin:/bin"}})
	if err != nil || strings.TrimSpace(result.Stdout) != expected {
		t.Fatalf("wrapper output=%q err=%v", result.Stdout, err)
	}
}

func TestRufloSameVersionLockUpgradeKeepsPriorProfileAndRollback(t *testing.T) {
	fixture, spec, runner := newRufloProfileFixture(t)
	first, err := fixture.installer.ensure(context.Background(), spec, config.ComponentState{})
	if err != nil {
		t.Fatal(err)
	}
	oldPrefix := fixture.installer.rufloProfilePath()
	oldWrapper, _ := os.ReadFile(first.Path)
	oldLock, _ := os.ReadFile(filepath.Join(oldPrefix, "package-lock.json"))
	oldLauncher, err := rufloProfileLauncher(oldPrefix, spec)
	if err != nil {
		t.Fatal(err)
	}
	oldPayload, _ := os.ReadFile(oldLauncher)
	if info, err := os.Stat(oldLauncher); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("new staged launcher permissions were not normalized privately")
	}
	runRufloWrapper(t, first.Path, "profile-1")
	unchanged, err := fixture.installer.ensure(context.Background(), spec, first)
	if err != nil || unchanged != first || runner.npmCalls.Load() != 1 || runner.versionCalls.Load() != 1 {
		t.Fatal("valid current graph was not an idempotent no-op")
	}
	changeRufloProfileLock(t)
	newPrefix := fixture.installer.rufloProfilePath()
	if oldPrefix == newPrefix {
		t.Fatal("lock replacement did not change the runtime profile")
	}
	second, err := fixture.installer.ensure(context.Background(), spec, first)
	if err != nil || second.Version != first.Version || second.Path != first.Path || runner.npmCalls.Load() != 2 || runner.versionCalls.Load() != 2 {
		t.Fatalf("same-version graph was not upgraded: %v", err)
	}
	newWrapper, _ := os.ReadFile(second.Path)
	if bytes.Equal(oldWrapper, newWrapper) || !bytes.Contains(newWrapper, []byte(newPrefix)) || bytes.Contains(newWrapper, []byte(".ruflo-stage-")) {
		t.Fatal("wrapper did not atomically select the promoted immutable profile")
	}
	for name, expected := range map[string][]byte{filepath.Join(oldPrefix, "package-lock.json"): oldLock, oldLauncher: oldPayload} {
		actual, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatal("upgrade changed a previous runtime profile")
		}
	}
	runRufloWrapper(t, second.Path, "profile-2")
	// Existing update rollback restores wrapper/state bytes, so its retained
	// target must still execute without reinstalling the old dependency graph.
	if err := platform.AtomicWriteFile(oldWrapper, first.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	runRufloWrapper(t, first.Path, "profile-1")
}

func TestRufloLegacyFlatProfileMigratesWithoutInPlaceNPM(t *testing.T) {
	fixture, spec, runner := newRufloProfileFixture(t)
	legacy := filepath.Join(runner.root, "ruflo", "node_modules", ".bin", "ruflo")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("#!/bin/sh\nprintf 'legacy-flat\\n'\n")
	if err := os.WriteFile(legacy, payload, 0o700); err != nil {
		t.Fatal(err)
	}
	wrapper, err := fixture.installer.installWrapper(spec.Executable, legacy, filepath.Dir(runner.node))
	if err != nil {
		t.Fatal(err)
	}
	previous := config.ComponentState{Installed: true, Managed: true, Version: spec.Version, Path: wrapper}
	if _, err := fixture.installer.ensure(context.Background(), spec, previous); err != nil {
		t.Fatal(err)
	}
	if runner.npmCalls.Load() != 1 {
		t.Fatal("same top-level version incorrectly bypassed graph migration")
	}
	actual, err := os.ReadFile(legacy)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("legacy flat launcher was modified")
	}
}

func TestRufloFailedCandidateNeverReplacesExistingWrapper(t *testing.T) {
	for _, failure := range []string{"npm", "version", "manifest", "launcher escape"} {
		t.Run(failure, func(t *testing.T) {
			fixture, spec, runner := newRufloProfileFixture(t)
			previous, err := fixture.installer.ensure(context.Background(), spec, config.ComponentState{})
			if err != nil {
				t.Fatal(err)
			}
			oldWrapper, _ := os.ReadFile(previous.Path)
			changeRufloProfileLock(t)
			outside := filepath.Join(fixture.root, "outside-launcher")
			if err := os.WriteFile(outside, []byte("outside sentinel"), 0o700); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "npm":
				runner.npmFailure = true
			case "version":
				runner.wrongVersion = true
			case "manifest":
				runner.afterNPM = func(prefix string) error {
					return os.WriteFile(filepath.Join(prefix, "package-lock.json"), []byte("tampered"), 0o600)
				}
			case "launcher escape":
				runner.afterNPM = func(prefix string) error {
					launcher := filepath.Join(prefix, "node_modules", ".bin", "ruflo")
					if err := os.Remove(launcher); err != nil {
						return err
					}
					return os.Symlink(outside, launcher)
				}
			}
			if _, err := fixture.installer.ensure(context.Background(), spec, previous); err == nil {
				t.Fatal("unsafe candidate was accepted")
			}
			actual, _ := os.ReadFile(previous.Path)
			if !bytes.Equal(actual, oldWrapper) {
				t.Fatal("failed install replaced or removed the prior wrapper")
			}
			if _, err := os.Lstat(fixture.installer.rufloProfilePath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed staged runtime was promoted")
			}
			actual, _ = os.ReadFile(outside)
			if string(actual) != "outside sentinel" {
				t.Fatal("profile escape modified external bytes")
			}
			runRufloWrapper(t, previous.Path, "profile-1")
			entries, _ := filepath.Glob(filepath.Join(runner.root, ".ruflo-stage-*"))
			if len(entries) != 0 {
				t.Fatal("failed staging directory leaked")
			}
		})
	}
}

func TestRufloProfileAndWrapperMustMatchBeforeFastSkip(t *testing.T) {
	fixture, spec, runner := newRufloProfileFixture(t)
	previous, err := fixture.installer.ensure(context.Background(), spec, config.ComponentState{})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(previous.Path)
	if err := platform.AtomicWriteFile(append(body, []byte("# changed\n")...), previous.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.installer.ensure(context.Background(), spec, previous); err != nil || runner.npmCalls.Load() != 1 || runner.versionCalls.Load() != 2 {
		t.Fatal("noncanonical wrapper was skipped or runtime was overwritten")
	}
	if err := os.WriteFile(filepath.Join(fixture.installer.rufloProfilePath(), "package-lock.json"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.installer.ensure(context.Background(), spec, previous); err == nil || runner.npmCalls.Load() != 1 {
		t.Fatal("corrupt immutable profile was skipped or reinstalled in place")
	}
}

func TestRufloAdmissionRejectsSymlinksWithoutOutsideMutation(t *testing.T) {
	for _, kind := range []string{"profile", "lock", "bin"} {
		t.Run(kind, func(t *testing.T) {
			fixture, spec, runner := newRufloProfileFixture(t)
			outside := filepath.Join(fixture.root, "outside")
			if err := os.MkdirAll(outside, 0o700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(outside, "sentinel")
			if err := os.WriteFile(sentinel, []byte("unchanged"), 0o600); err != nil {
				t.Fatal(err)
			}
			target, link := outside, fixture.installer.rufloProfilePath()
			if kind == "lock" {
				target, link = sentinel, filepath.Join(runner.root, "ruflo-install.lock")
			} else if kind == "bin" {
				link = fixture.store.Paths.BinDir
				if err := os.Remove(link); err != nil && !errors.Is(err, os.ErrNotExist) {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.installer.installNPMTool(context.Background(), spec); err == nil {
				t.Fatal("unsafe runtime path was accepted")
			}
			body, _ := os.ReadFile(sentinel)
			if string(body) != "unchanged" {
				t.Fatal("symlink modified outside data")
			}
		})
	}
}

func TestRufloPromotedProfilePermissionsAreNeverRepairedInPlace(t *testing.T) {
	fixture, spec, runner := newRufloProfileFixture(t)
	previous, err := fixture.installer.ensure(context.Background(), spec, config.ComponentState{})
	if err != nil {
		t.Fatal(err)
	}
	launcher, err := rufloProfileLauncher(fixture.installer.rufloProfilePath(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(launcher, 0o775); err != nil {
		t.Fatal(err)
	}
	wrapper, _ := os.ReadFile(previous.Path)
	if _, err := fixture.installer.ensure(context.Background(), spec, previous); err == nil || runner.npmCalls.Load() != 1 {
		t.Fatal("unsafe immutable launcher was accepted or reinstalled")
	}
	info, _ := os.Stat(launcher)
	after, _ := os.ReadFile(previous.Path)
	if info.Mode().Perm() != 0o775 || !bytes.Equal(wrapper, after) {
		t.Fatal("immutable launcher permissions or old wrapper were changed")
	}
}

func TestRufloConcurrentInstallAdmitsOnlyOneBuilder(t *testing.T) {
	fixture, spec, runner := newRufloProfileFixture(t)
	runner.started, runner.resume = make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := fixture.installer.installNPMTool(context.Background(), spec); done <- err }()
	<-runner.started
	_, secondErr := fixture.installer.installNPMTool(context.Background(), spec)
	close(runner.resume)
	firstErr := <-done
	if firstErr != nil || secondErr == nil || !strings.Contains(secondErr.Error(), "already active") || runner.npmCalls.Load() != 1 {
		t.Fatalf("concurrent builders not bounded: first=%v second=%v", firstErr, secondErr)
	}
}
