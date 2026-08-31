package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func TestExtractKnowledgeSourcesIsExplicitAndPreservesAgentArgs(t *testing.T) {
	rest, sources, err := extractKnowledgeSources([]string{"--knowledge-source", "mindsite", "--knowledge-source=voicecorp", "--", "--model", "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sources, []string{"mindsite", "voicecorp"}) || !reflect.DeepEqual(rest, []string{"--", "--model", "fixture"}) {
		t.Fatalf("rest=%q sources=%q", rest, sources)
	}
}

func TestMultiServerCLIListShowAndSelectiveDisconnect(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	var output bytes.Buffer
	a, err := app.New("test", strings.NewReader(""), &output, &output)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	for _, alias := range []string{"voicecorp", "mindsite"} {
		cfg.Connections.Servers[alias] = config.ServerProfile{ID: "srv_" + alias, Alias: alias, URL: "https://" + alias + ".example.invalid", Status: "connected", Enabled: true, Purpose: alias, Protocol: 1, ContextMCPURL: "https://" + alias + ".example.invalid/context"}
		if err := (secrets.Store{Path: a.Store.Paths.Secrets}).Set("srv_"+alias, secrets.ClientCredential{Token: "never-print-" + alias}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), a, []string{"connect", "server", "list", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"alias":"mindsite"`) || !strings.Contains(output.String(), `"alias":"voicecorp"`) || strings.Contains(output.String(), "never-print") {
		t.Fatalf("server list JSON=%s", output.String())
	}
	output.Reset()
	if err := Run(context.Background(), a, []string{"connect", "server", "show", "mindsite", "--json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"credential_configured":true`) || strings.Contains(output.String(), "never-print") {
		t.Fatalf("server show JSON=%s", output.String())
	}
	if err := Run(context.Background(), a, []string{"disconnect", "server", "voicecorp"}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := a.Store.Load()
	if err != nil || len(reloaded.Connections.Servers) != 1 || reloaded.Connections.Servers["mindsite"].Alias != "mindsite" {
		t.Fatalf("selective disconnect config=%+v err=%v", reloaded.Connections.Servers, err)
	}
	if _, ok, err := (secrets.Store{Path: a.Store.Paths.Secrets}).Get("srv_mindsite"); err != nil || !ok {
		t.Fatalf("unrelated credential changed: ok=%v err=%v", ok, err)
	}
	if err := Run(context.Background(), a, []string{"disconnect", "server", "--all"}); err != nil {
		t.Fatal(err)
	}
	reloaded, _ = a.Store.Load()
	if len(reloaded.Connections.Servers) != 0 {
		t.Fatalf("disconnect all left profiles: %+v", reloaded.Connections.Servers)
	}
}

func TestMultiServerCLIRejectsHostileAliasBeforeNetwork(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	a, newErr := app.New("test", strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	if newErr != nil {
		t.Fatal(newErr)
	}
	err := connectServerProfile(context.Background(), a, "../mindsite", []string{"--url", "http://127.0.0.1:1", "--enrollment-code", "fixture"})
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("hostile alias error=%v", err)
	}
}
