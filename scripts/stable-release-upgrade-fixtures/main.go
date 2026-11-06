// Command stable-release-upgrade-fixtures emits the reviewed native catalog and
// initializes only disposable IVOAI configuration. It never installs a client.
package main

import (
	"encoding/json"
	"os"
	"runtime"

	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func main() {
	if len(os.Args) != 2 {
		panic("use catalog or initialize")
	}
	if os.Args[1] == "bootstrap" {
		python, cutoff := components.ManagedBootstrapRequirements()
		if err := json.NewEncoder(os.Stdout).Encode(struct {
			Assets []components.Asset `json:"assets"`
			Python string             `json:"python_version"`
			Cutoff string             `json:"exclude_newer"`
		}{components.ManagedBootstrapAssets(), python, cutoff}); err != nil {
			panic(err)
		}
		return
	}
	if os.Args[1] == "catalog" {
		catalog := []components.Asset{}
		for _, spec := range components.ClientSetupCatalog(false) {
			if asset, ok := spec.Assets[runtime.GOOS+"/"+runtime.GOARCH]; ok {
				catalog = append(catalog, asset)
			}
		}
		if err := json.NewEncoder(os.Stdout).Encode(catalog); err != nil {
			panic(err)
		}
		return
	}
	if os.Args[1] != "initialize" {
		panic("unsupported fixture operation")
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		panic(err)
	}
	store := config.NewStore(paths)
	if err := store.Ensure(); err != nil {
		panic(err)
	}
	cfg := config.Default()
	cfg.Compression.Provider, cfg.Compression.Source = "direct", config.CompressionSourceExplicit
	cfg.Memory.Enabled, cfg.Headroom.Enabled, cfg.Orchestration.Enabled = false, false, false
	cfg.Skills.Ponytail = "off"
	if err := store.Save(cfg); err != nil {
		panic(err)
	}
	data := secrets.Data{Schema: secrets.SchemaVersion, Servers: map[string]secrets.ClientCredential{"upgrade_fixture": {Token: "SYNTHETIC-UPGRADE-MARKER", ClientID: "upgrade-fixture", Scopes: []string{"context:read"}}}}
	if err := (secrets.Store{Path: paths.Secrets}).Save(data); err != nil {
		panic(err)
	}
}
