package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/app"
)

func TestMenuCanExitAndDoctorJSON(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("IVOAI_TEST_MODE", "1")
	var out bytes.Buffer
	a, err := app.New("v0.1.0", strings.NewReader("0\n"), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), a, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Launch Codex") {
		t.Fatal("menu missing")
	}
	out.Reset()
	if err := a.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run(context.Background(), a, []string{"doctor", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"overall": "READY"`) || !strings.Contains(out.String(), `"test_mode": true`) {
		t.Fatalf("doctor JSON: %s", out.String())
	}
	if !strings.Contains(out.String(), `"ruflo"`) {
		t.Fatalf("doctor JSON omits Ruflo: %s", out.String())
	}
}

func TestUserErrorRedactsSecrets(t *testing.T) {
	got := UserError(assertError("Authorization: Bearer secret-token"))
	if strings.Contains(got, "secret-token") {
		t.Fatal("secret leaked")
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
