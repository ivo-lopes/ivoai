package components

// ManagedBootstrapAssets exposes the same pinned toolchain inputs used by the
// installer, for offline upgrade qualification. It does not install anything.
func ManagedBootstrapAssets() []Asset { return []Asset{managedNodeAsset(), managedUVAsset()} }

// ManagedBootstrapRequirements exposes the existing reviewed Python/cutoff
// policy so isolated cache preparation cannot silently use a different graph.
func ManagedBootstrapRequirements() (python, cutoff string) {
	return managedPythonVersion, dependencyCutoff
}
