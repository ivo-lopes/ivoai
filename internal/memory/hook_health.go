package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

// HookHealth deliberately excludes commands, environment and provider output.
type HookHealth struct {
	Agent            string `json:"agent"`
	Event            string `json:"event"`
	Owner            string `json:"owner"`
	State            string `json:"state"`
	Reason           string `json:"reason,omitempty"`
	TargetExists     bool   `json:"target_exists"`
	TargetExecutable bool   `json:"target_executable"`
	Repaired         bool   `json:"repaired,omitempty"`
}

// HookMaintenance is shared by preflight, doctor and explicit repair. The
// caller must establish managed component ownership before allowing repair.
type HookMaintenance struct {
	Agent, ConfigPath, Binary, HooksDir, DataDir string
	Managed                                      bool
	// Additional former component paths must come from ownership receipts or
	// an explicitly verified migration, never from the hook command itself.
	PreviousBinaries []string
}

var hookEvents = map[string]string{"SessionStart": "session-start", "UserPromptSubmit": "user-prompt-submit", "PreToolUse": "pre-tool-use", "PostToolUse": "post-tool-use", "PreCompact": "pre-compact", "Stop": "stop", "SessionEnd": "session-end", "SubagentStart": "subagent-start", "SubagentStop": "subagent-stop"}

// legacyHook accepts only the declarative command shape emitted by the known
// ai-memory installer. Shell operators/expansions and custom flags are foreign.
func legacyHook(command, agent, event string) (string, bool) {
	if strings.ContainsAny(command, "\n\r;|&<>`$\\\"'") {
		return "", false
	}
	args := strings.Fields(command)
	if len(args) < 8 || !filepath.IsAbs(args[0]) || !strings.HasSuffix(filepath.ToSlash(args[0]), "/ivoai/bin/ai-memory") {
		return "", false
	}
	values := map[string]string{}
	hook := false
	for i := 1; i < len(args); i++ {
		if args[i] == "hook" && !hook {
			hook = true
			continue
		}
		key := args[i]
		if key != "--data-dir" && key != "--event" && key != "--agent" && key != "--server-url" && key != "--auth-token" && key != "--project-strategy" {
			return "", false
		}
		if i+1 >= len(args) || values[key] != "" {
			return "", false
		}
		i++
		values[key] = args[i]
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(args[0])))
	if !hook || values["--data-dir"] != filepath.Join(root, "ai-memory") || values["--agent"] != agent || values["--event"] != event || values["--project-strategy"] != "repo-root" {
		return "", false
	}
	return args[0], true
}

func executableHealth(target string) (bool, bool, string) {
	info, err := os.Stat(target)
	if err != nil {
		return false, false, "target_unavailable"
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return true, false, "target_not_executable"
	}
	f, err := os.Open(target)
	if err != nil {
		return true, false, "target_unreadable"
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	if bytes.HasPrefix(buf[:n], []byte("#!")) {
		line := strings.SplitN(string(buf[:n]), "\n", 2)[0]
		parts := strings.Fields(strings.TrimPrefix(line, "#!"))
		if len(parts) == 0 {
			return true, false, "interpreter_unavailable"
		}
		if _, err := exec.LookPath(parts[0]); err != nil {
			return true, false, "interpreter_unavailable"
		}
		if filepath.Base(parts[0]) == "env" && (len(parts) != 2 || func() bool { _, e := exec.LookPath(parts[1]); return e != nil }()) {
			return true, false, "interpreter_path_unavailable"
		}
	}
	return true, true, ""
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func (m HookMaintenance) wrapper(event string) string {
	// Optional hooks never flood the frontend with process errors. Operational
	// degradation is reported once by preflight/doctor, not once per hook event.
	return "#!/bin/sh\n# ivoai-owned lifecycle hook v1\n" +
		"if [ ! -x " + shellQuote(m.Binary) + " ]; then exit 0; fi\n" +
		shellQuote(m.Binary) + " --data-dir " + shellQuote(m.DataDir) + " hook --event " + shellQuote(event) + " --agent " + shellQuote(m.Agent) + " --project-strategy repo-root 2>/dev/null\nexit 0\n"
}

func (m HookMaintenance) Inspect(repair bool) ([]HookHealth, error) {
	if m.Agent != "codex" && m.Agent != "claude-code" {
		return nil, errors.New("unsupported hook agent")
	}
	if repair {
		if _, err := os.Lstat(m.ConfigPath); errors.Is(err, os.ErrNotExist) {
			return []HookHealth{}, nil
		}
		fd, err := unix.Open(filepath.Join(filepath.Dir(m.ConfigPath), ".ivoai-hook-maintenance.lock"), unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
		if err != nil {
			return nil, errors.New("hook repair lock unavailable")
		}
		defer unix.Close(fd)
		if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
			return nil, errors.New("hook repair already active")
		}
		defer unix.Flock(fd, unix.LOCK_UN)
	}
	body, err := platform.ReadRegularFile(m.ConfigPath, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return []HookHealth{}, nil
	}
	if err != nil {
		return nil, errors.New("hook configuration unreadable")
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(body, &doc) != nil {
		return nil, errors.New("hook configuration invalid")
	}
	var events map[string][]json.RawMessage
	if len(doc["hooks"]) == 0 {
		return []HookHealth{}, nil
	}
	if json.Unmarshal(doc["hooks"], &events) != nil {
		return nil, errors.New("hook registrations invalid")
	}
	result := []HookHealth{}
	changed := false
	for event, groups := range events {
		mapped, known := hookEvents[event]
		if !known {
			continue
		}
		for gi, raw := range groups {
			var group map[string]json.RawMessage
			if json.Unmarshal(raw, &group) != nil {
				continue
			}
			var hooks []map[string]json.RawMessage
			if json.Unmarshal(group["hooks"], &hooks) != nil {
				continue
			}
			groupChanged := false
			for _, h := range hooks {
				var command, kind string
				_ = json.Unmarshal(h["command"], &command)
				_ = json.Unmarshal(h["type"], &kind)
				if kind != "command" {
					continue
				}
				wrapper := filepath.Join(m.HooksDir, m.Agent, "ivoai-"+mapped+".sh")
				target, owned := legacyHook(command, m.Agent, mapped)
				if owned {
					proven := target == m.Binary
					for _, previous := range m.PreviousBinaries {
						proven = proven || target == previous
					}
					owned = proven
					if !proven {
						result = append(result, HookHealth{Agent: m.Agent, Event: event, Owner: "unverified", State: "degraded", Reason: "ownership_verification_required"})
					}
				}
				modern := command == "/bin/sh "+shellQuote(wrapper)
				if modern {
					owned = true
					target = m.Binary
				}
				if !owned || !m.Managed {
					continue
				}
				exists, runnable, reason := executableHealth(target)
				if modern {
					w, e := platform.ReadRegularFile(wrapper, 16<<10)
					if e != nil || string(w) != m.wrapper(mapped) {
						runnable = false
						reason = "managed_wrapper_stale"
					}
				}
				health := HookHealth{Agent: m.Agent, Event: event, Owner: "ivoai/ai-memory", State: "healthy", TargetExists: exists, TargetExecutable: runnable}
				if !runnable || !modern {
					health.State = "degraded"
					health.Reason = reason
					if reason == "" {
						health.Reason = "legacy_wiring"
					}
				}
				if repair && health.State != "healthy" {
					if !filepath.IsAbs(m.Binary) || !filepath.IsAbs(m.DataDir) || !filepath.IsAbs(m.HooksDir) {
						return nil, errors.New("managed hook paths must be absolute")
					}
					if err := platform.AtomicWritePrivate([]byte(m.wrapper(mapped)), wrapper); err != nil {
						return nil, errors.New("managed hook wrapper repair failed")
					}
					h["command"], _ = json.Marshal("/bin/sh " + shellQuote(wrapper))
					groupChanged = true
					changed = true
					health.TargetExists, health.TargetExecutable, health.Reason = executableHealth(m.Binary)
					health.State = "healthy"
					if !health.TargetExecutable {
						health.State = "degraded"
					}
					health.Repaired = true
				}
				result = append(result, health)
			}
			if groupChanged {
				group["hooks"], _ = json.Marshal(hooks)
				groups[gi], _ = json.Marshal(group)
			}
		}
		events[event] = groups
	}
	if changed {
		doc["hooks"], _ = json.Marshal(events)
		next, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return nil, err
		}
		current, err := platform.ReadRegularFile(m.ConfigPath, 1<<20)
		if err != nil || !bytes.Equal(body, current) {
			return nil, errors.New("hook configuration changed during repair")
		}
		if err := platform.AtomicWriteFile(append(next, '\n'), m.ConfigPath, 0600); err != nil {
			return nil, errors.New("hook configuration repair failed")
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Event < result[j].Event })
	return result, nil
}
