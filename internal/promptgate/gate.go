// Package promptgate performs bounded, side-effect-free AUTO intake. It never
// grants tools or turns the user's request into a different request.
package promptgate

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxPromptBytes = 128 << 10

var (
	ErrEmptyInput     = errors.New("PROMPT_EMPTY")
	ErrInputTooLarge  = errors.New("PROMPT_TOO_LARGE")
	ErrInvalidUTF8    = errors.New("PROMPT_INVALID_UTF8")
	ErrUnsafeControls = errors.New("PROMPT_UNSAFE_CONTROLS")
)

// ValidateInput admits bounded text without judging task quality or requiring
// objectives, acceptance criteria, a plan, or even a complete description. It
// grants no tools or permissions. Errors are fixed labels, never prompt text.
func ValidateInput(prompt string) error {
	switch {
	case len(prompt) > MaxPromptBytes:
		return ErrInputTooLarge
	case !utf8.ValidString(prompt):
		return ErrInvalidUTF8
	case strings.TrimSpace(prompt) == "":
		return ErrEmptyInput
	}
	for index, char := range prompt {
		// LF, tab and CRLF are ordinary pasted text. A standalone CR,
		// terminal controls and directional overrides are unsafe input.
		if char == '\n' || char == '\t' || char == '\r' && index+1 < len(prompt) && prompt[index+1] == '\n' {
			continue
		}
		if unicode.IsControl(char) || unicode.Is(unicode.Bidi_Control, char) {
			return ErrUnsafeControls
		}
	}
	return nil
}

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

// Assess preserves the legacy diagnostic shape for CLI/status consumers.
// Managed admission uses ValidateInput directly; Ready is not a task-quality
// requirement and does not approve a plan, authorize tools, or resolve ambiguity.
func Assess(prompt string) Result {
	r := Result{State: "waiting_for_refinement"}
	switch ValidateInput(prompt) {
	case ErrInputTooLarge:
		r.Missing = []string{"bounded prompt (maximum 128 KiB)"}
	case ErrInvalidUTF8:
		r.Missing = []string{"valid UTF-8 text"}
	case ErrEmptyInput:
		r.Missing = []string{"non-empty text"}
	case ErrUnsafeControls:
		r.Missing = []string{"text without unsafe control characters"}
	default:
		r.Ready, r.State = true, "ready"
	}
	return r
}
