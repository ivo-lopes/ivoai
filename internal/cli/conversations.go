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
					{id: "mode", label: "Resume Codex in another mode", description: "Same native thread; explicitly change admission for future turns", disabled: disabledUnless(value.PrimaryExecutor == "codex" && value.Resumable(), "requires an inactive Codex conversation"), run: func() (bool, error) {
						mode := "direct"
						if value.Mode == "direct" {
							mode = "orchestrated"
						}
						fmt.Fprintf(s.app.Out, "Resume as %s. Direct bypasses IVOAI orchestration; orchestrated requires Prompt Gate and plan approval for new turns.\n", mode)
						if !s.confirm("RESUME " + mode) {
							return false, nil
						}
						return true, s.app.SessionResumeMode(s.ctx, value.SessionID, mode, true)
					}},
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
						for _, provider := range []string{"codex", "claude", "opencode"} {
							provider := provider
							if provider == value.PrimaryExecutor {
								continue
							}
							for _, mode := range []string{"direct", "orchestrated"} {
								if provider == "opencode" && mode != "direct" {
									continue
								}
								mode := mode
								choices = append(choices, menuAction{id: provider + "." + mode, label: provider + " — " + mode, description: "EXPLICIT_HANDOFF: new native conversation; bounded approved context, no transcript migration", run: func() (bool, error) {
									if !s.confirm("HANDOFF " + provider + " " + mode) {
										return false, nil
									}
									frontend := ""
									if mode == "orchestrated" && (value.Frontend == "opencode" || value.PrimaryExecutor == "opencode") {
										frontend = "opencode"
									}
									return true, s.app.SessionHandoffDestination(s.ctx, value.SessionID, provider, mode, frontend, true)
								}})
							}
						}
						return s.loop("Handoff destination", choices)
					}},
				})
			}})
	}
	return s.loop("IVOAI Conversations", actions)
}

func (s *menuSession) nativeConversations() (bool, error) {
	threads, err := s.app.SessionNativeDiscover(s.ctx, "")
	if err != nil {
		return false, err
	}
	if len(threads) == 0 {
		fmt.Fprintln(s.app.Out, "No eligible Codex threads in the current project. Native history stays with Codex.")
		return false, nil
	}
	actions := make([]menuAction, 0, len(threads))
	for _, thread := range threads {
		thread := thread
		actions = append(actions, menuAction{id: thread.ID, label: "Codex " + thread.ID[:8], description: fmt.Sprintf("Native provider=%s | project=%s | explicit adoption", thread.Provider, thread.Directory), run: func() (bool, error) {
			fmt.Fprintf(s.app.Out, "Associate native Codex thread %s with IVOAI. No transcript, credential or personal configuration will be copied. Existing mappings keep their mode.\n", thread.ID)
			if !s.confirm("ADOPT") {
				return false, nil
			}
			value, err := s.app.SessionAdoptCodex(s.ctx, thread.ID, true)
			if err != nil {
				return false, err
			}
			fmt.Fprintf(s.app.Out, "Associated with %s. Use Conversations to inspect or resume.\n", value.SessionID)
			return false, nil
		}})
	}
	return s.loop("Native Codex — current project", actions)
}
