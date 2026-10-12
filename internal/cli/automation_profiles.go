package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/config"
)

func printAutomationProfile(a *app.App, id string) (config.AutomationValues, error) {
	c, err := a.Store.Load()
	if err != nil {
		return config.AutomationValues{}, err
	}
	current := config.AutomationValuesOf(c.Orchestration.Auto)
	value := map[string]any{"effective_profile": config.EffectiveAutomationProfile(c), "current": current, "applies": "future sessions only", "provider_and_permissions": "preserved"}
	if id != "" {
		p, err := config.FindAutomationProfile(id)
		if err != nil {
			return current, err
		}
		value["proposed"] = p
	}
	err = json.NewEncoder(a.Out).Encode(value)
	return current, err
}

func runAutomationProfile(a *app.App, args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch {
	case len(args) == 1 && args[0] == "list":
		return json.NewEncoder(a.Out).Encode(config.AutomationProfiles())
	case len(args) == 1 && args[0] == "show":
		_, err := printAutomationProfile(a, "")
		return err
	case len(args) == 2 && args[0] == "preview":
		_, err := printAutomationProfile(a, args[1])
		return err
	case len(args) == 2 && args[0] == "apply":
		if err := a.Store.ApplyAutomationProfile(args[1], nil); err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "Automation profile applied; effective for future sessions.")
		return nil
	case len(args) == 1 && args[0] == "restore":
		if err := a.Store.RestoreAutomationProfile(); err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "Previous automation settings restored.")
		return nil
	default:
		return errors.New("usage: ivoai config profile <list|show|preview ID|apply ID|restore>")
	}
}

func (s *menuSession) automationProfiles() (bool, error) {
	actions := []menuAction{{id: "profile.show", label: "Effective configuration / custom values", run: s.simple(func() error { _, err := printAutomationProfile(s.app, ""); return err })}}
	for _, p := range config.AutomationProfiles() {
		actions = append(actions, menuAction{id: "profile." + p.ID, label: p.ID, description: p.Purpose, run: func() (bool, error) {
			before, err := printAutomationProfile(s.app, p.ID)
			if err != nil {
				return false, err
			}
			if !s.confirm("APPLY") {
				return false, nil
			}
			return false, s.app.Store.ApplyAutomationProfile(p.ID, &before)
		}})
	}
	actions = append(actions, menuAction{id: "profile.restore", label: "Restore previous automation settings", run: func() (bool, error) {
		if !s.confirm("RESTORE") {
			return false, nil
		}
		return false, s.app.Store.RestoreAutomationProfile()
	}})
	return s.loop("Automation Profiles — preview before applying", actions)
}
