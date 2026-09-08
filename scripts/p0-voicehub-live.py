#!/usr/bin/env python3
"""Operator acceptance through the real ivoai auto process and native TUI.

Requires an official Codex login and real configured Memory/Context.
Claude live acceptance is optional when its official client is not authenticated.
No provider credentials are inspected. Raw terminal evidence is private.
"""
import argparse
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
import termios
import time

PROMPT = "Consulte o contexto e memória e comente um pouco sobre o projeto Voicehub."
parser = argparse.ArgumentParser()
parser.add_argument("binary")
parser.add_argument("--executor", choices=("codex", "claude"), required=True)
parser.add_argument("--evidence", required=True)
args = parser.parse_args()
os.umask(0o077)
evidence = Path(args.evidence)
evidence.mkdir(parents=True, exist_ok=True)
sessions = Path(os.environ.get("XDG_STATE_HOME", str(Path.home() / ".local/state"))) / "ivoai/sessions"
cache = Path(os.environ.get("XDG_CACHE_HOME", str(Path.home() / ".cache"))) / "ivoai/capabilities.json"
known = set(sessions.glob("*.json"))
master, slave = pty.openpty()
fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 120, 0, 0))

def terminal_owner():
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)

process = subprocess.Popen([str(Path(args.binary).resolve()), "auto"], stdin=slave,
                           stdout=slave, stderr=slave, preexec_fn=terminal_owner)
os.close(slave)
transcript = (evidence / (args.executor + "-tui.raw")).open("wb")
terminal_history = ""

def pump(seconds):
    global terminal_history
    deadline = time.monotonic() + seconds
    chunks = []
    while time.monotonic() < deadline:
        if select.select([master], [], [], min(.1, max(0, deadline-time.monotonic())))[0]:
            try:
                data = os.read(master, 65536)
            except OSError:
                break
            if not data:
                break
            chunks.append(data)
            transcript.write(data)
            transcript.flush()
        if process.poll() is not None:
            break

    rendered = b"".join(chunks).decode(errors="replace")
    terminal_history = (terminal_history + rendered)[-(1 << 20):]
    return rendered

def send(text):
    os.write(master, text.encode())
    return pump(.4)

def command(text):
    return send(text) + send("\r") + pump(1)

def wait_rendered(expected, initial="", seconds=8):
    rendered = initial
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline and process.poll() is None:
        plain = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", rendered)
        if expected.lower() in plain.lower():
            return True
        rendered += pump(.2)
    return False

try:
    current_path = None
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline and process.poll() is None:
        for path in set(sessions.glob("*.json"))-known:
            value = json.loads(path.read_text())
            if value.get("state") == "running" and value.get("frontend") == "opencode":
                current_path = path
                break
        if current_path:
            break
        pump(.3)
    if current_path is None:
        raise RuntimeError("IVOAI_AUTO_START_FAILED (inspect private terminal evidence)")
    # Session metadata becomes running before the frontend is fully attached.
    # Wait for a rendered managed surface instead of racing fixed startup sleeps.
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline and process.poll() is None:
        pump(.3)
        rendered = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", terminal_history)
        # "managed OpenCode frontend" also occurs in IVOAI's preflight stdout;
        # require plugin-only wording, otherwise keys can reach startup stdin.
        if ("IVOAI control plane" in rendered or "IVOAI knowledge" in rendered or
                "IVOAI Automatic Orchestration" in rendered):
            break
    else:
        raise RuntimeError("MANAGED_TUI_READINESS_UNCONFIRMED")
    registry = json.loads(cache.read_text()).get("providers", {})
    provider = registry.get(args.executor, {})
    models = provider.get("models", [])
    if args.executor == "claude" and not provider.get("authenticated"):
        # Only missing external live authentication is optional. An
        # authenticated client with broken discovery or execution must fail.
        result = {"executor": "claude", "live_e2e": "SKIPPED_NOT_CONFIGURED",
                  "operator_auth": "NOT_AUTHENTICATED" if provider else "NOT_CONFIGURED"}
        (evidence / "claude-live-status.json").write_text(json.dumps(result, indent=2)+"\n")
        print("CLAUDE_LIVE_E2E=SKIPPED_NOT_CONFIGURED CLAUDE_SUPPORT=OPTIONAL_CAPABILITY")
        raise SystemExit(0)
    if not provider.get("authenticated") or not models:
        raise RuntimeError("EXPLICIT_MODEL_UNAVAILABLE: no authenticated runtime catalog for " + args.executor)
    model = next((v for v in models if v.get("is_default")), models[0])
    model_id = model["name"]
    efforts = model.get("supported_efforts", [])
    effort = model.get("default_effort") or (efforts[0] if efforts else "")
    command("/new")
    if not wait_rendered("Select model", command("/models")):
        raise RuntimeError("NATIVE_MODEL_DIALOG_UNCONFIRMED")
    model_render = send(model.get("display_name") or model_id) + send("\r") + pump(1)
    if efforts:
        # Pinned DialogModel may open DialogVariant automatically after selecting
        # a model. Otherwise Ctrl+T opens it (not a cycle). Never toggle an
        # already open dialog or type an effort into the conversation composer.
        if not wait_rendered("variant", model_render, seconds=2) and not wait_rendered("variant", send("\x14")):
            raise RuntimeError("NATIVE_REASONING_DIALOG_UNCONFIRMED")
        send(effort)
        send("\r")
        pump(1)
    baseline = json.loads(current_path.read_text()).get("executor_trace")
    command(PROMPT)
    deadline = time.monotonic()+300
    trace = None
    while time.monotonic() < deadline and process.poll() is None:
        value = json.loads(current_path.read_text())
        candidate = value.get("executor_trace")
        if candidate and candidate != baseline:
            trace = candidate
            break
        pump(.3)
    if trace is None:
        raise RuntimeError("VOICEHUB_COMPLETION_TIMEOUT")
    (evidence / (args.executor+"-trace.json")).write_text(json.dumps(trace, indent=2)+"\n")
    completed = {v.get("name") for v in trace.get("mcp_operations", [])
                 if v.get("status") == "completed" and not v.get("failure_class")}
    memory = bool(completed & {"memory_query", "memory_read_page"})
    context = "context_search" in completed
    if (trace.get("failure_class") or trace.get("exit_code") != 0 or
            not trace.get("completion_event") or not trace.get("final_response_present") or
            not memory or not context):
        raise RuntimeError(f"VOICEHUB_ACCEPTANCE_FAILED memory={memory} context={context} failure={trace.get('failure_class', '')}")
    if (value.get("requested_executor") != args.executor or
            value.get("requested_model") != model_id or value.get("effective_model") != model_id or
            value.get("requested_effort", "") != effort or value.get("effective_effort", "") != effort):
        raise RuntimeError("REQUESTED_EFFECTIVE_MISMATCH_OR_UNKNOWN")
    command("/models")
    send("Automatic")
    send("\r")
    print(json.dumps({"executor": args.executor, "requested_model": model_id,
                      "effective_model": value.get("effective_model"), "reasoning": effort,
                      "evidence_source": value.get("configuration_source")}))
    print("VOICEHUB_REPRODUCER=PASS TURN_SUCCESS=PASS MEMORY_LOOKUP=PASS CONTEXT_LOOKUP=PASS_OR_EMPTY_VALID BRIDGE_ERROR=false FINAL_RESPONSE_PRESENT=true REQUESTED_EFFECTIVE_MATCH=PASS")
finally:
    if process.poll() is None:
        command("/exit")
        pump(5)
    if process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
    transcript.close()
    os.close(master)
