package core

import "context"

// CodeIntelligenceProvider is a local, read-only repository intelligence boundary.
// Implementations never derive authority from repository content or request text.
// Repository, organization and permission scope are bound by the host at creation.
type CodeIntelligenceProvider interface {
	Component
	Query(context.Context, CodeQuery) (CodeResult, error)
}

type CodeQuery struct {
	Operation   string `json:"operation"`
	Query       string `json:"query,omitempty"`
	In          string `json:"in,omitempty"`
	IgnoreCase  bool   `json:"ignore_case,omitempty"`
	File        string `json:"file,omitempty"`
	Symbol      string `json:"symbol,omitempty"`
	Direction   string `json:"direction,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	Depth       int    `json:"depth,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	BudgetBytes int    `json:"budget_bytes,omitempty"`
}

type CodeResult struct {
	// Operation is normalized by the host, never copied from result text.
	Operation       string `json:"operation,omitempty"`
	Text            string `json:"text"`
	SourceDigest    string `json:"source_digest"`
	ProviderVersion string `json:"provider_version"`
	CacheHit        bool   `json:"cache_hit"`
	Truncated       bool   `json:"truncated"`
	Fresh           bool   `json:"fresh"`
	Untrusted       bool   `json:"untrusted"`
	Files           int    `json:"files"`
	IndexedFiles    int    `json:"indexed_files"`
	Nodes           int    `json:"nodes"`
	DurationMS      int64  `json:"duration_ms"`
	// Nil means unavailable. Structural processing uses no inference tokens;
	// downstream consumption of Text remains provider context, not free inference.
	PeakMemoryBytes *int64 `json:"peak_memory_bytes,omitempty"`
	CPUMS           *int64 `json:"cpu_ms,omitempty"`
	ArtifactPath    string `json:"artifact_path,omitempty"`
}
