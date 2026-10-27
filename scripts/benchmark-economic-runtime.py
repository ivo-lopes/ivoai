#!/usr/bin/env python3
"""Opt-in, provider-consuming comparison of separately compiled App test binaries.

Build the same primary_prefetch_live_test.go harness on baseline and candidate.
Both production implementations retain Graft. This isolates automatic retrieval;
it does not replace full frontend, worker or institutional acceptance benchmarks.
Only official executor authentication is used. No prompts/answers are retained.
Exit status verifies execution/evidence/quality; economic targets are diagnostic.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import tempfile
import time


SCENARIOS = ("api", "implementation", "multi_module_callers")
MAX_EVIDENCE_BYTES = 64 << 10
MAX_INTEGER = (1 << 53) - 1
COUNTS = ("TotalMS", "ProviderMS", "PrefetchMS", "PromptBytes", "EvidenceBytes",
          "SourceBytes", "ToolCalls", "ToolResultBytes", "PrefetchCalls",
          "StructuralCalls", "CacheHits")
OPTIONAL_COUNTS = ("InputTokens", "CachedInputTokens", "OutputTokens", "ReasoningTokens",
                   "ToolDescriptionTokens", "NativeCPUMS", "NativePeakMemoryBytes")


def sha256(path):
    try:
        with path.open("rb") as stream:
            digest = hashlib.file_digest(stream, "sha256")
        return digest.hexdigest()
    except OSError:
        return None


def write_json(path, value):
    """Atomic private output; never follow a path supplied by the subprocess."""
    descriptor, temporary = tempfile.mkstemp(prefix=".evidence-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, indent=2, allow_nan=False)
            stream.write("\n")
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ValueError("duplicate JSON field")
        value[key] = item
    return value


def read_evidence(path, scenario, arm, repetition, code, inventory_hash):
    """Return one allowlisted row, including a fixed-label failure for missing tests."""
    base = {"Scenario": scenario, "Arm": arm, "Repetition": repetition,
            "TestExitCode": code, "Correct": False, "MeasurementValid": False}
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, "rb") as stream:
            if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
                raise ValueError("nonregular evidence")
            body = stream.read(MAX_EVIDENCE_BYTES + 1)
        if len(body) > MAX_EVIDENCE_BYTES:
            raise ValueError("oversized evidence")
        batch = json.loads(body, object_pairs_hook=unique_object,
                           parse_constant=lambda _: (_ for _ in ()).throw(ValueError("nonfinite JSON")))
        if type(batch) is not list or len(batch) != 1 or type(batch[0]) is not dict:
            raise ValueError("one executed scenario required")
        row = batch[0]
        if row.get("Scenario") != scenario or row.get("Arm") != arm:
            raise ValueError("unexpected scenario or arm")
        if row.get("Model") != "gpt-6.1-sol" or row.get("Effort") != "low":
            raise ValueError("unexpected client selection")
        if not inventory_hash or row.get("DistributionInventorySHA256") != inventory_hash:
            raise ValueError("unexpected distribution")
        for key in ("Correct", "JSONDecoded"):
            if type(row.get(key)) is not bool:
                raise ValueError("nonboolean verdict")
        for key in COUNTS + OPTIONAL_COUNTS:
            value = row.get(key)
            if value is None and key in OPTIONAL_COUNTS:
                continue
            if type(value) is not int or not 0 <= value <= MAX_INTEGER:
                raise ValueError("invalid measurement")
        if row.get("CachedInputTokens") is not None and (
                row.get("InputTokens") is None or row["CachedInputTokens"] > row["InputTokens"]):
            raise ValueError("cache is a subset of input")
        if row["ProviderMS"] + row["PrefetchMS"] > row["TotalMS"]:
            raise ValueError("inconsistent phase durations")
        if row["Correct"] and (not row["JSONDecoded"] or row["TotalMS"] <= 0 or
                               any(row.get(key) is None for key in
                                   ("InputTokens", "CachedInputTokens", "OutputTokens"))):
            raise ValueError("missing successful execution measurements")
        clean = {key: row.get(key) for key in COUNTS + OPTIONAL_COUNTS}
        clean.update({key: row[key] for key in ("Model", "Effort", "DistributionInventorySHA256", "JSONDecoded")})
        clean.update(base, Correct=row["Correct"] and code == 0, MeasurementValid=True)
        if code != 0 or not row["Correct"]:
            clean["Error"] = "test_execution_failed" if code != 0 else "correctness_failed"
        return clean
    except FileNotFoundError:
        base["Error"] = "no_evidence"
    except (OSError, ValueError, UnicodeError, RecursionError):
        base["Error"] = "invalid_evidence"
    return base



def wait4_process(process, timeout):
    """Reap this owned PID exactly once; Popen must not poll/wait before wait4."""
    deadline = time.monotonic() + timeout
    timed_out = False
    try:
        while True:
            pid, status, usage = os.wait4(process.pid, os.WNOHANG)
            if pid:
                process.returncode = os.waitstatus_to_exitcode(status)
                return (124 if timed_out else process.returncode), usage
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                timed_out = True
                # SIGKILL has been delivered to our disposable group. Reap the
                # owned test PID, preserving partial resource usage on timeout.
                pid, status, usage = os.wait4(process.pid, 0)
                process.returncode = os.waitstatus_to_exitcode(status)
                return 124, usage
            time.sleep(min(0.01, remaining))
    except BaseException:
        if process.returncode is None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            _, status, _ = os.wait4(process.pid, 0)
            process.returncode = os.waitstatus_to_exitcode(status)
        raise


def run_attempt(command, env, resource_path, timeout=250):
    # GNU time reports wait4 usage for the command and its reaped descendants.
    # It is not whole-host attribution or an instantaneous process-tree RSS sum.
    resources = {key: None for key in ("LocalUserCPUMS", "LocalSystemCPUMS",
                                      "LocalPeakRSSBytes", "LocalElapsedMS")}
    resources["LocalResourceSource"] = "unavailable"
    timer = Path("/usr/bin/time")
    wrapped = timer.is_file() and os.access(timer, os.X_OK)
    if wrapped:
        descriptor = os.open(resource_path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        os.close(descriptor)
        command = [str(timer), "-q", "-f", '{"user":%U,"system":%S,"rss_kib":%M,"elapsed":%e}',
                   "-o", str(resource_path), "--", *command]
    started = time.monotonic()
    try:
        process = subprocess.Popen(command, env=env, stdout=subprocess.DEVNULL,
                                   stderr=subprocess.DEVNULL, start_new_session=True)
        if not wrapped and sys.platform.startswith("linux") and hasattr(os, "wait4"):
            code, usage = wait4_process(process, timeout)
            resources.update(LocalUserCPUMS=round(usage.ru_utime * 1000),
                             LocalSystemCPUMS=round(usage.ru_stime * 1000),
                             LocalPeakRSSBytes=usage.ru_maxrss * 1024,
                             LocalElapsedMS=round((time.monotonic() - started) * 1000),
                             LocalResourceSource="linux_wait4_reaped_descendants_max_rss_not_sum")
        else:
            try:
                code = process.wait(timeout=timeout)
            except subprocess.TimeoutExpired:
                # The test and ordinary client descendants share this disposable group.
                # Structural helpers additionally have their existing parent-death guard.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
                code = 124
            except BaseException:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
                raise
    except OSError:
        code = 127
    if wrapped:
        try:
            descriptor = os.open(resource_path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            with os.fdopen(descriptor, "rb") as stream:
                if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
                    raise ValueError("nonregular resource report")
                data = stream.read(4097)
            if len(data) > 4096:
                raise ValueError("oversized resource report")
            report = json.loads(data, object_pairs_hook=unique_object)
            if set(report) != {"user", "system", "rss_kib", "elapsed"}:
                raise ValueError("invalid resource fields")
            for key, value in report.items():
                if type(value) not in (int, float) or not 0 <= value <= MAX_INTEGER:
                    raise ValueError("invalid resource value")
            if type(report["rss_kib"]) is not int or report["rss_kib"] > MAX_INTEGER // 1024:
                raise ValueError("invalid RSS")
            resources.update(LocalUserCPUMS=round(report["user"] * 1000),
                             LocalSystemCPUMS=round(report["system"] * 1000),
                             LocalPeakRSSBytes=report["rss_kib"] * 1024,
                             LocalElapsedMS=round(report["elapsed"] * 1000),
                             LocalResourceSource="linux_gnu_time_wait4_reaped_descendants")
        except (OSError, ValueError, TypeError):
            pass
        finally:
            resource_path.unlink(missing_ok=True)
    return code, resources


def totals(rows):
    result = {"attempts": len(rows), "correct": sum(row["Correct"] for row in rows),
              "measured_rows": sum(row["MeasurementValid"] for row in rows)}
    for metric in ("InputTokens", "CachedInputTokens", "OutputTokens", "TotalMS", "ToolCalls"):
        values = [row[metric] for row in rows if row["MeasurementValid"] and row.get(metric) is not None]
        result[metric] = {"sum": sum(values) if values else None, "observed_rows": len(values)}
    return result


def reductions(arms):
    result = {}
    for metric in ("InputTokens", "TotalMS"):
        a, b = arms["A"][metric], arms["B"][metric]
        complete = a["observed_rows"] == arms["A"]["attempts"] and b["observed_rows"] == arms["B"]["attempts"]
        result[metric] = 100 * (a["sum"] - b["sum"]) / a["sum"] if complete and a["sum"] else None
    return result


def summarize(rows, repetitions):
    all_arms = {arm: totals([row for row in rows if row["Arm"] == arm]) for arm in ("A", "B")}
    matched = []
    for repetition in range(repetitions):
        for scenario in SCENARIOS:
            pair = [row for row in rows if row["Repetition"] == repetition and row["Scenario"] == scenario]
            if len(pair) == 2 and {row["Arm"] for row in pair} == {"A", "B"} and all(row["Correct"] for row in pair):
                matched.extend(pair)
    matched_arms = {arm: totals([row for row in matched if row["Arm"] == arm]) for arm in ("A", "B")}
    change = reductions(matched_arms)
    per_repetition = []
    for repetition in range(repetitions):
        sample = [row for row in matched if row["Repetition"] == repetition]
        arms = {arm: totals([row for row in sample if row["Arm"] == arm]) for arm in ("A", "B")}
        per_repetition.append({"repetition": repetition, "matched_pairs": len(sample) // 2,
                               "reduction_percent": reductions(arms)})
    improved = sum(item["matched_pairs"] == len(SCENARIOS) and
                   all(value is not None and value > 0 for value in item["reduction_percent"].values())
                   for item in per_repetition)
    complete = len(rows) == repetitions * len(SCENARIOS) * 2 and all(row["Correct"] for row in rows)
    targets = change["InputTokens"] is not None and change["TotalMS"] is not None and change["InputTokens"] >= 10 and change["TotalMS"] >= 5
    diagnostic = "INCOMPLETE" if not complete or repetitions < 3 else (
        "PASS" if targets and improved * 3 >= repetitions * 2 else "FAIL")
    return {"schema_version": 1, "scope": "app_primary_prefetch_subset",
            "global_economic_gate": "INCOMPLETE", "required_scenarios": 16,
            "measured_scenarios": list(SCENARIOS), "all_attempts": all_arms,
            "matched_pairs": {"count": len(matched) // 2, "arms": matched_arms,
                              "reduction_percent": change},
            "per_repetition": per_repetition, "improved_repetitions": improved,
            "subset_economic_diagnostic": diagnostic,
            "targets": {"input_reduction_percent": 10, "time_reduction_percent": 5,
                        "minimum_repetitions": 3, "improved_fraction": "2/3"},
            "provider_usage": "reported_by_client_event_protocol",
            "effective_model_observed": None, "monetary_cost": None,
            "cache_accounting": "cached_input_tokens_are_subset_of_input_tokens",
            "workers": "not_applicable_primary_only", "delegations": "not_applicable_primary_only",
            "quota_consumption": None}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("baseline", "candidate", "codex", "node", "distribution", "sandbox-binary", "output"):
        parser.add_argument("--" + name, required=True, type=Path)
    parser.add_argument("--repetitions", type=int, default=3, choices=range(1, 6))
    parser.add_argument("--harness", type=Path, default=Path(__file__).resolve().parents[1] / "internal/app/primary_prefetch_live_test.go",
                        help="source harness to hash when accessible (must be identical in both builds)")
    args = parser.parse_args(argv)
    # Refuse to overwrite existing evidence, including failed comparisons.
    args.output.mkdir(mode=0o700, parents=True, exist_ok=False)
    temporary = args.output / "tmp"
    temporary.mkdir(mode=0o700)
    env = {k: v for k, v in os.environ.items() if k not in ("OPENAI_API_KEY", "CODEX_API_KEY")}
    env.update(TMPDIR=str(temporary.resolve()), GOMAXPROCS="2", IVOAI_GRAFT_PREFETCH_LIVE="1",
               IVOAI_LIVE_CODEX_PATH=str(args.codex.resolve()), IVOAI_GRAFT_TEST_NODE=str(args.node.resolve()),
               IVOAI_GRAFT_TEST_DIST=str(args.distribution.resolve()),
               IVOAI_GRAFT_SMOKE_BINARY=str(args.sandbox_binary.resolve()))
    inventory_hash = sha256(args.distribution / "files.json")
    manifest = {"schema_version": 1, "scope": "app_primary_prefetch_subset", "repetitions": args.repetitions,
                "model_requested": "gpt-6.1-sol", "effort_requested": "low", "effective_model_observed": None,
                "tools_exposed": 0,
                "runtime_flags": {"experimental_evidence_gateway": os.environ.get("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY") == "1"},
                "baseline_test_sha256": sha256(args.baseline),
                "candidate_test_sha256": sha256(args.candidate), "codex_sha256": sha256(args.codex),
                "node_sha256": sha256(args.node), "sandbox_sha256": sha256(args.sandbox_binary),
                "distribution_inventory_sha256": inventory_hash, "harness_sha256": sha256(args.harness)}
    write_json(args.output / "manifest.json", manifest)
    rows = []
    for repetition in range(args.repetitions):
        for index, scenario in enumerate(SCENARIOS):
            for arm in (("A", "B") if (repetition + index) % 2 == 0 else ("B", "A")):
                output = args.output / f"{repetition}-{scenario}-{arm}.json"
                env.update(IVOAI_ECONOMIC_RUNTIME_ARM=arm, IVOAI_GRAFT_PREFETCH_OUTPUT=str(output.resolve()))
                binary = args.baseline if arm == "A" else args.candidate
                code, resources = run_attempt([str(binary.resolve()),
                    "-test.run=^TestLivePrimaryCodePrefetchBenchmark/" + scenario + "/",
                    "-test.timeout=4m"], env, temporary / f"{repetition}-{scenario}-{arm}-resources.json")
                row = read_evidence(output, scenario, arm, repetition, code, inventory_hash)
                row.update(resources)
                # Raw subprocess output is never propagated; persist only fixed labels and scalar allowlist.
                write_json(output, [row])
                rows.append(row)
                write_json(args.output / "runtime-comparison.json", rows)
                write_json(args.output / "summary.json", summarize(rows, args.repetitions))
                print(repetition, scenario, arm, "exit", code, "valid", row["MeasurementValid"], flush=True)
    return int(any(not row["Correct"] for row in rows))


if __name__ == "__main__":
    raise SystemExit(main())
