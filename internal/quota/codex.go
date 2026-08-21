package quota

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type CodexAdapter struct {
	Binary  string
	Timeout time.Duration
	Now     func() time.Time
}

type codexWindow struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins *int64  `json:"windowDurationMins"`
	ResetsAt           *int64  `json:"resetsAt"`
}

type codexLimit struct {
	LimitID              *string               `json:"limitId"`
	LimitName            *string               `json:"limitName"`
	Primary              *codexWindow          `json:"primary"`
	Secondary            *codexWindow          `json:"secondary"`
	RateLimitReachedType *string               `json:"rateLimitReachedType"`
	SpendControlReached  *bool                 `json:"spendControlReached"`
	IndividualLimit      *codexIndividualLimit `json:"individualLimit"`
}

type codexIndividualLimit struct {
	RemainingPercent float64 `json:"remainingPercent"`
	ResetsAt         int64   `json:"resetsAt"`
}

type codexRateResponse struct {
	RateLimits          codexLimit            `json:"rateLimits"`
	RateLimitsByLimitID map[string]codexLimit `json:"rateLimitsByLimitId"`
}

func (a CodexAdapter) Probe(ctx context.Context) (ProviderQuota, error) {
	observed := time.Now().UTC()
	if a.Now != nil {
		observed = a.Now().UTC()
	}
	result := ProviderQuota{Provider: ProviderCodex, Authenticated: true, Eligible: true, Source: "codex app-server", ObservedAt: observed}
	if a.Binary == "" || !filepath.IsAbs(a.Binary) || filepath.Base(a.Binary) != "codex" {
		result.Authenticated, result.Eligible, result.Reason = false, false, "Codex executable unavailable"
		return result, errors.New(result.Reason)
	}
	timeout := a.Timeout
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = 12 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payload, err := codexRPC(probeCtx, a.Binary)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "authentication") {
			result.Authenticated, result.Eligible, result.Reason = false, false, "Codex authentication unavailable"
		}
		return result, err
	}
	result.Windows = codexWindows(payload, observed)
	limits := payload.RateLimits
	if byID, ok := payload.RateLimitsByLimitID["codex"]; ok {
		limits = byID
	}
	result.HardLimitReached = limits.RateLimitReachedType != nil || limits.SpendControlReached != nil && *limits.SpendControlReached
	for _, window := range result.Windows {
		if window.Authoritative && window.Available && window.RemainingPercent <= 0 && window.Kind != KindContext && window.Kind != KindCredits && window.Kind != KindModelWeekly {
			result.HardLimitReached = true
		}
	}
	if result.HardLimitReached {
		result.Eligible, result.Reason = false, "Codex subscription quota exhausted"
	}
	return result, nil
}

func codexRPC(ctx context.Context, binary string) (codexRateResponse, error) {
	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	cmd.Env = subscriptionEnvironment(binary)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return codexRateResponse{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return codexRateResponse{}, err
	}
	var stderr strings.Builder
	cmd.Stderr = &boundedWriter{target: &stderr, remaining: 4096}
	if err := cmd.Start(); err != nil {
		return codexRateResponse{}, err
	}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		_ = cmd.Wait()
	}()
	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(map[string]any{"method": "initialize", "id": 1, "params": map[string]any{"clientInfo": map[string]string{"name": "ivoai", "title": "ivoai quota probe", "version": "1"}}}); err != nil {
		return codexRateResponse{}, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	if _, err := waitRPC(scanner, 1); err != nil {
		return codexRateResponse{}, fmt.Errorf("Codex app-server initialize: %w", err)
	}
	if err := encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return codexRateResponse{}, err
	}
	if err := encoder.Encode(map[string]any{"method": "account/rateLimits/read", "id": 2, "params": map[string]any{}}); err != nil {
		return codexRateResponse{}, err
	}
	message, err := waitRPC(scanner, 2)
	if err != nil {
		diagnostic := strings.TrimSpace(stderr.String())
		if diagnostic != "" {
			err = fmt.Errorf("%w: %s", err, sanitize(diagnostic))
		}
		return codexRateResponse{}, err
	}
	var envelope struct {
		Result codexRateResponse `json:"result"`
	}
	if err := json.Unmarshal(message, &envelope); err != nil {
		return codexRateResponse{}, err
	}
	return envelope.Result, nil
}

func waitRPC(scanner *bufio.Scanner, id int) ([]byte, error) {
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		var envelope struct {
			ID    *int            `json:"id"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(line, &envelope) != nil || envelope.ID == nil || *envelope.ID != id {
			continue
		}
		if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			var value struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(envelope.Error, &value)
			return nil, errors.New(sanitize(value.Message))
		}
		return line, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func codexWindows(payload codexRateResponse, observed time.Time) []Window {
	base := payload.RateLimits
	if value, ok := payload.RateLimitsByLimitID["codex"]; ok {
		base = value
	}
	result := windowsFromCodexLimit(base, observed, false)
	for id, value := range payload.RateLimitsByLimitID {
		if id == "codex" {
			continue
		}
		label := id
		if value.LimitName != nil && *value.LimitName != "" {
			label = *value.LimitName
		}
		label = safeModel(label)
		for _, candidate := range []*codexWindow{value.Primary, value.Secondary} {
			if candidate == nil {
				continue
			}
			window := fromCodexWindow(KindModelWeekly, *candidate, observed)
			window.Model = label
			result = append(result, window)
		}
	}
	return result
}

func windowsFromCodexLimit(value codexLimit, observed time.Time, _ bool) []Window {
	result := []Window{}
	for _, candidate := range []*codexWindow{value.Primary, value.Secondary} {
		if candidate == nil {
			continue
		}
		kind := KindSession
		if candidate.WindowDurationMins != nil && *candidate.WindowDurationMins >= 6*24*60 {
			kind = KindWeekly
		}
		result = append(result, fromCodexWindow(kind, *candidate, observed))
	}
	if value.IndividualLimit != nil {
		reset := time.Unix(value.IndividualLimit.ResetsAt, 0).UTC()
		remaining := Clamp(value.IndividualLimit.RemainingPercent)
		result = append(result, Window{Kind: KindMonthly, UsedPercent: Clamp(100 - remaining), RemainingPercent: remaining, ResetsAt: &reset, Source: "codex app-server", ObservedAt: observed, Authoritative: true, Available: true})
	}
	return result
}

func fromCodexWindow(kind Kind, value codexWindow, observed time.Time) Window {
	var reset *time.Time
	if value.ResetsAt != nil {
		parsed := time.Unix(*value.ResetsAt, 0).UTC()
		reset = &parsed
	}
	return FromUsed(kind, value.UsedPercent, reset, "codex app-server", observed)
}

func subscriptionEnvironment(binary string) []string {
	prohibited := map[string]bool{
		"OPENAI_API_KEY": true, "ANTHROPIC_API_KEY": true, "OPENROUTER_API_KEY": true,
		"GEMINI_API_KEY": true, "GOOGLE_API_KEY": true, "GOOGLE_GEMINI_API_KEY": true,
		"AZURE_OPENAI_API_KEY": true, "ANTHROPIC_BASE_URL": true, "OPENAI_BASE_URL": true,
	}
	result := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && prohibited[key] {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "PATH="+filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

type boundedWriter struct {
	target    *strings.Builder
	remaining int
}

func (w *boundedWriter) Write(value []byte) (int, error) {
	original := len(value)
	if len(value) > w.remaining {
		value = value[:max(0, w.remaining)]
	}
	if len(value) > 0 {
		_, _ = w.target.Write(value)
		w.remaining -= len(value)
	}
	return original, nil
}

func sanitize(value string) string {
	value = platform.Redact(value)
	value = strings.Map(func(char rune) rune {
		if char == '\x1b' || char == '\r' || char == '\n' || char == '\x00' {
			return ' '
		}
		return char
	}, value)
	if len(value) > 1024 {
		value = value[:1024]
	}
	return value
}
