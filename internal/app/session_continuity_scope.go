package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/serverpool"
)

// conversationScopeID describes the sources a conversation may use, not the
// transient subset selected for an idle composer or one prompt. Health and
// credentials are deliberately absent: neither changes organization identity.
func conversationScopeID(cwd string, cfg config.Config, selectors []string) (string, error) {
	selected, err := conversationScopeSelection(cfg, selectors)
	if err != nil {
		return "", err
	}
	sources := []string{}
	for _, group := range selected.Groups {
		for _, p := range group.Profiles {
			fields := []string{p.ID, p.URL, p.ContextMCPURL, p.MemoryMCPURL, p.MemoryHooksURL, p.Purpose}
			body, _ := json.Marshal(fields)
			sources = append(sources, string(body))
		}
	}
	// Legacy manually registered knowledge servers are also privacy boundaries.
	for name, p := range cfg.MCP.Servers {
		if p.Enabled && (p.Kind == "memory" || p.Kind == "context") {
			body, _ := json.Marshal([]string{name, p.ID, p.Kind, p.URL, p.HooksURL})
			sources = append(sources, string(body))
		}
	}
	sort.Strings(sources)
	body, _ := json.Marshal(struct {
		Directory string
		Sources   []string
	}{cwd, sources})
	digest := sha256.Sum256(body)
	return fmt.Sprintf("cs_v1_%x", digest[:16]), nil
}

func verifyConversationScope(previous, current string) error {
	if !strings.HasPrefix(previous, "cs_v1_") {
		return errors.New("SESSION_KNOWLEDGE_SCOPE_UNVERIFIED: retained history predates verified organization scope; preview explicit scope adoption or start a separate conversation; existing history is preserved")
	}
	if previous != current {
		return errors.New("SESSION_KNOWLEDGE_SCOPE_MISMATCH: restore the original source selection or start a separate conversation")
	}
	return nil
}

func conversationScopeSelection(cfg config.Config, selectors []string) (serverpool.Selection, error) {
	profiles := make(map[string]config.ServerProfile, len(cfg.Connections.Servers))
	for alias, p := range cfg.Connections.Servers {
		p.Status = "connected"
		profiles[alias] = p
	}
	pool, err := serverpool.New(profiles)
	if err != nil {
		return serverpool.Selection{}, err
	}
	return pool.Resolve(selectors)
}
