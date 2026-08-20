package connections

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type AgentAuth struct {
	Runner   platform.Runner
	Store    *config.Store
	In       io.Reader
	Out, Err io.Writer
	Binary   string
}

func (a AgentAuth) Connect(ctx context.Context, target string) error {
	command, statusArgs, loginArgs, err := authCommands(target)
	if err != nil {
		return err
	}
	path := a.Binary
	if path == "" {
		path, err = a.Runner.LookPath(command)
	}
	if err != nil || path == "" {
		return fmt.Errorf("%s is not installed; run ivoai setup first", command)
	}
	if !a.authenticated(ctx, path, statusArgs) {
		options := platform.RunOptions{Stdin: a.In, Stdout: a.Out, Stderr: a.Err, TTY: true}
		if target == "claude" {
			options.Env = []string{"DISABLE_AUTOUPDATER=1"}
		}
		_, err = a.Runner.Run(ctx, path, loginArgs, options)
		if err != nil && target == "claude" {
			// Older stable Claude builds expose subscription login only inside
			// the official interactive client (/login), not `auth login`.
			_, err = a.Runner.Run(ctx, path, nil, options)
		}
		if err != nil {
			return fmt.Errorf("official %s login failed: %w", target, err)
		}
	}
	if !a.authenticated(ctx, path, statusArgs) {
		return fmt.Errorf("%s did not report an authenticated session after login", command)
	}
	c, err := a.Store.Load()
	if err != nil {
		return err
	}
	if target == "chatgpt" {
		c.Connections.ChatGPT.Status = "connected"
	} else {
		c.Connections.Claude.Status = "connected"
	}
	return a.Store.Save(c)
}

func (a AgentAuth) Disconnect(ctx context.Context, target string) error {
	_, _, _, err := authCommands(target)
	if err != nil {
		return err
	}
	_ = ctx // Disconnect only removes ivoai state; official client login remains owned by the user.
	c, err := a.Store.Load()
	if err != nil {
		return err
	}
	if target == "chatgpt" {
		c.Connections.ChatGPT.Status = "not-connected"
	} else {
		c.Connections.Claude.Status = "not-connected"
	}
	return a.Store.Save(c)
}

func (a AgentAuth) Status(ctx context.Context, target string) bool {
	command, statusArgs, _, err := authCommands(target)
	if err != nil {
		return false
	}
	path := a.Binary
	if path == "" {
		path, err = a.Runner.LookPath(command)
	}
	if err != nil || path == "" {
		return false
	}
	return a.authenticated(ctx, path, statusArgs)
}

func (a AgentAuth) authenticated(ctx context.Context, path string, args []string) bool {
	options := platform.RunOptions{Timeout: 15 * time.Second}
	if strings.Contains(strings.ToLower(filepath.Base(path)), "claude") {
		options.Env = []string{"DISABLE_AUTOUPDATER=1"}
	}
	r, err := a.Runner.Run(ctx, path, args, options)
	return AuthenticationStatus(r, err)
}

// AuthenticationStatus accepts the documented JSON and human-readable status
// forms emitted by Codex and Claude. Unknown successful output is deliberately
// treated as unauthenticated so ivoai never skips an official login based only
// on an exit code.
func AuthenticationStatus(result platform.Result, runErr error) bool {
	if runErr != nil {
		return false
	}
	raw := strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
	if raw == "" {
		return false
	}
	for _, candidate := range []string{strings.TrimSpace(result.Stdout), strings.TrimSpace(result.Stderr), raw} {
		if candidate == "" {
			continue
		}
		var value any
		if json.Unmarshal([]byte(candidate), &value) == nil {
			if authenticated, found := authenticationJSON(value); found {
				return authenticated
			}
		}
	}
	text := normalizeAuthText(raw)
	for _, denied := range []string{"not logged in", "not authenticated", "unauthenticated", "logged out", "signed out", "no active session", "login required", "loggedin false", "authenticated false"} {
		if strings.Contains(text, denied) {
			return false
		}
	}
	for _, accepted := range []string{"logged in", "authenticated", "authentication valid", "signed in", "loggedin true", "authenticated true"} {
		if strings.Contains(text, accepted) {
			return true
		}
	}
	return false
}

func authenticationJSON(value any) (bool, bool) {
	switch current := value.(type) {
	case map[string]any:
		for key, item := range current {
			normalizedKey := strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(key))
			switch normalizedKey {
			case "loggedin", "authenticated", "isauthenticated":
				if boolean, ok := item.(bool); ok {
					return boolean, true
				}
			case "status", "authstatus":
				if status, ok := item.(string); ok {
					if authenticated, found := authenticationStatusWord(status); found {
						return authenticated, true
					}
				}
			}
		}
		for _, item := range current {
			if authenticated, found := authenticationJSON(item); found {
				return authenticated, true
			}
		}
	case []any:
		for _, item := range current {
			if authenticated, found := authenticationJSON(item); found {
				return authenticated, true
			}
		}
	}
	return false, false
}

func authenticationStatusWord(status string) (bool, bool) {
	normalized := strings.NewReplacer("_", " ", "-", " ").Replace(strings.ToLower(strings.TrimSpace(status)))
	switch strings.Join(strings.Fields(normalized), " ") {
	case "authenticated", "logged in", "signed in", "connected", "valid":
		return true, true
	case "unauthenticated", "not authenticated", "not logged in", "logged out", "signed out", "disconnected", "invalid":
		return false, true
	default:
		return false, false
	}
}

func normalizeAuthText(text string) string {
	text = strings.ToLower(text)
	text = strings.NewReplacer("_", " ", "-", " ", `"`, " ", "'", " ", ":", " ", "{", " ", "}", " ").Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

func authCommands(target string) (string, []string, []string, error) {
	switch target {
	case "chatgpt":
		return "codex", []string{"login", "status"}, []string{"login"}, nil
	case "claude":
		return "claude", []string{"auth", "status"}, []string{"auth", "login"}, nil
	default:
		return "", nil, nil, fmt.Errorf("unsupported connection %q", target)
	}
}
