package quota

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type ClaudeAdapter struct {
	Binary string
	Runner platform.Runner
	Store  Store
	TTL    time.Duration
	Now    func() time.Time
}

func (a ClaudeAdapter) Probe(ctx context.Context) (ProviderQuota, error) {
	now := time.Now().UTC()
	if a.Now != nil {
		now = a.Now().UTC()
	}
	result := ProviderQuota{Provider: ProviderClaude, Eligible: true, Authenticated: true, Source: "claude statusline", ObservedAt: now, Reason: "awaiting first response"}
	result.Windows = []Window{Unavailable(KindSession, TelemetryPending, result.Source, now), Unavailable(KindWeekly, TelemetryPending, result.Source, now)}
	if a.Binary == "" || !filepath.IsAbs(a.Binary) || filepath.Base(a.Binary) != "claude" {
		result.Authenticated, result.Eligible, result.Reason = false, false, "Claude executable unavailable"
		return result, errors.New(result.Reason)
	}
	auth, err := a.Runner.Run(ctx, a.Binary, []string{"auth", "status"}, platform.RunOptions{Timeout: 10 * time.Second})
	if err != nil || !claudeAuthenticated(auth.Stdout) {
		result.Authenticated, result.Eligible, result.Reason = false, false, "Claude authentication unavailable"
		return result, errors.New(result.Reason)
	}
	snapshot, loadErr := a.Store.Load()
	if loadErr != nil {
		return result, loadErr
	}
	if cached, ok := snapshot.Providers[ProviderClaude]; ok {
		result = cached
		result.Authenticated = true
		result.Eligible = !result.HardLimitReached
		if now.Sub(result.ObservedAt) > a.ttl() {
			result.Reason = "stale quota telemetry; awaiting a current Claude response"
			for index := range result.Windows {
				if result.Windows[index].Available {
					result.Windows[index].State = TelemetryStale
				}
			}
		}
	}
	return result, nil
}

func claudeAuthenticated(body string) bool {
	var value struct {
		LoggedIn    bool   `json:"loggedIn"`
		APIProvider string `json:"apiProvider"`
		AuthMethod  string `json:"authMethod"`
	}
	return json.Unmarshal([]byte(body), &value) == nil && value.LoggedIn && value.APIProvider == "firstParty" && value.AuthMethod != "apiKey"
}

type ClaudeStatusline struct {
	Model struct {
		ID string `json:"id"`
	} `json:"model"`
	Context struct {
		Used      *float64 `json:"used_percentage"`
		Remaining *float64 `json:"remaining_percentage"`
	} `json:"context_window"`
	RateLimits *struct {
		FiveHour *claudeLimit `json:"five_hour"`
		SevenDay *claudeLimit `json:"seven_day"`
		Monthly  *claudeLimit `json:"monthly"`
	} `json:"rate_limits"`
}

type claudeLimit struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}

func ParseClaudeStatusline(body []byte, observed time.Time) (ProviderQuota, error) {
	if len(body) == 0 || len(body) > 64<<10 {
		return ProviderQuota{}, errors.New("invalid Claude statusline payload size")
	}
	var input *ClaudeStatusline
	if err := json.Unmarshal(body, &input); err != nil {
		return ProviderQuota{}, errors.New("invalid Claude statusline payload")
	}
	if input == nil {
		return ProviderQuota{}, errors.New("invalid Claude statusline payload")
	}
	value := ProviderQuota{Provider: ProviderClaude, Model: safeModel(input.Model.ID), Authenticated: true, Eligible: true, Source: "claude statusline", ObservedAt: observed.UTC()}
	if input.Context.Used != nil {
		value.Windows = append(value.Windows, FromUsed(KindContext, *input.Context.Used, nil, "claude statusline", observed.UTC()))
	} else if input.Context.Remaining != nil {
		remaining := Clamp(*input.Context.Remaining)
		value.Windows = append(value.Windows, Window{Kind: KindContext, UsedPercent: Clamp(100 - remaining), RemainingPercent: remaining, Source: "claude statusline", ObservedAt: observed.UTC(), Authoritative: true, Available: true})
	}
	if input.RateLimits == nil {
		value.Reason = "awaiting first response"
		value.Windows = append(value.Windows,
			Unavailable(KindSession, TelemetryPending, value.Source, observed),
			Unavailable(KindWeekly, TelemetryPending, value.Source, observed),
		)
		return value, nil
	}
	for _, item := range []struct {
		kind  Kind
		limit *claudeLimit
	}{{KindSession, input.RateLimits.FiveHour}, {KindWeekly, input.RateLimits.SevenDay}, {KindMonthly, input.RateLimits.Monthly}} {
		kind, limit := item.kind, item.limit
		if limit == nil {
			if kind != KindMonthly {
				value.Windows = append(value.Windows, Unavailable(kind, TelemetryNotExposed, value.Source, observed))
			}
			continue
		}
		var reset *time.Time
		if limit.ResetsAt > 0 {
			parsed := time.Unix(limit.ResetsAt, 0).UTC()
			reset = &parsed
		}
		window := FromUsed(kind, limit.UsedPercentage, reset, "claude statusline", observed.UTC())
		value.Windows = append(value.Windows, window)
		if kind != KindContext && window.RemainingPercent <= 0 {
			value.HardLimitReached, value.Eligible, value.Reason = true, false, "Claude subscription quota exhausted"
		}
	}
	return value, nil
}

func safeModel(value string) string {
	value = strings.Map(func(char rune) rune {
		if char < 0x20 || char == '\x7f' || char == '\x1b' {
			return -1
		}
		return char
	}, value)
	if len(value) > 128 {
		return value[:128]
	}
	return value
}

func (a ClaudeAdapter) ttl() time.Duration {
	if a.TTL < 30*time.Second || a.TTL > 5*time.Minute {
		return 45 * time.Second
	}
	return a.TTL
}

func IsClaudeLimitError(value string) bool {
	lower := strings.ToLower(value)
	for _, contradiction := range []string{"authentication", "unauthorized", "network error", "connection refused", "connection reset", "mcp server", "mcp failure"} {
		if strings.Contains(lower, contradiction) {
			return false
		}
	}
	for _, marker := range []string{"rate limit reached", "usage limit reached", "subscription limit", "quota exhausted", "spend control reached"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func IsCodexLimitError(value string) bool { return IsClaudeLimitError(value) }

func executableAvailable(path, name string) bool {
	if path == "" || !filepath.IsAbs(path) || filepath.Base(path) != name {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}
