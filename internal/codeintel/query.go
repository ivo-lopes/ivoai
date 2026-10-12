package codeintel

import (
	"errors"
	"github.com/ivo-lopes/ivoai/internal/core"
	"path/filepath"
	"strings"
)

// Normalize enforces host budgets, never upstream's unbounded depth/full options.
func Normalize(q core.CodeQuery) (core.CodeQuery, error) {
	invalid := errors.New("invalid or unbounded code intelligence query")
	switch q.Operation {
	case "build", "map", "check", "viz":
	case "ask", "grep":
		if strings.TrimSpace(q.Query) == "" {
			return q, invalid
		}
	case "skeleton":
		if q.File == "" {
			return q, invalid
		}
	case "callers", "blast":
		if q.Symbol == "" && q.File == "" {
			return q, invalid
		}
	default:
		return q, invalid
	}
	for _, v := range []string{q.Query, q.File, q.Symbol, q.In} {
		if len(v) > 2048 || strings.ContainsAny(v, "\x00\r\n") {
			return q, invalid
		}
	}
	for _, path := range []string{q.File, q.In} {
		if path != "" && (!filepath.IsLocal(path) || filepath.Clean(path) != path) {
			return q, invalid
		}
	}
	if q.StartLine < 0 || q.EndLine < 0 || q.EndLine > 10000000 || q.StartLine > q.EndLine || ((q.StartLine != 0 || q.EndLine != 0) && (q.Operation != "blast" || q.File == "" || q.StartLine == 0)) {
		return q, invalid
	}
	if q.BudgetBytes == 0 {
		q.BudgetBytes = 8192
	}
	if q.BudgetBytes < 256 || q.BudgetBytes > 32768 {
		return q, invalid
	}
	if q.Limit == 0 {
		q.Limit = 5
	}
	if q.Limit < 1 || q.Limit > 20 {
		return q, invalid
	}
	if q.Depth == 0 {
		q.Depth = 1
		if q.Operation == "blast" {
			q.Depth = 3
		}
	}
	if q.Depth < 1 || q.Depth > 8 {
		return q, invalid
	}
	if q.Direction == "" {
		q.Direction = "in"
	}
	if q.Direction != "in" && q.Direction != "out" {
		return q, invalid
	}
	return q, nil
}
