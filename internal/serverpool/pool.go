package serverpool

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
)

const MaxSelectedSources = 8

type Pool struct {
	profiles map[string]config.ServerProfile
}

type SourceGroup struct {
	Purpose         string
	RedundancyGroup string
	Profiles        []config.ServerProfile
}

type Selection struct {
	Groups []SourceGroup
}

func New(profiles map[string]config.ServerProfile) (Pool, error) {
	copy := make(map[string]config.ServerProfile, len(profiles))
	ids := map[string]string{}
	for alias, profile := range profiles {
		if err := ValidateAlias(alias); err != nil {
			return Pool{}, err
		}
		if profile.Alias != "" && profile.Alias != alias {
			return Pool{}, fmt.Errorf("server profile %q alias mismatch", alias)
		}
		if err := ValidateID(profile.ID); err != nil {
			return Pool{}, fmt.Errorf("server profile %q: %w", alias, err)
		}
		if previous, exists := ids[profile.ID]; exists {
			return Pool{}, fmt.Errorf("server profiles %q and %q share an identity", previous, alias)
		}
		ids[profile.ID] = alias
		profile.Alias = alias
		base, err := validateBaseURL(profile.URL)
		if err != nil {
			return Pool{}, fmt.Errorf("server profile %q: %w", alias, err)
		}
		for _, endpoint := range []string{profile.ContextMCPURL, profile.MemoryMCPURL, profile.MemoryHooksURL} {
			if endpoint != "" && !sameOrigin(base, endpoint) {
				return Pool{}, fmt.Errorf("server profile %q contains a cross-origin or invalid endpoint", alias)
			}
		}
		if profile.Purpose == "" {
			profile.Purpose = alias
		}
		if err := ValidateLabel("purpose", profile.Purpose); err != nil {
			return Pool{}, fmt.Errorf("server profile %q: %w", alias, err)
		}
		if profile.RedundancyGroup != "" {
			if err := ValidateLabel("redundancy group", profile.RedundancyGroup); err != nil {
				return Pool{}, fmt.Errorf("server profile %q: %w", alias, err)
			}
		}
		copy[alias] = profile
	}
	return Pool{profiles: copy}, nil
}

func validateBaseURL(raw string) (*url.URL, error) {
	value, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || value.Host == "" || value.User != nil || value.RawQuery != "" || value.Fragment != "" {
		return nil, errors.New("invalid server URL")
	}
	host := value.Hostname()
	ip := net.ParseIP(host)
	loopback := strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback()
	if value.Scheme != "https" && !(value.Scheme == "http" && loopback) {
		return nil, errors.New("server URL must use HTTPS outside loopback")
	}
	return value, nil
}

func sameOrigin(base *url.URL, raw string) bool {
	value, err := url.Parse(raw)
	return err == nil && value.IsAbs() && value.Scheme == base.Scheme && strings.EqualFold(value.Host, base.Host) && value.User == nil && value.Fragment == ""
}

func NewID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate server identity: %w", err)
	}
	return "srv_" + hex.EncodeToString(value[:]), nil
}

func ValidateAlias(value string) error { return ValidateLabel("server alias", value) }

func ValidateLabel(kind, value string) error {
	if len(value) < 1 || len(value) > 64 {
		return fmt.Errorf("%s must contain 1..64 characters", kind)
	}
	for i, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !valid || i == 0 && (r == '-' || r == '_') {
			return fmt.Errorf("%s contains unsafe characters", kind)
		}
	}
	return nil
}

func ValidateID(value string) error {
	if len(value) < 8 || len(value) > 128 || !strings.HasPrefix(value, "srv_") {
		return errors.New("invalid server identity")
	}
	for _, r := range value[4:] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return errors.New("invalid server identity")
		}
	}
	return nil
}

func (p Pool) Profiles() []config.ServerProfile {
	result := make([]config.ServerProfile, 0, len(p.profiles))
	for _, profile := range p.profiles {
		result = append(result, profile)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Priority != result[j].Priority {
			return result[i].Priority < result[j].Priority
		}
		return result[i].Alias < result[j].Alias
	})
	return result
}

func (p Pool) Get(alias string) (config.ServerProfile, bool) {
	profile, ok := p.profiles[alias]
	return profile, ok
}

// Resolve turns explicit aliases or purposes into logical source groups. With
// no selector, only a single enabled profile (or the legacy default) is safe.
func (p Pool) Resolve(selectors []string) (Selection, error) {
	if len(selectors) > MaxSelectedSources {
		return Selection{}, errors.New("too many knowledge sources selected")
	}
	profiles := p.Profiles()
	if len(selectors) == 0 {
		enabled := make([]config.ServerProfile, 0, len(profiles))
		for _, profile := range profiles {
			if profile.Enabled && profile.Status == "connected" {
				enabled = append(enabled, profile)
			}
		}
		if len(enabled) == 1 {
			profiles = enabled
		} else if profile, ok := p.profiles["default"]; ok && profile.Enabled && profile.Status == "connected" {
			profiles = []config.ServerProfile{profile}
		} else if len(enabled) == 0 {
			return Selection{}, nil
		} else {
			return Selection{}, errors.New("multiple knowledge purposes are connected; select a source explicitly")
		}
	} else {
		seen := map[string]bool{}
		profiles = nil
		for _, raw := range selectors {
			for _, selector := range strings.Split(raw, ",") {
				selector = strings.TrimSpace(selector)
				if err := ValidateLabel("knowledge source", selector); err != nil {
					return Selection{}, err
				}
				matched := false
				for alias, profile := range p.profiles {
					if alias != selector && profile.Purpose != selector {
						continue
					}
					matched = true
					if profile.Enabled && profile.Status == "connected" && !seen[alias] {
						profiles = append(profiles, profile)
						seen[alias] = true
					}
				}
				if !matched {
					return Selection{}, fmt.Errorf("unknown knowledge source %q", selector)
				}
			}
		}
	}
	groups := map[string][]config.ServerProfile{}
	for _, profile := range profiles {
		group := profile.RedundancyGroup
		if group == "" {
			group = "server:" + profile.ID
		}
		key := profile.Purpose + "\x00" + group
		groups[key] = append(groups[key], profile)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := Selection{Groups: make([]SourceGroup, 0, len(keys))}
	for _, key := range keys {
		members := groups[key]
		sort.Slice(members, func(i, j int) bool {
			if members[i].Priority != members[j].Priority {
				return members[i].Priority < members[j].Priority
			}
			return members[i].Alias < members[j].Alias
		})
		result.Groups = append(result.Groups, SourceGroup{Purpose: members[0].Purpose, RedundancyGroup: members[0].RedundancyGroup, Profiles: members})
	}
	return result, nil
}

func (s Selection) PurposeCount() int {
	seen := map[string]bool{}
	for _, group := range s.Groups {
		seen[group.Purpose] = true
	}
	return len(seen)
}
