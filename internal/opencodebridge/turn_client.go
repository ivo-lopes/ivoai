package opencodebridge

import "context"

// SubmitTurn is the provider-neutral frontend boundary. It uses the same
// admission, planning, approvals and runner as the managed OpenCode transport.
// Adapters never invoke an executor themselves.
func (b *Bridge) SubmitTurn(ctx context.Context, sessionID, messageID, prompt, model, effort string) (string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	body := map[string]any{"model": model, "reasoning_effort": effort, "messages": []map[string]string{{"role": "user", "content": prompt}}}
	if err := b.terminalRequest(ctx, "POST", "/turn", body, &response, sessionID, messageID); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 {
		return "", nil
	}
	return response.Choices[0].Message.Content, nil
}

func (b *Bridge) PendingDecisions(ctx context.Context) ([]PermissionView, error) {
	var result []PermissionView
	err := b.terminalRequest(ctx, "GET", "/native-permissions", nil, &result, "", "")
	return result, err
}

func (b *Bridge) ReplyDecision(ctx context.Context, id string, allow bool) error {
	return b.terminalRequest(ctx, "POST", "/native-permissions/reply", map[string]any{"id": id, "allow": allow}, nil, "", "")
}

func (b *Bridge) OperationalStatus(ctx context.Context) (Status, error) {
	var result Status
	err := b.terminalRequest(ctx, "GET", "/status", nil, &result, "", "")
	return result, err
}
