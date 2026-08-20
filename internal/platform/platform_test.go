package platform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAtomicWritePrivatePermissionsAndReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "secret")
	if err := AtomicWritePrivate([]byte("first"), path); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWritePrivate([]byte("second"), path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "second" {
		t.Fatalf("got %q", b)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	info, _ = os.Stat(filepath.Dir(path))
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", info.Mode().Perm())
	}
}

func TestAtomicWriteRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWritePrivate([]byte("bad"), link); err == nil {
		t.Fatal("expected symlink refusal")
	}
	b, _ := os.ReadFile(target)
	if string(b) != "safe" {
		t.Fatal("target changed")
	}
}

func TestRedact(t *testing.T) {
	input := "Authorization: Bearer abcdef\nCookie: session=browser-secret; other=value\napi_key=secret enrollment_code=one ivo_0123456789012345 ivoai-client_0123456789abcdef_secretvalue ivoai-enroll_0123456789abcdef_codevalue"
	got := Redact(input)
	for _, secret := range []string{"abcdef", "browser-secret", "secret", "one", "ivo_0123456789012345", "ivoai-client_0123456789abcdef_secretvalue", "ivoai-enroll_0123456789abcdef_codevalue"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %q leaked in %q", secret, got)
		}
	}
}

func TestDebugLogIsStructuredAndRedacted(t *testing.T) {
	t.Setenv("IVOAI_LOG_LEVEL", "debug")
	var output strings.Builder
	DebugLog(&output, "operation", map[string]string{"error": "Authorization: Bearer private-token"})
	if !strings.Contains(output.String(), `"level":"debug"`) || strings.Contains(output.String(), "private-token") {
		t.Fatalf("unsafe debug record: %s", output.String())
	}
}

func TestExecRunnerUsesArgvAndTimeout(t *testing.T) {
	r := ExecRunner{}
	result, err := r.Run(context.Background(), "/bin/printf", []string{"%s", "$(touch /tmp/ivoai-must-not-exist)"}, RunOptions{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "$(touch /tmp/ivoai-must-not-exist)" {
		t.Fatalf("unexpected output %q", result.Stdout)
	}
	if _, err := os.Stat("/tmp/ivoai-must-not-exist"); !os.IsNotExist(err) {
		t.Fatal("argument was shell-expanded")
	}
	if _, err := r.Run(context.Background(), "/bin/sleep", []string{"1"}, RunOptions{Timeout: time.Millisecond}); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestExecRunnerCleanEnvironmentDoesNotInheritSecrets(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-leak")
	result, err := (ExecRunner{}).Run(context.Background(), "/usr/bin/env", nil, RunOptions{
		Env:      []string{"PATH=/usr/bin:/bin", "LANG=C"},
		CleanEnv: true,
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Stdout, "OPENAI_API_KEY") || strings.Contains(result.Stdout, "must-not-leak") {
		t.Fatal("clean subprocess environment inherited a provider credential")
	}
}
