package components

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

// Set only by the official release build after the patched runtime archive is
// built and hashed. Development builds omit Graft until this identity is supplied.
// This is not a user config or environment override and never changes an existing object.
var managedGraftBuild string

type managedGraftMetadata struct {
	Version          string `json:"version"`
	Platform         string `json:"platform"`
	Revision         string `json:"revision"`
	SHA256           string `json:"sha256"`
	Archive          string `json:"archive"`
	URL              string `json:"url"`
	UpstreamRevision string `json:"upstream_revision"`
	SourceSHA256     string `json:"source_sha256"`
	PatchSHA256      string `json:"patch_sha256"`
}

func managedGraftSpec(encoded, platform string) (Spec, error) {
	invalid := errors.New("invalid managed Graft release metadata")
	if len(encoded) > 8192 {
		return Spec{}, invalid
	}
	body, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Spec{}, invalid
	}
	var metadata managedGraftMetadata
	if json.Unmarshal(body, &metadata) != nil || metadata.Version != "0.21.1-ivoai.1" || metadata.Platform != platform || (platform != "linux/amd64" && platform != "linux/arm64") {
		return Spec{}, invalid
	}
	for _, value := range []string{metadata.Revision, metadata.SHA256, metadata.SourceSHA256, metadata.PatchSHA256} {
		if decoded, err := hex.DecodeString(value); err != nil || len(decoded) != 32 || value != strings.ToLower(value) {
			return Spec{}, invalid
		}
	}
	if metadata.UpstreamRevision != "fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad" || metadata.SourceSHA256 != "ab27f9123eeb59b3987a342a24b7e357ad3412f41c556fde3a76ca9e250d2db1" {
		return Spec{}, invalid
	}
	// Bind the object to the compiled archive too: compiler/CPU target changes
	// must never collide with an existing immutable source/patch object.
	identity := sha256.Sum256([]byte(metadata.SourceSHA256 + "\n" + metadata.PatchSHA256 + "\n" + metadata.SHA256 + "\n"))
	if metadata.Revision != hex.EncodeToString(identity[:]) {
		return Spec{}, invalid
	}
	archive := "ivoai-graft_" + strings.ReplaceAll(platform, "/", "_") + ".tar.gz"
	parsed, err := url.Parse(metadata.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || metadata.Archive != archive {
		return Spec{}, invalid
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(parts) != 6 || strings.Join(parts[:4], "/") != "ivo-lopes/ivoai/releases/download" || parts[5] != archive || !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`).MatchString(parts[4]) {
		return Spec{}, invalid
	}
	result := Spec{Name: "graft", Executable: "graft-runtime", Strategy: StrategySupplyChain, License: "MIT", PayloadFormat: "tar_gzip", PayloadPath: "graft-runtime"}
	result.Version, result.Revision = metadata.Version, metadata.Revision
	// This is an IVOAI-published rebuild, not the upstream distribution.
	// Use the existing digest-only policy tier; do not invent a trust label
	// that the component promotion gate cannot validate.
	result.DefaultBranch, result.TrustLevel = "main", "checksum_only"
	result.SignatureStatus, result.AttestationStatus = "not_exposed", "not_exposed"
	result.Assets = map[string]Asset{platform: {URL: metadata.URL, SHA256: metadata.SHA256}}
	return result, nil
}

// ManagedGraftAvailable reports whether this build contains a reviewed artifact identity.
// A development build must diagnose this absence instead of installing upstream Graft.
func ManagedGraftAvailable() bool { return managedGraftBuild != "" }

// Graft is a patched runtime contract, never an ambient user CLI. Suppress only
// its external lookup while retaining the component manager's promotion/rollback.
type managedGraftRunner struct{ platform.Runner }

func (r managedGraftRunner) LookPath(name string) (string, error) {
	if name == "graft-runtime" {
		return "", errors.New("Graft requires its reviewed managed runtime")
	}
	return r.Runner.LookPath(name)
}
