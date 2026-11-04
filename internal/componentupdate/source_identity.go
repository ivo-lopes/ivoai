package componentupdate

import (
	"reflect"
	"regexp"

	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

var graftReleaseAsset = regexp.MustCompile(`^https://github\.com/ivo-lopes/ivoai/releases/download/v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?/ivoai-graft_linux_(amd64|arm64)\.tar\.gz$`)

// SameResolvedSource preserves exact provenance matching, allowing only an
// identical managed Graft archive republished under another official IVOAI tag.
// Callers must still validate the stored object's manifest and health. This does
// not authorize fetching an arbitrary mirror or changing immutable provenance:
// the original active source, digest, revision and object remain authoritative.
func SameResolvedSource(active, expected supplychain.ResolvedSource) bool {
	if active.Validate() != nil || expected.Validate() != nil {
		return false
	}
	if reflect.DeepEqual(active, expected) {
		return true
	}
	if active.ID != "graft" || active.Kind != supplychain.KindComponent || active.PayloadFormat != "tar_gzip" || active.PayloadPath != "graft-runtime" {
		return false
	}
	a, b := graftReleaseAsset.FindStringSubmatch(active.Source), graftReleaseAsset.FindStringSubmatch(expected.Source)
	if a == nil || b == nil || a[len(a)-1] != b[len(b)-1] {
		return false
	}
	// Normalize only the transport address in a copy. Any change to identity,
	// integrity, executable paths, version, license or trust remains a denial.
	active.Source = expected.Source
	return reflect.DeepEqual(active, expected)
}
