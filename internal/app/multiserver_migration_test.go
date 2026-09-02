package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/migration"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func TestLegacyUpdaterCompatibilityProjectionOmitsUnknownSecretSchema(t *testing.T) {
	a := &App{Version: "0.7.3"}
	legacy := a.LegacyUpdateCompatibility()
	if _, ok := legacy.TargetSchemas[migration.ArtifactSecrets]; ok {
		t.Fatal("legacy updater metadata exposes the unknown secrets target schema")
	}
	if _, ok := legacy.SupportedSourceSchemas[migration.ArtifactSecrets]; ok {
		t.Fatal("legacy updater metadata exposes unsupported secrets migration sources")
	}
	v060Schemas := migration.Schemas{
		migration.ArtifactConfig:     config.ConfigSchemaVersion,
		migration.ArtifactState:      config.StateSchemaVersion,
		migration.ArtifactOwnership:  config.OwnershipSchemaVersion,
		migration.ArtifactComponents: 1,
		migration.ArtifactServer:     1,
	}
	manager := migration.Manager{Root: t.TempDir(), Registry: migration.Registry{}}
	tx, err := manager.Begin(context.Background(), "0.6.0", "0.7.3", v060Schemas, legacy.TargetSchemas)
	if err != nil {
		t.Fatalf("published v0.6 transaction rejected projected metadata: %v", err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	complete := a.UpdateCompatibility()
	if complete.TargetSchemas[migration.ArtifactSecrets] != secrets.SchemaVersion {
		t.Fatalf("complete metadata secrets schema=%d", complete.TargetSchemas[migration.ArtifactSecrets])
	}
	if got := complete.SupportedSourceSchemas[migration.ArtifactSecrets]; len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("complete metadata secret schemas=%v", got)
	}
}

func TestCurrentSecretStoreRemainsReadableByPublishedV060(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	store := secrets.Store{Path: path}
	want := secrets.ClientCredential{Token: "typed-placeholder", ClientID: "legacy", Scopes: []string{"context:read"}}
	if err := store.Save(secrets.Data{Server: &want}); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// This mirrors the published v0.6 decoder: unknown schema/servers fields
	// are ignored, while the rollback bridge remains available as server.
	var v060 struct {
		Server *secrets.ClientCredential `json:"server,omitempty"`
	}
	if err := json.Unmarshal(payload, &v060); err != nil {
		t.Fatal(err)
	}
	if v060.Server == nil || v060.Server.Token != want.Token || v060.Server.ClientID != want.ClientID {
		t.Fatalf("v0.6 rollback bridge=%#v", v060.Server)
	}
}

func TestLegacySecretMigrationIsReversibleAndPreservesToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	legacy := []byte(`{"server":{"token":"typed-placeholder","client_id":"legacy","scopes":["context:read"]}}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	registry := updateMigrationRegistry()
	plan, err := registry.Resolve(
		migration.Schemas{migration.ArtifactSecrets: 1},
		migration.Schemas{migration.ArtifactSecrets: secrets.SchemaVersion},
	)
	if err != nil {
		t.Fatal(err)
	}
	workspace := migration.Workspace{Files: map[migration.Artifact]string{migration.ArtifactSecrets: path}}
	if err := plan.Apply(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	migrated, err := (secrets.Store{Path: path}).Load()
	if err != nil || migrated.Schema != secrets.SchemaVersion || migrated.Servers[config.LegacyServerID].Token != "typed-placeholder" {
		t.Fatalf("migration=%#v err=%v", migrated, err)
	}
	if err := registry.Steps[0].Rollback(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := (secrets.Store{Path: path}).Load()
	if err != nil || rolledBack.Server == nil || rolledBack.Server.Token != "typed-placeholder" {
		t.Fatalf("rollback=%#v err=%v", rolledBack, err)
	}
}

func TestUnversionedV060SecretStoreMigratesToMultiServerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := updateMigrationRegistry()
	plan, err := registry.Resolve(
		migration.Schemas{migration.ArtifactSecrets: 0},
		migration.Schemas{migration.ArtifactSecrets: secrets.SchemaVersion},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].ID != "secrets-0-to-1" || plan.Steps[1].ID != "secrets-1-to-2" {
		t.Fatalf("unexpected migration plan: %#v", plan.Steps)
	}
	workspace := migration.Workspace{Files: map[migration.Artifact]string{migration.ArtifactSecrets: path}}
	if err := plan.Apply(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}
	migrated, err := (secrets.Store{Path: path}).Load()
	if err != nil || migrated.Schema != secrets.SchemaVersion || len(migrated.Servers) != 0 {
		t.Fatalf("migration=%#v err=%v", migrated, err)
	}
	supported, err := registry.SupportedSources(migration.Schemas{migration.ArtifactSecrets: secrets.SchemaVersion})
	if err != nil {
		t.Fatal(err)
	}
	if got := supported[migration.ArtifactSecrets]; len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("supported secret schemas=%v", got)
	}
}
