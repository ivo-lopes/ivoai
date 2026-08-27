package quota

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type fakeProbe struct {
	value ProviderQuota
	err   error
	calls int
}

type quotaRunner struct{ result platform.Result }

func (r quotaRunner) LookPath(string) (string, error) { return "", errors.New("not found") }
func (r quotaRunner) Run(context.Context, string, []string, platform.RunOptions) (platform.Result, error) {
	return r.result, nil
}

func (p *fakeProbe) Probe(context.Context) (ProviderQuota, error) {
	p.calls++
	return p.value, p.err
}

func TestRemainingClampAndCodexWindows(t *testing.T) {
	observed := time.Unix(100, 0).UTC()
	reset := int64(200)
	session := int64(300)
	weekly := int64(10080)
	payload := codexRateResponse{RateLimits: codexLimit{
		Primary:         &codexWindow{UsedPercent: 25, WindowDurationMins: &session, ResetsAt: &reset},
		Secondary:       &codexWindow{UsedPercent: 120, WindowDurationMins: &weekly},
		IndividualLimit: &codexIndividualLimit{RemainingPercent: 71, ResetsAt: 300},
	}}
	windows := codexWindows(payload, observed)
	fiveHour, fiveOK := windowByDuration(windows, 300)
	weeklyWindow, weeklyOK := windowByDuration(windows, 10080)
	individual := Window{}
	for _, window := range windows {
		if window.Kind == KindIndividual {
			individual = window
		}
	}
	if len(windows) != 3 || !fiveOK || fiveHour.Kind != KindRolling || fiveHour.RemainingPercent != 75 || !weeklyOK || weeklyWindow.Kind != KindWeekly || weeklyWindow.RemainingPercent != 0 || individual.Kind != KindIndividual || individual.RemainingPercent != 71 {
		t.Fatalf("windows=%+v", windows)
	}
	if Clamp(-1) != 0 || Clamp(101) != 100 || math.Abs(FromUsed(KindWeekly, 33.5, nil, "test", observed).RemainingPercent-66.5) > 0.001 {
		t.Fatal("percentage normalization failed")
	}
}

func TestCodexAdapterUsesStructuredAppServerProtocol(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "codex")
	script := `#!/bin/sh
read -r initialize
printf '%s\n' '{"id":1,"result":{"protocolVersion":"1"}}'
read -r initialized
read -r limits
printf '%s\n' '{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":14,"windowDurationMins":300,"resetsAt":200},"secondary":{"usedPercent":29,"windowDurationMins":10080,"resetsAt":300},"individualLimit":{"remainingPercent":82,"resetsAt":400}},"rateLimitsByLimitId":{}}}'
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	value, err := (CodexAdapter{Binary: path}).Probe(context.Background())
	if err != nil || !value.Authenticated || !value.Eligible {
		t.Fatalf("value=%+v err=%v", value, err)
	}
	for duration, remaining := range map[int64]float64{300: 86, 10080: 71} {
		window, ok := value.WindowByDuration(duration)
		if !ok || window.RemainingPercent != remaining || window.Source != "codex app-server" {
			t.Fatalf("duration=%d window=%+v", duration, window)
		}
	}
	if individual, ok := value.Window(KindIndividual); !ok || individual.RemainingPercent != 82 {
		t.Fatalf("individual=%+v", individual)
	}
}

func TestCodexPrimarySecondaryOrderIsSemanticallyEquivalent(t *testing.T) {
	observed := time.Unix(100, 0).UTC()
	five, weekly := int64(300), int64(10080)
	a := codexWindows(codexRateResponse{RateLimits: codexLimit{Primary: &codexWindow{UsedPercent: 20, WindowDurationMins: &five}, Secondary: &codexWindow{UsedPercent: 30, WindowDurationMins: &weekly}}}, observed)
	b := codexWindows(codexRateResponse{RateLimits: codexLimit{Primary: &codexWindow{UsedPercent: 30, WindowDurationMins: &weekly}, Secondary: &codexWindow{UsedPercent: 20, WindowDurationMins: &five}}}, observed)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("primary/secondary order changed semantics:\nA=%+v\nB=%+v", a, b)
	}
}

func TestCodexWindowFixturesCoverDurationOrderAndMissingTelemetry(t *testing.T) {
	observed := time.Unix(100, 0).UTC()
	normal := codexWindows(loadCodexFixture(t, "codex_300_10080.json"), observed)
	inverted := codexWindows(loadCodexFixture(t, "codex_10080_300.json"), observed)
	if !reflect.DeepEqual(normal, inverted) {
		t.Fatalf("inverted fixture changed normalized result:\nnormal=%+v\ninverted=%+v", normal, inverted)
	}
	weeklyOnly := codexWindows(loadCodexFixture(t, "codex_weekly_only.json"), observed)
	fiveHour, ok := windowByDuration(weeklyOnly, 300)
	if !ok || fiveHour.TelemetryState() != TelemetryNotExposed || fiveHour.Available {
		t.Fatalf("weekly-only fixture fabricated 5h: %+v", weeklyOnly)
	}
	other := codexWindows(loadCodexFixture(t, "codex_60_1440.json"), observed)
	for _, duration := range []int64{60, 1440} {
		if window, ok := windowByDuration(other, duration); !ok || !window.Available {
			t.Fatalf("duration %d fixture missing: %+v", duration, other)
		}
	}
	individual := codexWindows(loadCodexFixture(t, "codex_individual.json"), observed)
	if window, ok := (ProviderQuota{Windows: individual}).Window(KindIndividual); !ok || window.RemainingPercent != 42 {
		t.Fatalf("individual fixture missing: %+v", individual)
	}
}

func loadCodexFixture(t *testing.T, name string) codexRateResponse {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var value codexRateResponse
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCodexPreservesArbitraryDurationsAndMissingFiveHour(t *testing.T) {
	observed := time.Unix(100, 0).UTC()
	oneHour, oneDay := int64(60), int64(1440)
	windows := codexWindows(codexRateResponse{RateLimits: codexLimit{Primary: &codexWindow{UsedPercent: 10, WindowDurationMins: &oneHour}, Secondary: &codexWindow{UsedPercent: 20, WindowDurationMins: &oneDay}}}, observed)
	for _, duration := range []int64{60, 1440} {
		window, ok := windowByDuration(windows, duration)
		if !ok || window.Kind != KindRolling || !window.Available {
			t.Fatalf("duration %d was not preserved: %+v", duration, windows)
		}
	}
	fiveHour, ok := windowByDuration(windows, 300)
	if !ok || fiveHour.TelemetryState() != TelemetryNotExposed || fiveHour.Available || fiveHour.RemainingPercent != 0 {
		t.Fatalf("missing 5h was fabricated or lost: %+v", fiveHour)
	}
}

func TestCodexIndividualLimitIsNotMonthly(t *testing.T) {
	windows := codexWindows(codexRateResponse{RateLimits: codexLimit{IndividualLimit: &codexIndividualLimit{RemainingPercent: 42, ResetsAt: 200}}}, time.Unix(100, 0))
	individual, ok := func() (Window, bool) {
		for _, window := range windows {
			if window.Kind == KindIndividual {
				return window, true
			}
		}
		return Window{}, false
	}()
	if !ok || individual.RemainingPercent != 42 {
		t.Fatalf("individual limit missing: %+v", windows)
	}
	if _, monthly := (ProviderQuota{Windows: windows}).Window(KindMonthly); monthly {
		t.Fatalf("individual limit was mislabeled monthly: %+v", windows)
	}
}

func TestCodexFiveHourExhaustionHasCanonicalReasonAndMissingDoesNotBlock(t *testing.T) {
	exhausted := FromUsedDuration(KindRolling, 300, 100, nil, "fixture", time.Now())
	if reason := codexWindowExhaustedReason(exhausted); reason != "Codex 5-hour quota exhausted" {
		t.Fatalf("reason=%q", reason)
	}
	missing := UnavailableDuration(KindRolling, 300, TelemetryNotExposed, "fixture", time.Now())
	value := ProviderQuota{Provider: ProviderCodex, Authenticated: true, Eligible: true, Windows: []Window{missing}}
	if !eligibleForModel(value, "") {
		t.Fatalf("missing 5h incorrectly blocked Codex: %+v", value)
	}
}

func TestFiveHourHardLimitFallbackAndOfficialReprobeReenable(t *testing.T) {
	now := time.Now().UTC()
	fiveReset := now.Add(time.Hour)
	codex := &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, Authenticated: true, HardLimitReached: true, Reason: "Codex 5-hour quota exhausted", Windows: []Window{FromUsedDuration(KindRolling, 300, 100, &fiveReset, "fixture", now)}, ObservedAt: now}}
	claude := &fakeProbe{value: ProviderQuota{Provider: ProviderClaude, Authenticated: true, Windows: []Window{FromUsedDuration(KindSession, 300, 20, nil, "fixture", now)}, ObservedAt: now}}
	manager := Manager{Store: Store{Root: filepath.Join(t.TempDir(), "quota")}, Probes: map[Provider]Probe{ProviderCodex: codex, ProviderClaude: claude}, Now: func() time.Time { return now }}
	decision, err := manager.Resolve(context.Background(), ProviderCodex, "", true)
	if err != nil || decision.Resolved != ProviderClaude || !decision.Fallback || decision.Reason != "Codex 5-hour quota exhausted" {
		t.Fatalf("5h hard limit did not route to Claude: %+v err=%v", decision, err)
	}
	now = fiveReset.Add(time.Second)
	codex.value = ProviderQuota{Provider: ProviderCodex, Authenticated: true, Windows: []Window{FromUsedDuration(KindRolling, 300, 20, nil, "fixture", now)}, ObservedAt: now}
	decision, err = manager.Resolve(context.Background(), ProviderCodex, "", true)
	if err != nil || decision.Resolved != ProviderCodex || decision.Fallback {
		t.Fatalf("valid post-reset probe did not re-enable Codex: %+v err=%v", decision, err)
	}
}

func TestMissingFiveHourDoesNotCauseFalseFallback(t *testing.T) {
	now := time.Now().UTC()
	codex := &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, Authenticated: true, Windows: []Window{UnavailableDuration(KindRolling, 300, TelemetryNotExposed, "fixture", now), FromUsedDuration(KindWeekly, 10080, 20, nil, "fixture", now)}, ObservedAt: now}}
	manager := Manager{Store: Store{Root: filepath.Join(t.TempDir(), "quota")}, Probes: map[Provider]Probe{ProviderCodex: codex}, Now: func() time.Time { return now }}
	decision, err := manager.Resolve(context.Background(), ProviderCodex, "", true)
	if err != nil || decision.Resolved != ProviderCodex || decision.Fallback {
		t.Fatalf("missing 5h caused false fallback: %+v err=%v", decision, err)
	}
}

func TestClaudeAdapterAcceptsOnlyFirstPartySubscriptionAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude")
	adapter := ClaudeAdapter{Binary: path, Runner: quotaRunner{result: platform.Result{Stdout: `{"loggedIn":true,"apiProvider":"firstParty","authMethod":"oauth"}`}}, Store: Store{Root: filepath.Join(t.TempDir(), "quota")}}
	value, err := adapter.Probe(context.Background())
	if err != nil || !value.Authenticated || !value.Eligible {
		t.Fatalf("subscription auth rejected: %+v %v", value, err)
	}
	adapter.Runner = quotaRunner{result: platform.Result{Stdout: `{"loggedIn":true,"apiProvider":"firstParty","authMethod":"apiKey"}`}}
	if value, err = adapter.Probe(context.Background()); err == nil || value.Authenticated {
		t.Fatalf("PAYG auth accepted: %+v %v", value, err)
	}
}

func TestQuotaProbeEnvironmentRemovesPAYGCredentials(t *testing.T) {
	for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_BASE_URL"} {
		t.Setenv(key, "must-not-leak")
	}
	for _, entry := range subscriptionEnvironment("/tmp/codex") {
		if strings.Contains(entry, "must-not-leak") {
			t.Fatalf("PAYG credential leaked to quota probe: %s", entry)
		}
	}
}

func TestClaudeStatuslineSeparatesContextSessionWeeklyAndMonthly(t *testing.T) {
	body := []byte(`{"model":{"id":"claude-test"},"context_window":{"used_percentage":26},"rate_limits":{"five_hour":{"used_percentage":14,"resets_at":200},"seven_day":{"used_percentage":36,"resets_at":300},"monthly":{"used_percentage":29,"resets_at":400}}}`)
	value, err := ParseClaudeStatusline(body, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	for kind, remaining := range map[Kind]float64{KindContext: 74, KindSession: 86, KindWeekly: 64, KindMonthly: 71} {
		window, ok := value.Window(kind)
		if !ok || window.RemainingPercent != remaining || !window.Authoritative {
			t.Fatalf("kind=%s window=%+v", kind, window)
		}
	}
	if value.Model != "claude-test" || !value.Eligible {
		t.Fatalf("quota=%+v", value)
	}
}

func TestClaudeOfficialRateLimitFixturePreservesResetsAndDecimals(t *testing.T) {
	observed := time.Unix(1738000000, 0).UTC()
	value, err := ParseClaudeStatusline([]byte(`{
  "rate_limits": {
    "five_hour": {"used_percentage": 23.5, "resets_at": 1738425600},
    "seven_day": {"used_percentage": 41.2, "resets_at": 1738857600}
  }
}`), observed)
	if err != nil {
		t.Fatal(err)
	}
	fiveHour, ok := value.Window(KindSession)
	if !ok || math.Abs(fiveHour.RemainingPercent-76.5) > 0.001 || fiveHour.ResetsAt == nil || fiveHour.ResetsAt.Unix() != 1738425600 {
		t.Fatalf("five-hour window=%+v", fiveHour)
	}
	weekly, ok := value.Window(KindWeekly)
	if !ok || math.Abs(weekly.RemainingPercent-58.8) > 0.001 || weekly.ResetsAt == nil || weekly.ResetsAt.Unix() != 1738857600 {
		t.Fatalf("weekly window=%+v", weekly)
	}
	if fiveHour.TelemetryState() != TelemetryAvailable || weekly.TelemetryState() != TelemetryAvailable {
		t.Fatalf("telemetry states=%s/%s", fiveHour.TelemetryState(), weekly.TelemetryState())
	}
}

func TestClaudeMissingRateLimitsProgressesFromPendingToNotExposed(t *testing.T) {
	observed := time.Now().UTC()
	pending, err := ParseClaudeStatusline([]byte(`{"model":{"id":"claude-test"}}`), observed)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []Kind{KindSession, KindWeekly} {
		window, ok := pending.Window(kind)
		if !ok || window.TelemetryState() != TelemetryPending || window.Available {
			t.Fatalf("pending %s=%+v", kind, window)
		}
	}
	notExposed, err := ParseClaudeStatusline([]byte(`{"rate_limits":{}}`), observed)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []Kind{KindSession, KindWeekly} {
		window, ok := notExposed.Window(kind)
		if !ok || window.TelemetryState() != TelemetryNotExposed || window.Available {
			t.Fatalf("not-exposed %s=%+v", kind, window)
		}
	}
}

func TestClaudeStatuslineBoundsAndInvalidPayloads(t *testing.T) {
	value, err := ParseClaudeStatusline([]byte(`{"rate_limits":{"five_hour":{"used_percentage":-1},"seven_day":{"used_percentage":101}}}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fiveHour, _ := value.Window(KindSession)
	weekly, _ := value.Window(KindWeekly)
	if fiveHour.RemainingPercent != 100 || weekly.RemainingPercent != 0 || !value.HardLimitReached || value.Eligible {
		t.Fatalf("clamped value=%+v", value)
	}
	for name, body := range map[string][]byte{
		"empty": {}, "malformed": []byte(`{"rate_limits":`), "null": []byte(`null`),
		"oversize": make([]byte, (64<<10)+1), "nan": []byte(`{"rate_limits":{"five_hour":{"used_percentage":NaN}}}`),
	} {
		if _, err := ParseClaudeStatusline(body, time.Now()); err == nil {
			t.Fatalf("%s payload accepted", name)
		}
	}
}

func TestLimitClassificationRequiresProviderQuotaEvidence(t *testing.T) {
	for _, message := range []string{"usage limit reached", "subscription limit reached", "rate limit reached", "quota exhausted", "spend control reached"} {
		if !IsClaudeLimitError(message) {
			t.Fatalf("quota signal not classified: %q", message)
		}
	}
	for _, message := range []string{"network error while checking rate limit", "authentication failed: usage limit unknown", "MCP server failure: limit reached", "connection refused"} {
		if IsClaudeLimitError(message) {
			t.Fatalf("non-quota error misclassified: %q", message)
		}
	}
}

func TestClaudeMonthlyUnavailableIsNotFabricated(t *testing.T) {
	value, err := ParseClaudeStatusline([]byte(`{"rate_limits":{"seven_day":{"used_percentage":50,"resets_at":300}}}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := value.Window(KindMonthly); ok {
		t.Fatal("monthly quota was fabricated")
	}
}

func TestManagerCachesRoutesAndNeverDispatchesExhaustedProvider(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	codex := &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, Authenticated: true, HardLimitReached: true, Reason: "weekly quota exhausted", ObservedAt: now}}
	claude := &fakeProbe{value: ProviderQuota{Provider: ProviderClaude, Authenticated: true, Eligible: true, ObservedAt: now}}
	manager := Manager{Store: Store{Root: filepath.Join(t.TempDir(), "quota")}, Probes: map[Provider]Probe{ProviderCodex: codex, ProviderClaude: claude}, TTL: 45 * time.Second, Now: func() time.Time { return now }}
	decision, err := manager.Resolve(context.Background(), ProviderCodex, "", true)
	if err != nil || decision.Resolved != ProviderClaude || !decision.Fallback || codex.calls != 1 || claude.calls != 1 {
		t.Fatalf("decision=%+v err=%v calls=%d/%d", decision, err, codex.calls, claude.calls)
	}
	if _, err := manager.Probe(context.Background(), ProviderClaude, false); err != nil || claude.calls != 1 {
		t.Fatalf("cache not used err=%v calls=%d", err, claude.calls)
	}
}

func TestManagerBothExhaustedBlocksWithoutTreatingUnknownAsZero(t *testing.T) {
	now := time.Now().UTC()
	manager := Manager{Store: Store{Root: filepath.Join(t.TempDir(), "quota")}, Probes: map[Provider]Probe{
		ProviderCodex:  &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, Authenticated: true, HardLimitReached: true, Reason: "weekly exhausted", ObservedAt: now}},
		ProviderClaude: &fakeProbe{value: ProviderQuota{Provider: ProviderClaude, Authenticated: true, HardLimitReached: true, Reason: "session exhausted", ObservedAt: now}},
	}, Now: func() time.Time { return now }}
	decision, err := manager.Resolve(context.Background(), ProviderClaude, "", true)
	if err == nil || decision.Resolved != "" {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	unknown := &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, Authenticated: true, Eligible: true, ObservedAt: now}}
	manager.Probes[ProviderCodex] = unknown
	decision, err = manager.Resolve(context.Background(), ProviderCodex, "", true)
	if err != nil || decision.Resolved != ProviderCodex {
		t.Fatalf("unknown quota should remain eligible: %+v %v", decision, err)
	}
}

func TestStaleCacheRefreshes(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	probe := &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, Authenticated: true, Eligible: true, ObservedAt: now}}
	manager := Manager{Store: Store{Root: filepath.Join(t.TempDir(), "quota")}, Probes: map[Provider]Probe{ProviderCodex: probe}, TTL: 30 * time.Second, Now: func() time.Time { return now }}
	if _, err := manager.Probe(context.Background(), ProviderCodex, false); err != nil {
		t.Fatal(err)
	}
	now = now.Add(31 * time.Second)
	probe.value.ObservedAt = now
	if _, err := manager.Probe(context.Background(), ProviderCodex, false); err != nil || probe.calls != 2 {
		t.Fatalf("err=%v calls=%d", err, probe.calls)
	}
}

func TestProbeFailureUsesStaleMetadataButReturnsError(t *testing.T) {
	now := time.Now().UTC()
	probe := &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, Authenticated: true, Eligible: true, ObservedAt: now}}
	manager := Manager{Store: Store{Root: filepath.Join(t.TempDir(), "quota")}, Probes: map[Provider]Probe{ProviderCodex: probe}, Now: func() time.Time { return now }}
	if _, err := manager.Probe(context.Background(), ProviderCodex, true); err != nil {
		t.Fatal(err)
	}
	probe.err = errors.New("offline")
	now = now.Add(time.Minute)
	value, err := manager.Probe(context.Background(), ProviderCodex, true)
	if err == nil || !value.Eligible || value.Reason == "" {
		t.Fatalf("value=%+v err=%v", value, err)
	}
}

func TestAuthenticationTransitionInvalidatesPreviousAccountAndReprobes(t *testing.T) {
	now := time.Now().UTC()
	store := Store{Root: filepath.Join(t.TempDir(), "quota")}
	accountA := ProviderQuota{Provider: ProviderCodex, Authenticated: true, Eligible: false, HardLimitReached: true, Reason: "old account exhausted", ObservedAt: now}
	if err := store.Put(accountA); err != nil {
		t.Fatal(err)
	}
	probe := &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, Authenticated: true, Windows: []Window{FromUsedDuration(KindRolling, 300, 20, nil, "fixture", now), FromUsedDuration(KindWeekly, 10080, 30, nil, "fixture", now)}, ObservedAt: now}}
	manager := Manager{Store: store, Probes: map[Provider]Probe{ProviderCodex: probe}, Now: func() time.Time { return now }}
	accountB, err := manager.AuthenticationChanged(context.Background(), ProviderCodex, true)
	if err != nil || !accountB.Eligible || accountB.HardLimitReached || probe.calls != 1 {
		t.Fatalf("new account contaminated by prior quota: value=%+v calls=%d err=%v", accountB, probe.calls, err)
	}
	five, fiveOK := accountB.WindowByDuration(300)
	weekly, weeklyOK := accountB.WindowByDuration(10080)
	if !fiveOK || !weeklyOK || five.RemainingPercent != 80 || weekly.RemainingPercent != 70 {
		t.Fatalf("new account windows=%+v", accountB.Windows)
	}
	snapshot, err := store.Load()
	if err != nil || snapshot.Providers[ProviderCodex].Reason == accountA.Reason {
		t.Fatalf("old account remained in cache: %+v err=%v", snapshot, err)
	}
}

func TestRuntimeMarkExhaustedDoesNotContaminateValidAuthenticationTransition(t *testing.T) {
	now := time.Now().UTC()
	store := Store{Root: filepath.Join(t.TempDir(), "quota")}
	probe := &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, Authenticated: true, Windows: []Window{FromUsedDuration(KindRolling, 300, 10, nil, "fixture", now)}, ObservedAt: now}}
	manager := Manager{Store: store, Probes: map[Provider]Probe{ProviderCodex: probe}, Now: func() time.Time { return now }}
	if err := manager.MarkExhausted(ProviderCodex, "official runtime limit"); err != nil {
		t.Fatal(err)
	}
	value, err := manager.AuthenticationChanged(context.Background(), ProviderCodex, true)
	if err != nil || !value.Eligible || value.HardLimitReached {
		t.Fatalf("runtime marker contaminated new auth context: %+v err=%v", value, err)
	}
}

func TestAuthenticationTransitionProbeFailureNeverResurrectsOldHardLimit(t *testing.T) {
	now := time.Now().UTC()
	store := Store{Root: filepath.Join(t.TempDir(), "quota")}
	if err := store.Put(ProviderQuota{Provider: ProviderCodex, Authenticated: true, HardLimitReached: true, Reason: "old account exhausted", ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	manager := Manager{Store: store, Probes: map[Provider]Probe{ProviderCodex: &fakeProbe{value: ProviderQuota{Provider: ProviderCodex, ObservedAt: now}, err: errors.New("probe unavailable")}}, Now: func() time.Time { return now }}
	value, err := manager.AuthenticationChanged(context.Background(), ProviderCodex, true)
	if err == nil || value.HardLimitReached || value.Eligible {
		t.Fatalf("previous account quota was resurrected: %+v err=%v", value, err)
	}
	snapshot, loadErr := store.Load()
	if loadErr != nil || len(snapshot.Providers) != 0 {
		t.Fatalf("failed reprobe restored stale cache: %+v err=%v", snapshot, loadErr)
	}
}

func TestInvalidatePreservesOtherProviderAndLegacySnapshot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "quota")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy, err := os.ReadFile(filepath.Join("testdata", "snapshot_v050_no_duration.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "snapshot.json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	store := Store{Root: root}
	if err := store.Invalidate(ProviderCodex); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.Load()
	if err != nil || len(snapshot.Providers) != 1 || snapshot.Providers[ProviderClaude].Provider != ProviderClaude {
		t.Fatalf("legacy invalidation failed: %+v err=%v", snapshot, err)
	}
}

func TestModelQuotaOnlyBlocksExactRequestedModel(t *testing.T) {
	value := availableQuotaForTest(ProviderCodex)
	value.Windows = append(value.Windows, Window{Kind: KindModelWeekly, Model: "limited-model", Available: true, Authoritative: true, RemainingPercent: 0})
	if !eligibleForModel(value, "") || !eligibleForModel(value, "another-model") || eligibleForModel(value, "limited-model") {
		t.Fatalf("model-scoped eligibility leaked into provider eligibility: %+v", value)
	}
}

func TestCodexModelBucketDoesNotMarkWholeProviderExhausted(t *testing.T) {
	weekly := int64(10080)
	zero := 100.0
	model := "limited-model"
	payload := codexRateResponse{RateLimits: codexLimit{Secondary: &codexWindow{UsedPercent: 10, WindowDurationMins: &weekly}}, RateLimitsByLimitID: map[string]codexLimit{
		"limited": {LimitName: &model, Secondary: &codexWindow{UsedPercent: zero, WindowDurationMins: &weekly}},
	}}
	windows := codexWindows(payload, time.Now().UTC())
	value := ProviderQuota{Provider: ProviderCodex, Authenticated: true, Eligible: true, Windows: windows}
	if !eligibleForModel(value, "") || eligibleForModel(value, "limited-model") {
		t.Fatalf("model bucket routing invalid: %+v", windows)
	}
}

func TestConcurrentQuotaWritesPreserveBothProviders(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "quota")}
	start := make(chan struct{})
	var wait sync.WaitGroup
	for _, provider := range []Provider{ProviderCodex, ProviderClaude} {
		provider := provider
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for index := 0; index < 25; index++ {
				if err := store.Put(availableQuotaForTest(provider)); err != nil {
					t.Errorf("put %s: %v", provider, err)
					return
				}
			}
		}()
	}
	close(start)
	wait.Wait()
	snapshot, err := store.Load()
	if err != nil || len(snapshot.Providers) != 2 {
		t.Fatalf("concurrent snapshot=%+v err=%v", snapshot, err)
	}
}

func availableQuotaForTest(provider Provider) ProviderQuota {
	return ProviderQuota{Provider: provider, Authenticated: true, Eligible: true, Source: "fixture", ObservedAt: time.Now().UTC()}
}
