package skills

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const maxFrontmatterBytes = 64 << 10

type Quarantine struct {
	Path   string `json:"path"`
	ID     string `json:"id,omitempty"`
	Reason string `json:"reason"`
}

type Index struct {
	Entries     []Entry      `json:"entries"`
	Quarantined []Quarantine `json:"quarantined"`
}

type Indexer struct {
	MaxEntries int
	Open       func(string) (io.ReadCloser, error)
}

func (i Indexer) Discover(root string) (Index, error) {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) {
		return Index{}, errors.New("skill discovery root must be absolute")
	}
	limit := i.MaxEntries
	if limit <= 0 || limit > MaxRegistryEntries {
		limit = MaxRegistryEntries
	}
	result := Index{Entries: []Entry{}, Quarantined: []Quarantine{}}
	seen := map[string]int{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("skill discovery path escaped its root")
		}
		if !utf8.ValidString(relative) {
			result.Quarantined = append(result.Quarantined, Quarantine{Path: "invalid-utf8", Reason: "invalid_utf8_path"})
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			result.Quarantined = append(result.Quarantined, Quarantine{Path: filepath.ToSlash(relative), Reason: "unexpected_symlink"})
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}
		if len(result.Entries)+len(result.Quarantined) >= limit {
			return errors.New("skill discovery entry limit exceeded")
		}
		metadata, err := i.readMetadata(path)
		if err != nil {
			result.Quarantined = append(result.Quarantined, Quarantine{Path: filepath.ToSlash(relative), Reason: boundedQuarantineReason(err)})
			return nil
		}
		candidate, err := entryFromMetadata(metadata)
		if err == nil {
			expected := strings.ToLower(filepath.Base(filepath.Dir(path)))
			if candidate.ID != expected || filepath.ToSlash(relative) != candidate.Provenance.Source.Path {
				err = errors.New("id_path_mismatch")
			}
		}
		if err != nil {
			result.Quarantined = append(result.Quarantined, Quarantine{Path: filepath.ToSlash(relative), ID: candidate.ID, Reason: boundedQuarantineReason(err)})
			return nil
		}
		if previous, duplicate := seen[candidate.ID]; duplicate {
			first := result.Entries[previous]
			result.Entries = append(result.Entries[:previous], result.Entries[previous+1:]...)
			for id, position := range seen {
				if position > previous {
					seen[id] = position - 1
				}
			}
			delete(seen, candidate.ID)
			result.Quarantined = append(result.Quarantined,
				Quarantine{Path: first.Provenance.Source.Path, ID: first.ID, Reason: "duplicate_id"},
				Quarantine{Path: filepath.ToSlash(relative), ID: candidate.ID, Reason: "duplicate_id"},
			)
			return nil
		}
		seen[candidate.ID] = len(result.Entries)
		result.Entries = append(result.Entries, candidate)
		return nil
	})
	if err != nil {
		return Index{}, err
	}
	available := make(map[string]bool, len(result.Entries))
	for _, entry := range result.Entries {
		available[entry.ID] = true
	}
	valid := result.Entries[:0]
	for _, entry := range result.Entries {
		missing := ""
		for _, dependency := range entry.RequiredDependencies {
			if !available[dependency] {
				missing = dependency
				break
			}
		}
		if missing != "" {
			result.Quarantined = append(result.Quarantined, Quarantine{Path: entry.Provenance.Source.Path, ID: entry.ID, Reason: "missing_dependency"})
			continue
		}
		valid = append(valid, entry)
	}
	result.Entries = valid
	sort.Slice(result.Entries, func(a, b int) bool { return result.Entries[a].ID < result.Entries[b].ID })
	sort.Slice(result.Quarantined, func(a, b int) bool {
		if result.Quarantined[a].Path == result.Quarantined[b].Path {
			return result.Quarantined[a].Reason < result.Quarantined[b].Reason
		}
		return result.Quarantined[a].Path < result.Quarantined[b].Path
	})
	return result, nil
}

func (i Indexer) readMetadata(path string) (map[string][]string, error) {
	open := i.Open
	if open == nil {
		open = openRegularNoFollow
	}
	reader, err := open(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return parseFrontmatter(reader)
}

func openRegularNoFollow(path string) (io.ReadCloser, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("skill metadata source is not a regular file")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		file.Close()
		return nil, errors.New("skill metadata source changed during safe open")
	}
	return file, nil
}

func parseFrontmatter(source io.Reader) (map[string][]string, error) {
	reader := bufio.NewReaderSize(source, 16)
	read := 0
	line, err := readBoundedLine(reader, &read)
	if err != nil || strings.TrimSpace(line) != "---" {
		return nil, errors.New("malformed_frontmatter")
	}
	values := map[string][]string{}
	current := ""
	for {
		line, err = readBoundedLine(reader, &read)
		if err != nil {
			return nil, errors.New("malformed_frontmatter")
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			if current == "" {
				return nil, errors.New("malformed_frontmatter")
			}
			value, err := scalar(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			if err != nil {
				return nil, err
			}
			values[current] = append(values[current], value)
			continue
		}
		key, raw, ok := strings.Cut(trimmed, ":")
		key = strings.TrimSpace(key)
		if !ok || !safeLabel(key, 64) {
			return nil, errors.New("malformed_frontmatter")
		}
		if _, duplicate := values[key]; duplicate {
			return nil, errors.New("duplicate_metadata_key")
		}
		current = key
		raw = strings.TrimSpace(raw)
		if raw == "" {
			values[key] = []string{}
			continue
		}
		list, err := metadataList(raw)
		if err != nil {
			return nil, err
		}
		values[key] = list
	}
	return values, nil
}

func readBoundedLine(reader *bufio.Reader, total *int) (string, error) {
	var bytes []byte
	for {
		value, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		*total++
		if *total > maxFrontmatterBytes {
			return "", errors.New("metadata_too_large")
		}
		if value == '\n' {
			break
		}
		bytes = append(bytes, value)
	}
	if !utf8.Valid(bytes) {
		return "", errors.New("invalid_utf8")
	}
	return strings.TrimSuffix(string(bytes), "\r"), nil
}

func metadataList(raw string) ([]string, error) {
	if strings.HasPrefix(raw, "[") {
		if !strings.HasSuffix(raw, "]") {
			return nil, errors.New("malformed_metadata_list")
		}
		raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]"))
		if raw == "" {
			return []string{}, nil
		}
		parts := strings.Split(raw, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			value, err := scalar(strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	}
	value, err := scalar(raw)
	if err != nil {
		return nil, err
	}
	return []string{value}, nil
}

func scalar(value string) (string, error) {
	if value == "" {
		return "", errors.New("empty_metadata_value")
	}
	if value[0] == '"' || value[0] == '\'' {
		if value[0] == '\'' {
			if len(value) < 2 || value[len(value)-1] != '\'' {
				return "", errors.New("malformed_quoted_metadata")
			}
			value = value[1 : len(value)-1]
		} else {
			decoded, err := strconv.Unquote(value)
			if err != nil {
				return "", errors.New("malformed_quoted_metadata")
			}
			value = decoded
		}
	}
	if !safeMetadataValue(value) {
		return "", errors.New("unsafe_metadata_value")
	}
	return value, nil
}

func entryFromMetadata(values map[string][]string) (Entry, error) {
	allowed := map[string]bool{
		"schema": true, "id": true, "name": true, "description": true, "source_kind": true, "source_url": true,
		"repository": true, "path": true, "default_branch": true, "commit": true, "tag": true, "version": true,
		"checksum": true, "domain": true, "triggers": true, "keywords": true, "requires": true,
		"optional_dependencies": true, "conflicts": true, "phase": true, "role": true, "capabilities": true,
		"risk": true, "executors": true, "operating_systems": true, "architectures": true, "minimum_ivoai": true,
	}
	for key := range values {
		if !allowed[key] {
			return Entry{}, fmt.Errorf("unsupported_metadata_key_%s", key)
		}
	}
	one := func(key string) string {
		if len(values[key]) == 1 {
			return values[key][0]
		}
		return ""
	}
	if one("schema") != strconv.Itoa(RegistrySchemaVersion) {
		return Entry{}, errors.New("unsupported_schema")
	}
	checksum := strings.TrimPrefix(strings.ToLower(one("checksum")), "sha256:")
	entry := Entry{
		ID: one("id"), Name: one("name"), Description: one("description"), Domain: one("domain"),
		Triggers: values["triggers"], Keywords: values["keywords"], RequiredDependencies: values["requires"], OptionalDependencies: values["optional_dependencies"], Conflicts: values["conflicts"],
		Phase: Phase(one("phase")), Role: one("role"), Capabilities: values["capabilities"], Risk: RiskTier(one("risk")), Lifecycle: LifecycleStaged,
		Compatibility: Compatibility{Executors: values["executors"], OperatingSystems: values["operating_systems"], Architectures: values["architectures"], MinimumIVOAI: one("minimum_ivoai")},
		Provenance:    Provenance{Source: Source{Kind: one("source_kind"), URL: one("source_url"), Repository: one("repository"), Path: one("path"), DefaultBranch: one("default_branch")}, Revision: Revision{Commit: one("commit"), Tag: one("tag"), LogicalVersion: one("version")}, Integrity: Integrity{Algorithm: "sha256", Digest: checksum, SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "checksum_only"}},
	}
	normalizeEntry(&entry)
	if err := entry.Validate(); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func boundedQuarantineReason(err error) string {
	value := strings.ToLower(strings.TrimSpace(err.Error()))
	value = strings.ReplaceAll(value, " ", "_")
	if !safeLabel(value, 128) {
		return "invalid_metadata"
	}
	return value
}

type SearchQuery struct {
	Text        string
	Domain      string
	Executor    string
	MaximumRisk RiskTier
	Limit       int
}

type Candidate struct {
	Entry Entry `json:"entry"`
	Score int   `json:"score"`
}

func (index Index) Search(query SearchQuery) []Candidate {
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	terms := normalizedTerms(query.Text)
	var result []Candidate
	for _, entry := range index.Entries {
		if query.Domain != "" && entry.Domain != strings.ToLower(query.Domain) {
			continue
		}
		if query.Executor != "" && len(entry.Compatibility.Executors) > 0 && !contains(entry.Compatibility.Executors, strings.ToLower(query.Executor)) {
			continue
		}
		if riskWeight(entry.Risk) > riskWeight(query.MaximumRisk) && query.MaximumRisk != "" {
			continue
		}
		score := 0
		for _, term := range terms {
			if contains(entry.Triggers, term) {
				score += 100
			}
			if contains(entry.Keywords, term) {
				score += 30
			}
			if term == entry.Domain {
				score += 40
			}
			if strings.Contains(strings.ToLower(entry.Name+" "+entry.Description), term) {
				score += 10
			}
		}
		if score > 0 {
			result = append(result, Candidate{Entry: entry, Score: score})
		}
	}
	sort.Slice(result, func(a, b int) bool {
		if result[a].Score == result[b].Score {
			return result[a].Entry.ID < result[b].Entry.ID
		}
		return result[a].Score > result[b].Score
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func normalizedTerms(value string) []string {
	return normalizedList(strings.Fields(strings.ToLower(value)))
}

func contains(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func riskWeight(value RiskTier) int {
	switch value {
	case RiskLow:
		return 1
	case RiskModerate:
		return 2
	case RiskHigh:
		return 3
	case RiskCritical:
		return 4
	default:
		return 0
	}
}
