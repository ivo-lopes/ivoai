package codeintel

import (
	"slices"
	"testing"
)

func TestEconomicToolProjection(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		want   []string
	}{
		{"Locate function Multiply and inspect its callers", []string{"code_inspect"}},
		{"Inspect APIs and dependencies across modules", []string{"code_inspect"}},
		{"", []string{"code_inspect"}},
		{"Use graft_trace_calls to inspect Multiply", []string{"graft_trace_calls"}},
		{"Use graft_find_code and graft_file_api", []string{"graft_file_api", "graft_find_code"}},
		{"Read institutional documentation in Context only", nil},
		{"Consultar decisões históricas no ai-memory", nil},
		{"Read institutional documentation then inspect function Multiply", []string{"code_inspect"}},
	} {
		if got := ProjectTools(tc.prompt); !slices.Equal(got, tc.want) {
			t.Errorf("%q: %v want %v", tc.prompt, got, tc.want)
		}
	}
}

func TestAPITaskProjectsSpecificSufficientCapability(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		want   []string
	}{
		{"Report the public signature of Report and which package defines it. Do not enumerate unrelated implementations.", []string{"graft_find_code"}},
		{"Inspect API in mathpkg/scale.go", []string{"graft_file_api"}},
		{"Report the signature of Report and trace dependencies", []string{"code_inspect"}},
		{"Report the signature of Report and Scale", []string{"code_inspect"}},
	} {
		if got := ProjectTools(tc.prompt); !slices.Equal(got, tc.want) {
			t.Errorf("%q: got %v want %v", tc.prompt, got, tc.want)
		}
	}
}
