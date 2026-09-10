package orchestrator

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// scopedBrief never broadcasts the session objective, facts, decisions, or
// unrelated references. The host intersects requested references with already
// admitted session evidence; text remains private input, not status metadata.
func scopedBrief(shared session.SharedContextBrief, task routing.TaskInput) (string, error) {
	for _, items := range [][]string{task.Acceptance, task.Constraints, task.ContextReferences, task.AllowedMCPs, task.Skills, task.WritePaths} {
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
	}{task.Task, task.Acceptance, task.Constraints, references, task.Dependencies, task.Skills, task.AllowedMCPs, task.WritePaths}
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if len(body) > 16<<10 {
		return "", errors.New("worker context budget exceeded")
	}
	return string(body), nil
}
