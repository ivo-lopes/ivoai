// Package skillcatalog contains IVOAI-owned normalization overlays for
// third-party skill sources. Upstream metadata remains untrusted data and can
// never grant permissions or control-plane authority.
package skillcatalog

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/policy"
	"github.com/ivo-lopes/ivoai/internal/skills"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

const (
	SchemaVersion      = 1
	maxCatalogBytes    = 1 << 20
	maxCatalogSources  = 256
	maxSkillsPerSource = 1024
	maxSkillDocument   = 1 << 20
)

//go:embed catalog.json
var catalogFS embed.FS

type Catalog struct {
	Schema  int      `json:"schema"`
	Sources []Source `json:"sources"`
}

// Source deliberately separates metadata supplied by the upstream from
// provenance observed by IVOAI and IVOAI-owned policy classification.
type Source struct {
	ID              string           `json:"id"`
	DisplayName     string           `json:"display_name"`
	Upstream        Upstream         `json:"upstream"`
	Provenance      Provenance       `json:"provenance"`
	Skills          []UpstreamSkill  `json:"skills"`
	Classifications []Classification `json:"classifications"`
}

type Upstream struct {
	Repository    string `json:"repository"`
	DefaultBranch string `json:"default_branch"`
	License       string `json:"license"`
	ObservedCount int    `json:"observed_skill_count"`
}

type Provenance struct {
	Revision          string `json:"revision"`
	ResolutionSource  string `json:"resolution_source"`
	SignatureStatus   string `json:"signature_status"`
	AttestationStatus string `json:"attestation_status"`
	TrustLevel        string `json:"trust_level"`
}

type UpstreamSkill struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SHA256      string `json:"sha256"`
}

type Classification struct {
	Path                  string          `json:"path"`
	CanonicalID           string          `json:"canonical_id"`
	Domain                string          `json:"domain"`
	Triggers              []string        `json:"triggers,omitempty"`
	Keywords              []string        `json:"keywords,omitempty"`
	Phase                 skills.Phase    `json:"phase"`
	Role                  string          `json:"role,omitempty"`
	RoleMode              skills.RoleMode `json:"role_mode,omitempty"`
	Dependencies          []string        `json:"dependencies,omitempty"`
	OptionalDependencies  []string        `json:"optional_dependencies,omitempty"`
	Conflicts             []string        `json:"conflicts,omitempty"`
	Risk                  skills.RiskTier `json:"risk"`
	RequestedCapabilities []string        `json:"requested_capabilities,omitempty"`
	Executors             []string        `json:"executors,omitempty"`
}

type PolicyDecision struct {
	SkillID string        `json:"skill_id"`
	Result  policy.Result `json:"result"`
}

func Load() (Catalog, error) {
	data, err := catalogFS.ReadFile("catalog.json")
	if err != nil {
		return Catalog{}, err
	}
	if len(data) > maxCatalogBytes {
		return Catalog{}, errors.New("skill catalog exceeds bounded size")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var catalog Catalog
	if err := decoder.Decode(&catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode skill catalog: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Catalog{}, errors.New("skill catalog contains trailing data")
	}
	if err := catalog.Validate(); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

func (c *Catalog) Validate() error {
	if c.Schema != SchemaVersion || len(c.Sources) == 0 || len(c.Sources) > maxCatalogSources {
		return errors.New("invalid skill catalog schema or source count")
	}
	sort.Slice(c.Sources, func(i, j int) bool { return c.Sources[i].ID < c.Sources[j].ID })
	seenSources, seenSkills := map[string]bool{}, map[string]bool{}
	for index := range c.Sources {
		source := &c.Sources[index]
		if !safeID(source.ID) || seenSources[source.ID] || !bounded(source.DisplayName, 256) || !githubRepository(source.Upstream.Repository) || !bounded(source.Upstream.DefaultBranch, 256) || !bounded(source.Upstream.License, 128) || source.Upstream.ObservedCount < len(source.Skills) || source.Upstream.ObservedCount > 10000 || !immutableRevision(source.Provenance.Revision) || source.Provenance.ResolutionSource != "github_api" || source.Provenance.TrustLevel != "commit_pinned_local_digest" || !safeStatus(source.Provenance.SignatureStatus) || !safeStatus(source.Provenance.AttestationStatus) {
			return fmt.Errorf("invalid source %q", source.ID)
		}
		seenSources[source.ID] = true
		if len(source.Skills) == 0 || len(source.Skills) > maxSkillsPerSource || len(source.Classifications) == 0 || len(source.Classifications) > len(source.Skills) {
			return fmt.Errorf("source %q has invalid skill selection", source.ID)
		}
		upstream := map[string]UpstreamSkill{}
		for _, item := range source.Skills {
			if !safeRelativePath(item.Path) || !bounded(item.Name, 256) || !bounded(item.Description, skills.MaxDescriptionBytes) || !sha256Digest(item.SHA256) || upstream[item.Path].Path != "" {
				return fmt.Errorf("source %q has invalid upstream skill metadata", source.ID)
			}
			upstream[item.Path] = item
		}
		for _, classification := range source.Classifications {
			item, ok := upstream[classification.Path]
			if !ok || seenSkills[classification.CanonicalID] {
				return fmt.Errorf("source %q has invalid or duplicate classification", source.ID)
			}
			entry := classification.entry(*source, item, item.Path)
			if err := entry.Validate(); err != nil {
				return fmt.Errorf("source %q classification %q: %w", source.ID, classification.CanonicalID, err)
			}
			seenSkills[classification.CanonicalID] = true
		}
	}
	return nil
}

func (c Catalog) Source(id string) (Source, bool) {
	for _, source := range c.Sources {
		if source.ID == id {
			return source, true
		}
	}
	return Source{}, false
}

func (c Catalog) Reference(id string) (supplychain.Reference, error) {
	source, ok := c.Source(id)
	if !ok {
		return supplychain.Reference{}, fmt.Errorf("unknown curated skill source %q", id)
	}
	return supplychain.Reference{ID: source.ID, Kind: supplychain.KindSkill, Source: source.Upstream.Repository}, nil
}

// PolicyReport evaluates only the IVOAI-owned classification. It does not
// parse or obey permission-like text supplied by the upstream document.
func (c Catalog) PolicyReport(engine policy.Engine) ([]PolicyDecision, error) {
	var report []PolicyDecision
	for _, source := range c.Sources {
		for _, classification := range source.Classifications {
			result := engine.Evaluate(policy.Request{SubjectID: classification.CanonicalID, SubjectKind: policy.SubjectSkill, DeclaredCapabilities: classification.RequestedCapabilities, RequestedCapabilities: classification.RequestedCapabilities, Risk: classification.Risk, Scope: "skill_catalog", MetadataValid: true, ConflictResolved: true})
			report = append(report, PolicyDecision{SkillID: classification.CanonicalID, Result: result})
		}
	}
	sort.Slice(report, func(i, j int) bool { return report[i].SkillID < report[j].SkillID })
	return report, nil
}

// Classifier normalizes an immutable staged repository using the curated
// IVOAI overlay. It reads selected skill documents as data only and verifies
// their locally recorded digest; it never runs hooks, installers, or scripts.
type Classifier struct{ Catalog Catalog }

func (c Classifier) Classify(ctx context.Context, resolved supplychain.ResolvedSource, root string) ([]skills.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, ok := c.Catalog.Source(resolved.ID)
	if !ok || source.Upstream.Repository != resolved.Source {
		return nil, errors.New("resolved source is absent from curated skill catalog")
	}
	if source.Provenance.Revision != resolved.Revision {
		return nil, errors.New("resolved revision requires a reviewed catalog refresh")
	}
	upstream := map[string]UpstreamSkill{}
	for _, item := range source.Skills {
		upstream[item.Path] = item
	}
	entries := make([]skills.Entry, 0, len(source.Classifications))
	for _, classification := range source.Classifications {
		item := upstream[classification.Path]
		actualPath, err := stagedPath(root, item.Path)
		if err != nil {
			return nil, fmt.Errorf("skill %s: %w", classification.CanonicalID, err)
		}
		data, err := platform.ReadRegularFile(actualPath, maxSkillDocument)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(digest[:]), item.SHA256) {
			return nil, fmt.Errorf("skill %s content digest mismatch", classification.CanonicalID)
		}
		relative, err := filepath.Rel(root, actualPath)
		if err != nil || strings.HasPrefix(filepath.ToSlash(relative), "../") {
			return nil, errors.New("selected skill escaped staged root")
		}
		entries = append(entries, classification.entry(source, item, filepath.ToSlash(relative)))
	}
	return entries, nil
}

func (c Classification) entry(source Source, upstream UpstreamSkill, storedPath string) skills.Entry {
	return skills.Entry{
		ID: c.CanonicalID, Name: upstream.Name, Description: upstream.Description, Domain: c.Domain,
		Triggers: c.Triggers, Keywords: c.Keywords, RequiredDependencies: c.Dependencies,
		OptionalDependencies: c.OptionalDependencies, Conflicts: c.Conflicts, Phase: c.Phase,
		Role: c.Role, RoleMode: c.RoleMode, Capabilities: c.RequestedCapabilities, Risk: c.Risk,
		Compatibility: skills.Compatibility{Executors: c.Executors}, Lifecycle: skills.LifecycleStaged,
		Provenance: skills.Provenance{
			Source:    skills.Source{Kind: "git", URL: source.Upstream.Repository, Repository: source.Upstream.Repository, Path: storedPath, DefaultBranch: source.Upstream.DefaultBranch},
			Revision:  skills.Revision{Commit: source.Provenance.Revision, LogicalVersion: source.Provenance.Revision},
			Integrity: skills.Integrity{Algorithm: "sha256", Digest: upstream.SHA256, Verified: true, SignatureStatus: source.Provenance.SignatureStatus, AttestationStatus: source.Provenance.AttestationStatus, TrustLevel: source.Provenance.TrustLevel},
		},
	}
}

func stagedPath(root, path string) (string, error) {
	direct := filepath.Join(root, filepath.FromSlash(path))
	if info, err := os.Lstat(direct); err == nil && info.Mode().IsRegular() {
		return direct, nil
	}
	children, err := osReadDir(root)
	if err != nil {
		return "", errors.New("read staged repository root")
	}
	var roots []os.DirEntry
	for _, child := range children {
		if child.Name() == ".ivoai-manifest.json" || child.Name() == ".ivoai-provenance.json" {
			continue
		}
		if child.IsDir() && child.Type()&os.ModeSymlink == 0 {
			roots = append(roots, child)
		}
	}
	if len(roots) != 1 {
		return "", errors.New("staged repository does not have a unique safe root")
	}
	candidate := filepath.Join(root, roots[0].Name(), filepath.FromSlash(path))
	info, err := os.Lstat(candidate)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("selected skill path is missing or not a regular file")
	}
	return candidate, nil
}

var osReadDir = func(path string) ([]os.DirEntry, error) { return os.ReadDir(path) }

var safeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func githubRepository(value string) bool {
	return strings.HasPrefix(value, "https://github.com/") && strings.Count(strings.TrimPrefix(value, "https://github.com/"), "/") == 1
}

func safeID(value string) bool { return safeIDPattern.MatchString(value) }
func immutableRevision(value string) bool {
	return len(value) == 40 && sha256Digest(value+strings.Repeat("0", 24))
}
func sha256Digest(value string) bool {
	_, err := hex.DecodeString(value)
	return len(value) == 64 && err == nil
}
func bounded(value string, limit int) bool {
	return value != "" && len(value) <= limit && strings.TrimSpace(value) == value
}
func safeStatus(value string) bool {
	return bounded(value, 64) && !strings.ContainsAny(value, "\r\n\t")
}
func safeRelativePath(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(value))
	return value != "" && value == clean && !filepath.IsAbs(value) && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !strings.Contains(value, "\\")
}
