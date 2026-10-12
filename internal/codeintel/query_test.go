package codeintel

import (
	"github.com/ivo-lopes/ivoai/internal/core"
	"testing"
)

func TestQueryBoundsAndOperations(t *testing.T) {
	for _, op := range []string{"build", "ask", "skeleton", "callers", "grep", "map", "blast", "check", "viz"} {
		q := core.CodeQuery{Operation: op, Query: "literal", File: "main.go", Symbol: "Run"}
		got, e := Normalize(q)
		if e != nil || got.BudgetBytes <= 0 || got.BudgetBytes > 32768 || got.Depth > 8 {
			t.Fatalf("%s: %+v %v", op, got, e)
		}
	}
	for _, q := range []core.CodeQuery{{Operation: "init"}, {Operation: "ask", Query: "x", BudgetBytes: 999999}, {Operation: "skeleton", File: "../private"}, {Operation: "callers", Symbol: "f", Depth: 999}, {Operation: "grep", Query: ""}} {
		if _, e := Normalize(q); e == nil {
			t.Fatalf("accepted %+v", q)
		}
	}
}
