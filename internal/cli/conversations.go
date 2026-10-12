package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/app"
)

func (s *menuSession) conversations() (bool, error) {
	values, err := s.app.SessionList()
	if err != nil {
		return false, err
	}
	if len(values) == 0 {
		fmt.Fprintln(s.app.Out, "No IVOAI conversations yet.")
		return false, nil
	}
	actions := make([]menuAction, 0, len(values))
	for _, value := range values {
		value := value
		actions = append(actions, menuAction{id: value.SessionID, label: value.SessionID,
			description: fmt.Sprintf("%s | %s | primary=%s | %s | resumable=%t", value.Mode, value.Frontend, value.PrimaryExecutor, value.State, value.Resumable()),
			run: func() (bool, error) {
				return s.loop("Conversation "+value.SessionID, []menuAction{
					{id: "inspect", label: "Inspect metadata", description: "Native mappings, state and bounded continuity; no transcript", run: s.simple(func() error {
						current, err := s.app.SessionShow(value.SessionID)
						if err != nil {
							return err
						}
						encoder := json.NewEncoder(s.app.Out)
						encoder.SetIndent("", "  ")
						return encoder.Encode(current)
					})},
					{id: "resume", label: "Resume native conversation", description: "Reopen and wait for new input; do not replay the last turn", disabled: disabledUnless(value.Resumable(), "no resumable mapping or session currently active"), run: func() (bool, error) { return s.resumeConversation(value.SessionID, "", false) }},
					{id: "frontend", label: "Resume in another frontend", description: "Keep the provider and IVOAI identity; switch presentation only", disabled: disabledUnless(value.Mode == "auto" && value.Resumable(), "requires an inactive orchestrated conversation"), run: func() (bool, error) {
						choices := []menuAction{}
						for _, frontend := range []string{"codex", "opencode"} {
							frontend := frontend
							if frontend == value.Frontend {
								continue
							}
							choices = append(choices, menuAction{id: frontend, label: frontend, description: "Provider conversation and policies remain owned by IVOAI", run: func() (bool, error) { return s.resumeConversation(value.SessionID, frontend, false) }})
						}
						return s.loop("Resume frontend", choices)
					}},
					{id: "recover", label: "Resume interrupted turn", description: "Reconcile the saved DAG; require a new approval; never repeat uncertain writes", disabled: disabledUnless(value.CheckpointAvailable && value.Resumable(), "no checkpoint or session currently active"), run: func() (bool, error) {
						if !s.confirm("RESUME") {
							return false, nil
						}
						return s.resumeConversation(value.SessionID, "", true)
					}},
					{id: "stop", label: "Stop owned session", description: "Stop matching processes; preserve continuity metadata", disabled: disabledUnless(value.Active(), "session is not active"), run: s.simple(func() error {
						if !s.confirm("STOP") {
							return nil
						}
						return s.app.SessionStop(value.SessionID)
					})},
					{id: "handoff", label: "Handoff to another provider", description: "Explicit transfer of a bounded brief; not native resume", run: func() (bool, error) {
						choices := []menuAction{}
						for _, provider := range []string{"codex", "claude"} {
							provider := provider
							if provider == value.PrimaryExecutor {
								continue
							}
							choices = append(choices, menuAction{id: provider, label: provider, description: "Transfer approved context; preserve direct/orchestrated mode", run: func() (bool, error) {
								if !s.confirm("HANDOFF " + provider) {
									return false, nil
								}
								return true, s.app.SessionHandoff(s.ctx, value.SessionID, provider, true)
							}})
						}
						return s.loop("Handoff destination", choices)
					}},
					{id: "session.scope.adopt", label: "Preview / adopt organization scope", description: "Preserve native history; require explicit adoption and verified account", disabled: disabledUnless(value.Mode == "auto" && value.Resumable(), "requires an inactive orchestrated conversation"), run: func() (bool, error) { return s.changeConversationScope(value.SessionID, false) }},
					{id: "session.scope.restore", label: "Preview / restore scope backup", description: "Restore only the previous organization binding from a private backup", disabled: disabledUnless(value.Mode == "auto" && value.Resumable(), "requires an inactive orchestrated conversation"), run: func() (bool, error) { return s.changeConversationScope(value.SessionID, true) }},
				})
			}})
	}
	return s.loop("IVOAI Conversations", actions)
}

func writeScopeAdoption(out io.Writer, result app.ScopeAdoption, jsonOutput bool) error {
	if jsonOutput {
		return json.NewEncoder(out).Encode(result)
	}
	fmt.Fprintf(out, "Session: %s\nProvider: %s\nDirectory: %s\nPrevious scope: %s\nProposed scope: %s\nSelected sources: %s\n", result.SessionID, result.Provider, result.WorkingDirectory, result.PreviousScope, result.ProposedScope, strings.Join(result.SelectedSources, ", "))
	fmt.Fprintln(out, "Existing native history is preserved, including information from its previous organization scope. No transcript or authentication is copied.")
	if result.Applied {
		fmt.Fprintf(out, "Scope applied. Backup: %s\n", result.BackupPath)
	} else {
		fmt.Fprintln(out, "Preview only. Review the retained history and selected organization sources before repeating with --confirm.")
	}
	return nil
}

func (s *menuSession) conversationSources() ([]string, error) {
	value, err := s.prompt("Knowledge source aliases/purposes (comma-separated; blank uses enabled sources)", false, "")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return []string{value}, nil
}

func (s *menuSession) resumeConversation(id, frontend string, recover bool) (bool, error) {
	selectors, err := s.conversationSources()
	if err != nil {
		return false, err
	}
	if recover {
		return true, s.app.SessionRecoverWithKnowledge(s.ctx, id, selectors)
	}
	return true, s.app.SessionResumeWithKnowledge(s.ctx, id, frontend, selectors)
}

func (s *menuSession) changeConversationScope(id string, restore bool) (bool, error) {
	var preview app.ScopeAdoption
	var selectors []string
	var backup string
	var err error
	if restore {
		backup, err = s.prompt("Private scope backup path", false, "")
		if err != nil {
			return false, err
		}
		preview, err = s.app.SessionRestoreScope(s.ctx, id, backup, false)
	} else {
		selectors, err = s.conversationSources()
		if err != nil {
			return false, err
		}
		preview, err = s.app.SessionAdoptScope(s.ctx, id, selectors, false)
	}
	if err != nil {
		return false, err
	}
	if err = writeScopeAdoption(s.app.Out, preview, false); err != nil {
		return false, err
	}
	phrase := "ADOPT SCOPE"
	if restore {
		phrase = "RESTORE SCOPE"
	}
	if !s.confirm(phrase) {
		return false, nil
	}
	if restore {
		preview, err = s.app.SessionRestoreScope(s.ctx, id, backup, true)
	} else {
		preview, err = s.app.SessionAdoptScope(s.ctx, id, selectors, true)
	}
	if err != nil {
		return false, err
	}
	return false, writeScopeAdoption(s.app.Out, preview, false)
}
