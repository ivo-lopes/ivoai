package serverpool

import (
	"errors"
	"sort"
	"strings"
	"unicode"
)

type KnowledgePolicy string

const (
	PurposeAuto  KnowledgePolicy = "purpose-auto"
	AllEnabled   KnowledgePolicy = "all-enabled"
	ExplicitOnly KnowledgePolicy = "explicit-only"
)

// ResolvePurposes accepts the intake planner's relevance decision as labels,
// never endpoints or credentials. Explicit session selectors remain authoritative.
// An empty relevance decision is deliberately an empty selection, not federation.
func (p Pool) ResolvePurposes(policy KnowledgePolicy, explicit, relevant []string) (Selection, error) {
	if len(explicit) > 0 {
		return p.Resolve(explicit)
	}
	switch policy {
	case AllEnabled:
		return p.Resolve(nil)
	case ExplicitOnly:
		return Selection{}, nil
	case PurposeAuto:
		if len(relevant) == 0 {
			return Selection{}, nil
		}
		return p.Resolve(relevant)
	default:
		return Selection{}, errors.New("unknown knowledge routing policy")
	}
}

// MentionedPurposes is a zero-I/O hint for intake, not a semantic classifier.
// Matching complete labels avoids selecting 'corp' for 'corporate'. Ambiguous
// relevance must be resolved by intake rather than querying every configured host.
func (p Pool) MentionedPurposes(prompt string) []string {
	words := func(value string) []string {
		return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' })
	}
	mentioned, excluded := map[string]bool{}, map[string]bool{}
	// A negative source instruction is an exclusion, not a reason to contact
	// that source. Conservative exclusions win over positive mentions. More
	// ambiguous routing stays empty and can be overridden explicitly.
	for _, clause := range strings.FieldsFunc(strings.ToLower(prompt), func(r rune) bool { return strings.ContainsRune(".;!\n", r) }) {
		tokens := words(clause)
		negative := false
		for _, word := range tokens {
			switch word {
			case "not", "no", "never", "without", "exclude", "excluded", "excluding", "não", "nao", "sem", "exceto", "excluir":
				negative = true
			}
		}
		for _, word := range tokens {
			mentioned[word] = true
			if negative {
				excluded[word] = true
			}
		}
	}
	selected := map[string]bool{}
	excludedPurposes := map[string]bool{}
	for alias, profile := range p.profiles {
		if excluded[strings.ToLower(alias)] || excluded[strings.ToLower(profile.Purpose)] {
			excludedPurposes[profile.Purpose] = true
		}
	}
	for alias, profile := range p.profiles {
		if !profile.Enabled || excludedPurposes[profile.Purpose] {
			continue
		}
		aliasLabel, purposeLabel := strings.ToLower(alias), strings.ToLower(profile.Purpose)
		if excluded[aliasLabel] || excluded[purposeLabel] {
			continue
		}
		if mentioned[aliasLabel] || mentioned[purposeLabel] {
			selected[profile.Purpose] = true
		}
	}
	result := make([]string, 0, len(selected))
	for purpose := range selected {
		result = append(result, purpose)
	}
	sort.Strings(result)
	return result
}
