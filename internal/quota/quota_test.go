package quota

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
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
	if len(windows) != 3 || windows[0].Kind != KindSession || windows[0].RemainingPercent != 75 || windows[1].Kind != KindWeekly || windows[1].RemainingPercent != 0 || windows[2].Kind != KindMonthly || windows[2].RemainingPercent != 71 {
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
	for kind, remaining := range map[Kind]float64{KindSession: 86, KindWeekly: 71, KindMonthly: 82} {
		window, ok := value.Window(kind)
		if !ok || window.RemainingPercent != remaining || window.Source != "codex app-server" {
			t.Fatalf("kind=%s window=%+v", kind, window)
		}
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
