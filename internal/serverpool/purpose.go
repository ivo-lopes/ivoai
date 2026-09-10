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
	mentioned := map[string]bool{}
	for _, word := range words(prompt) {
		mentioned[word] = true
	}
	selected := map[string]bool{}
	for alias, profile := range p.profiles {
		if !profile.Enabled {
			continue
		}
		if mentioned[strings.ToLower(alias)] || mentioned[strings.ToLower(profile.Purpose)] {
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
