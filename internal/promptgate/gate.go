// Package promptgate performs bounded, side-effect-free AUTO intake. It never
// grants tools or turns the user's request into a different request.
package promptgate

import (
	"regexp"
	"strings"
	"unicode"
)

const MaxPromptBytes = 128 << 10

type Result struct {
	Ready   bool     `json:"ready"`
	State   string   `json:"state"`
	Missing []string `json:"missing,omitempty"`
}

func (r Result) Message() string {
	if r.Ready {
		return "Prompt readiness: ready"
	}
	return "Prompt readiness: insufficient\n\nMissing:\n- " + strings.Join(r.Missing, "\n- ") + "\n\nState: waiting_for_refinement"
}

// Labels segment clauses, rather than satisfying a requirement by their mere
// presence. Content after a label must contain an actionable objective or an
// observable result. Bullets, prose, and headings use the same representation.
var label = regexp.MustCompile(`(?i)(?:^|[\n.;])\s*[#* ]*(objetivo|objective|goal|entregáveis|entregavel|entregável|deliverables?|expected output|acceptance criteria|acceptance|critérios de aceite|criterios de aceite|aceite|restrições|restricoes|constraints|contexto|context|escopo|scope)\s*[:\n]`)
var action = regexp.MustCompile(`(?i)\b(analis[ea]|anal[yi][sz]e|identifi(?:que|y)|implement[ea]?|corrija|fix|create|crie|read|leia|investigu[ea]|investigate|compare|document[ea]?|return|retorne|informe|report|gere|generate)\b`)
var artifact = regexp.MustCompile(`(?i)\b(relatório|relatorio|report|findings|root cause|causa raiz|feature|features|versão|versao|version|documentação|documentation|docs|arquivo|file|patch|testes?|tests?|resultado|result|valor|value|implementação|implementation|alterações|changes)\b`)
var observable = regexp.MustCompile(`(?i)(?:\b(?:retornar|retorne|return|returns|informe|output|deve|must|shall|passes|passa|funciona|works|intact[oa]|unchanged|updated|atualizad[oa]s?|implementad[oa]s?|implemented|sem alteração|no changes|contém|contains|exit|identifi(?:que|ed)|com findings)\b|(?:=|==|>=|<=)\s*\S+)`)
var vague = regexp.MustCompile(`(?i)^(?:[-*\d.)\s]*)(?:ok|done|feito|tbd|todo|a/b/c|a, b, c|yes|sim|sucesso|success|tudo funcionando|everything works|critérios|criteria)[.!\s]*$`)

var unresolvedTarget = regexp.MustCompile(`(?i)\b(?:this|that|it|isso|isto|aquilo)\b`)
var namedTarget = regexp.MustCompile("(?i)(?:`[^`]+`|[a-z0-9_-]+\\.[a-z0-9_-]+|https?://[^ ]+|\\b(?:repo(?:sitory)?|repositório|servidor|server|file|arquivo|error|erro)\\s+[A-Z0-9][A-Za-z0-9_-]+)")

// Assess is deliberately conservative: ambiguous prose is returned to the
// user, not guessed at by a premium model. A keyword list or an empty heading
// is not a contract. Constraints/context are required only when the objective
// otherwise has an unresolved referent ("this/isso" without a named target).
// The result contains only fixed labels, never prompt fragments.
func Assess(prompt string) Result {
	r := Result{State: "waiting_for_refinement"}
	if len(prompt) > MaxPromptBytes {
		r.Missing = []string{"bounded prompt (maximum 128 KiB)"}
		return r
	}
	sections := split(prompt)
	objective := sections["objective"]
	if objective == "" {
		objective = prompt
	}
	objectiveOK := action.MatchString(objective) && substantive(objective)
	// A referent is a relation, not a keyword requirement: an explicit scope
	// or named target resolves it; a deliverable/acceptance heading alone does
	// not tell the control plane what "fix this" is allowed to change.
	if unresolvedTarget.MatchString(objective) && !namedTarget.MatchString(objective) && !substantive(sections["context"]) {
		objectiveOK = false
	}
	deliverable := sections["deliverable"]
	if deliverable == "" {
		deliverable = objective
	}
	deliverableOK := (artifact.MatchString(deliverable) || namedTarget.MatchString(deliverable)) && substantive(deliverable)
	criteria := sections["acceptance"]
	// Natural-language acceptance is accepted without a prescribed heading:
	// e.g. "The result must contain ..." / "O relatório deve conter ...".
	if criteria == "" {
		for _, clause := range regexp.MustCompile(`[\n;.!]+`).Split(prompt, -1) {
			if regexp.MustCompile(`(?i)\b(must|shall|deve|somente|only|aceito quando|successful when)\b`).MatchString(clause) && observable.MatchString(clause) {
				criteria += clause + "\n"
			}
		}
	}
	criteriaOK := false
	for _, clause := range strings.Split(criteria, "\n") {
		clause = strings.TrimSpace(clause)
		if substantive(clause) && observable.MatchString(clause) && !vague.MatchString(clause) {
			criteriaOK = true
		}
	}
	if !objectiveOK {
		r.Missing = append(r.Missing, "objective and scope")
	}
	if !deliverableOK {
		r.Missing = append(r.Missing, "expected deliverable")
	}
	if !criteriaOK {
		r.Missing = append(r.Missing, "observable acceptance criteria")
	}
	r.Ready = len(r.Missing) == 0
	if r.Ready {
		r.State = "ready"
	}
	return r
}

func substantive(s string) bool {
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	if len(words) < 3 {
		return false
	}
	return !vague.MatchString(strings.TrimSpace(s))
}

func split(prompt string) map[string]string {
	matches := label.FindAllStringSubmatchIndex(prompt, -1)
	sections := map[string]string{}
	for i, m := range matches {
		end := len(prompt)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		name := strings.ToLower(prompt[m[2]:m[3]])
		key := "context"
		switch name {
		case "objetivo", "objective", "goal":
			key = "objective"
		case "entregáveis", "entregavel", "entregável", "deliverable", "deliverables", "expected output":
			key = "deliverable"
		case "acceptance criteria", "acceptance", "critérios de aceite", "criterios de aceite", "aceite":
			key = "acceptance"
		}
		sections[key] += strings.TrimSpace(prompt[m[1]:end]) + "\n"
	}
	return sections
}
