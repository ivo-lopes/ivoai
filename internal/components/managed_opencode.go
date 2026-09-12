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
)

// Set only by the official release build after the patched frontend archive is
// built and hashed. Development builds retain the upstream catalog. This is not
// a user config or environment override and never changes an existing object.
var managedOpenCodeBuild string

type managedOpenCodeMetadata struct {
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

func managedOpenCodeSpec(original Spec, encoded, platform string) (Spec, error) {
	invalid := errors.New("invalid managed OpenCode release metadata")
	if len(encoded) > 8192 {
		return Spec{}, invalid
	}
	body, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Spec{}, invalid
	}
	var metadata managedOpenCodeMetadata
	if json.Unmarshal(body, &metadata) != nil || metadata.Version != "1.18.25-ivoai.1" || metadata.Platform != platform || (platform != "linux/amd64" && platform != "linux/arm64") {
		return Spec{}, invalid
	}
	for _, value := range []string{metadata.Revision, metadata.SHA256, metadata.SourceSHA256, metadata.PatchSHA256} {
		if decoded, err := hex.DecodeString(value); err != nil || len(decoded) != 32 || value != strings.ToLower(value) {
			return Spec{}, invalid
		}
	}
	if metadata.UpstreamRevision != "cb7d8b2f5e44876ef98b661dc10590c915af3a9f" {
		return Spec{}, invalid
	}
	identity := sha256.Sum256([]byte(metadata.SourceSHA256 + "\n" + metadata.PatchSHA256 + "\n"))
	if metadata.Revision != hex.EncodeToString(identity[:]) {
		return Spec{}, invalid
	}
	archive := "ivoai-opencode_" + strings.ReplaceAll(platform, "/", "_") + ".tar.gz"
	parsed, err := url.Parse(metadata.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || metadata.Archive != archive {
		return Spec{}, invalid
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(parts) != 6 || strings.Join(parts[:4], "/") != "ivo-lopes/ivoai/releases/download" || parts[5] != archive || !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`).MatchString(parts[4]) {
		return Spec{}, invalid
	}
	result := original
	result.Version, result.Revision = metadata.Version, metadata.Revision
	// This is an IVOAI-published rebuild, not the upstream signed binary.
	// Use the existing digest-only policy tier; do not invent a trust label
	// that the component promotion gate cannot validate.
	result.DefaultBranch, result.TrustLevel = "main", "checksum_only"
	result.SignatureStatus, result.AttestationStatus = "not_exposed", "not_exposed"
	result.Assets = map[string]Asset{platform: {URL: metadata.URL, SHA256: metadata.SHA256}}
	return result, nil
}
