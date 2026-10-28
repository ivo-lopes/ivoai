package cli

import (
	"bytes"
	"context"
	"github.com/ivo-lopes/ivoai/internal/admin"
	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminAccessCLIOnlyIssuesPrivateLocalReadScope(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{ConfigDir: filepath.Join(root, "config"), StateDir: filepath.Join(root, "state")}
	for _, p := range []string{paths.ConfigDir, paths.StateDir} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	var out, errOut bytes.Buffer
	a := &app.App{Store: config.NewStore(paths), Out: &out, Err: &errOut, In: strings.NewReader("")}
	if err := Run(context.Background(), a, []string{"admin", "access", "create", "--ttl", "1m"}); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if !strings.Contains(body, "Activation code (shown once): ivoai-web_") || strings.Contains(errOut.String(), "ivoai-web_") {
		t.Fatal("intentional code channel incorrect")
	}
	store, err := admin.AccessStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := store.ListGrants()
	if err != nil || len(grants) != 1 || len(grants[0].Scopes) != 1 || grants[0].Scopes[0] != "admin:read" {
		t.Fatal("unexpected grant scope", err)
	}
	out.Reset()
	if err := Run(context.Background(), a, []string{"admin", "access", "list"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "ivoai-web_") {
		t.Fatal("list re-exposed activation")
	}
	if err := Run(context.Background(), a, []string{"admin", "access", "revoke", grants[0].ID}); err != nil {
		t.Fatal(err)
	}
	grants, _ = store.ListGrants()
	if len(grants) != 1 || grants[0].Status != "revoked" {
		t.Fatal("grant not revoked")
	}
	before := out.Len()
	if err := runAdmin(context.Background(), a, []string{"serve", "--listen", "0.0.0.0:0"}); err == nil || out.Len() != before {
		t.Fatal("remote bind accepted")
	}
	if err := runAdmin(context.Background(), a, []string{"access", "create", "--ttl", "25h"}); err == nil {
		t.Fatal("unbounded TTL accepted")
	}
}
