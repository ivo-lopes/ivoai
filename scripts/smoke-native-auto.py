#!/usr/bin/env python3
"""Isolated artifact smoke: official AUTO + pinned frontend + official client.

Only fixture content is submitted. No transcript is persisted or printed.
The caller supplies isolated XDG directories and component metadata.

IVOAI_NATIVE_SMOKE_RECOVERY=1 adds a real approved readonly recovery case,
independently of CONTINUITY/SWITCH. It requires Linux pidfd support and a real
two-task checkpoint: the first task must complete while the second pauses.
Missing the interruption window is a failure; checkpoints are never fabricated.
This mode consumes provider turns and must be explicitly scheduled by the caller.
"""
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import termios
import time

binary, root = Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve()
complex_case = os.environ.get("IVOAI_NATIVE_SMOKE_COMPLEX") == "1"
frontend = os.environ.get("IVOAI_NATIVE_SMOKE_FRONTEND", "opencode")
assert frontend in {"codex", "opencode", "auto"}
codex_frontend = frontend == "codex"
ux_smoke = os.environ.get("IVOAI_NATIVE_SMOKE_UX") == "1"
continuity_smoke = os.environ.get("IVOAI_NATIVE_SMOKE_CONTINUITY") == "1"
switch_smoke = os.environ.get("IVOAI_NATIVE_SMOKE_SWITCH") == "1"
# Real approved readonly DAG interruption; independent of picker/switch smoke.
recovery_smoke = os.environ.get("IVOAI_NATIVE_SMOKE_RECOVERY") == "1"
assert not recovery_smoke or (not complex_case and not ux_smoke)
assert not switch_smoke or (continuity_smoke and codex_frontend)
assert not continuity_smoke or not complex_case
target_session_id = None
assert not ux_smoke or (not codex_frontend and not complex_case)
repo = root / "fixture"
repo.mkdir(mode=0o700)
(repo / "VERSION").write_text("fixture-1\n")
if complex_case:
    (repo / "a.sh").write_text("#!/bin/sh\nprintf 'old-a\\n'\n")
    (repo / "b.sh").write_text("#!/bin/sh\nprintf 'old-b\\n'\n")
    (repo / "protected.txt").write_text("do not modify\n")
for arguments in (["init", "-q"], ["config", "user.name", "Fixture"],
                  ["config", "user.email", "fixture@example.invalid"],
                  ["add", "."], ["commit", "-qm", "fixture baseline"]):
    subprocess.run(["git", *arguments], cwd=repo, check=True, capture_output=True)
sessions = Path(os.environ["XDG_STATE_HOME"]) / "ivoai/sessions"
master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))

def owner():
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)

process = subprocess.Popen([str(binary), frontend], cwd=repo, stdin=slave,
                           stdout=slave, stderr=slave, preexec_fn=owner)
os.close(slave)
history = ""
handled_routing = set()
peak_workers = 0

def pump(seconds=.2):
    global history
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if select.select([master], [], [], .05)[0]:
            try:
                body = os.read(master, 65536)
            except OSError:
                break
            if b"\x1b[6n" in body:
                os.write(master, b"\x1b[1;1R")
            if ux_smoke and b"\x1b[?u" in body:
                os.write(master, b"\x1b[?1u")
            history = (history + body.decode(errors="replace"))[-(1 << 20):]
        if process.poll() is not None:
            break
    return re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", history)

def send(text):
    os.write(master, text.encode())
    pump(.3)

def fixture_position(text):
    index = history.rfind(text)
    moves = list(re.finditer(r"\x1b\[(\d+);(\d+)[Hf]", history[:index]))
    assert index >= 0 and moves, "FIXTURE_POSITION_UNAVAILABLE"
    move = moves[-1]
    row, column = map(int, move.groups())
    prefix = history[move.end():index]
    for part in re.split(r"(\x1b\[[0-?]*[ -/]*[@-~])", prefix):
        if part.startswith("\x1b["):
            amount = int(part[2:-1]) if part[2:-1].isdigit() else 1
            command = part[-1]
            if command == "A": row -= amount
            elif command in "Be": row += amount
            elif command in "Ca": column += amount
            elif command == "D": column -= amount
            elif command in "G`": column = amount
            elif command == "d": row = amount
            elif command == "E": row, column = row + amount, 1
            elif command == "F": row, column = row - amount, 1
        else:
            for char in part:
                if char == "\r": column = 1
                elif char == "\n": row += 1
                elif char == "\b": column -= 1
                elif ord(char) >= 32: column += 1
    assert 0 < row <= 40 and 0 < column <= 120, "FIXTURE_POSITION_OUTSIDE_PTY"
    return row, column

def clipboard_read(target="UTF8_STRING"):
    command = subprocess.Popen(["xclip", "-selection", "clipboard", "-o", "-t", target],
                               stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    data = bytearray()
    deadline = time.monotonic() + 3
    try:
        while time.monotonic() < deadline:
            if select.select([command.stdout], [], [], .05)[0]:
                part = os.read(command.stdout.fileno(), 65536)
                if not part:
                    assert command.wait(timeout=1) == 0, "CLIPBOARD_READ_FAILED"
                    return bytes(data)
                data.extend(part)
                assert len(data) <= 1 << 20, "CLIPBOARD_TOO_LARGE_TO_PRESERVE"
        raise RuntimeError("CLIPBOARD_READ_TIMEOUT")
    finally:
        if command.poll() is None: command.kill()
        command.wait()

previous_clipboard = None

def metadata():
    for path in (list(sessions.glob("*.json")) + list((sessions / ".native-opencode").glob("*.json"))
                 + list((sessions / ".orchestrated-frontends").glob("*.json"))):
        value = json.loads(path.read_text())
        if value.get("working_directory") == str(repo) and (target_session_id is None or value.get("session_id") == target_session_id):
            return value
    return {}

def codex_composer_ready(text):
    # 0.162.1 uses these landmarks without either historical footer phrase.
    # Its /model startup hint disappears on resume. Reject admission/picker UI.
    if any(dialog in text for dialog in ("Resume a previous session", "Sign in with ChatGPT", "Do you trust")):
        return False
    return ("context left" in text or "? for shortcuts" in text or
            all(marker in text for marker in ("OpenAI Codex", "›", "shortcuts")))

def wait_for(predicate, timeout, failure, failure_baseline=0):
    global peak_workers
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline and process.poll() is None:
        plain = pump()
        value = metadata()
        peak_workers = max(peak_workers, sum(w.get("state") == "running" for w in value.get("workers", [])))
        for decision in value.get("decisions", []):
            if (decision.get("kind") == "routing" and decision.get("state") == "pending"
                    and decision["id"] not in handled_routing and
                    ("IVOAI quota routing approval" in plain or codex_frontend and "Keep current" in plain)):
                # This smoke never authorizes a real cross-provider change or
                # consumes quota deliberately. Keep the original strong route.
                pump(1)
                if codex_frontend:
                    send("\x1b[B\r")
                    pump(.5)
                    send("\r")
                else:
                    send("\x1b")
                handled_routing.add(decision["id"])
                print("LOW_QUOTA_VISIBLE=true CONSERVATION_DECISION=KEEP_CURRENT", flush=True)
        if predicate(plain, value):
            return
        if len(value.get("turn_attempts", [])) > failure_baseline and value["turn_attempts"][-1].get("turn_state") == "failed":
            break
    value = metadata()
    trace = value.get("executor_trace") or {}
    # Fixed operational metadata only, never terminal/model text.
    print(json.dumps({"phase": value.get("current_phase"),
                      "events": trace.get("stdout_events"),
                      "last_event": trace.get("last_valid_event"),
                      "failure_class": trace.get("failure_class"),
                      "exit_code": trace.get("exit_code"),
                      "tasks": len(value.get("tasks", [])),
                      "workers": len(value.get("workers", [])),
                      "tools": trace.get("mcp_operations", []),
                      "task_states": [t.get("state") for t in value.get("tasks", [])],
                      "trust_screen": "Do you trust" in history,
                      "login_screen": "Sign in with ChatGPT" in history,
                      "native_picker_empty": "No sessions found" in history,
                      "native_ui_landmarks": [marker for marker in ["OpenAI Codex", "context left", "? for shortcuts", "shortcuts", "/model", "›", "Press Enter to continue", "Sign in with ChatGPT", "Do you trust"] if marker in history],
                      "native_errors": [code for code in ["NATIVE_THREAD_MAPPING_FAILED", "NATIVE_SESSION_NOT_MANAGED", "TURN_STILL_ACTIVE", "Failed to resume", "failed to connect to remote app server"] if code in history],
                      "codex_version": value.get("codex_version"),
                      "turn_failure": (value.get("turn_attempts") or [{}])[-1].get("failure_class")}), flush=True)
    raise RuntimeError(failure)

def close_frontend():
    send("/exit")
    send("\r")
    deadline = time.monotonic() + 12
    while process.poll() is None and time.monotonic() < deadline:
        pump(.2)
    assert process.poll() is not None, "FRONTEND_DID_NOT_EXIT"
    assert process.returncode == 0, "FRONTEND_EXIT_FAILED"
    os.close(master)

def reopen(arguments, failure_baseline=0):
    global master, process, history
    master, new_slave = pty.openpty()
    fcntl.ioctl(new_slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    process = subprocess.Popen([str(binary), *arguments], cwd=repo, stdin=new_slave,
                               stdout=new_slave, stderr=new_slave, preexec_fn=owner)
    os.close(new_slave)
    history = ""
    wait_for(lambda text, _: codex_composer_ready(text) if codex_frontend else ("IVOAI control plane" in text or "IVOAI Automatic Orchestration" in text),
             120, "RESUMED_NATIVE_COMPOSER_NOT_READY", failure_baseline)


def interrupt_owned_frontend():
    interrupted = metadata()
    native_pid = interrupted["frontend_pid"]
    native_start = interrupted["frontend_process_start"]
    native_fd = os.pidfd_open(native_pid)
    try:
        native_stat = Path(f"/proc/{native_pid}/stat").read_text().rsplit(")", 1)[1].split()
        assert native_stat[19] == native_start, "INTERRUPTION_PID_IDENTITY_CHANGED"
        assert os.getpgid(native_pid) == process.pid, "INTERRUPTION_NOT_HARNESS_OWNED"
        signal.pidfd_send_signal(native_fd, signal.SIGKILL)
    finally:
        os.close(native_fd)
    process.wait(timeout=10)
    os.close(master)

def readonly_recovery_snapshot(value, checkpoint, require_partial):
    """Require a declared readonly graph and actual host completion evidence."""
    recovery = checkpoint.get("recovery") or {}
    plan = recovery.get("plan") or {}
    tasks = plan.get("tasks") or []
    if (recovery.get("integrated") or recovery.get("directory") != value.get("working_directory")
            or not plan.get("id") or plan["id"] != value.get("plan_id")
            or len(tasks) != 2 or value.get("workers")):
        return None
    if any(t.get("execution_mode") != "primary" or t.get("role") != "research"
           or t.get("write_paths") or t.get("allowed_mcp_tools") or t.get("allowed_mcps") for t in tasks):
        return None
    ids = [t.get("id") for t in tasks]
    if (not all(ids) or len(set(ids)) != 2 or tasks[0].get("dependencies")
            or tasks[1].get("dependencies") != [ids[0]]):
        return None
    states = {t.get("id"): t.get("state") for t in value.get("tasks", [])}
    if set(states) != set(ids):
        return None
    completed = [i for i in ids if states[i] == "completed"]
    pending = [i for i in ids if states[i] != "completed"]
    if require_partial and (recovery.get("approved") is not True or not completed or not pending):
        return None
    return completed, pending

def recovery_checkpoint():
    path = sessions / "checkpoints" / (target_session_id + ".json")
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, "rb") as checkpoint_file:
        body = checkpoint_file.read(32769)
    assert len(body) <= 32768, "RECOVERY_CHECKPOINT_OVERSIZED"
    return json.loads(body)

try:
    if ux_smoke:
        assert os.environ.get("DISPLAY") and not os.environ.get("WAYLAND_DISPLAY"), "X11_SESSION_REQUIRED"
        targets = clipboard_read("TARGETS").decode().split()
        assert all(t in {"TARGETS", "MULTIPLE", "TIMESTAMP", "SAVE_TARGETS", "UTF8_STRING", "TEXT", "STRING", "text/plain", "text/plain;charset=utf-8", "text/plain;charset=UTF-8"} for t in targets), "PRESERVING_RICH_OPERATOR_CLIPBOARD"
        previous_clipboard = clipboard_read()
    wait_for(lambda text, _: "IVOAI control plane" in text or
             "IVOAI Automatic Orchestration" in text or codex_frontend and "OpenAI Codex" in text, 120, "FRONTEND_NOT_READY")
    if codex_frontend:
        # The banner can precede startup/config initialization. Do not send
        # fixture input until the native composer has rendered its footer.
        wait_for(lambda text, _: codex_composer_ready(text),
                 60, "NATIVE_COMPOSER_NOT_READY")
    send("corrija o projeto")
    send("\r")
    wait_for(lambda text, _: "insufficient" in text.lower(), 45, "PROMPT_GATE_FAILED")
    assert not metadata().get("workers"), "WORKER_STARTED_BEFORE_GATE"
    assert not any(e.get("operation") == "skill.gate" for e in metadata().get("observability", [])), "SKILL_GATE_BEFORE_PROMPT_GATE"
    print("INSUFFICIENT_PROMPT_REJECTED=PASS NO_WORKER_BEFORE_GATE=true", flush=True)
    prompt = "Read VERSION in this fixture repository and report its value without modifying files. Acceptance: return exactly fixture-1 as the final response. Use a single primary-owned research task, record its completion and integrate the plan before synthesis."
    if complex_case:
        prompt = "Objective: implement two independent changes in this small fixture repository and document them. Deliverables: a.sh must print exactly A, b.sh must print exactly B, and README.md explains both commands. Constraints: do not change protected.txt or VERSION; no external knowledge or MCP is necessary; use two independent implementation workers in isolated worktrees, plus bounded documentation and validation tasks. Acceptance: sh a.sh returns A with exit 0; sh b.sh returns B with exit 0; README.md documents both commands; protected.txt and VERSION are unchanged; final validation passes. Present the native DAG for approval; let IVOAI automatically dispatch workers, integrate and synthesize only after all local acceptance passes."
    if ux_smoke:
        objective, acceptance = prompt.split(" Acceptance:", 1)
        before = len(metadata().get("turn_attempts", []))
        send(objective)
        pump(.5)
        send("\x1b[13;2u")
        pump(.5)
        assert len(metadata().get("turn_attempts", [])) == before, "SHIFT_ENTER_SUBMITTED"
        send("Acceptance:" + acceptance)
        # The renderer normally emits character-level diffs; request a full
        # owned-PTY redraw before locating the two logical composer lines.
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 41, 121, 0, 0))
        os.killpg(process.pid, signal.SIGWINCH)
        pump(.5)
        fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
        os.killpg(process.pid, signal.SIGWINCH)
        pump(1)
        first, _ = fixture_position("Read VERSION")
        second, _ = fixture_position("Acceptance:")
        assert second > first, "SHIFT_ENTER_DID_NOT_INSERT_NEWLINE"
    else:
        send(prompt)
    send("\r")
    wait_for(lambda text, value: any(d.get("kind") == "plan" and d.get("state") == "pending"
             for d in value.get("decisions", [])) and
             ("IVOAI plan approval" in text or codex_frontend and "Approve" in text),
             480 if complex_case else 240, "PLAN_NOT_PRESENTED")
    assert not metadata().get("workers"), "WORKER_STARTED_BEFORE_APPROVAL"
    send("\r")  # Only the fixture plan is approved.
    if codex_frontend:
        pump(.5)
        send("\r")  # Native question review/submit, not a custom command.
    print("PLAN_PRESENTED=true PLAN_APPROVAL_SENT=true", flush=True)
    wait_for(lambda _, value: value.get("current_phase") == "synthesizing" and
             (value.get("executor_trace") or {}).get("final_response_present"),
             540 if complex_case else 240, "DAG_NOT_COMPLETED")
    value = metadata()
    assert all(t.get("state") == "completed" for t in value.get("tasks", [])), "TASK_INCOMPLETE"
    assert not value.get("executor_trace", {}).get("failure_class"), "EXECUTOR_FAILED"
    assert (repo / "VERSION").read_text() == "fixture-1\n", "FIXTURE_MODIFIED"
    if complex_case:
        assert (repo / "protected.txt").read_text() == "do not modify\n", "PROTECTED_MODIFIED"
        for file, expected in (("a.sh", "A"), ("b.sh", "B")):
            result = subprocess.run(["sh", file], cwd=repo, check=True, capture_output=True, text=True)
            assert result.stdout.strip() == expected, "FEATURE_ACCEPTANCE_FAILED"
        assert "a.sh" in (repo / "README.md").read_text() and "b.sh" in (repo / "README.md").read_text(), "DOCS_MISSING"
        paths = {w.get("worktree_path") for w in value.get("workers", []) if w.get("worktree_path")}
        assert len(paths) >= 2 and str(repo) not in paths, "WORKTREES_NOT_ISOLATED"
        assert peak_workers >= 2, "PARALLEL_WORKERS_NOT_OBSERVED"
        if os.environ.get("IVOAI_NATIVE_SMOKE_CAPABILITIES") == "1":
            capability_workers = value.get("workers", [])
            implementations = [w for w in capability_workers if w.get("role") == "implementation"]
            assert implementations and any("ponytail" in w.get("selected_skills", []) for w in implementations), "PONYTAIL_NOT_SELECTED"
            assert all("ponytail" not in w.get("selected_skills", []) for w in capability_workers if w.get("role") != "implementation"), "PONYTAIL_ROLE_CROSSOVER"
            assert all(len(w.get("selected_skills", [])) <= 3 for w in capability_workers), "SKILL_BROADCAST"
            print("NATIVE_CAPABILITY_METADATA=PASS PONYTAIL_IMPLEMENTATION_ONLY=PASS NO_GLOBAL_SKILL_BROADCAST=true", flush=True)
        print("DAG_COMPLEX=PASS PARALLEL_WORKERS=PASS WORKTREES=PASS FIXTURE_ACCEPTANCE=PASS", flush=True)
    assert value.get("frontend") == ("codex" if codex_frontend else "opencode")
    if ux_smoke:
        pump(1)
        row, column = fixture_position("fixture-1")
        send(f"\x1b[<0;{column};{row}M")
        pump(.2)
        send(f"\x1b[<32;{column + 9};{row}M")
        pump(.2)
        send(f"\x1b[<0;{column + 9};{row}m")
        pump(1)
        assert clipboard_read() == b"fixture-1", "NATIVE_MOUSE_CLIPBOARD_MISMATCH"
        print("SHIFT_ENTER_NEWLINE=PASS ENTER_SUBMIT=PASS PUBLIC_NATIVE_MOUSE_COPY=PASS X11_READBACK=PASS", flush=True)
    print(f"PLAN_APPROVAL=PASS FINAL_SYNTHESIS=PASS FRONTEND={frontend}", flush=True)
    if continuity_smoke:
        original = metadata()
        target_session_id = original["session_id"]
        original_thread = original["frontend_session_id"]
        original_provider = original["executor_session_id"]

        close_frontend()
        listing = subprocess.run([str(binary), "session", "list", "--json"], cwd=repo,
                                 check=True, capture_output=True, text=True)
        assert any(v["session_id"] == target_session_id for v in json.loads(listing.stdout)), "SESSION_LIST_MISSING"
        shown = subprocess.run([str(binary), "session", "show", "--json", target_session_id],
                               cwd=repo, check=True, capture_output=True, text=True)
        assert json.loads(shown.stdout)[0]["frontend_session_id"] == original_thread, "SESSION_SHOW_MAPPING_MISSING"
        if codex_frontend:
            reopen(["codex"])
            send("/resume")
            send("\r")
            wait_for(lambda text, _: "Resume a previous session" in text, 30, "NATIVE_PICKER_NOT_OPEN")
            assert "failed to connect to remote app server" not in history.lower(), "PICKER_CONNECTION_FAILED"
            send("\x1b")
            pump(1)
            assert process.poll() is None, "PICKER_CANCEL_KILLED_PRIMARY"
            history = ""
            send("/resume")
            send("\r")
            wait_for(lambda text, _: "Resume a previous session" in text, 30, "NATIVE_PICKER_REOPEN_FAILED")
            # Search the fixture conversation, not the newest empty startup thread.
            send("fixture")
            pump(2)
            send("\r")
        else:
            reopen(["session", "resume", target_session_id])
        wait_for(lambda _, v: v.get("state") == "running" and v.get("frontend_pid", 0) > 0,
                 30, "NATIVE_PICKER_SELECTION_FAILED")
        assert metadata()["frontend_session_id"] == original_thread, "NATIVE_THREAD_MAPPING_CHANGED"
        prior_attempts = len(metadata().get("turn_attempts", []))
        history = ""
        send("corrija isso")
        send("\r")
        wait_for(lambda text, _: "insufficient" in text.lower(), 30, "RESUMED_PROMPT_GATE_FAILED")
        assert len(metadata().get("turn_attempts", [])) == prior_attempts, "REJECTED_RESUME_STARTED_EXECUTOR"
        history = ""
        send("Objective: report the exact version value you reported in the preceding turn, without reading files or tools for discovery. Deliverable: only that remembered value. Acceptance: return the previous value alone; do not invent a new value; create one primary-owned research task, approve its plan, complete and integrate before synthesis.")
        send("\r")
        wait_for(lambda text, v: any(d.get("kind") == "plan" and d.get("state") == "pending" for d in v.get("decisions", [])) and ("IVOAI plan approval" in text or codex_frontend and "Approve" in text),
                 240, "RESUMED_PLAN_NOT_PRESENTED")
        send("\r")
        if codex_frontend:
            pump(.5)
            send("\r")
        wait_for(lambda _, v: v.get("current_phase") == "synthesizing" and
                 (v.get("executor_trace") or {}).get("final_response_present") and
                 len(v.get("turn_attempts", [])) > prior_attempts, 240, "RESUMED_TURN_NOT_COMPLETED")
        assert metadata()["executor_session_id"] == original_provider, "PROVIDER_NATIVE_RESUME_NOT_USED"
        pump(1)
        assert "fixture-1" in history, "NATIVE_CONVERSATION_FACT_NOT_RECALLED"
        close_frontend()
        reopen(["session", "resume", target_session_id])
        wait_for(lambda _, v: v.get("state") == "running" and v.get("frontend_pid", 0) > 0,
                 30, "SESSION_CLI_RESUME_FAILED")
        assert metadata()["frontend_session_id"] == original_thread, "CLI_AND_PICKER_DIVERGED"
        if switch_smoke:
            # Interrupt only the verified idle native frontend. The owning CLI
            # remains alive to clean up its App Server and release its lease.
            # An interrupted approved DAG is a separate proof.
            idle_attempts = len(metadata().get("turn_attempts", []))
            interrupt_owned_frontend()
            reopen(["session", "resume", target_session_id])
            assert len(metadata().get("turn_attempts", [])) == idle_attempts, "IDLE_RESTART_REPLAYED_TURN"
            assert metadata()["executor_session_id"] == original_provider, "IDLE_RESTART_CHANGED_PROVIDER"
            close_frontend()
            frontend, codex_frontend = "opencode", False
            reopen(["session", "resume", target_session_id, "--frontend", "opencode"])
            wait_for(lambda _, v: v.get("state") == "running" and v.get("frontend") == "opencode",
                     30, "FRONTEND_SWITCH_FAILED")
            assert metadata().get("frontend_sessions", {}).get("codex") == original_thread, "FRONTEND_SWITCH_LOST_NATIVE_MAPPING"
            assert len(metadata().get("turn_attempts", [])) == idle_attempts, "FRONTEND_SWITCH_REPLAYED_TURN"
            history = ""
            send("Objective: report the version value from our preceding turn using only retained conversation history. Deliverable: that value alone. Constraints: do not read files or use external tools for discovery. Acceptance: create one primary-owned research task, obtain plan approval, complete and integrate the plan, and return the remembered value.")
            send("\r")
            wait_for(lambda text, v: any(d.get("kind") == "plan" and d.get("state") == "pending" for d in v.get("decisions", [])) and "IVOAI plan approval" in text,
                     240, "SWITCHED_PLAN_NOT_PRESENTED")
            send("\r")
            wait_for(lambda _, v: v.get("current_phase") == "synthesizing" and
                     (v.get("executor_trace") or {}).get("final_response_present") and
                     len(v.get("turn_attempts", [])) > idle_attempts, 240, "SWITCHED_TURN_NOT_COMPLETED")
            pump(1)
            assert "fixture-1" in history, "FRONTEND_SWITCH_LOST_PROVIDER_HISTORY"
            assert metadata()["executor_session_id"] == original_provider, "FRONTEND_SWITCH_CHANGED_PROVIDER"
            assert metadata()["session_id"] == target_session_id, "FRONTEND_SWITCH_CHANGED_LOGICAL_ID"
            print("IDLE_INTERRUPTION=PASS FRONTEND_SWITCH=PASS PROVIDER_HISTORY=PASS NO_REPLAY=PASS", flush=True)
        if codex_frontend:
            print("CODEX_SLASH_RESUME=PASS NATIVE_PICKER=PASS PICKER_CANCEL=PASS", flush=True)
        print(f"FRONTEND={frontend} SESSION_LIST=PASS SESSION_SHOW=PASS SESSION_RESUME=PASS PROVIDER_NATIVE_RESUME=PASS NEW_TURN_GATE=PASS", flush=True)
    if recovery_smoke:
        # Opt-in real recovery: never write/patch session metadata or checkpoint.
        # Only an actually approved two-task readonly graph may be interrupted.
        original = metadata()
        target_session_id = original["session_id"]
        recovery_provider = original["executor_session_id"]
        recovery_scope = original["knowledge_scope_id"]
        fixture_head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repo,
                                      check=True, capture_output=True, text=True).stdout.strip()
        history = ""
        send("Objective: read VERSION in the fixture repository using exactly two sequential primary-owned research tasks, with no workers. Deliverable: report the version and confirm it is unchanged. Constraints: use no write paths, external MCP tools or external sources; do not modify any file. Task one reads VERSION and records its completion. Task two depends on task one and, only after task one is recorded complete, runs the readonly shell command sleep 20 before reading VERSION again. The pause allows the operator to interrupt this approved fixture execution. If recovering after an interruption, skip the pause and execute only the still-pending task. Acceptance: present the two-task native plan for approval; preserve already-completed work during recovery; integrate after both tasks complete; return fixture-1.")
        send("\r")
        wait_for(lambda text, v: any(d.get("kind") == "plan" and d.get("state") == "pending" for d in v.get("decisions", [])) and
                 ("IVOAI plan approval" in text or codex_frontend and "Approve" in text),
                 240, "READONLY_RECOVERY_PLAN_NOT_PRESENTED")
        assert readonly_recovery_snapshot(metadata(), recovery_checkpoint(), False) is not None, "RECOVERY_PLAN_NOT_STRICTLY_READONLY"
        interrupted_plan = metadata()["plan_id"]
        send("\r")
        if codex_frontend:
            pump(.5)
            send("\r")
        wait_for(lambda _, v: readonly_recovery_snapshot(v, recovery_checkpoint(), True) is not None,
                 120, "NO_SAFE_APPROVED_RECOVERY_WINDOW")
        observed = readonly_recovery_snapshot(metadata(), recovery_checkpoint(), True)
        assert observed is not None, "APPROVED_RECOVERY_WINDOW_CLOSED"
        completed_before, pending_before = observed
        interrupt_owned_frontend()
        # Cleanup must preserve a reconciliable host checkpoint; never repair it
        # artificially if the primary finished or a write appeared in the race.
        interrupted = metadata()
        assert readonly_recovery_snapshot(interrupted, recovery_checkpoint(), True) == observed, "INTERRUPTED_CHECKPOINT_CHANGED"
        assert interrupted["plan_id"] == interrupted_plan, "INTERRUPTED_PLAN_CHANGED"
        failed_attempts = len(interrupted.get("turn_attempts", []))
        reopen(["session", "recover", target_session_id], failed_attempts)
        wait_for(lambda text, v: v.get("plan_id") != interrupted_plan and
                 any(d.get("kind") == "plan" and d.get("state") == "pending" and d.get("id") == v.get("plan_id") for d in v.get("decisions", [])) and
                 ("IVOAI plan approval" in text or codex_frontend and "Approve" in text),
                 240, "RECOVERY_NEW_APPROVAL_NOT_PRESENTED", failed_attempts)
        recovered_plan = metadata()["plan_id"]
        recovered = readonly_recovery_snapshot(metadata(), recovery_checkpoint(), False)
        assert recovered == observed, "RECOVERY_REOPENED_COMPLETED_TASK"
        assert recovery_checkpoint()["recovery"]["approved"] is False, "RECOVERY_BYPASSED_NEW_APPROVAL"
        send("\r")
        if codex_frontend:
            pump(.5)
            send("\r")
        wait_for(lambda _, v: v.get("plan_id") == recovered_plan and v.get("current_phase") == "synthesizing" and
                 (v.get("executor_trace") or {}).get("final_response_present") and
                 all(t.get("state") == "completed" for t in v.get("tasks", [])),
                 300, "RECOVERED_DAG_NOT_COMPLETED", failed_attempts)
        final = metadata()
        checkpoint = recovery_checkpoint()["recovery"]
        assert checkpoint["approved"] is True and checkpoint["integrated"] is True, "RECOVERY_NOT_APPROVED_AND_INTEGRATED"
        assert {t["id"] for t in final["tasks"]} == set(completed_before + pending_before), "RECOVERY_CHANGED_TASK_GRAPH"
        assert final["executor_session_id"] == recovery_provider, "RECOVERY_CHANGED_PROVIDER_HISTORY"
        assert final["knowledge_scope_id"] == recovery_scope and final["session_id"] == target_session_id, "RECOVERY_CHANGED_IDENTITY"
        assert (repo / "VERSION").read_text() == "fixture-1\n", "RECOVERY_MODIFIED_FIXTURE"
        current_head = subprocess.run(["git", "rev-parse", "HEAD"], cwd=repo, check=True, capture_output=True, text=True).stdout.strip()
        dirty = subprocess.run(["git", "status", "--porcelain"], cwd=repo, check=True, capture_output=True, text=True).stdout
        assert current_head == fixture_head and not dirty, "RECOVERY_MUTATED_REPOSITORY"
        print("APPROVED_READONLY_INTERRUPTION=PASS FRESH_RECOVERY_APPROVAL=PASS COMPLETED_TASKS_PRESERVED=PASS RECOVERY_INTEGRATED=PASS RECOVERY_IDENTITY=PASS", flush=True)
finally:
    if process.poll() is None:
        send("/exit")
        send("\r")
        pump(3)
    if process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
    try:
        os.close(master)
    except OSError:
        pass  # A failed restart can leave the preceding owned PTY closed.
    if previous_clipboard is not None:
        # Do not overwrite a concurrent operator clipboard change.
        if clipboard_read() == b"fixture-1":
            subprocess.run(["xclip", "-selection", "clipboard", "-i"], input=previous_clipboard,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=3)
            assert clipboard_read() == previous_clipboard, "CLIPBOARD_RESTORE_FAILED"
