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

// Official release builds set this after building, scanning and hashing the
// reviewed local rebuild. It is never supplied by user configuration or env.
var managedCavemanBuild string

const managedCavemanVersion = "4.0.2-ivoai.1"
const managedCavemanUpstream = "b4531dfc05a3ef0ba93b57e8bfa5f48564a4b4d6"
const managedCavemanSource = "29ef4c5bccee70dbc486bae266f966b8bd53132c6e12e27e9b06b36cf35c51f4"
const managedCavemanPatch = "5f95e80d41d5744f16e09d534b1d56585fe746f9d5aed1e14c2586bbe953810d"

// The archive identity contract is shared with the existing Graft rebuild.
// One immutable archive contains both Caveman programs and their notices.
func managedCavemanSpecs(encoded, platform string) ([]Spec, error) {
	invalid := errors.New("invalid managed Caveman release metadata")
	if len(encoded) > 8192 {
		return nil, invalid
	}
	body, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, invalid
	}
	var metadata managedGraftMetadata
	if json.Unmarshal(body, &metadata) != nil || metadata.Version != managedCavemanVersion || metadata.Platform != platform || (platform != "linux/amd64" && platform != "linux/arm64") {
		return nil, invalid
	}
	if metadata.UpstreamRevision != managedCavemanUpstream || metadata.SourceSHA256 != managedCavemanSource || metadata.PatchSHA256 != managedCavemanPatch {
		return nil, invalid
	}
	for _, value := range []string{metadata.Revision, metadata.SHA256} {
		if decoded, err := hex.DecodeString(value); err != nil || len(decoded) != 32 || value != strings.ToLower(value) {
			return nil, invalid
		}
	}
	identity := sha256.Sum256([]byte(metadata.SourceSHA256 + "\n" + metadata.PatchSHA256 + "\n" + metadata.SHA256 + "\n"))
	if metadata.Revision != hex.EncodeToString(identity[:]) {
		return nil, invalid
	}
	archive := "ivoai-caveman_" + strings.ReplaceAll(platform, "/", "_") + ".tar.gz"
	parsed, err := url.Parse(metadata.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || metadata.Archive != archive {
		return nil, invalid
	}
	parts := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	if len(parts) != 6 || strings.Join(parts[:4], "/") != "ivo-lopes/ivoai/releases/download" || parts[5] != archive || !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`).MatchString(parts[4]) {
		return nil, invalid
	}
	var result []Spec
	for _, name := range []string{"caveman", "caveman-mcp"} {
		executable := "caveman-proxy"
		if name == "caveman-mcp" {
			executable = name
		}
		spec := Spec{Name: name, Executable: executable, Version: metadata.Version, Revision: metadata.Revision, Strategy: StrategySupplyChain, License: "Apache-2.0", PayloadFormat: "tar_gzip", PayloadPath: executable, Executables: []string{"caveman-mcp", "caveman-proxy"}, NoVersionProbe: true, DefaultBranch: "main", TrustLevel: "checksum_only", SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", Assets: map[string]Asset{platform: {URL: metadata.URL, SHA256: metadata.SHA256}}}
		if name == "caveman-mcp" {
			spec.RequiresManaged = "caveman"
		}
		result = append(result, spec)
	}
	return result, nil
}

// ManagedCavemanAvailable reports whether this release has a reviewed local
// rebuild identity. Missing metadata never selects a vulnerable upstream binary.
func ManagedCavemanAvailable() bool { return managedCavemanBuild != "" }
