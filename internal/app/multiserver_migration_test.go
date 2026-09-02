package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/migration"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

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
