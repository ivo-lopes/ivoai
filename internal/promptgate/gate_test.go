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
		{"shallow", "corrija o projeto", false},
		{"referent", "Corrija isso", false},
		{"unresolved structured referent", "Objective: Fix this and generate a patch.\nAcceptance: tests must pass.", false},
		{"resolved structured referent", "Objective: Fix this in handler.go and generate a patch.\nAcceptance: tests must pass.", true},
		{"no acceptance", "Analise o repositório fixture e gere relatório de findings.", false},
		{"empty heading", "Objetivo: Leia VERSION e informe o valor.\nAcceptance:", false},
		{"vague acceptance", "Objetivo: Leia VERSION e informe o valor.\nAcceptance: tudo funcionando", false},
		{"keyword stuffing", "objective deliverable acceptance criteria constraints", false},
		{"simple", "Leia VERSION e informe o valor.\nAcceptance: retornar somente a versão.", true},
		{"prose", "Read VERSION and report its value. The output must contain only the version.", true},
		{"concrete file deliverables", "Objective: implement two independent changes in this small fixture repository. Deliverables: a.sh prints A and b.sh prints B; README.md explains both. Constraints: do not modify protected.txt. Acceptance: sh a.sh returns A; sh b.sh returns B; protected.txt is unchanged.", true},
		{"complex", "Objetivo:\nAnalise o repositório fixture e implemente duas features independentes.\nEntregáveis:\n- feature A\n- feature B\n- documentação\nRestrições:\nNão modificar protected.txt.\nAcceptance:\n- feature A funciona\n- feature B funciona\n- docs atualizadas\n- protected.txt permanece intacto", true},
		{"bounded", strings.Repeat("a", MaxPromptBytes+1), false},
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
	r := Assess(private)
	if strings.Contains(r.Message(), private) {
		t.Fatal("prompt leaked")
	}
}
