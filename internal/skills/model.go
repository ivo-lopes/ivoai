// Package skills implements the provider-neutral IVOAI Skill Control Plane
// domain. Skill documents and their metadata are always untrusted data.
package skills

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	RegistrySchemaVersion = 1
	MaxRegistryEntries    = 4096
	MaxDescriptionBytes   = 2048
	MaxMetadataValues     = 128
)

type Lifecycle string

const (
	LifecycleStaged      Lifecycle = "staged"
	LifecycleActive      Lifecycle = "active"
	LifecycleQuarantined Lifecycle = "quarantined"
	LifecyclePrevious    Lifecycle = "previous"
)

type RiskTier string

const (
	RiskLow      RiskTier = "low"
	RiskModerate RiskTier = "moderate"
	RiskHigh     RiskTier = "high"
	RiskCritical RiskTier = "critical"
)

type Phase string

const (
	PhasePlanning           Phase = "planning"
	PhaseResearch           Phase = "research_context"
	PhaseArtDirection       Phase = "art_direction"
	PhaseImplementation     Phase = "implementation"
	PhaseAudit              Phase = "audit_review"
	PhaseSecurity           Phase = "security"
	PhaseOrchestration      Phase = "orchestration"
	PhaseInteractionProfile Phase = "interaction_profile"
)

type Integrity struct {
	Algorithm         string `json:"algorithm"`
	Digest            string `json:"digest"`
	Verified          bool   `json:"verified"`
	SignatureStatus   string `json:"signature_status,omitempty"`
	AttestationStatus string `json:"attestation_status,omitempty"`
	TrustLevel        string `json:"trust_level,omitempty"`
}

type Source struct {
	Kind          string `json:"kind"`
	URL           string `json:"url"`
	Repository    string `json:"repository,omitempty"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

type Revision struct {
	Commit         string `json:"commit"`
	Tag            string `json:"tag,omitempty"`
	LogicalVersion string `json:"logical_version"`
}

type Provenance struct {
	Source       Source    `json:"source"`
	Revision     Revision  `json:"revision"`
	Integrity    Integrity `json:"integrity"`
	DiscoveredAt time.Time `json:"discovered_at,omitempty"`
	ResolvedAt   time.Time `json:"resolved_at,omitempty"`
}

type Compatibility struct {
	Executors        []string `json:"executors,omitempty"`
	OperatingSystems []string `json:"operating_systems,omitempty"`
	Architectures    []string `json:"architectures,omitempty"`
	MinimumIVOAI     string   `json:"minimum_ivoai,omitempty"`
}

type Entry struct {
	ID                   string        `json:"id"`
	Name                 string        `json:"name"`
	Description          string        `json:"description"`
	Domain               string        `json:"domain"`
	Triggers             []string      `json:"triggers,omitempty"`
	Keywords             []string      `json:"keywords,omitempty"`
	RequiredDependencies []string      `json:"requires,omitempty"`
	OptionalDependencies []string      `json:"optional_dependencies,omitempty"`
	Conflicts            []string      `json:"conflicts,omitempty"`
	Phase                Phase         `json:"phase"`
	Role                 string        `json:"role,omitempty"`
	Capabilities         []string      `json:"capabilities,omitempty"`
	Risk                 RiskTier      `json:"risk"`
	Compatibility        Compatibility `json:"compatibility"`
	Lifecycle            Lifecycle     `json:"lifecycle"`
	Provenance           Provenance    `json:"provenance"`
	QuarantineReason     string        `json:"quarantine_reason,omitempty"`
}

type Registry struct {
	Schema    int       `json:"schema"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	Entries   []Entry   `json:"entries"`
}

func EmptyRegistry() Registry { return Registry{Schema: RegistrySchemaVersion, Entries: []Entry{}} }

func (r *Registry) Normalize() error {
	if r.Schema == 0 {
		r.Schema = RegistrySchemaVersion
	}
	if r.Schema != RegistrySchemaVersion {
		return fmt.Errorf("unsupported skill registry schema %d", r.Schema)
	}
	if len(r.Entries) > MaxRegistryEntries {
		return fmt.Errorf("skill registry exceeds %d entries", MaxRegistryEntries)
	}
	sort.Slice(r.Entries, func(i, j int) bool { return r.Entries[i].ID < r.Entries[j].ID })
	seen := make(map[string]struct{}, len(r.Entries))
	for index := range r.Entries {
		entry := &r.Entries[index]
		normalizeEntry(entry)
		if err := entry.Validate(); err != nil {
			return fmt.Errorf("skill %q: %w", entry.ID, err)
		}
		if _, duplicate := seen[entry.ID]; duplicate {
			return fmt.Errorf("duplicate skill ID %q", entry.ID)
		}
		seen[entry.ID] = struct{}{}
	}
	return nil
}

func (e Entry) Validate() error {
	if !safeID(e.ID) || !boundedText(e.Name, 256) || !boundedText(e.Description, MaxDescriptionBytes) || !safeLabel(e.Domain, 128) {
		return errors.New("invalid bounded identity metadata")
	}
	if !validLifecycle(e.Lifecycle) || !validRisk(e.Risk) || !validPhase(e.Phase) {
		return errors.New("invalid lifecycle, risk, or execution phase")
	}
	if !safeLabel(e.Role, 128) || !safeReason(e.QuarantineReason) {
		return errors.New("invalid role or quarantine reason")
	}
	for _, values := range [][]string{e.Triggers, e.Keywords, e.RequiredDependencies, e.OptionalDependencies, e.Conflicts, e.Capabilities, e.Compatibility.Executors, e.Compatibility.OperatingSystems, e.Compatibility.Architectures} {
		if len(values) > MaxMetadataValues {
			return errors.New("metadata list exceeds bounded size")
		}
		for _, value := range values {
			if !safeMetadataValue(value) {
				return fmt.Errorf("unsafe metadata value %q", value)
			}
		}
	}
	for _, dependency := range append(append([]string{}, e.RequiredDependencies...), e.OptionalDependencies...) {
		if dependency == e.ID {
			return errors.New("skill cannot depend on itself")
		}
	}
	return e.Provenance.Validate()
}

func (p Provenance) Validate() error {
	if !validSourceKind(p.Source.Kind) || !validHTTPSURL(p.Source.URL) || !safeRelativePath(p.Source.Path) {
		return errors.New("invalid skill source")
	}
	if p.Source.Repository != "" && !validHTTPSURL(p.Source.Repository) {
		return errors.New("invalid skill repository")
	}
	if !safeBranch(p.Source.DefaultBranch) || !immutableRevision(p.Revision.Commit) || !boundedText(p.Revision.LogicalVersion, 128) {
		return errors.New("skill revision must resolve to an immutable commit")
	}
	return p.Integrity.Validate()
}

func (i Integrity) Validate() error {
	if i.Algorithm != "sha256" || len(i.Digest) != 64 {
		return errors.New("skill integrity must use sha256")
	}
	if _, err := hex.DecodeString(i.Digest); err != nil {
		return errors.New("invalid skill integrity digest")
	}
	for _, value := range []string{i.SignatureStatus, i.AttestationStatus, i.TrustLevel} {
		if !safeLabel(value, 64) {
			return errors.New("invalid provenance status")
		}
	}
	return nil
}

func normalizeEntry(e *Entry) {
	e.ID = strings.TrimSpace(strings.ToLower(e.ID))
	e.Name = strings.TrimSpace(e.Name)
	e.Description = strings.TrimSpace(e.Description)
	e.Domain = strings.TrimSpace(strings.ToLower(e.Domain))
	e.Role = strings.TrimSpace(strings.ToLower(e.Role))
	e.QuarantineReason = strings.TrimSpace(e.QuarantineReason)
	e.Triggers = normalizedList(e.Triggers)
	e.Keywords = normalizedList(e.Keywords)
	e.RequiredDependencies = normalizedList(e.RequiredDependencies)
	e.OptionalDependencies = normalizedList(e.OptionalDependencies)
	e.Conflicts = normalizedList(e.Conflicts)
	e.Capabilities = normalizedList(e.Capabilities)
	e.Compatibility.Executors = normalizedList(e.Compatibility.Executors)
	e.Compatibility.OperatingSystems = normalizedList(e.Compatibility.OperatingSystems)
	e.Compatibility.Architectures = normalizedList(e.Compatibility.Architectures)
	e.Provenance.Revision.Commit = strings.ToLower(strings.TrimSpace(e.Provenance.Revision.Commit))
	e.Provenance.Integrity.Digest = strings.ToLower(strings.TrimSpace(e.Provenance.Integrity.Digest))
}

func normalizedList(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func safeID(value string) bool { return safeLabel(value, 128) && !strings.Contains(value, "..") }

func safeLabel(value string, limit int) bool {
	if value == "" {
		return true
	}
	if len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._:-", char)) {
			return false
		}
	}
	return true
}

func safeMetadataValue(value string) bool {
	return boundedText(value, 512) && !strings.ContainsAny(value, "\x00\x1b\r\n")
}

func boundedText(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\x1b")
}

func safeReason(value string) bool {
	return value == "" || len(value) <= 256 && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\x1b\r\n")
}

func safeRelativePath(value string) bool {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return false
	}
	clean := strings.TrimPrefix(strings.TrimSpace(value), "./")
	return clean != "" && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !strings.Contains(clean, "/../")
}

func safeBranch(value string) bool {
	return value == "" || len(value) <= 256 && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\x1b\r\n ~^:?*[\\")
}

func immutableRevision(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validHTTPSURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}

func validSourceKind(value string) bool {
	return value == "git" || value == "release" || value == "bundle" || value == "embedded"
}

func validLifecycle(value Lifecycle) bool {
	return value == LifecycleStaged || value == LifecycleActive || value == LifecycleQuarantined || value == LifecyclePrevious
}

func validRisk(value RiskTier) bool {
	return value == RiskLow || value == RiskModerate || value == RiskHigh || value == RiskCritical
}

func validPhase(value Phase) bool {
	switch value {
	case PhasePlanning, PhaseResearch, PhaseArtDirection, PhaseImplementation, PhaseAudit, PhaseSecurity, PhaseOrchestration, PhaseInteractionProfile:
		return true
	default:
		return false
	}
}
