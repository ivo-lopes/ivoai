package opencodebridge

import (
	"context"
	"encoding/json"
)

type singleUserMessageKey struct{}

// SingleUserMessage reports an observation made by this transport over the
// complete input envelope. Absence, system/developer messages and any history
// are ineligible for experimental local metadata handling. It grants no tool,
// filesystem or execution permission.
func SingleUserMessage(ctx context.Context) bool {
	v, _ := ctx.Value(singleUserMessageKey{}).(bool)
	return v
}

// Structured/multipart inputs remain with the legacy path even if its text
// extractor can flatten them. This pilot requires a complete literal message.
func singleUserInput(messages []chatMessage) bool {
	if len(messages) != 1 || messages[0].Role != "user" {
		return false
	}
	var text string
	return json.Unmarshal(messages[0].Content, &text) == nil
}
