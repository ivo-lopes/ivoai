package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/session"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
	"golang.org/x/term"
)

func runMonitor(ctx context.Context, a *app.App, args []string) error {
	fs := flag.NewFlagSet("monitor", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	watch := fs.Bool("watch", false, "watch session updates")
	id := fs.String("session", "", "session ID")
	jsonOutput := fs.Bool("json", false, "JSON output")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return errors.New("usage: ivoai monitor [--watch] [--session <id>] [--json]")
	}
	var last time.Time
	for {
		value, err := monitoredSession(a, *id)
		if err != nil {
			return err
		}
		if !value.UpdatedAt.Equal(last) {
			if *jsonOutput {
				body, err := json.Marshal(value)
				if err != nil {
					return err
				}
				fmt.Fprintln(a.Out, string(body))
			} else {
				if *watch && terminalui.HumanOutput(a.Out) {
					fmt.Fprint(a.Out, "\x1b[2J\x1b[H", terminalui.ScreenHeader(a.Out, a.Version))
				}
				renderMonitor(a.Out, value)
			}
			last = value.UpdatedAt
		}
		if !*watch || !value.Active() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func monitoredSession(a *app.App, id string) (session.Session, error) {
	if id != "" {
		return a.SessionShow(id)
	}
	values, err := a.SessionList()
	if err != nil {
		return session.Session{}, err
	}
	if len(values) == 0 {
		return session.Session{}, errors.New("no sessions recorded")
	}
	for _, value := range values {
		if value.Active() {
			return value, nil
		}
	}
	return values[0], nil
}

func writeSessions(out io.Writer, values []session.Session, jsonOutput bool) error {
	if jsonOutput {
		body, err := json.MarshalIndent(values, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(body))
		return nil
	}
	if len(values) == 0 {
		fmt.Fprintln(out, "No sessions recorded.")
		return nil
	}
	for _, value := range values {
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s (%s)\n", clean(value.SessionID), strings.ToUpper(string(value.Mode)), strings.ToUpper(string(value.State)), clean(value.PrimaryExecutor), clean(value.PrimaryModel.Name), strings.ReplaceAll(string(value.PrimaryModel.Source), "_", " "))
	}
	return nil
}

func renderMonitor(out io.Writer, value session.Session) {
	width := 80
	if file, ok := out.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		if measured, _, err := term.GetSize(int(file.Fd())); err == nil {
			width = measured
		}
	}
	renderMonitorSized(out, value, width)
}

func renderMonitorSized(out io.Writer, value session.Session, width int) {
	if width < 20 {
		width = 20
	}
	color := terminalui.ColorEnabled(out)
	modeText := strings.ToUpper(string(value.Mode))
	stateText := strings.ToUpper(string(value.State))
	mode := terminalui.Info(terminalui.Fit(modeText, width-15), color)
	state := terminalui.Success(terminalui.Fit(stateText, width-15), color)
	if value.State == session.StateDegraded || value.State == session.StateStopping {
		state = terminalui.Warning(terminalui.Fit(stateText, width-15), color)
	} else if value.State == session.StateFailed {
		state = terminalui.Failure("FAILED", color)
	}
	monitorRow(out, "Session", clean(value.SessionID), width)
	fmt.Fprintf(out, "%-15s%s\n%-15s%s\n\n", "Mode", mode, "State", state)
	if value.Mode == session.ModeAuto {
		fmt.Fprintln(out, "Automatic Session")
		monitorRow(out, "  Conversation", providerDisplay(value.CurrentPrimary), width)
		monitorRow(out, "  Initial", providerDisplay(value.InitialPlanner), width)
		monitorRow(out, "  Failovers", fmt.Sprint(value.FailoverCount), width)
		monitorRow(out, "  Phase", clean(value.CurrentPhase), width)
		monitorRow(out, "  Checkpoint", checkpointMonitor(value), width)
		monitorRow(out, "  Strategy", clean(value.OptimizationStrategy), width)
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, "Primary")
	monitorRow(out, "  Executor", clean(value.PrimaryExecutor), width)
	monitorRow(out, "  Model", clean(value.PrimaryModel.Name)+" ("+strings.ReplaceAll(string(value.PrimaryModel.Source), "_", " ")+")", width)
	monitorRow(out, "  PID", fmt.Sprint(value.PrimaryPID), width)
	monitorRow(out, "  Headroom", activeLabel(value.HeadroomUsed), width)
	fmt.Fprintln(out, "\nOrchestration")
	monitorRow(out, "  Ruflo", activeLabel(value.RufloHealthy), width)
	monitorRow(out, "  Safe Mode", yesNo(value.RufloSafeMode), width)
	monitorRow(out, "  Provider", disabledLabel(value.ProviderExecution), width)
	monitorRow(out, "  Swarm", clean(value.SwarmID), width)
	monitorRow(out, "  Workers", fmt.Sprintf("%d/%d", activeWorkers(value.Workers), value.MaxWorkers), width)
	if value.Mode == session.ModeAuto {
		fmt.Fprintln(out, "\nQuota")
		for _, provider := range []quota.Provider{quota.ProviderCodex, quota.ProviderClaude} {
			current := value.Quota[provider]
			fmt.Fprintln(out, "  "+providerDisplay(string(provider)))
			if provider == quota.ProviderCodex {
				quotaMonitorDurationRow(out, "    5h", current, 300, width)
				quotaMonitorDurationRow(out, "    Weekly", current, 10080, width)
				for _, window := range current.Windows {
					if window.Model == "" && window.DurationMinutes > 0 && window.DurationMinutes != 300 && window.DurationMinutes != 10080 {
						quotaMonitorWindowRow(out, "    "+quota.DurationLabel(window.DurationMinutes), window, width)
					}
				}
				quotaMonitorRow(out, "    Individual", provider, current, quota.KindIndividual, width)
			} else {
				quotaMonitorRow(out, "    5h", provider, current, quota.KindSession, width)
				quotaMonitorRow(out, "    Weekly", provider, current, quota.KindWeekly, width)
			}
			monitorRow(out, "    Eligible", yesNo(current.Eligible), width)
		}
	}
	if len(value.Workers) > 0 {
		fmt.Fprintln(out, "\nWorkers")
		for _, worker := range value.Workers {
			monitorRow(out, "  "+clean(worker.Role), clean(worker.Executor)+" "+clean(worker.Model.Name)+" "+clean(string(worker.State)), width)
		}
	}
	if value.Mode == session.ModeAuto {
		fmt.Fprintln(out, "\nKnowledge")
		monitorRow(out, "  Memory", clean(value.KnowledgeBootstrap.MemoryStatus), width)
		monitorRow(out, "  Context", clean(value.KnowledgeBootstrap.ContextStatus), width)
		bootstrap := "PENDING"
		if value.KnowledgeBootstrap.Performed {
			bootstrap = fmt.Sprintf("READY / %d references", value.KnowledgeBootstrap.ReferenceCount)
		}
		monitorRow(out, "  Bootstrap", bootstrap, width)
		fmt.Fprintln(out, "\nPlan")
		monitorRow(out, "  Tasks", fmt.Sprint(len(value.Tasks)), width)
		monitorRow(out, "  Running", fmt.Sprint(activeTasks(value.Tasks)), width)
		monitorRow(out, "  Completed", fmt.Sprint(completedTasks(value.Tasks)), width)
		for _, task := range value.Tasks {
			profile := clean(task.Executor) + " " + clean(task.Model.Name)
			if task.Effort != "" {
				profile += " effort=" + clean(task.Effort)
			} else {
				profile += " effort=default"
			}
			mode := task.ExecutionMode
			if mode == "" {
				mode = "legacy"
			}
			monitorRow(out, "  "+clean(task.ID), fmt.Sprintf("%s score=%d %s %s mode=%s", task.State, task.CapabilityScore, task.Tier, profile, clean(mode)), width)
			if len(task.Dependencies) > 0 {
				monitorRow(out, "    depends", strings.Join(task.Dependencies, ","), width)
			}
			if task.DelegationReason != "" {
				monitorRow(out, "    routing", fmt.Sprintf("benefit=%d overhead=%d %s", task.DelegationBenefit, task.DelegationOverhead, clean(task.DelegationReason)), width)
			}
		}
		fmt.Fprintln(out, "\nRouting")
		monitorRow(out, "  Strategy", clean(value.OptimizationStrategy), width)
		monitorRow(out, "  Escalations", fmt.Sprint(value.EscalationCount), width)
	}
	if len(value.Observability) > 0 {
		fmt.Fprintln(out, "\nObservability")
		start := len(value.Observability) - 8
		if start < 0 {
			start = 0
		}
		for _, event := range value.Observability[start:] {
			detail := event.Summary()
			if event.RoutingReason != "" {
				detail += " reason=" + string(event.RoutingReason)
			}
			if event.FallbackReason != "" {
				detail += " fallback=" + string(event.FallbackReason)
			}
			monitorRow(out, "  Event", detail, width)
		}
	}
	fmt.Fprintln(out, "\nServices")
	monitorRow(out, "  Context", clean(value.ContextStatus), width)
	monitorRow(out, "  ai-memory", clean(value.MemoryStatus), width)
	monitorRow(out, "  Server", clean(value.ServerStatus), width)
}

func activeTasks(values []session.TaskMetadata) int {
	count := 0
	for _, value := range values {
		if value.State == session.StateStarting || value.State == session.StateRunning || value.State == session.StateQueued {
			count++
		}
	}
	return count
}

func completedTasks(values []session.TaskMetadata) int {
	count := 0
	for _, value := range values {
		if value.State == session.StateCompleted {
			count++
		}
	}
	return count
}

func quotaMonitorRow(out io.Writer, label string, provider quota.Provider, value quota.ProviderQuota, kind quota.Kind, width int) {
	window, ok := value.Window(kind)
	if !ok {
		if provider == quota.ProviderClaude && (kind == quota.KindSession || kind == quota.KindWeekly) {
			monitorRow(out, label, "awaiting first response", width)
			return
		}
		monitorRow(out, label, "N/A / not exposed", width)
		return
	}
	quotaMonitorWindowRow(out, label, window, width)
}

func quotaMonitorDurationRow(out io.Writer, label string, value quota.ProviderQuota, durationMinutes int64, width int) {
	window, ok := value.WindowByDuration(durationMinutes)
	if !ok && durationMinutes == 10080 {
		window, ok = value.Window(quota.KindWeekly)
	}
	if !ok {
		monitorRow(out, label, "N/A / not exposed", width)
		return
	}
	quotaMonitorWindowRow(out, label, window, width)
}

func quotaMonitorWindowRow(out io.Writer, label string, window quota.Window, width int) {
	state := window.TelemetryState()
	if state == quota.TelemetryPending {
		monitorRow(out, label, "awaiting first response", width)
		return
	}
	if state == quota.TelemetryNotExposed || !window.Available || !window.Authoritative {
		monitorRow(out, label, "N/A / not exposed", width)
		return
	}
	monitorRow(out, label, formatQuotaPercent(window.RemainingPercent)+"% remaining", width)
	reset := "N/A / not exposed"
	if window.ResetsAt != nil {
		reset = window.ResetsAt.UTC().Format(time.RFC3339)
	}
	monitorRow(out, "      Reset", reset, width)
	monitorRow(out, "      Source", clean(window.Source), width)
	freshness := "fresh"
	if state == quota.TelemetryStale {
		freshness = "stale"
	}
	if !window.ObservedAt.IsZero() {
		freshness += " · observed " + window.ObservedAt.UTC().Format(time.RFC3339)
	}
	monitorRow(out, "      Freshness", freshness, width)
}

func formatQuotaPercent(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.1f", value)
}

func providerDisplay(value string) string {
	switch value {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	default:
		return "N/A"
	}
}

func checkpointMonitor(value session.Session) string {
	if !value.CheckpointAvailable || value.CheckpointUpdatedAt == nil {
		return "unavailable"
	}
	return "available · " + value.CheckpointUpdatedAt.UTC().Format(time.RFC3339)
}

func monitorRow(out io.Writer, label, value string, width int) {
	label = terminalui.Fit(label, 14)
	available := width - 15
	if available < 1 {
		available = 1
	}
	fmt.Fprintf(out, "%-15s%s\n", label, terminalui.Fit(value, available))
}

func activeWorkers(values []session.Worker) int {
	count := 0
	for _, value := range values {
		if value.State == session.StateStarting || value.State == session.StateRunning || value.State == session.StateStopping {
			count++
		}
	}
	return count
}

func activeLabel(value bool) string {
	if value {
		return "ACTIVE"
	}
	return "INACTIVE"
}

func yesNo(value bool) string {
	if value {
		return "YES"
	}
	return "NO"
}

func disabledLabel(enabled bool) string {
	if enabled {
		return "ENABLED"
	}
	return "DISABLED"
}

func clean(value string) string {
	value = platform.Redact(value)
	value = strings.Map(func(char rune) rune {
		if char == '\x1b' || char == '\r' || char == '\n' || char == '\x00' {
			return -1
		}
		return char
	}, value)
	if len(value) > 256 {
		return value[:256] + "..."
	}
	return value
}
