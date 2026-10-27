// Package promptgate performs bounded, side-effect-free AUTO intake. It never
// grants tools or turns the user's request into a different request.
package promptgate

import (
	"strings"
	"unicode"
	"unicode/utf8"
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

// Assess accepts natural-language input without imposing a task template.
// Readiness describes bounded text only: it does not approve a plan, authorize
// tools, resolve an ambiguous request, or establish completed acceptance.
// Messages contain fixed labels, never private prompt fragments.
func Assess(prompt string) Result {
	r := Result{State: "waiting_for_refinement"}
	switch {
	case len(prompt) > MaxPromptBytes:
		r.Missing = []string{"bounded prompt (maximum 128 KiB)"}
	case !utf8.ValidString(prompt):
		r.Missing = []string{"valid UTF-8 text"}
	case strings.TrimSpace(prompt) == "":
		r.Missing = []string{"non-empty text"}
	default:
		for index, char := range prompt {
			// LF, tab and CRLF are ordinary pasted text. A standalone CR,
			// terminal controls and directional overrides are unsafe input.
			if char == '\n' || char == '\t' || char == '\r' && index+1 < len(prompt) && prompt[index+1] == '\n' {
				continue
			}
			if unicode.IsControl(char) || unicode.Is(unicode.Bidi_Control, char) {
				r.Missing = []string{"text without unsafe control characters"}
				return r
			}
		}
		r.Ready, r.State = true, "ready"
	}
	return r
}
