package cli

import (
	"fmt"

	"github.com/ivo-lopes/ivoai/internal/terminalui"
)

func (s *menuSession) capabilities() (bool, error) {
	for {
		rows, err := s.app.NativeCapabilities(s.ctx)
		if err != nil {
			return false, err
		}
		cfg, err := s.app.Store.Load()
		if err != nil {
			return false, err
		}
		actions := []menuAction{
			{id: "capabilities.update", label: "Update native pack (reviewed release baseline)", run: s.simple(func() error { return s.app.NativeCapabilityAction(s.ctx, "update", "") })},
			{id: "capabilities.ponytail", label: "Ponytail: " + cfg.Skills.ResolvedPonytail(), description: "Auto applies only to implementation workers", run: s.ponytailPolicy},
		}
		for _, row := range rows {
			actions = append(actions, menuAction{id: "capability." + row.ID, label: row.Name + " — " + row.Status, description: fmt.Sprintf("Risk: %s | policy: %s | pinned: %t | update: %t", row.Risk, row.SelectionPolicy, row.Pinned, row.UpdateAvailable), run: func() (bool, error) { return s.manageCapability(row.ID) }})
		}
		id, err := s.choose("Skills & Capabilities — installed does not mean authorized", actions, nil)
		if err != nil || id == "" {
			return false, err
		}
		exit, err := s.execute(findAction(actions, id))
		if err != nil {
			fmt.Fprintln(s.app.Err, "Error:", UserError(err))
		}
		if exit {
			return true, err
		}
		terminalui.Pause(s.app.In, s.app.Out)
	}
}

func (s *menuSession) manageCapability(id string) (bool, error) {
	for {
		cfg, err := s.app.Store.Load()
		if err != nil {
			return false, err
		}
		preference := cfg.Skills.Sources[id]
		toggle, pin := "disable", "pin"
		if preference.Disabled {
			toggle = "enable"
		}
		if preference.Pinned {
			pin = "unpin"
		}
		actions := []menuAction{{id: "capability.inspect", label: "Inspect metadata / provenance", run: s.simple(func() error { return s.app.PrintNativeCapabilities(s.ctx, id) })}}
		for _, action := range []string{toggle, pin, "update", "rollback"} {
			run := s.simple(func() error { return s.app.NativeCapabilityAction(s.ctx, action, id) })
			if action == "rollback" {
				run = s.confirmed("ROLLBACK", func() error { return s.app.NativeCapabilityAction(s.ctx, action, id) })
			}
			actions = append(actions, menuAction{id: "capability." + action, label: action, run: run})
		}
		selected, err := s.choose("Native capability: "+id, actions, nil)
		if err != nil || selected == "" {
			return false, err
		}
		exit, err := s.execute(findAction(actions, selected))
		if err != nil {
			fmt.Fprintln(s.app.Err, "Error:", UserError(err))
		}
		if exit {
			return true, err
		}
		terminalui.Pause(s.app.In, s.app.Out)
	}
}

func (s *menuSession) ponytailPolicy() (bool, error) {
	actions := []menuAction{}
	for _, mode := range []string{"auto", "off", "on"} {
		label := mode
		if mode == "auto" {
			label = "Auto — implementation workers only"
		}
		if mode == "on" {
			label = "On — explicit selection, still subject to worker policy"
		}
		actions = append(actions, menuAction{id: mode, label: label, run: s.simple(func() error { return s.app.ConfigSet("skills.ponytail", mode) })})
	}
	id, err := s.choose("Ponytail cannot change acceptance, permissions, or routing", actions, nil)
	if err != nil || id == "" {
		return false, err
	}
	return s.execute(findAction(actions, id))
}
