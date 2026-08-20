package components

import (
	"os"
	"strings"
	"testing"
)

func TestCompiledCatalogMatchesReviewedManifestValues(t *testing.T) {
	data, err := os.ReadFile("../../manifest/components.yaml")
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(data)
	for _, spec := range DefaultCatalog() {
		values := []string{spec.Name, spec.Version}
		if spec.Package != "" {
			values = append(values, spec.Package)
		}
		if spec.PackageURL != "" {
			values = append(values, spec.PackageURL)
		}
		if spec.Integrity != "" {
			values = append(values, spec.Integrity)
		}
		for _, asset := range spec.Assets {
			values = append(values, asset.URL, asset.SHA256)
		}
		for _, value := range values {
			if value != "" && !strings.Contains(manifest, value) {
				t.Errorf("compiled component %s value is absent from manifest: %s", spec.Name, value)
			}
		}
	}
}
