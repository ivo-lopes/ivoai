package orchestrator

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

// scopedBrief never broadcasts the session objective, facts, decisions, or
// unrelated references. The host intersects requested references with already
// admitted session evidence; text remains private input, not status metadata.
func scopedBrief(shared session.SharedContextBrief, task routing.TaskInput) (string, error) {
	for _, items := range [][]string{task.Acceptance, task.Constraints, task.ContextReferences, task.KnowledgeSources, task.AllowedMCPs, task.Skills, task.WritePaths} {
		if len(items) > 32 {
			return "", errors.New("worker context list exceeds limit")
		}
		for _, v := range items {
			if strings.TrimSpace(v) == "" || len(v) > 1024 || strings.ContainsAny(v, "\x00\x1b") {
				return "", errors.New("worker context item exceeds limit")
			}
		}
	}
	references := []string{}
	available := map[string]bool{}
	for _, ref := range shared.References {
		available[ref] = true
	}
	for _, ref := range task.ContextReferences {
		if available[ref] {
			references = append(references, ref)
		}
	}
	value := struct {
		Objective    string   `json:"local_objective"`
		Acceptance   []string `json:"local_acceptance"`
		Constraints  []string `json:"constraints"`
		References   []string `json:"references"`
		Dependencies []string `json:"dependencies"`
		Skills       []string `json:"skills"`
		MCPs         []string `json:"allowed_mcps"`
		WritePaths   []string `json:"write_paths"`
		Sources      []string `json:"knowledge_sources"`
	}{task.Task, task.Acceptance, task.Constraints, references, task.Dependencies, task.Skills, task.AllowedMCPs, task.WritePaths, task.KnowledgeSources}
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(body) > 16<<10 {
		return "", errors.New("worker context budget exceeded")
	}
	return string(body), nil
}

// Only direct dependency findings enter this task's brief. Transcripts and
// unrelated sibling results never do; source scopes cannot silently broaden.
func (s *Server) addDependencyFindings(planID string, task routing.Task, brief string) (string, error) {
	type finding struct {
		TaskID  string                     `json:"task_id"`
		Summary string                     `json:"summary"`
		Refs    []workingcontext.ResultRef `json:"result_refs"`
		Sources []string                   `json:"sources"`
	}
	scopes := map[string]bool{}
	for _, source := range task.KnowledgeSources {
		scopes[source] = true
	}
	findings := []finding{}
	s.mu.Lock()
	plan := s.plans[planID]
	for _, id := range task.Dependencies {
		dependency := plan.Tasks[id]
		for _, source := range dependency.Task.KnowledgeSources {
			if !scopes[source] {
				s.mu.Unlock()
				return "", errors.New("KNOWLEDGE_SCOPE_DENIED: dependency finding requires an admitted worker source")
			}
		}
		summary := dependency.Result.Result.Summary
		if len(summary) > 2048 {
			summary = "Dependency evidence exceeds brief budget; use its ResultRefs for exact review by the primary."
		}
		refs := append([]workingcontext.ResultRef(nil), dependency.Result.Result.Evidence...)
		if len(refs) > 8 {
			refs = refs[:8]
		}
		findings = append(findings, finding{TaskID: id, Summary: summary, Refs: refs, Sources: append([]string(nil), dependency.Task.KnowledgeSources...)})
	}
	s.mu.Unlock()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(brief), &envelope); err != nil {
		return "", err
	}
	envelope["dependency_findings"] = findings
	body, err := json.Marshal(envelope)
	if err != nil || len(body) > 16<<10 {
		return "", errors.New("worker dependency context budget exceeded")
	}
	return string(body), nil
}
