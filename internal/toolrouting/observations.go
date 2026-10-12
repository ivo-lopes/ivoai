package toolrouting

import "slices"

// History holds bounded local aggregate measurements, never prompts or source.
// Exponential smoothing adapts to host measurements without retaining events.
// Missing resource/token measurements remain unknown; bytes/4 is an estimate.
type History struct {
	Entries map[string]Aggregate `json:"entries"`
}
type Aggregate struct {
	Samples                int `json:"samples"`
	LatencyMS, ResultBytes float64
	CPU, Memory            *float64
	Failures               int `json:"failures"`
}
type Sample struct {
	LatencyMS   int64
	ResultBytes int
	CPU, Memory *int64
	Success     bool
}

func historyKey(operation, tool string) string {
	if !slices.Contains([]string{"build", "ask", "skeleton", "callers", "grep", "map", "blast", "check", "viz"}, operation) || !slices.Contains([]string{"graft", "direct"}, tool) {
		return ""
	}
	return operation + ":" + tool
}
func (h *History) Observe(operation, tool string, s Sample) {
	key := historyKey(operation, tool)
	if key == "" || s.LatencyMS < 0 || s.LatencyMS > 90000 || s.ResultBytes < 0 || s.ResultBytes > 32768 {
		return
	}
	if s.CPU != nil && (*s.CPU < 0 || *s.CPU > 86400000) || s.Memory != nil && (*s.Memory < 0 || *s.Memory > 1<<40) {
		return
	}
	if h.Entries == nil {
		h.Entries = map[string]Aggregate{}
	}
	a := h.Entries[key]
	if a.Samples >= 10000 {
		a.Samples /= 2
		a.Failures /= 2
	}
	mean := func(old, next float64) float64 {
		if a.Samples == 0 {
			return next
		}
		return old*.8 + next*.2
	}
	a.LatencyMS = mean(a.LatencyMS, float64(s.LatencyMS))
	a.ResultBytes = mean(a.ResultBytes, float64(s.ResultBytes))
	update := func(old *float64, next *int64) *float64 {
		if next == nil {
			return old
		}
		v := float64(*next)
		if old != nil {
			v = mean(*old, v)
		}
		return &v
	}
	a.CPU = update(a.CPU, s.CPU)
	a.Memory = update(a.Memory, s.Memory)
	a.Samples++
	if !s.Success {
		a.Failures++
	}
	h.Entries[key] = a
}
func (h History) Cost(operation, tool string) (Cost, bool) {
	a, ok := h.Entries[historyKey(operation, tool)]
	if !ok || a.Samples <= 0 {
		return Cost{}, false
	}
	estimate := func(v float64) Estimate { return Estimate{Value: v, Known: true, Estimated: true} }
	c := Cost{LatencyMS: estimate(a.LatencyMS), ResultTokens: estimate(a.ResultBytes / 4)}
	if a.CPU != nil {
		c.CPUMS = estimate(*a.CPU)
	}
	if a.Memory != nil {
		c.PeakMemoryBytes = estimate(*a.Memory)
	}
	return c, true
}
