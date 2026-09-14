package cli

import (
	"encoding/json"
	"fmt"
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
					{id: "resume", label: "Resume native conversation", description: "Reopen and wait for new input; do not replay the last turn", disabled: disabledUnless(value.Resumable(), "no resumable mapping or session currently active"), run: func() (bool, error) { return true, s.app.SessionResume(s.ctx, value.SessionID) }},
					{id: "frontend", label: "Resume in another frontend", description: "Keep the provider and IVOAI identity; switch presentation only", disabled: disabledUnless(value.Mode == "auto" && value.Resumable(), "requires an inactive orchestrated conversation"), run: func() (bool, error) {
						choices := []menuAction{}
						for _, frontend := range []string{"codex", "opencode"} {
							frontend := frontend
							if frontend == value.Frontend {
								continue
							}
							choices = append(choices, menuAction{id: frontend, label: frontend, description: "Provider conversation and policies remain owned by IVOAI", run: func() (bool, error) { return true, s.app.SessionResumeFrontend(s.ctx, value.SessionID, frontend) }})
						}
						return s.loop("Resume frontend", choices)
					}},
					{id: "recover", label: "Resume interrupted turn", description: "Reconcile the saved DAG; require a new approval; never repeat uncertain writes", disabled: disabledUnless(value.CheckpointAvailable && value.Resumable(), "no checkpoint or session currently active"), run: func() (bool, error) {
						if !s.confirm("RESUME") {
							return false, nil
						}
						return true, s.app.SessionRecover(s.ctx, value.SessionID)
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
				})
			}})
	}
	return s.loop("IVOAI Conversations", actions)
}
