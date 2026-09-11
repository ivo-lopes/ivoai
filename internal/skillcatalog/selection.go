package skillcatalog

import (
	"strings"

	"github.com/ivo-lopes/ivoai/internal/skills"
)

// WorkerCandidates ranks only reviewed native metadata for this task. Bodies,
// upstream permission prose and scripts are never consulted for this decision.
// The caller must still resolve dependencies, policy and executor compatibility.
func WorkerCandidates(registry skills.Registry, role, intent, executor, ponytail string) []string {
	catalog, err := Load()
	if err != nil {
		return nil
	}
	roles := map[string][]string{
		"anthropic-cybersecurity-skills": {"security"},
		"caveman-skills":                 {"implementation"},
		"codex-security-skills":          {"security"},
		"hallmark":                       {"implementation"},
		"impeccable":                     {"implementation"},
		"marketing-skills":               {"documentation"},
		"mattpocock-skills":              {"implementation", "documentation"},
		"reverse-skill":                  {"security"},
		"superpowers":                    {"implementation"},
		"taste-skill":                    {"implementation"},
		"ui-ux-pro-max":                  {"implementation"},
	}
	var entries []skills.Entry
	var selected []string
	for _, entry := range registry.Entries {
		source, known := catalog.Source(entry.ArtifactID)
		if !known || entry.Provenance.Source.URL != source.Upstream.Repository {
			continue
		}
		reviewed := false
		for _, classification := range source.Classifications {
			if classification.CanonicalID == entry.ID {
				reviewed = true
			}
		}
		if !reviewed {
			continue
		}
		if entry.Lifecycle != skills.LifecycleActive {
			continue
		}
		if entry.ArtifactID == "ponytail" {
			if ponytail == "on" || (ponytail == "auto" || ponytail == "") && role == "implementation" {
				selected = append(selected, entry.ID)
			}
			continue
		}
		for _, allowed := range roles[entry.ArtifactID] {
			if role == allowed {
				// Automatic injection needs a curated trigger/keyword/domain
				// match; incidental prose words in descriptions are too broad.
				entry.Name, entry.Description = "", ""
				entries = append(entries, entry)
				break
			}
		}
	}
	for _, match := range (skills.Index{Entries: entries}).Search(skills.SearchQuery{Text: strings.TrimSpace(intent), Executor: executor, Limit: 3}) {
		selected = append(selected, match.Entry.ID)
	}
	return selected
}
