#!/usr/bin/env python3
"""Frozen synthetic-command output-view diagnostic; no LLM or personal config."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import tempfile
import time

SCENARIOS = {"generated_git_status", "generated_git_log", "generated_grep"}
DIAGNOSTICS = {"disabled", "policy_bypass", "provider_unavailable", "provider_failed", "provider_timeout", "view_rejected", "applied", "gateway_error"}
EXPECTED_RTK_SHA = "0b80810970ce2c79c92deaa474bbcf6f1cf01ac93baab0e273ee6cb4ef950d10"
EXPECTED_ARCHIVE_SHA = "4dec94a64035e08189ae82fd5408fe4ae9ae27a665fc07e3e3ec3c4c2d8e8a3e"
INTEGER_FIELDS = {"repetition", "arm", "order", "duration_us", "raw_bytes", "view_bytes", "facts_total", "facts_present", "facts_missing", "capture_us"}
BOOL_FIELDS = {"derived", "exact_recovery", "baseline_correct", "provider_failure", "view_error", "artifact_persisted"}
STRING_FIELDS = {"scenario", "diagnostic", "artifact_id", "snapshot_sha256"}
MARKER = re.compile(r"^    output_view_generated_test\.go:[0-9]+: GENERATED_RTK_JSON=(\{.*\})$")


def sha(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def validate_row(row):
    if not isinstance(row, dict) or set(row) != INTEGER_FIELDS | BOOL_FIELDS | STRING_FIELDS:
        raise ValueError("row_schema")
    if any(type(row[k]) is not int or row[k] < 0 for k in INTEGER_FIELDS):
        raise ValueError("row_number")
    if any(type(row[k]) is not bool for k in BOOL_FIELDS):
        raise ValueError("row_flag")
    if any(type(row[k]) is not str for k in STRING_FIELDS):
        raise ValueError("row_identity")
    if row["scenario"] not in SCENARIOS or row["diagnostic"] not in DIAGNOSTICS:
        raise ValueError("row_label")
    if row["repetition"] >= 6 or row["arm"] > 1 or row["order"] > 1 or row["arm"] != (row["repetition"] + row["order"]) % 2:
        raise ValueError("row_pair")
    if not re.fullmatch(r"artifact_[a-zA-Z0-9_-]+", row["artifact_id"]) or not re.fullmatch(r"[0-9a-f]{64}", row["snapshot_sha256"]):
        raise ValueError("row_digest")
    if row["facts_present"] + row["facts_missing"] != row["facts_total"] or row["facts_total"] == 0:
        raise ValueError("row_facts")
    return row


def observations(stdout):
    rows = []
    for line in stdout.splitlines():
        match = MARKER.fullmatch(line)
        if match:
            rows.append(validate_row(json.loads(match[1])))
    return rows


def summarize(rows):
    keys = [(r["scenario"], r["repetition"], r["arm"]) for r in rows]
    expected = {(s, r, a) for s in SCENARIOS for r in range(6) for a in range(2)}
    complete = len(keys) == 36 and len(set(keys)) == 36 and set(keys) == expected
    summary = {"complete_36_observations": complete, "no_rows_excluded": True, "semantic_parity_scored": False, "session_economics_scored": False, "scenarios": {}}
    for scenario in sorted(SCENARIOS):
        selected = [r for r in rows if r["scenario"] == scenario]
        arms = {}
        for arm in (0, 1):
            values = [r for r in selected if r["arm"] == arm]
            arms[str(arm)] = {"count": len(values), "duration_total_us": sum(r["duration_us"] for r in values), "view_bytes_total": sum(r["view_bytes"] for r in values), "derived_count": sum(r["derived"] for r in values), "provider_failures": sum(r["provider_failure"] for r in values), "gateway_errors": sum(r["view_error"] for r in values), "exact_recoveries": sum(r["exact_recovery"] for r in values), "all_literal_facts_present_count": sum(r["facts_missing"] == 0 for r in values), "literal_facts_missing_total": sum(r["facts_missing"] for r in values), "baseline_correct_count": sum(r["baseline_correct"] for r in values), "diagnostics": {label: sum(r["diagnostic"] == label for r in values) for label in sorted({r["diagnostic"] for r in values})}}
        summary["scenarios"][scenario] = {"arms": arms}
    return summary


def private_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".rtk-generated-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(value, stream, indent=2, sort_keys=True)
            stream.write("\n")
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rtk", required=True, type=Path)
    parser.add_argument("--archive", required=True, type=Path)
    parser.add_argument("--go", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--module-cache", required=True, type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    private = Path(tempfile.mkdtemp(prefix="ivoai-rtk-generated-frozen-"))
    for name in ("home", "cache"):
        (private / name).mkdir(mode=0o700)
    env = {"HOME": str(private / "home"), "XDG_CONFIG_HOME": str(private / "home"), "PATH": "/usr/bin:/bin", "LANG": "C", "LC_ALL": "C", "GOTOOLCHAIN": "local", "GOMODCACHE": str(args.module_cache.resolve()), "GOCACHE": str(private / "cache"), "GOMAXPROCS": "2", "CGO_ENABLED": "0", "GOAMD64": "v1", "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_SYSTEM": "/dev/null"}
    report = {"schema_version": 1, "runtime_source_commit": "ae3f4b186b932dc8bd2b4c272ad672bf2704c027", "recorded_at_unix": int(time.time()), "kind": "synthetic_command_output_views_abba", "observations": [], "phases": [], "provider_tokens": None, "provider_cost": None, "quota_consumption": None, "cpu_measurement": None, "resource_limits_are_not_measurements": True, "new_inference_invocations": 0, "private_build_directory": str(private), "status": "INCOMPLETE"}

    def run(label, command, timeout=180):
        start = time.monotonic()
        proc = subprocess.Popen([str(a) for a in command], cwd=root, env=env, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, start_new_session=True)
        timed_out = False
        try:
            stdout, _ = proc.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            timed_out = True
            os.killpg(proc.pid, signal.SIGKILL)
            stdout, _ = proc.communicate()
        report["phases"].append({"label": label, "exit_code": proc.returncode, "timed_out": timed_out, "elapsed_ms": round((time.monotonic() - start) * 1000, 3)})
        if proc.returncode or timed_out:
            raise RuntimeError("phase_failed_" + label)
        return stdout

    try:
        source = run("source_commit", ["/usr/bin/git", "rev-parse", "HEAD"]).strip()
        if not re.fullmatch(r"[0-9a-f]{40}", source):
            raise ValueError("source_identity")
        report["source_commit"] = source
        if run("clean_source", ["/usr/bin/git", "status", "--porcelain", "--untracked-files=all"]):
            raise ValueError("source_not_frozen_clean")
        report["source_go_sha256"] = {p: sha(root / p) for p in run("source_go_inventory", ["/usr/bin/git", "ls-files", "*.go"]).splitlines()}
        report["harness_sha256"] = sha(Path(__file__).resolve())
        report["go_mod_sha256"], report["go_sum_sha256"] = sha(root / "go.mod"), sha(root / "go.sum")
        for label, path, expected in (("rtk_binary_sha256", args.rtk, EXPECTED_RTK_SHA), ("rtk_archive_sha256", args.archive, EXPECTED_ARCHIVE_SHA)):
            digest = sha(path)
            report[label] = digest
            if digest != expected:
                raise ValueError("pin_mismatch")
        for label, command, expression in (("go_version", [args.go, "version"], r"go version go1\.27\.3 linux/amd64"), ("git_version", ["/usr/bin/git", "--version"], r"git version [0-9]+\.[0-9]+\.[0-9]+"), ("grep_version", ["/usr/bin/grep", "--version"], r"grep \(GNU grep\) [0-9]+\.[0-9]+")):
            first = run(label, command).splitlines()[0]
            if not re.fullmatch(expression, first):
                raise ValueError("unexpected_tool_version")
            report[label] = first
        helper, test = private / "ivoai-output-view-sandbox", private / "evidence-generated.test"
        run("build_helper", [args.go, "build", "-trimpath", "-buildvcs=false", "-o", helper, "./cmd/ivoai-output-view-sandbox"])
        helper.chmod(0o500)
        report["helper_sha256"], report["helper_mode"] = sha(helper), "0500"
        run("build_test_binary", [args.go, "test", "-c", "-trimpath", "-buildvcs=false", "-o", test, "./internal/evidence"])
        test.chmod(0o500)
        report["test_binary_sha256"] = sha(test)
        report["build_flags"] = {"CGO_ENABLED": "0", "GOAMD64": "v1", "trimpath": True, "buildvcs": False}
        env["IVOAI_TEST_RTK_OSS"], env["IVOAI_TEST_RTK_HELPER"] = str(args.rtk.resolve()), str(helper)
        # Retain parsed allowlisted observations even when the test exits nonzero.
        start = time.monotonic()
        proc = subprocess.Popen([str(test), "-test.run=^TestRealRTKGeneratedGatewayABBA$", "-test.v", "-test.timeout=60s"], cwd=root, env=env, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, start_new_session=True)
        timed_out = False
        try:
            stdout, _ = proc.communicate(timeout=90)
        except subprocess.TimeoutExpired:
            timed_out = True
            os.killpg(proc.pid, signal.SIGKILL)
            stdout, _ = proc.communicate()
        report["phases"].append({"label": "frozen_generated_abba", "exit_code": proc.returncode, "timed_out": timed_out, "elapsed_ms": round((time.monotonic()-start)*1000, 3)})
        report["observations"] = observations(stdout)
        report["summary"] = summarize(report["observations"])
        if proc.returncode or not report["summary"]["complete_36_observations"]:
            raise RuntimeError("generated_round_incomplete_or_failed")
        if run("clean_after_round", ["/usr/bin/git", "status", "--porcelain", "--untracked-files=all"]):
            raise ValueError("source_changed_during_round")
        if run("source_commit_after_round", ["/usr/bin/git", "rev-parse", "HEAD"]).strip() != source:
            raise ValueError("source_commit_changed")
        report["status"] = "COMPLETE_DIAGNOSTIC_ONLY"
    except (OSError, ValueError, RuntimeError, subprocess.TimeoutExpired) as error:
        report["failure_label"] = str(error) if isinstance(error, (ValueError, RuntimeError)) else type(error).__name__
        report["status"] = "FAILED_PREFLIGHT_OR_ROUND"
    finally:
        private_json(args.output, report)
    print(json.dumps({"status": report["status"], "observations": len(report["observations"]), "output_sha256": sha(args.output)}))
    return 0 if report["status"] == "COMPLETE_DIAGNOSTIC_ONLY" else 1


if __name__ == "__main__":
    raise SystemExit(main())
