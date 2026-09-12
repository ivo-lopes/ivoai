#!/usr/bin/env python3
"""Isolated artifact smoke: official AUTO + pinned frontend + official client.

Only fixture content is submitted. No transcript is persisted or printed.
The caller supplies isolated XDG directories and component metadata.
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
        if value.get("working_directory") == str(repo):
            return value
    return {}

def wait_for(predicate, timeout, failure):
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
        if value.get("turn_attempts", []) and value["turn_attempts"][-1].get("turn_state") == "failed":
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
                      "turn_failure": (value.get("turn_attempts") or [{}])[-1].get("failure_class")}), flush=True)
    raise RuntimeError(failure)

try:
    if ux_smoke:
        assert os.environ.get("DISPLAY") and not os.environ.get("WAYLAND_DISPLAY"), "X11_SESSION_REQUIRED"
        targets = clipboard_read("TARGETS").decode().split()
        assert all(t in {"TARGETS", "MULTIPLE", "TIMESTAMP", "SAVE_TARGETS", "UTF8_STRING", "TEXT", "STRING", "text/plain", "text/plain;charset=utf-8", "text/plain;charset=UTF-8"} for t in targets), "PRESERVING_RICH_OPERATOR_CLIPBOARD"
        previous_clipboard = clipboard_read()
    wait_for(lambda text, _: "IVOAI control plane" in text or
             "IVOAI Automatic Orchestration" in text or codex_frontend and "OpenAI Codex" in text, 120, "FRONTEND_NOT_READY")
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
        pump(.5)
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
    os.close(master)
    if previous_clipboard is not None:
        # Do not overwrite a concurrent operator clipboard change.
        if clipboard_read() == b"fixture-1":
            subprocess.run(["xclip", "-selection", "clipboard", "-i"], input=previous_clipboard,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=3)
            assert clipboard_read() == previous_clipboard, "CLIPBOARD_RESTORE_FAILED"
