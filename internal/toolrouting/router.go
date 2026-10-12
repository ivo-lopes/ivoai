// Package toolrouting selects capabilities using trusted runtime evidence only.
// Repository text and model suggestions never grant authority here.
package toolrouting

import (
	"errors"
	"math"
	"slices"
	"strings"
)

// Estimate distinguishes unavailable measurements from an observed zero.
type Estimate struct {
	Value     float64 `json:"value"`
	Known     bool    `json:"known"`
	Estimated bool    `json:"estimated"`
}
type Cost struct {
	InputTokens, OutputTokens, ToolDescriptionTokens, ResultTokens, ReasoningTokens Estimate
	Calls, LatencyMS, CPUMS, PeakMemoryBytes, Processes, IndexMS, EnrichmentTokens  Estimate
}
type Candidate struct {
	ID                                       string
	Authorized, Compatible, Available, Fresh bool
	Capabilities                             []string
	Coverage, Reliability                    float64
	Cost                                     Cost
}
type Request struct {
	Capability                  string
	MinCoverage, MinReliability float64
	RequireFresh                bool
	Weights                     *Cost
}
type Decision struct {
	Tool              string  `json:"tool"`
	Reason            string  `json:"reason"`
	EstimatedCost     float64 `json:"estimated_cost"`
	UnknownDimensions int     `json:"unknown_dimensions"`
}

// Select filters policy and quality before comparing cost. Unknown dimensions
// are ranked conservatively ahead of the weighted comparison, never as zero.
// Weights are trusted host policy, not model-controlled tool arguments.
func Select(request Request, candidates []Candidate) (Decision, error) {
	if request.Capability == "" || !unit(request.MinCoverage) || !unit(request.MinReliability) {
		return Decision{}, errors.New("invalid capability requirements")
	}
	var best Decision
	found := false
	for _, c := range candidates {
		if !c.Authorized || !c.Compatible || !c.Available || !slices.Contains(c.Capabilities, request.Capability) || !unit(c.Coverage) || !unit(c.Reliability) || c.Coverage < request.MinCoverage || c.Reliability < request.MinReliability || request.RequireFresh && !c.Fresh || c.ID == "" || len(c.ID) > 128 || strings.ContainsAny(c.ID, "\r\n\t") {
			continue
		}
		values := dimensions(c.Cost)
		weights := []float64{1, 1, 1, 1, 1, 1, 0.01, 0.01, 1.0 / (64 * 1024 * 1024), 1, 0.01, 1}
		if request.Weights != nil {
			for i, v := range dimensions(*request.Weights) {
				if !v.Known || !finite(v.Value) {
					return Decision{}, errors.New("invalid host cost weights")
				}
				weights[i] = v.Value
			}
		}
		score := 0.0
		unknown := 0
		valid := true
		for i, v := range values {
			if !v.Known {
				if weights[i] > 0 {
					unknown++
				}
				continue
			}
			if !finite(v.Value) {
				valid = false
				break
			}
			score += v.Value * weights[i]
		}
		if !valid || !finite(score) {
			continue
		}
		if !found || unknown < best.UnknownDimensions || unknown == best.UnknownDimensions && (score < best.EstimatedCost || score == best.EstimatedCost && c.ID < best.Tool) {
			best = Decision{Tool: c.ID, Reason: "eligible_minimum_expected_cost", EstimatedCost: score, UnknownDimensions: unknown}
			found = true
		}
	}
	if !found {
		return Decision{}, errors.New("no eligible capability")
	}
	return best, nil
}
func dimensions(c Cost) []Estimate {
	return []Estimate{c.InputTokens, c.OutputTokens, c.ToolDescriptionTokens, c.ResultTokens, c.ReasoningTokens, c.Calls, c.LatencyMS, c.CPUMS, c.PeakMemoryBytes, c.Processes, c.IndexMS, c.EnrichmentTokens}
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
func unit(v float64) bool   { return finite(v) && v <= 1 }
