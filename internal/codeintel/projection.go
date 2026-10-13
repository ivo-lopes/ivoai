package codeintel

import (
	"regexp"
	"strings"
)

var additionalAPIWork = regexp.MustCompile(`(?i)\b(callers?|dependenc\w*|impact|refactor\w*|rename|change|behavior|implementation|chamadores|dependências|impacto|alterar)\b`)
var secondNamedTarget = regexp.MustCompile(`\b(?:and|e)\s+[A-Z_][A-Za-z0-9_]*`)

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
	// A single API lookup needs exact declaration/package evidence, not ranked
	// repository exploration. Multi-purpose requests retain the general facade.
	if !additionalAPIWork.MatchString(prompt) && !secondNamedTarget.MatchString(prompt) {
		if query, ok := ForTask(prompt); ok {
			if query.Operation == "ask" && requestedSignature.MatchString(prompt) {
				return []string{"graft_find_code"}
			}
			if query.Operation == "skeleton" {
				return []string{"graft_file_api"}
			}
		}
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
