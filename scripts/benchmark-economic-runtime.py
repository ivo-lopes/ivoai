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
import benchmark_catalog
from benchmark_process_sampler import ProcessSampler


SCENARIOS = ("api", "implementation", "multi_module_callers")
MAX_EVIDENCE_BYTES = 64 << 10
MAX_INTEGER = (1 << 53) - 1
COUNTS = ("TotalMS", "ProviderMS", "PrefetchMS", "PromptBytes", "EvidenceBytes",
          "ValidationMS", "EditFiles", "SourceBytes", "ToolCalls", "ToolResultBytes", "PrefetchCalls",
          "StructuralCalls", "CacheHits")
OPTIONAL_COUNTS = ("InputTokens", "CachedInputTokens", "OutputTokens", "ReasoningTokens",
                   "ToolDescriptionTokens", "NativeCPUMS", "NativePeakMemoryBytes")
GATEWAY_COUNTS = ("GatewayCodeResolves", "GatewayCodeCacheHits", "GatewayCodeCacheMisses", "GatewayCodeCacheUnknown")
GATEWAY_LABELS = ("GatewayObservationBasis", "GatewayCacheCondition")
CLIENT_ENVIRONMENT = (
    "HOME", "CODEX_HOME", "PATH", "LANG", "TZ", "TERM", "TMPDIR",
    "LC_ALL", "LC_CTYPE", "LC_COLLATE", "LC_MESSAGES", "LC_MONETARY", "LC_NUMERIC", "LC_TIME",
    "LC_PAPER", "LC_NAME", "LC_ADDRESS", "LC_TELEPHONE", "LC_MEASUREMENT", "LC_IDENTIFICATION",
    "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR",
    "DBUS_SESSION_BUS_ADDRESS",
)


def benchmark_environment():
    """Keep native CLI auth scope; no arbitrary secrets, proxies, CAs or hooks.

    Benchmark paths and process controls are added explicitly by main. No auth
    file is read, copied or rewritten; official CLI login remains its own.
    """
    environment = {key: os.environ[key] for key in CLIENT_ENVIRONMENT if key in os.environ}
    if os.environ.get("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY") == "1":
        environment["IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY"] = "1"
    return environment


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



def validate_native_events(value, tool_calls):
    keys = {"schema_version", "stream_status", "coverage", "tool_completions", "repeated_exact_payloads",
            "collaboration_completions", "reported_spawn_receivers", "unclassified_errors", "duplicate_terminal_items",
            "unknown_events", "unknown_items", "native_workers", "native_delegations", "native_retries"}
    if type(value) is not dict or set(value) != keys or type(value["schema_version"]) is not int or value["schema_version"] != 1:
        raise ValueError("invalid native observation schema")
    if value["coverage"] != "partial_client_projection" or value["stream_status"] not in ("complete", "partial", "failed", "invalid"):
        raise ValueError("invalid native observation scope")
    for field, allowed in (("tool_completions", {"command_execution", "mcp_tool_call", "file_change", "web_search", "collab_tool_call"}),
                           ("repeated_exact_payloads", {"command_execution", "mcp_tool_call"}),
                           ("collaboration_completions", {"spawn_agent", "send_input", "wait", "close_agent"})):
        counts = value[field]
        if type(counts) is not dict or set(counts) != allowed or any(type(v) is not int or not 0 <= v <= 4096 for v in counts.values()):
            raise ValueError("invalid enum-only event counts")
    for field in ("unclassified_errors", "duplicate_terminal_items", "unknown_events", "unknown_items"):
        if type(value[field]) is not int or not 0 <= value[field] <= 4096:
            raise ValueError("invalid event counters")
    if any(value[field] is not None for field in ("native_workers", "native_delegations", "native_retries")):
        raise ValueError("partial projection cannot attest actual totals")
    reported = value["reported_spawn_receivers"]
    if reported is not None and (type(reported) is not int or not 1 <= reported <= 1024 or not value["collaboration_completions"]["spawn_agent"]):
        raise ValueError("invalid reported recipients")
    if sum(value["collaboration_completions"].values()) > value["tool_completions"]["collab_tool_call"]:
        raise ValueError("inconsistent collaboration observations")
    if sum(value["tool_completions"].values()) > 1024 or reported is not None and reported > 64 * value["collaboration_completions"]["spawn_agent"]:
        raise ValueError("event identity limit exceeded")
    if tool_calls != value["tool_completions"]["command_execution"] + value["tool_completions"]["mcp_tool_call"]:
        raise ValueError("inconsistent legacy tool counter")
    for kind, count in value["repeated_exact_payloads"].items():
        if count > max(0, value["tool_completions"][kind] - 1):
            raise ValueError("invalid repeated exact-payload count")
    if value["stream_status"] == "complete" and (value["unknown_events"] or value["unknown_items"]):
        raise ValueError("unknown events cannot prove complete coverage")
    return value

def gateway_observations(row):
    """Additive host observations; absent legacy fields remain unreported."""
    fields = GATEWAY_COUNTS + GATEWAY_LABELS
    if not any(key in row for key in fields):
        return {}
    if any(key not in row for key in fields):
        raise ValueError("partial Gateway observations")
    if any(type(row[key]) is not int or not 0 <= row[key] <= MAX_INTEGER for key in GATEWAY_COUNTS):
        raise ValueError("invalid Gateway counters")
    if row["GatewayCodeResolves"] != sum(row[key] for key in GATEWAY_COUNTS[1:]):
        raise ValueError("inconsistent Gateway cache observations")
    if row["GatewayObservationBasis"] != "COMPLETED_SESSION_EVENTS_NOT_ECONOMY" or row["GatewayCacheCondition"] != "FRESH_APP_SESSION_NO_PREWARM":
        raise ValueError("invalid Gateway observation basis or cache condition")
    return {key: row[key] for key in fields}


def read_evidence(path, scenario, arm, repetition, code, inventory_hash, capabilities=None):
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
        if capabilities is not None:
            definition = next(s for s in benchmark_catalog.load_catalog() if s.id == scenario)
            identities = {"CatalogSHA256": capabilities["catalog_sha256"],
                          "HarnessSHA256": capabilities["harness_sha256"],
                          "FixtureSHA256": capabilities["fixtures"][definition.fixture]}
            if any(row.get(key) != value for key, value in identities.items()):
                raise ValueError("unexpected source or harness identity")
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
        if row["ProviderMS"] + row["PrefetchMS"] + row["ValidationMS"] > row["TotalMS"]:
            raise ValueError("inconsistent phase durations")
        if row["Correct"] and (not row["JSONDecoded"] or row["TotalMS"] <= 0 or
                               any(row.get(key) is None for key in
                                   ("InputTokens", "CachedInputTokens", "OutputTokens"))):
            raise ValueError("missing successful execution measurements")
        definition = next(s for s in benchmark_catalog.load_catalog() if s.id == scenario)
        if row.get("Execution") != definition.execution or type(row.get("EditValidated")) is not bool or row.get("NativeWorkerObservation") != "PARTIAL_CLIENT_PROJECTION_TOTALS_UNKNOWN":
            raise ValueError("invalid execution contract")
        if definition.execution == "workspace_edit":
            expected_oracle = capabilities.get("oracle_tools", {}).get(scenario) if capabilities else None
            if not expected_oracle or row.get("OracleToolsSHA256") != expected_oracle:
                raise ValueError("unbound edit oracle")
            expected_files = 1 if scenario == "small_known_file_correction" else 3
            if row["Correct"] and (not row["EditValidated"] or row["EditFiles"] != expected_files):
                raise ValueError("answer without validated patch")
        elif row["EditValidated"] or row["EditFiles"] != 0 or row.get("OracleToolsSHA256") != "":
            raise ValueError("unexpected edit verdict")
        native = validate_native_events(row.get("NativeEventMetrics"), row["ToolCalls"])
        if row["Correct"] and native["stream_status"] != "complete":
            raise ValueError("incomplete native event stream")
        clean = {key: row.get(key) for key in COUNTS + OPTIONAL_COUNTS}
        clean.update(gateway_observations(row))
        clean["NativeEventMetrics"] = native
        clean.update(Execution=definition.execution, EditValidated=row["EditValidated"], OracleToolsSHA256=row["OracleToolsSHA256"], NativeWorkerObservation=row["NativeWorkerObservation"])
        clean.update({key: row[key] for key in ("Model", "Effort", "DistributionInventorySHA256", "JSONDecoded")})
        clean.update(base, Correct=row["Correct"] and code == 0, MeasurementValid=True)
        if capabilities is not None:
            clean.update(identities)
        if code != 0 or not row["Correct"]:
            clean["Error"] = "test_execution_failed" if code != 0 else "correctness_failed"
        return clean
    except FileNotFoundError:
        base["Error"] = "no_evidence"
    except (OSError, ValueError, UnicodeError, RecursionError):
        base["Error"] = "invalid_evidence"
    return base


def wait4_process(process, timeout, sampler=None, cancel_event=None):
    """Reap this owned PID exactly once; Popen must not poll/wait before wait4."""
    deadline = time.monotonic() + timeout
    try:
        while True:
            if sampler is not None:
                sampler.tick()
            pid, status, usage = os.wait4(process.pid, os.WNOHANG)
            if pid:
                process.returncode = os.waitstatus_to_exitcode(status)
                return process.returncode, usage
            remaining = deadline - time.monotonic()
            cancelled = cancel_event is not None and cancel_event.is_set()
            if remaining <= 0 or cancelled:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                # SIGKILL has been delivered to our disposable group. Reap the
                # owned test PID, preserving partial resource usage on timeout.
                pid, status, usage = os.wait4(process.pid, 0)
                process.returncode = os.waitstatus_to_exitcode(status)
                return 130 if cancelled else 124, usage
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


def run_attempt(command, env, resource_path, timeout=250, cwd=None, sample_processes=False, on_start=None, cancel_event=None):
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
    sampler = None
    try:
        process = subprocess.Popen(command, env=env, stdout=subprocess.DEVNULL,
                                   stderr=subprocess.DEVNULL, start_new_session=True, cwd=cwd)
        if on_start is not None:
            try:
                on_start(process.pid)
            except BaseException:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
                raise
        if sample_processes and sys.platform.startswith("linux"):
            sampler = ProcessSampler(process.pid)
        if not wrapped and sys.platform.startswith("linux") and hasattr(os, "wait4"):
            code, usage = wait4_process(process, timeout, sampler, cancel_event)
            resources.update(LocalUserCPUMS=round(usage.ru_utime * 1000),
                             LocalSystemCPUMS=round(usage.ru_stime * 1000),
                             LocalPeakRSSBytes=usage.ru_maxrss * 1024,
                             LocalElapsedMS=round((time.monotonic() - started) * 1000),
                             LocalResourceSource="linux_wait4_reaped_descendants_max_rss_not_sum")
        else:
            try:
                if sampler is None and cancel_event is None:
                    code = process.wait(timeout=timeout)
                else:
                    deadline = time.monotonic() + timeout
                    while process.poll() is None:
                        if sampler is not None:
                            sampler.tick()
                        if time.monotonic() >= deadline or cancel_event is not None and cancel_event.is_set():
                            raise subprocess.TimeoutExpired(command, timeout)
                        time.sleep(0.01)
                    code = process.returncode
            except subprocess.TimeoutExpired:
                # The test and ordinary client descendants share this disposable group.
                # Structural helpers additionally have their existing parent-death guard.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
                code = 130 if cancel_event is not None and cancel_event.is_set() else 124
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
    resources["ProcessSampling"] = sampler.result() if sampler is not None else None
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


def summarize(rows, repetitions, scenarios=None):
    scenarios = tuple(scenarios) if scenarios is not None else SCENARIOS
    all_arms = {arm: totals([row for row in rows if row["Arm"] == arm]) for arm in ("A", "B")}
    matched = []
    for repetition in range(repetitions):
        for scenario in scenarios:
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
    improved = sum(item["matched_pairs"] == len(scenarios) and
                   all(value is not None and value > 0 for value in item["reduction_percent"].values())
                   for item in per_repetition)
    complete = len(rows) == repetitions * len(scenarios) * 2 and all(row["Correct"] for row in rows)
    targets = change["InputTokens"] is not None and change["TotalMS"] is not None and change["InputTokens"] >= 10 and change["TotalMS"] >= 5
    diagnostic = "INCOMPLETE" if not complete or repetitions < 3 else (
        "PASS" if targets and improved * 3 >= repetitions * 2 else "FAIL")
    return {"schema_version": 3, "scope": "app_primary_prefetch_subset",
            "global_economic_gate": "INCOMPLETE", "required_scenarios": 16,
            "measured_scenarios": sorted({row["Scenario"] for row in rows if row["MeasurementValid"]}),
            "requested_scenarios": list(scenarios), "all_attempts": all_arms,
            "matched_pairs": {"count": len(matched) // 2, "arms": matched_arms,
                              "reduction_percent": change},
            "per_repetition": per_repetition, "improved_repetitions": improved,
            "subset_economic_diagnostic": diagnostic,
            "targets": {"input_reduction_percent": 10, "time_reduction_percent": 5,
                        "minimum_repetitions": 3, "improved_fraction": "2/3"},
            "provider_usage": "reported_by_client_event_protocol",
            "effective_model_observed": None, "monetary_cost": None,
            "cache_accounting": "cached_input_tokens_are_subset_of_input_tokens",
            "core_workers": "not_applicable_primary_only",
            "workers": None, "delegations": None,
            "native_worker_observation": "PARTIAL_CLIENT_PROJECTION_TOTALS_UNKNOWN",
            "quota_consumption": None}


def discover_capabilities(binary, directory, expected):
    """Run one offline test under private HOME/cwd with no client opt-in/auth."""
    directory.mkdir(mode=0o700)
    output = directory / "capabilities.json"
    env = {"PATH": "/nonexistent", "HOME": str(directory), "TMPDIR": str(directory),
           "GOMAXPROCS": "2", "LC_ALL": "C", "TZ": "UTC",
           "IVOAI_BENCHMARK_CATALOG_OUTPUT": str(output.resolve())}
    if expected.get("schema_version") == 4:
        env["IVOAI_CONCURRENT_DISCOVERY"] = "1"
    code, _ = run_attempt([str(binary.resolve()), "-test.run=^TestPrimaryPrefetchBenchmarkCatalog$",
                          "-test.timeout=10s"], env, directory / "resources.json", timeout=15, cwd=directory)
    try:
        descriptor = os.open(output, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, "rb") as stream:
            if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
                raise ValueError("nonregular discovery")
            body = stream.read(32769)
        if len(body) > 32768:
            raise ValueError("oversized discovery")
        value = json.loads(body, object_pairs_hook=unique_object)
        if code != 0 or type(value) is not dict or json.dumps(value, sort_keys=True) != json.dumps(expected, sort_keys=True):
            raise ValueError("different frozen contract")
        return {"status": "READY", "test_exit_code": code, "capabilities": expected}
    except (OSError, ValueError, TypeError, RecursionError):
        return {"status": "BLOCKED", "reason": "capability_discovery_missing_invalid_or_mismatched",
                "test_exit_code": code}


def discover_edit_oracle(binary, scenario, args, directory, identity):
    if not identity:
        return {"status": "BLOCKED", "reason": "explicit_oracle_tool_inputs_unavailable"}
    directory.mkdir(mode=0o700)
    output = directory / "oracle.json"
    env = {"PATH": "/usr/bin:/bin", "HOME": str(directory), "TMPDIR": str(directory), "LANG": "C.UTF-8",
           "IVOAI_BENCHMARK_EDIT_PREFLIGHT_OUTPUT": str(output.resolve()), "IVOAI_BENCHMARK_EDIT_SCENARIO": scenario.id,
           "IVOAI_BENCHMARK_BWRAP": str(args.oracle_bwrap.resolve()), "IVOAI_GRAFT_TEST_NODE": str(args.node.resolve())}
    if args.oracle_go:
        env["IVOAI_BENCHMARK_ORACLE_GO"] = str(args.oracle_go.resolve())
    code, _ = run_attempt([str(binary.resolve()), "-test.run=^TestPrimaryPrefetchBenchmarkEditPreflight$", "-test.timeout=20s"],
                          env, directory / "resources.json", timeout=25, cwd=directory)
    try:
        descriptor = os.open(output, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, "rb") as stream:
            if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
                raise ValueError("nonregular probe")
            body = stream.read(4097)
        if len(body) > 4096:
            raise ValueError("oversized probe")
        value = json.loads(body, object_pairs_hook=unique_object)
        expected = {"schema_version": 2, "scenario": scenario.id, "oracle_tools_sha256": identity, "ready": True, "provider_attempts": 0}
        if code != 0 or type(value) is not dict or json.dumps(value, sort_keys=True) != json.dumps(expected, sort_keys=True):
            raise ValueError("unproven sandbox")
        return {"status": "READY", "test_exit_code": code}
    except (OSError, ValueError, TypeError):
        return {"status": "BLOCKED", "reason": "oracle_sandbox_probe_missing_invalid_or_failed", "test_exit_code": code}


def matrix_preflight(args, scenarios, inventory_hash, temporary):
    catalog = {s.id: s for s in benchmark_catalog.load_catalog()}
    try:
        expected = benchmark_catalog.expected_capabilities(args.harness)
        discovery = {arm: discover_capabilities(binary, temporary / ("discovery-" + arm), expected)
                     for arm, binary in (("A", args.baseline), ("B", args.candidate))}
    except (OSError, ValueError):
        expected, discovery = None, {arm: {"status": "BLOCKED", "reason": "frozen_contract_unavailable"} for arm in ("A", "B")}
    paths = (args.baseline, args.candidate, args.codex, args.node, args.sandbox_binary)
    prerequisites = bool(inventory_hash) and all(p.is_file() and os.access(p, os.X_OK) and sha256(p) for p in paths)
    equivalent = all(row["status"] == "READY" for row in discovery.values())
    oracle_probes, oracle_hashes = {}, {}
    rows = []
    for name in scenarios:
        scenario = catalog[name]
        status = benchmark_catalog.planned_status(scenario)
        reason = scenario.blocker
        if scenario.runner_supported:
            status = "READY" if prerequisites and equivalent else "BLOCKED"
            reason = "" if status == "READY" else ("runtime_artifact_inputs_unavailable" if not prerequisites else "binary_contract_equivalence_unconfirmed")
        if status == "READY" and scenario.execution == "workspace_edit":
            identity = benchmark_catalog.oracle_identity(scenario, args.oracle_go, args.node, args.oracle_bwrap)
            probes = {arm: discover_edit_oracle(binary, scenario, args, temporary / ("oracle-"+name+"-"+arm), identity)
                      for arm, binary in (("A",args.baseline),("B",args.candidate))}
            oracle_probes[name] = probes
            if all(p["status"] == "READY" for p in probes.values()):
                oracle_hashes[name] = identity
            else:
                status, reason = "BLOCKED", "edit_oracle_inputs_or_containment_unavailable"
        rows.append({"scenario": name, "status": status, "reason": reason, "execution": scenario.execution, "client_execution": "NOT_RUN"})
    if expected is not None:
        expected = {**expected, "oracle_tools": oracle_hashes}
    return {"schema_version": 3, "scope": "matrix16_discovery_preflight_no_inference",
            "global_economic_gate": "INCOMPLETE", "provider_attempts": 0,
            "provider_cache_equivalence": "UNVERIFIED", "effective_model_observed": None,
            "discovery": discovery, "edit_oracle_probes": oracle_probes, "scenarios": rows}, expected


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("baseline", "candidate", "codex", "node", "distribution", "sandbox-binary", "output"):
        parser.add_argument("--" + name, required=True, type=Path)
    parser.add_argument("--repetitions", type=int, default=3, choices=range(1, 6))
    parser.add_argument("--harness", type=Path, default=Path(__file__).resolve().parents[1] / "internal/app/primary_prefetch_live_test.go",
                        help="source harness to hash when accessible (must be identical in both builds)")
    parser.add_argument("--oracle-go", type=Path, help="explicit reviewed Go toolchain executable for the Go edit oracle")
    parser.add_argument("--oracle-bwrap", type=Path, default=Path("/usr/bin/bwrap"), help="existing local sandbox executable; no installation is performed")
    parser.add_argument("--sample-processes", action="store_true", help="opt-in bounded Linux descendant/RSS observations; new measured condition")
    selection = parser.add_mutually_exclusive_group()
    selection.add_argument("--matrix16", action="store_true", help="plan all 16; unsupported/blocked cases cannot pass")
    selection.add_argument("--scenarios", nargs="+", choices=[s.id for s in benchmark_catalog.load_catalog()])
    selection.add_argument("--concurrent-projects", action="store_true", help="experimental exact two-client composition; matrix remains UNSUPPORTED")
    parser.add_argument("--preflight-only", action="store_true", help="discover frozen binary contracts without starting a client")
    args = parser.parse_args(argv)
    if args.concurrent_projects:
        import benchmark_concurrent_runtime
        return benchmark_concurrent_runtime.run(args, sys.modules[__name__])
    scenarios = tuple(s.id for s in benchmark_catalog.load_catalog()) if args.matrix16 else tuple(args.scenarios or SCENARIOS)
    if len(set(scenarios)) != len(scenarios):
        parser.error("scenario selection must be distinct")
    # Refuse to overwrite existing evidence, including failed comparisons.
    args.output.mkdir(mode=0o700, parents=True, exist_ok=False)
    temporary = args.output / "tmp"
    temporary.mkdir(mode=0o700)
    env = benchmark_environment()
    env.update(TMPDIR=str(temporary.resolve()), GOMAXPROCS="2", IVOAI_GRAFT_PREFETCH_LIVE="1",
               IVOAI_LIVE_CODEX_PATH=str(args.codex.resolve()), IVOAI_GRAFT_TEST_NODE=str(args.node.resolve()),
               IVOAI_GRAFT_TEST_DIST=str(args.distribution.resolve()),
               IVOAI_GRAFT_SMOKE_BINARY=str(args.sandbox_binary.resolve()))
    if args.oracle_go:
        env["IVOAI_BENCHMARK_ORACLE_GO"] = str(args.oracle_go.resolve())
    env["IVOAI_BENCHMARK_BWRAP"] = str(args.oracle_bwrap.resolve())
    inventory_hash = sha256(args.distribution / "files.json")
    # Every v3 invocation verifies both compiled contracts before admitting a
    # client turn, including the historical three-scenario default selection.
    preflight, capabilities = matrix_preflight(args, scenarios, inventory_hash, temporary)
    write_json(args.output / "preflight.json", preflight)
    manifest = {"schema_version": 3, "scope": "app_primary_prefetch_subset", "repetitions": args.repetitions,
                "driver_sha256": sha256(Path(__file__)), "catalog_loader_sha256": sha256(Path(benchmark_catalog.__file__)),
                "process_sampler_sha256": sha256(Path(sys.modules[ProcessSampler.__module__].__file__)),
                "process_sampling": {"enabled": args.sample_processes, "schema_version": 1, "interval_ms": 100, "window_budget_ms": 25, "max_processes": 128, "max_threads_per_process": 128, "max_reads_per_sample": 512, "max_samples": 3000, "max_observed_identities": 4096},
                "native_event_observation_schema": 1,
                "model_requested": "gpt-6.1-sol", "effort_requested": "low", "effective_model_observed": None,
                "host_mcp_tools_exposed": 0, "native_tools_exposed": None,
                "native_tools_inventory": "UNVERIFIED", "native_worker_observation": "PARTIAL_CLIENT_PROJECTION_TOTALS_UNKNOWN",
                "common_native_worker_policy": "agents.enabled=false;features.multi_agent_v2=false",
                "runtime_flags": {"experimental_evidence_gateway": os.environ.get("IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY") == "1"},
                "baseline_test_sha256": sha256(args.baseline),
                "candidate_test_sha256": sha256(args.candidate), "codex_sha256": sha256(args.codex),
                "node_sha256": sha256(args.node), "sandbox_sha256": sha256(args.sandbox_binary), "oracle_go_sha256": sha256(args.oracle_go) if args.oracle_go else None, "oracle_bwrap_sha256": sha256(args.oracle_bwrap), "oracle_prlimit_sha256": sha256(Path("/usr/bin/prlimit")),
                "distribution_inventory_sha256": inventory_hash, "harness_sha256": sha256(args.harness)}
    manifest.update(requested_scenarios=list(scenarios), catalog_sha256=benchmark_catalog.digest(benchmark_catalog.CATALOG),
                    fixture_contract_sha256=capabilities.get("fixture_contract_sha256") if capabilities else None, edit_oracle_identities=capabilities.get("oracle_tools", {}) if capabilities else {},
                    preflight_only=args.preflight_only, cache_policy="cold_private_local_caches_provider_cache_unverified")
    write_json(args.output / "manifest.json", manifest)
    rows = []
    if args.preflight_only:
        write_json(args.output / "summary.json", summarize([], args.repetitions, scenarios))
        return int(any(row["status"] != "READY" for row in preflight["scenarios"]))
    ready = {row["scenario"] for row in preflight["scenarios"] if row["status"] == "READY"}
    for repetition in range(args.repetitions):
        for index, scenario in enumerate(scenarios):
            if scenario not in ready:
                continue  # Planned unavailable cases remain preflight rows, never fake successes/attempts.
            for arm in (("A", "B") if (repetition + index) % 2 == 0 else ("B", "A")):
                output = args.output / f"{repetition}-{scenario}-{arm}.json"
                env.update(IVOAI_ECONOMIC_RUNTIME_ARM=arm, IVOAI_GRAFT_PREFETCH_OUTPUT=str(output.resolve()))
                binary = args.baseline if arm == "A" else args.candidate
                code, resources = run_attempt([str(binary.resolve()),
                    "-test.run=^TestLivePrimaryCodePrefetchBenchmark/" + scenario + "/",
                    "-test.timeout=4m"], env, temporary / f"{repetition}-{scenario}-{arm}-resources.json", sample_processes=args.sample_processes)
                row = read_evidence(output, scenario, arm, repetition, code, inventory_hash, capabilities)
                row.update(resources)
                # Raw subprocess output is never propagated; persist only fixed labels and scalar allowlist.
                write_json(output, [row])
                rows.append(row)
                write_json(args.output / "runtime-comparison.json", rows)
                write_json(args.output / "summary.json", summarize(rows, args.repetitions, scenarios))
                print(repetition, scenario, arm, "exit", code, "valid", row["MeasurementValid"], flush=True)
    if not rows:
        write_json(args.output / "summary.json", summarize([], args.repetitions, scenarios))
    return int(len(ready) != len(scenarios) or any(not row["Correct"] for row in rows))


if __name__ == "__main__":
    raise SystemExit(main())
