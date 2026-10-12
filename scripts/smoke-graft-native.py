#!/usr/bin/env python3
"""Opt-in real managed OpenCode + official Codex + pinned Graft smoke.

Invoked by TestLiveGraftManagedOpenCode with isolated XDG component state.
Prints only fixed assertions and operational counts; no terminal transcript,
model output, auth material, or provider configuration is saved or printed.
Use --self-test to test admission/evidence checks without processes or providers.
"""
import json
import sys

FIXTURE_PROMPT = ("Objective: analyze target.ts and caller.ts to locate the direct caller of function GraftNativeTarget. "
          "Deliverable: a report containing the caller function name. Constraints: one primary-owned research task, no workers, "
          "no edits, no external knowledge and no shell or alternate file-discovery tools. "
          "After native plan approval use ivoai-code graft_trace_calls with symbol GraftNativeTarget "
          "to obtain actual structural evidence; then complete and integrate the task before synthesis. "
          "Acceptance: the native code tool must return structural evidence and the final report must contain only the direct caller name.")


def admissible_plan(value, checkpoint, directory):
    recovery = checkpoint.get("recovery") or {}
    plan = recovery.get("plan") or {}
    tasks = plan.get("tasks") or []
    if (recovery.get("directory") != directory or recovery.get("approved") is not False
            or not plan.get("id") or plan["id"] != value.get("plan_id")
            or len(tasks) != 1 or value.get("workers")):
        return False
    task = tasks[0]
    tools = task.get("allowed_mcp_tools") or {}
    return (task.get("role") == "research" and task.get("execution_mode") == "primary"
            and task.get("id") and not task.get("knowledge_sources")
            and not task.get("write_paths") and not task.get("dependencies")
            and set(task.get("allowed_mcps") or []) <= {"ivoai-code"}
            and set(tools) <= {"ivoai-code"}
            and set(tools.get("ivoai-code") or []) <= {"graft_trace_calls"}
            and [t.get("id") for t in value.get("tasks", [])] == [task.get("id")])


def native_evidence(value):
    trace = value.get("executor_trace") or {}
    operation = any(e.get("operation") == "code.inspect" and e.get("provider") == "graft"
                    and e.get("state") == "completed" and (e.get("source_files") or 0) >= 2
                    and (e.get("index_files") or 0) >= 2
                    for e in value.get("observability", []))
    call = any(t.get("server") == "ivoai-code" and t.get("name") == "graft_trace_calls"
               and t.get("status") == "completed" and not t.get("failure_class")
               for t in trace.get("mcp_operations", []))
    return operation and call and trace.get("executor") == "codex"


def self_test():
    task = {"id": "fixture", "role": "research", "execution_mode": "primary",
            "allowed_mcp_tools": {"ivoai-code": ["graft_trace_calls"]}}
    checkpoint = {"recovery": {"directory": "/fixture", "approved": False,
                              "plan": {"id": "plan-fixture", "tasks": [task]}}}
    value = {"plan_id": "plan-fixture", "tasks": [{"id": "fixture"}], "workers": []}
    assert admissible_plan(value, checkpoint, "/fixture"), "VALID_FIXTURE_PLAN_REJECTED"
    for key, bad in (("write_paths", ["*"]), ("execution_mode", "worker"),
                     ("allowed_mcps", ["external"]),
                     ("allowed_mcp_tools", {"ivoai-code": ["execute"]})):
        changed = json.loads(json.dumps(checkpoint))
        changed["recovery"]["plan"]["tasks"][0][key] = bad
        assert not admissible_plan(value, changed, "/fixture"), "UNSAFE_PLAN_ACCEPTED"
    assert not native_evidence({"observability": [{"operation": "code.inspect", "state": "completed", "provider": "direct"}]}), "DIRECT_FALLBACK_ACCEPTED"
    good = {"observability": [{"operation": "code.inspect", "state": "completed", "provider": "graft", "source_files": 2, "index_files": 2}],
            "executor_trace": {"executor": "codex", "mcp_operations": [{"server": "ivoai-code", "name": "graft_trace_calls", "status": "completed"}]}}
    assert native_evidence(good), "NATIVE_EVIDENCE_REJECTED"
    del good["executor_trace"]
    assert not native_evidence(good), "EVENT_WITHOUT_REAL_TOOL_ACCEPTED"
    print("GRAFT_DRIVER_CONTRACTS=PASS")


def live(binary_argument, root_argument):
    import fcntl
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

    assert os.environ.get("IVOAI_GRAFT_NATIVE_LIVE") == "1", "EXPLICIT_LIVE_OPT_IN_REQUIRED"
    binary, root = Path(binary_argument).resolve(), Path(root_argument).resolve()
    assert all(Path(os.environ["XDG_" + k + "_HOME"]).resolve().is_relative_to(root)
               for k in ("CONFIG", "DATA", "STATE", "CACHE")), "NONISOLATED_XDG"
    repo = root / "graft-fixture"
    repo.mkdir(mode=0o700)
    files = {
        "target.ts": "export function GraftNativeTarget(): number { return 7; }\n",
        "caller.ts": "import { GraftNativeTarget } from './target';\nexport function GraftNativeCaller(): number { return GraftNativeTarget(); }\n",
    }
    for name, content in files.items():
        (repo / name).write_text(content)
    for args in (["init", "-q"], ["config", "user.name", "Fixture"],
                 ["config", "user.email", "fixture@example.invalid"],
                 ["add", "."], ["commit", "-qm", "graft fixture baseline"]):
        subprocess.run(["git", *args], cwd=repo, check=True, capture_output=True)
    sessions = Path(os.environ["XDG_STATE_HOME"]) / "ivoai/sessions"
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))

    def owner():
        os.setsid()
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)

    process = subprocess.Popen([str(binary), "opencode"], cwd=repo, stdin=slave,
                               stdout=slave, stderr=slave, preexec_fn=owner)
    os.close(slave)
    history = ""

    def pump():
        nonlocal history
        if select.select([master], [], [], .1)[0]:
            try:
                body = os.read(master, 65536)
            except OSError:
                return
            if b"\x1b[6n" in body:
                os.write(master, b"\x1b[1;1R")
            history = (history + body.decode(errors="replace"))[-(1 << 20):]

    def send(text):
        os.write(master, text.encode())
        # Drain native renderer output between paste and Enter, as in the
        # established native smoke driver; a full PTY output buffer stalls UI.
        deadline = time.monotonic() + .3
        while time.monotonic() < deadline:
            pump()

    def native_composer(value, prompt):
        # The distributed official SDK exposes these TUI operations. Resolve
        # only this fixture's attached child and keep its ephemeral password in
        # memory; never read personal credentials or print process environment.
        import base64
        import urllib.parse
        import urllib.request
        pid = value.get("frontend_pid")
        assert isinstance(pid, int) and pid > 0, "NATIVE_FRONTEND_PID_MISSING"
        proc = Path("/proc") / str(pid)
        stat = (proc / "stat").read_text().rsplit(")", 1)[1].split()
        assert int(stat[1]) == process.pid, "NATIVE_FRONTEND_NOT_OWNED_CHILD"
        assert stat[19] == str(value.get("frontend_process_start")), "NATIVE_FRONTEND_IDENTITY_CHANGED"
        assert os.path.samefile(proc / "exe", os.environ["IVOAI_GRAFT_SMOKE_OPENCODE"]), "NATIVE_FRONTEND_EXECUTABLE_MISMATCH"
        args = (proc / "cmdline").read_bytes().split(b"\0")
        endpoint = args[args.index(b"attach") + 1].decode()
        parsed = urllib.parse.urlsplit(endpoint)
        assert parsed.scheme == "http" and parsed.hostname == "127.0.0.1" and parsed.port and not parsed.username and parsed.path in ("", "/") and not parsed.query and not parsed.fragment, "NONLOCAL_NATIVE_BACKEND"
        environment = dict(part.split(b"=", 1) for part in (proc / "environ").read_bytes().split(b"\0") if b"=" in part)
        password = environment[b"OPENCODE_SERVER_PASSWORD"]
        del environment
        authorization = "Basic " + base64.b64encode(b"ivoai:" + password).decode()
        del password
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args, **kwargs):
                raise RuntimeError("NATIVE_BACKEND_REDIRECT_FORBIDDEN")
        client = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        query = "?directory=" + urllib.parse.quote(str(repo), safe="")
        def request(method, path, body=None):
            data = json.dumps(body).encode() if body is not None else None
            req = urllib.request.Request(endpoint.rstrip("/") + path, data=data, method=method,
                                         headers={"Authorization": authorization, "Content-Type": "application/json"})
            with client.open(req, timeout=10) as response:
                content = response.read((4 << 20) + 1)
                assert len(content) <= 4 << 20, "OVERSIZED_NATIVE_CONTRACT"
                return content
        schema = json.loads(request("GET", "/doc"))
        for path in ("/tui/append-prompt", "/tui/submit-prompt"):
            assert "post" in schema.get("paths", {}).get(path, {}), "OFFICIAL_TUI_CONTRACT_UNAVAILABLE"
        request("POST", "/tui/append-prompt" + query, {"text": prompt})
        wait_for(lambda text, _: "GraftNativeTarget" in text, 15, "FIXTURE_PROMPT_NOT_RENDERED")
        request("POST", "/tui/submit-prompt" + query)
        print("GRAFT_NATIVE_COMPOSER_API=PASS", flush=True)

    def metadata():
        matches = []
        for path in (list(sessions.glob("*.json")) + list((sessions / ".native-opencode").glob("*.json"))
                     + list((sessions / ".orchestrated-frontends").glob("*.json"))):
            value = json.loads(path.read_text())
            if value.get("working_directory") == str(repo):
                matches.append(value)
        assert len(matches) <= 1, "AMBIGUOUS_FIXTURE_SESSION"
        return matches[0] if matches else {}

    def checkpoint(value):
        path = sessions / "checkpoints" / (value["session_id"] + ".json")
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(descriptor, "rb") as source:
            body = source.read(32769)
        assert len(body) <= 32768, "OVERSIZED_CHECKPOINT"
        return json.loads(body)

    def wait_for(predicate, timeout, failure):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline and process.poll() is None:
            pump()
            value = metadata()
            assert not value.get("workers"), "UNREQUESTED_WORKER"
            assert not any(d.get("kind") == "routing" and d.get("state") == "pending"
                           for d in value.get("decisions", [])), "UNREQUESTED_PROVIDER_ROUTING"
            plain = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", history)
            if predicate(plain, value):
                return value
            if (value.get("executor_trace") or {}).get("failure_class"):
                break
        value = metadata()
        trace = value.get("executor_trace") or {}
        print(json.dumps({"failure": failure, "phase": value.get("current_phase"),
                          "tasks": len(value.get("tasks", [])), "events": trace.get("stdout_events"),
                          "failure_class": trace.get("failure_class"),
                          "terminal_bytes": len(history),
                          "terminal_landmarks": [x for x in ("IVOAI", "OpenCode", "opencode", "Error", "error", "Connecting", "connect") if x in history],
                          "native_evidence": native_evidence(value),
                          "quota": {name: {key: q.get(key) for key in
                                    ("authenticated", "eligible", "hard_limit_reached", "telemetry_unknown")}
                                    for name, q in (value.get("quota") or {}).items()}}), flush=True)
        raise RuntimeError(failure)

    try:
        wait_for(lambda text, _: not (termios.tcgetattr(master)[3] & termios.ICANON) and
                 ("IVOAI control plane" in text or "IVOAI Automatic Orchestration" in text),
                 120, "MANAGED_FRONTEND_PANEL_NOT_READY")
        prompt = FIXTURE_PROMPT
        native_composer(metadata(), prompt)
        value = wait_for(lambda text, v: "IVOAI plan approval" in text and
                         any(d.get("kind") == "plan" and d.get("state") == "pending"
                             for d in v.get("decisions", [])), 240, "PLAN_NOT_PRESENTED")
        assert not any(e.get("operation") == "code.inspect" and e.get("state") == "completed"
                       for e in value.get("observability", [])), "CODE_EXECUTED_BEFORE_APPROVAL"
        saved = checkpoint(value)
        if not admissible_plan(value, saved, str(repo)):
            recovery = saved.get("recovery") or {}
            plan = recovery.get("plan") or {}
            print(json.dumps({"failure": "PLAN_EXCEEDS_FIXTURE_AUTHORIZATION",
                "directory_matches": recovery.get("directory") == str(repo),
                "approved": recovery.get("approved"), "plan_matches": plan.get("id") == value.get("plan_id"),
                "tasks": [{"role": t.get("role"), "mode": t.get("execution_mode"),
                    "knowledge_count": len(t.get("knowledge_sources") or []),
                    "write_count": len(t.get("write_paths") or []),
                    "dependency_count": len(t.get("dependencies") or []),
                    "only_code_mcp": set(t.get("allowed_mcps") or []) <= {"ivoai-code"},
                    "exact_trace_grant": t.get("allowed_mcp_tools") == {"ivoai-code": ["graft_trace_calls"]}}
                    for t in plan.get("tasks", [])],
                "metadata_ids_match": [t.get("id") for t in value.get("tasks", [])] == [t.get("id") for t in plan.get("tasks", [])]}), flush=True)
            raise RuntimeError("PLAN_EXCEEDS_FIXTURE_AUTHORIZATION")
        send("\r")
        print("GRAFT_NATIVE_PLAN_APPROVED=PASS", flush=True)
        value = wait_for(lambda _, v: v.get("current_phase") == "synthesizing" and
                         (v.get("executor_trace") or {}).get("final_response_present"),
                         300, "NATIVE_TASK_DID_NOT_COMPLETE")
        assert value.get("tasks") and all(t.get("state") == "completed" for t in value["tasks"]), "TASK_INCOMPLETE"
        assert checkpoint(value)["recovery"]["integrated"] is True, "PLAN_NOT_INTEGRATED"
        assert native_evidence(value), "REAL_GRAFT_TOOL_CALL_NOT_PROVEN"
        print("GRAFT_NATIVE_TASK_INTEGRATED=PASS OFFICIAL_CODEX_NATIVE_TOOL=PASS GRAFT_STRUCTURAL_EVIDENCE=PASS", flush=True)
        # Session persistence can precede final SSE/TUI rendering. Wait for the
        # native renderer rather than treating persisted completion as visible.
        wait_for(lambda text, _: "GraftNativeCaller" in text, 30, "STRUCTURAL_ANSWER_NOT_VISIBLE")
        wait_for(lambda text, _: "code.inspect" in text, 15, "CODE_OBSERVATION_NOT_VISIBLE_IN_NATIVE_PANEL")
        for name, content in files.items():
            assert (repo / name).read_text() == content, "SOURCE_MODIFIED"
        clean = subprocess.run(["git", "status", "--porcelain"], cwd=repo, check=True, capture_output=True)
        assert not clean.stdout, "REPOSITORY_OR_INDEX_MUTATED"
        print("GRAFT_MANAGED_OPENCODE_PANEL=PASS OFFICIAL_CODEX_NATIVE_TOOL=PASS GRAFT_STRUCTURAL_EVIDENCE=PASS REPO_MUTATIONS=0", flush=True)
        send("/exit")
        time.sleep(.2)
        send("\r")
        deadline = time.monotonic() + 15
        while process.poll() is None and time.monotonic() < deadline:
            pump()
        assert process.poll() == 0, "FRONTEND_DID_NOT_EXIT_CLEANLY"
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=5)
        os.close(master)


if __name__ == "__main__":
    if sys.argv[1:] == ["--fixture-prompt"]:
        print(FIXTURE_PROMPT)
        raise SystemExit(0)
    if sys.argv[1:] == ["--self-test"]:
        self_test()
    else:
        live(*sys.argv[1:])
