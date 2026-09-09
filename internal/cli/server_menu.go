package cli

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"time"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
)

type serverObservation struct {
	value app.ServerView
	at    time.Time
}

// Health is an observation, not enrollment state. Cache only in this menu;
// never probe per frame or claim old observations are current after a restart.
func (s *menuSession) observedServer(value app.ServerView) app.ServerView {
	old, ok := s.serverHealth[value.ID]
	if ok && time.Since(old.at) < time.Minute && old.value.URL == value.URL && old.value.Enabled == value.Enabled && old.value.Status == value.Status && old.value.CredentialConfigured == value.CredentialConfigured {
		value.Health = old.value.Health
	}
	return value
}

func serverSummary(value app.ServerView) string {
	return fmt.Sprintf("Purpose: %s | Enabled: %s | Connection: %s | Health: %s", value.Purpose, yesNo(value.Enabled), value.Status, value.Health.State)
}

func (s *menuSession) servers() (bool, error) {
	for {
		values, err := s.app.ListServers()
		if err != nil {
			return false, err
		}
		actions := []menuAction{
			{id: "servers.list", label: "List servers", description: "Configuration and recent test results; no background probes", run: s.simple(func() error {
				current, err := s.app.ListServers()
				if err != nil {
					return err
				}
				for i := range current {
					current[i] = s.observedServer(current[i])
				}
				return writeServerViews(s.app.Out, current, false)
			})},
			{id: "servers.add", label: "Add server", description: "New alias, purpose, URL and hidden enrollment code", run: s.addServer},
		}
		enrolled, healthy := 0, 0
		for _, value := range values {
			value = s.observedServer(value)
			if value.Status == "connected" && value.CredentialConfigured {
				enrolled++
			}
			if value.Health.Probed && value.Health.State == "healthy" {
				healthy++
			}
			alias := value.Alias
			actions = append(actions, menuAction{id: "profile:" + value.ID, label: alias, description: serverSummary(value), run: func() (bool, error) { return s.manageServer(alias) }})
		}
		badges := []terminalui.Badge{{Label: "Servers", Value: fmt.Sprintf("%d configured / %d enrolled / %d tested healthy", len(values), enrolled, healthy)}, {Label: "Health", Value: "Test a server explicitly; results expire after 1 minute"}}
		id, err := s.choose("IVOAI Servers", actions, badges)
		if err != nil || id == "" {
			return false, err
		}
		_, err = s.execute(findAction(actions, id))
		if err != nil {
			fmt.Fprintln(s.app.Err, "Error:", UserError(err))
		}
		terminalui.Pause(s.app.In, s.app.Out)
	}
}

func validateServerURL(value string) error {
	_, err := connections.ValidateBaseURL(value)
	return err
}

func (s *menuSession) addServer() (bool, error) {
	alias, err := s.promptValidated("Server alias", false, "", serverpool.ValidateAlias)
	if err != nil {
		return false, err
	}
	values, err := s.app.ListServers()
	if err != nil {
		return false, err
	}
	for _, value := range values {
		if value.Alias == alias {
			return false, fmt.Errorf("server alias %q already exists; select it to manage or re-enroll", alias)
		}
	}
	purpose, err := s.promptValidated("Knowledge purpose", false, alias, func(value string) error { return serverpool.ValidateLabel("purpose", value) })
	if err != nil {
		return false, err
	}
	endpoint, err := s.promptValidated("Server URL", false, "", validateServerURL)
	if err != nil {
		return false, err
	}
	code, err := s.promptValidated("Enrollment code (hidden)", true, "", validateEnrollmentCode)
	if err != nil {
		return false, err
	}
	return false, runProgress(s.ctx, s.app, "Adding server "+alias, func() error {
		return s.app.ConnectServerProfile(s.ctx, connections.ConnectOptions{Mode: connections.EnrollmentCreate, Alias: alias, Purpose: purpose, Priority: 100, BaseURL: endpoint, Code: code})
	})
}

func (s *menuSession) manageServer(alias string) (bool, error) {
	for {
		value, err := s.app.ShowServer(alias)
		if err != nil {
			return false, err
		}
		value = s.observedServer(value)
		actions := []menuAction{
			{id: "servers.test", label: "Test connection", description: "Read-only authenticated Memory/Context probe", run: s.simple(func() error {
				var tested app.ServerView
				err := runProgress(s.ctx, s.app, "Testing server "+alias, func() error { var err error; tested, err = s.app.TestServer(s.ctx, alias); return err })
				if tested.ID != "" {
					if s.serverHealth == nil {
						s.serverHealth = map[string]serverObservation{}
					}
					s.serverHealth[tested.ID] = serverObservation{tested, time.Now()}
					_ = writeServerViews(s.app.Out, []app.ServerView{tested}, false)
				}
				return err
			})},
			{id: "servers.toggle", label: toggleLabel("Server", value.Enabled), description: "Preserve the credential; applies to new sessions", run: s.simple(func() error { delete(s.serverHealth, value.ID); return s.app.SetServerEnabled(alias, !value.Enabled) })},
			{id: "servers.edit", label: "Edit metadata", description: "Purpose and redundancy; stable alias and identity are preserved", run: func() (bool, error) { return s.editServer(value) }},
			{id: "servers.re-enroll", label: "Re-enroll / rotate connection", description: "Replace only this connection using a fresh one-time code", run: func() (bool, error) { delete(s.serverHealth, value.ID); return s.reenrollServer(value) }},
			{id: "servers.remove", label: "Remove server", description: "Remove only this profile and its credential; confirmation required", run: func() (bool, error) {
				if !s.confirm("REMOVE " + alias) {
					fmt.Fprintln(s.app.Out, "Cancelled.")
					return false, nil
				}
				if err := s.app.DisconnectServerProfile(s.ctx, alias, false); err != nil {
					return false, err
				}
				delete(s.serverHealth, value.ID)
				fmt.Fprintln(s.app.Out, "Server removed:", alias)
				return true, nil
			}},
		}
		badges := []terminalui.Badge{{Label: "Profile", Value: serverSummary(value)}, {Label: "URL", Value: value.URL}, {Label: "Credential", Value: configured(value.CredentialConfigured)}, {Label: "Memory", Value: value.Health.MemoryState}, {Label: "Context", Value: value.Health.ContextState}}
		id, err := s.choose("Server: "+alias, actions, badges)
		if err != nil || id == "" {
			return false, err
		}
		removed, err := s.execute(findAction(actions, id))
		if err != nil {
			fmt.Fprintln(s.app.Err, "Error:", UserError(err))
		}
		if removed {
			return false, nil
		}
		terminalui.Pause(s.app.In, s.app.Out)
	}
}

func (s *menuSession) reenrollServer(value app.ServerView) (bool, error) {
	endpoint, err := s.promptValidated("Server URL", false, value.URL, validateServerURL)
	if err != nil {
		return false, err
	}
	if !s.confirm("RE-ENROLL " + value.Alias) {
		fmt.Fprintln(s.app.Out, "Cancelled.")
		return false, nil
	}
	code, err := s.promptValidated("Enrollment code (hidden)", true, "", validateEnrollmentCode)
	if err != nil {
		return false, err
	}
	return false, runProgress(s.ctx, s.app, "Re-enrolling server "+value.Alias, func() error {
		return s.app.ConnectServerProfile(s.ctx, connections.ConnectOptions{Mode: connections.EnrollmentReplace, Alias: value.Alias, BaseURL: endpoint, Code: code})
	})
}

func (s *menuSession) editServer(value app.ServerView) (bool, error) {
	purpose, err := s.promptValidated("Knowledge purpose", false, value.Purpose, func(value string) error { return serverpool.ValidateLabel("purpose", value) })
	if err != nil {
		return false, err
	}
	group, err := s.prompt("Redundancy group (- clears)", false, value.RedundancyGroup)
	if err != nil {
		return false, err
	}
	if group == "-" {
		group = ""
	}
	priority, err := s.prompt("Priority (lower first)", false, strconv.Itoa(value.Priority))
	if err != nil {
		return false, err
	}
	n, err := strconv.Atoi(priority)
	if err != nil {
		return false, errors.New("priority must be an integer")
	}
	delete(s.serverHealth, value.ID)
	return false, s.app.EditServerMetadata(value.Alias, connections.ProfileMetadata{Purpose: purpose, RedundancyGroup: group, Priority: n})
}

func editServerMetadata(a *app.App, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ivoai connect server edit <alias> [--purpose PURPOSE] [--redundancy-group GROUP] [--priority N]")
	}
	value, err := a.ShowServer(args[0])
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("connect server edit", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	purpose := fs.String("purpose", value.Purpose, "knowledge purpose")
	group := fs.String("redundancy-group", value.RedundancyGroup, "redundancy group (empty clears)")
	priority := fs.Int("priority", value.Priority, "lower first")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected server metadata arguments")
	}
	return a.EditServerMetadata(args[0], connections.ProfileMetadata{Purpose: *purpose, RedundancyGroup: *group, Priority: *priority})
}
