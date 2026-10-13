package codeintel

import (
	"regexp"
	"strings"
)

var codeIntent = regexp.MustCompile(`(?i)\b(code|function|symbol|caller|implementation|refactor|repository|repo|api|bug|código|função|símbolo|implementação|refatorar|repositório)\b`)

// ProjectTools selects schemas, never execution authority. A single facade
// retains all local operations for ambiguous/code tasks. Explicit native names
// retain their smaller contracts. Pure institutional tasks need neither.
// No prompt, repository text or selection can override gateway/Skill Gate grants.
func ProjectTools(prompt string) []string {
	if len(prompt) > 32768 {
		return []string{"code_inspect"}
	}
	lower := strings.ToLower(prompt)
	var exact []string
	for _, name := range ToolNames() {
		if strings.Contains(lower, name) {
			exact = append(exact, name)
		}
	}
	if len(exact) != 0 {
		return exact
	}
	if !codeIntent.MatchString(prompt) && !namedFile.MatchString(prompt) {
		for _, term := range []string{"institutional", "institucional", "ai-memory", "decisões históricas", "historical decisions"} {
			if strings.Contains(lower, term) {
				return nil
			}
		}
	}
	return []string{"code_inspect"}
}
