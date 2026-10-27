package promptgate

import (
	"strings"
	"testing"
)

func TestIntakeContracts(t *testing.T) {
	for _, tt := range []struct {
		name, prompt string
		ready        bool
	}{
		{"informal objective", "corrija o projeto", true},
		{"unresolved referent is input, not permission", "Corrija isso", true},
		{"question", "Qual versão deste projeto?", true},
		{"explanation", "Explique a função Assess", true},
		{"no acceptance", "Analise o repositório fixture e gere relatório de findings.", true},
		{"empty acceptance heading", "Objetivo: Leia VERSION e informe o valor.\nAcceptance:", true},
		{"vague acceptance is not a grant", "Objetivo: Leia VERSION e informe o valor.\nAcceptance: tudo funcionando", true},
		{"keywords are valid text", "objective deliverable acceptance criteria constraints", true},
		{"single word", "oi", true},
		{"explicit acceptance", "Read VERSION and report its value. Acceptance: return only the version.", true},
		{"structured", "Objective: implement two independent changes.\nDeliverables: a.sh prints A and b.sh prints B.\nAcceptance: tests pass.", true},
		{"line feed and tab", "Leia VERSION\n\tRetorne a versão.", true},
		{"CRLF paste", "Leia VERSION\r\nRetorne a versão.", true},
		{"Unicode", "Explique a função 测试 🚀", true},
		{"maximum bytes", strings.Repeat("a", MaxPromptBytes), true},
		{"maximum Unicode bytes", strings.Repeat("é", MaxPromptBytes/2), true},
		{"empty", "", false},
		{"whitespace only", " \t\n\r\n", false},
		{"Unicode whitespace only", "\u2003\u00a0", false},
		{"bounded", strings.Repeat("a", MaxPromptBytes+1), false},
		{"invalid UTF-8", string([]byte{'a', 0xff}), false},
		{"NUL", "Leia VERSION\x00", false},
		{"terminal escape", "Leia VERSION\x1b[2J", false},
		{"bell", "Leia VERSION\a", false},
		{"delete", "Leia VERSION\x7f", false},
		{"standalone carriage return", "Leia VERSION\rRetorne a versão.", false},
		{"trailing carriage return", "Leia VERSION\r", false},
		{"bidirectional override", "Leia VERSION\u202e", false},
		{"bidirectional isolate", "Leia VERSION\u2066", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := Assess(tt.prompt)
			if r.Ready != tt.ready {
				t.Fatalf("readiness=%v missing=%v", r.Ready, r.Missing)
			}
			if !r.Ready && !strings.Contains(r.Message(), "waiting_for_refinement") {
				t.Fatal("missing refinement state")
			}
		})
	}
}

func TestGateDoesNotReturnPrompt(t *testing.T) {
	const private = "private-data-not-for-metadata"
	for _, prompt := range []string{private, private + "\x00"} {
		r := Assess(prompt)
		if strings.Contains(r.Message(), private) || strings.Contains(strings.Join(r.Missing, " "), private) {
			t.Fatal("prompt leaked")
		}
	}
}
