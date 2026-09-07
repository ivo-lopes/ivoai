// Package codexresolver selects an already installed official client before a
// session starts. It never installs software, modifies authentication, or changes
// the component store. The returned real path is the session's executable lease.
package codexresolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

// MinimumSupported is the oldest client with the CLI/app-server/Code Mode
// contracts exercised by IVOAI's existing integration suite, not an update pin.
const MinimumSupported = "0.148.0"
const Policy = "LATEST_STABLE_COMPATIBLE"

type Candidate struct {
	Path          string `json:"path"`
	RealPath      string `json:"realpath"`
	Version       string `json:"version"`
	Ownership     string `json:"ownership"`
	Provenance    string `json:"provenance"`
	SHA256        string `json:"sha256,omitempty"`
	Healthy       bool   `json:"healthy"`
	Authenticated bool   `json:"authenticated"`
	Compatible    bool   `json:"compatible"`
	Reason        string `json:"reason,omitempty"`
}

type Resolution struct {
	Policy           string      `json:"policy"`
	MinimumSupported string      `json:"minimum_supported"`
	Direct           Candidate   `json:"direct_shell"`
	Managed          Candidate   `json:"managed"`
	Effective        Candidate   `json:"effective"`
	Candidates       []Candidate `json:"candidates"`
	LatestStable     string      `json:"latest_stable"`
	UpdateState      string      `json:"update_state"`
	VersionDrift     bool        `json:"version_drift"`
	Reason           string      `json:"reason"`
}

type Resolver struct {
	Runner platform.Runner
	// PATH is captured by the caller, never taken from a managed child environment.
	PATH    string
	Managed config.ComponentState
	Host    config.ComponentState
}

var stableVersion = regexp.MustCompile(`^(?:codex-cli |v)?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func Version(value string) (string, bool) {
	match := stableVersion.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return "", false
	}
	for _, part := range match[1:] {
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return "", false
		}
	}
	return strings.Join(match[1:], "."), true
}

func Compare(a, b string) int {
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	if len(ap) != 3 || len(bp) != 3 {
		return 0
	}
	for i := range ap {
		x, _ := strconv.ParseUint(ap[i], 10, 32)
		y, _ := strconv.ParseUint(bp[i], 10, 32)
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func (r Resolver) Resolve(ctx context.Context) (Resolution, error) {
	result := Resolution{Policy: Policy, MinimumSupported: MinimumSupported, LatestStable: "NOT_CHECKED", UpdateState: "not_checked"}
	if r.Runner == nil {
		r.Runner = platform.ExecRunner{}
	}
	// LookPath gives the shell's first candidate. Also consider other absolute
	// PATH entries, in order, without ever executing a cwd-relative candidate.
	paths := []string{}
	direct, _ := r.Runner.LookPath("codex")
	if filepath.IsAbs(direct) {
		paths = append(paths, direct)
	}
	for _, dir := range filepath.SplitList(r.PATH) {
		if !filepath.IsAbs(dir) {
			continue
		}
		path := filepath.Join(dir, "codex")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			paths = append(paths, path)
		}
	}
	if r.Managed.Path != "" {
		paths = append(paths, r.Managed.Path)
	}
	managedReal, _ := filepath.EvalSymlinks(r.Managed.Path)
	seen := map[string]Candidate{}
	for _, path := range paths {
		real, _ := filepath.EvalSymlinks(path)
		c, ok := seen[real]
		if !ok || real == "" {
			ownership := "user/system"
			if r.Managed.Managed && real != "" && real == managedReal {
				ownership = "managed"
			}
			c = r.probe(ctx, path, ownership)
			seen[real] = c
			result.Candidates = append(result.Candidates, c)
		}
		if path == direct {
			result.Direct = c
			result.Direct.Path = path
		}
		if path == r.Managed.Path && r.Managed.Managed {
			result.Managed = c
		}
		if !c.Healthy || !c.Authenticated || !c.Compatible {
			continue
		}
		best := result.Effective
		if best.Path == "" || Compare(c.Version, best.Version) > 0 || Compare(c.Version, best.Version) == 0 && best.Ownership == "managed" && c.Ownership != "managed" {
			result.Effective = c
		}
	}
	if result.Effective.Path == "" {
		result.Reason = "CODEX_NO_COMPATIBLE_AUTHENTICATED_CLIENT"
		return result, errors.New(result.Reason)
	}
	result.VersionDrift = result.Direct.Version != "" && result.Direct.Version != result.Effective.Version
	result.Reason = "newest_healthy_authenticated_stable_local_client"
	if result.VersionDrift {
		result.Reason = "CODEX_VERSION_DRIFT: shell candidate differs; see candidate health/compatibility and versions"
	}
	return result, nil
}

func (r Resolver) probe(ctx context.Context, path, ownership string) Candidate {
	c := Candidate{Path: path, Ownership: ownership, Provenance: "UNVERIFIED"}
	if !filepath.IsAbs(path) {
		c.Reason = "relative_path"
		return c
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		c.Reason = "executable_unavailable"
		return c
	}
	c.RealPath = real
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 || info.Mode().Perm()&0002 != 0 {
		c.Reason = "unsafe_executable"
		return c
	}
	options := platform.RunOptions{Timeout: 8 * time.Second}
	v, err := r.Runner.Run(ctx, real, []string{"--version"}, options)
	if err != nil {
		c.Reason = "version_probe_failed"
		return c
	}
	// The official executable identifies itself with codex-cli, not a generic
	// semver. This is a native-client probe, not a signature assertion.
	if !strings.HasPrefix(strings.TrimSpace(v.Stdout), "codex-cli ") {
		c.Reason = "official_client_identity_unconfirmed"
		return c
	}
	c.Version, c.Compatible = Version(v.Stdout)
	if !c.Compatible {
		c.Reason = "version_parse_failure_or_prerelease"
		return c
	}
	c.Compatible = Compare(c.Version, MinimumSupported) >= 0
	if !c.Compatible {
		c.Reason = "below_minimum_supported"
		return c
	}
	for _, args := range [][]string{{"exec", "--help"}, {"app-server", "--help"}} {
		out, err := r.Runner.Run(ctx, real, args, options)
		if err != nil || !strings.Contains(out.Stdout, "Usage:") {
			c.Reason = "required_capability_unavailable"
			return c
		}
	}
	if ownership == "managed" {
		host := filepath.Join(filepath.Dir(path), "codex-code-mode-host")
		info, err := os.Stat(host)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 || !r.Host.Installed || !r.Host.Managed || r.Host.Path != host || r.Host.Version != c.Version {
			c.Reason = "managed_companion_incompatible"
			return c
		}
		c.Provenance = "ivoai_managed_manifest/native_client_probe"
	} else {
		c.Provenance = "user_owned/native_client_probe (not signature verified)"
	}
	c.SHA256, err = Fingerprint(real)
	if err != nil {
		c.Reason = "executable_unreadable"
		return c
	}
	c.Healthy = true
	auth, authErr := r.Runner.Run(ctx, real, []string{"login", "status"}, options)
	c.Authenticated = connections.AuthenticationStatus(auth, authErr)
	if !c.Authenticated {
		c.Reason = "official_client_authentication_required"
	}
	return c
}

func Fingerprint(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Apply returns an ephemeral state snapshot; install ownership and rollback
// metadata must never be rewritten merely because a different binary is used.
func (r Resolution) Apply(state config.State) config.State {
	copy := make(map[string]config.ComponentState, len(state.Components))
	for k, v := range state.Components {
		copy[k] = v
	}
	state.Components = copy
	c := r.Effective
	state.Components["codex"] = config.ComponentState{Installed: c.Path != "", Managed: c.Ownership == "managed", Path: c.RealPath, Version: c.Version}
	return state
}

// CheckLatest is an explicit diagnostic network operation. Offline launches do
// not call it and no cached upstream value is represented as current evidence.
func (r *Resolution) CheckLatest(ctx context.Context, client *http.Client) {
	r.LatestStable = "UNKNOWN_OFFLINE"
	r.UpdateState = "upstream_unavailable_local_fallback"
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/openai/codex/releases/latest", nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	response, err := client.Do(req)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release) != nil || release.Draft || release.Prerelease || !strings.HasPrefix(release.Tag, "rust-v") {
		return
	}
	version, ok := Version(strings.TrimPrefix(release.Tag, "rust-"))
	if !ok {
		return
	}
	r.LatestStable = version
	r.UpdateState = "current"
	if Compare(r.Effective.Version, version) < 0 {
		r.UpdateState = "newer_stable_available; explicit_verified_update_required"
	}
}

func (r Resolution) Summary() string {
	return fmt.Sprintf("Codex resolution: policy=%s ownership=%s resolved_path=%s effective_version=%s latest_stable=%s minimum_supported=%s authenticated=%t compatibility=%t update_state=%s CODEX_VERSION_DRIFT=%t", r.Policy, r.Effective.Ownership, r.Effective.RealPath, r.Effective.Version, r.LatestStable, r.MinimumSupported, r.Effective.Authenticated, r.Effective.Compatible, r.UpdateState, r.VersionDrift)
}
