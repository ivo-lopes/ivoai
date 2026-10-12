package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func hookFixture(t *testing.T) (string, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, "codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	root := filepath.Join(home, "owned-hooks")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(home, "ai-memory")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 127\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return root, binary, filepath.Join(home, "codex", "hooks.json")
}
func hookPayload(command string) []byte {
	b, _ := json.Marshal(map[string]any{"hooks": map[string]any{"SessionStart": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}}}})
	return b
}
func TestHookConfigurationPreservesForeignAndRepairsOnlyReceipt(t *testing.T) {
	root, binary, path := hookFixture(t)
	foreign := hookPayload("ai-memory-foreign hook --event custom")
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, foreign, 0600)
	plans := map[string][]byte{path: hookPayload("'" + binary + "' hook --event session-start")}
	if err := applyHookConfiguration(root, binary, plans); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	if !bytes.Contains(original, []byte("ai-memory-foreign")) {
		t.Fatal("foreign hook lost")
	}
	if err := applyHookConfiguration(root, binary, plans); err != nil {
		t.Fatal(err)
	}
	repeated, _ := os.ReadFile(path)
	if !bytes.Equal(original, repeated) {
		t.Fatal("installation not idempotent")
	}
	var receipt hookReceipt
	raw, _ := os.ReadFile(filepath.Join(root, hookReceiptName))
	json.Unmarshal(raw, &receipt)
	if len(receipt.Files) != 1 {
		t.Fatalf("wrappers: %d", len(receipt.Files))
	}
	wrapper := filepath.Join(root, receipt.Files[0].Path)
	os.Remove(wrapper)
	if err := RepairHooks(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wrapper); err != nil {
		t.Fatal("missing wrapper not repaired")
	}
	os.WriteFile(wrapper, []byte("#!/bin/sh\necho foreign\n"), 0700)
	if err := RepairHooks(root); err == nil {
		t.Fatal("modified wrapper overwritten")
	}
	retained, _ := os.ReadFile(wrapper)
	if !bytes.Contains(retained, []byte("foreign")) {
		t.Fatal("foreign modification lost")
	}
}
func TestHookWrapperDegradesOnceAfterExit127(t *testing.T) {
	root, binary, path := hookFixture(t)
	marker := filepath.Join(root, "attempts")
	os.WriteFile(binary, []byte("#!/bin/sh\necho attempt >> '"+marker+"'\nexit 127\n"), 0700)
	if err := applyHookConfiguration(root, binary, map[string][]byte{path: hookPayload("'" + binary + "' hook --event session-start")}); err != nil {
		t.Fatal(err)
	}
	var receipt hookReceipt
	raw, _ := os.ReadFile(filepath.Join(root, hookReceiptName))
	json.Unmarshal(raw, &receipt)
	wrapper := filepath.Join(root, receipt.Files[0].Path)
	var output bytes.Buffer
	for i := 0; i < 2; i++ {
		cmd := exec.Command(wrapper)
		cmd.Env = append(os.Environ(), "IVOAI_HOOK_SESSION_DIR="+filepath.Join(root, "session"))
		cmd.Stderr = &output
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	attempts, _ := os.ReadFile(marker)
	if strings.Count(string(attempts), "attempt") != 1 {
		t.Fatalf("repeated failed hook: %q", attempts)
	}
	if strings.Count(output.String(), "disabled") != 1 {
		t.Fatalf("warning spam: %q", output.String())
	}
}
func TestHookRepairRejectsSymlinkAndUnknownOwnership(t *testing.T) {
	root, binary, path := hookFixture(t)
	if err := RepairHooks(root); err == nil {
		t.Fatal("unowned repair accepted")
	}
	if err := applyHookConfiguration(root, binary, map[string][]byte{path: hookPayload("'" + binary + "' hook --event session-start")}); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	os.Remove(path)
	target := filepath.Join(root, "foreign.json")
	os.WriteFile(target, original, 0600)
	os.Symlink(target, path)
	if err := RepairHooks(root); err == nil {
		t.Fatal("symlink repair accepted")
	}
	retained, _ := os.ReadFile(target)
	if !bytes.Equal(original, retained) {
		t.Fatal("symlink target changed")
	}
}

func TestInspectHookReceiptDetectsBinaryAndRegistrationDrift(t *testing.T) {
	root, binary, path := hookFixture(t)
	if err := applyHookConfiguration(root, binary, map[string][]byte{path: hookPayload("'" + binary + "' hook --event session-start")}); err != nil {
		t.Fatal(err)
	}
	if healthy, issues := InspectHooks(root); !healthy {
		t.Fatalf("healthy fixture: %v", issues)
	}
	os.WriteFile(path, []byte(`{"hooks":{}}`), 0600)
	if healthy, issues := InspectHooks(root); healthy || !strings.Contains(strings.Join(issues, " "), "registration") {
		t.Fatalf("stale registration missed: %v", issues)
	}
	if err := RepairHooks(root); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0700)
	if healthy, issues := InspectHooks(root); healthy || !strings.Contains(strings.Join(issues, " "), "binary") {
		t.Fatalf("binary drift missed: %v", issues)
	}
}
func TestInspectHooksRejectsSymlinkParentAndMissingInterpreter(t *testing.T) {
	root, binary, path := hookFixture(t)
	if err := applyHookConfiguration(root, binary, map[string][]byte{path: hookPayload("'" + binary + "' hook --event session-start")}); err != nil {
		t.Fatal(err)
	}
	var receipt hookReceipt
	raw, _ := os.ReadFile(filepath.Join(root, hookReceiptName))
	json.Unmarshal(raw, &receipt)
	wrapper := filepath.Join(root, receipt.Files[0].Path)
	os.WriteFile(wrapper, []byte("#!/usr/bin/env ivoai-missing-shell\n"), 0700)
	_, issues := InspectHooks(root)
	if !strings.Contains(strings.Join(issues, " "), "PATH") {
		t.Fatalf("missing interpreter missed: %v", issues)
	}
}
func TestRepairDoesNotDuplicateModifiedOwnedRegistration(t *testing.T) {
	root, binary, path := hookFixture(t)
	if err := applyHookConfiguration(root, binary, map[string][]byte{path: hookPayload("'" + binary + "' hook --event session-start")}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	body = bytes.Replace(body, []byte(`"type": "command"`), []byte(`"timeout": 99, "type": "command"`), 1)
	os.WriteFile(path, body, 0600)
	if err := RepairHooks(root); err == nil {
		t.Fatal("modified registration duplicated instead of preserved")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(body, after) {
		t.Fatal("modified registration changed")
	}
}

func TestReviewedMemoryHookRendererIsolated(t *testing.T) {
	binary := os.Getenv("IVOAI_REVIEWED_MEMORY_BINARY")
	source := os.Getenv("IVOAI_REVIEWED_MEMORY_HOOKS")
	if binary == "" || source == "" {
		t.Skip("requires explicitly supplied reviewed ai-memory binary and hook assets")
	}
	root, _, path := hookFixture(t)
	if err := os.CopyFS(root, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Runner: platform.ExecRunner{}, Binary: binary, HooksDir: root}
	if err := manager.ConfigureHooks(context.Background(), "", ""); err != nil {
		t.Fatal(err)
	}
	if healthy, issues := InspectHooks(root); !healthy {
		t.Fatalf("rendered hook health: %v", issues)
	}
	original, _ := os.ReadFile(path)
	if err := manager.ConfigureHooks(context.Background(), "", ""); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) {
		t.Fatal("reviewed rendering is not idempotent")
	}
}

func TestHookRepairRestoresModeWithoutChangingForeignSettings(t *testing.T) {
	root, binary, path := hookFixture(t)
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, []byte(`{"model":"foreign-model","hooks":{"Stop":[{"hooks":[{"command":"echo foreign"}]}]}}`), 0600)
	if err := applyHookConfiguration(root, binary, map[string][]byte{path: hookPayload("'" + binary + "' hook --event session-start")}); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	var receipt hookReceipt
	raw, _ := os.ReadFile(filepath.Join(root, hookReceiptName))
	json.Unmarshal(raw, &receipt)
	wrapper := filepath.Join(root, receipt.Files[0].Path)
	os.Chmod(wrapper, 0600)
	if ok, issues := InspectHooks(root); ok || !strings.Contains(strings.Join(issues, " "), "not executable") {
		t.Fatalf("mode damage not reported: %v", issues)
	}
	if err := RepairHooks(root); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) {
		t.Fatal("foreign settings changed during mode repair")
	}
	if ok, issues := InspectHooks(root); !ok {
		t.Fatalf("repair unhealthy: %v", issues)
	}
}

func TestHookTransactionRollsBackEarlierWrites(t *testing.T) {
	root, _, _ := hookFixture(t)
	first := filepath.Join(root, "a")
	second := filepath.Join(root, "b")
	if err := os.WriteFile(first, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	write := func(body []byte, path string, mode os.FileMode) error {
		writes++
		if writes == 2 {
			return errors.New("injected write failure")
		}
		return platform.AtomicWriteFile(body, path, mode)
	}
	err := commitHookFilesUsing(root, map[string]hookFile{first: {Body: []byte("changed"), Mode: 0600}, second: {Body: []byte("never"), Mode: 0600}}, write)
	if err == nil || writes != 3 {
		t.Fatalf("rollback not exercised: writes=%d err=%v", writes, err)
	}
	body, _ := os.ReadFile(first)
	if string(body) != "original" {
		t.Fatal("earlier write was not restored")
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatal("failed new file left behind")
	}
	backups, _ := filepath.Glob(filepath.Join(root, ".ivoai-hook-backup-*", "0"))
	if len(backups) != 1 {
		t.Fatal("original backup missing")
	}
	info, _ := os.Stat(backups[0])
	if info.Mode().Perm() != 0600 {
		t.Fatal("backup not private")
	}
}

func TestHookSessionDisablesReceiptOwnedWrappersOnPreflightFailure(t *testing.T) {
	root, binary, path := hookFixture(t)
	if err := applyHookConfiguration(root, binary, map[string][]byte{path: hookPayload("'" + binary + "' hook --event session-start")}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(binary, []byte("#!/bin/sh\necho unsafe\n"), 0700)
	env, cleanup, issues := NewHookSession(root)
	defer cleanup()
	if len(issues) == 0 {
		t.Fatal("integrity failure not reported")
	}
	var receipt hookReceipt
	raw, _ := os.ReadFile(filepath.Join(root, hookReceiptName))
	json.Unmarshal(raw, &receipt)
	cmd := exec.Command(filepath.Join(root, receipt.Files[0].Path))
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatalf("disabled hook executed: %s %v", out, err)
	}
}

func TestHookFailureDisablesOtherEventsInSameSession(t *testing.T) {
	root, binary, path := hookFixture(t)
	marker := filepath.Join(root, "attempts")
	os.WriteFile(binary, []byte("#!/bin/sh\necho attempt >> '"+marker+"'\nexit 127\n"), 0700)
	var payload map[string]any
	json.Unmarshal(hookPayload("'"+binary+"' hook --event session-start"), &payload)
	hooks := payload["hooks"].(map[string]any)
	hooks["Stop"] = hooks["SessionStart"]
	body, _ := json.Marshal(payload)
	if err := applyHookConfiguration(root, binary, map[string][]byte{path: body}); err != nil {
		t.Fatal(err)
	}
	var receipt hookReceipt
	raw, _ := os.ReadFile(filepath.Join(root, hookReceiptName))
	json.Unmarshal(raw, &receipt)
	var output bytes.Buffer
	for _, file := range receipt.Files {
		cmd := exec.Command(filepath.Join(root, file.Path))
		cmd.Env = append(os.Environ(), "IVOAI_HOOK_SESSION_DIR="+filepath.Join(root, "session"))
		cmd.Stderr = &output
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	attempts, _ := os.ReadFile(marker)
	if strings.Count(string(attempts), "attempt") != 1 {
		t.Fatalf("failed backend retried by another event: %q", attempts)
	}
}

func TestDisableOwnedHooksPreservesForeignRegistrations(t *testing.T) {
	root, binary, path := hookFixture(t)
	os.MkdirAll(filepath.Dir(path), 0700)
	os.WriteFile(path, hookPayload("ai-memory-foreign hook --event custom"), 0600)
	if err := applyHookConfiguration(root, binary, map[string][]byte{path: hookPayload("'" + binary + "' hook --event session-start")}); err != nil {
		t.Fatal(err)
	}
	m := Manager{Runner: &captureRunner{}, Binary: binary, HooksDir: root}
	if err := m.DisableHooks(context.Background()); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if bytes.Contains(body, []byte(".ivoai-managed")) {
		t.Fatal("owned hook registration remains after disable")
	}
	if !bytes.Contains(body, []byte("ai-memory-foreign")) {
		t.Fatal("foreign hook removed")
	}
}

func TestVerifiedHookAssetsDoNotAdoptForeignFiles(t *testing.T) {
	root, _, _ := hookFixture(t)
	source := t.TempDir()
	os.WriteFile(filepath.Join(source, "known.sh"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	foreign := filepath.Join(root, "foreign.sh")
	os.WriteFile(foreign, []byte("#!/bin/sh\necho foreign\n"), 0700)
	if err := InstallHookAssets(source, root); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(root, hookAssetReceiptName))
	if bytes.Contains(body, []byte("foreign.sh")) {
		t.Fatal("foreign asset adopted")
	}
	os.WriteFile(filepath.Join(source, "foreign.sh"), []byte("#!/bin/sh\necho new\n"), 0700)
	if err := InstallHookAssets(source, root); err == nil {
		t.Fatal("foreign asset replaced")
	}
	retained, _ := os.ReadFile(foreign)
	if !bytes.Contains(retained, []byte("echo foreign")) {
		t.Fatal("foreign content changed")
	}
}
