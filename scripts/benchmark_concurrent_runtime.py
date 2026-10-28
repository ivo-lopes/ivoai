"""Experimental two-primary benchmark driver. No scheduler or authentication client."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import secrets
import stat
import sys
import threading
import time
import re
import select

import benchmark_catalog as catalog
import benchmark_concurrent_contract as contract

DIGEST = re.compile(r"[a-f0-9]{64}\Z")
IDENTITY_KEYS = {"attempt_id", "scenario", "arm", "fixture_sha256", "harness_sha256",
                 "composition_sha256", "client_sha256", "isolation_sha256"}
LIFECYCLE_KEYS = {"schema_version", "clock", "identity", "pid", "start_ticks", "started_ns", "reaped_ns", "exit_code"}


def capture_cohort(args, driver):
    """One initial scientific identity for the entire opt-in comparison."""
    sources = catalog.ROOT / 'internal/benchmarkfixtures'
    paths = {'baseline':args.baseline,'candidate':args.candidate,'client':args.codex,
             'node':getattr(args,'node',None),'sandbox':getattr(args,'sandbox_binary',None),
             'oracle_go':getattr(args,'oracle_go',None),'oracle_python':Path(sys.executable),
             'resource_timer':Path('/usr/bin/time'), 'driver':Path(driver.__file__),
             'composite_driver':Path(__file__),'catalog_loader':Path(catalog.__file__),
             'composition_loader':Path(contract.__file__),
             'harness':getattr(args,'harness',catalog.ROOT/'internal/app/primary_prefetch_live_test.go'),
             'distribution_inventory':args.distribution/'files.json',
             'catalog':catalog.CATALOG,'composition':contract.CONTRACT,
             'go_mod':catalog.ROOT/'go.mod','go_sum':catalog.ROOT/'go.sum'}
    for name in ('concurrent.go','fixtures.go','edit_oracle.go','exec_observation.go',
                 'client_lifecycle.go','client_lifecycle_linux.go','client_lifecycle_other.go'):
        paths[name]=sources/name
    return {'files':{name:driver.sha256(path) if path is not None else None for name,path in paths.items()},
            'locations':{name:hashlib.sha256(str(path.resolve()).encode()).hexdigest() if path is not None else None for name,path in paths.items()},
            'fixtures':{name:catalog.fixture_digest(fixture) for name,fixture in contract.CHILDREN},
            'composition_sha256':contract.contract_digest(),'observer_sha256':contract.client_lifecycle_digest()}


def cohort_unchanged(frozen,args,driver):
    try:
        return frozen==capture_cohort(args,driver)
    except (OSError,ValueError,UnicodeError,RecursionError):
        return False


def not_run_pair(arm,repetition,reason,cancelled=False):
    return {'schema_version':1,'scenario':'concurrent_distinct_projects','scenario_status':'UNSUPPORTED',
            'arm':arm,'repetition':repetition,'client_execution':'NOT_RUN','client_overlap':'UNVERIFIED',
            'reason':reason,'correct':False,'cancelled':cancelled,'batch_wall_ms':None,'child_total_ms_sum':None,
            'batch_peak_rss_bytes':None,
            'children':[{'Scenario':name,'Correct':False,'MeasurementValid':False,'Error':reason} for name,_ in contract.CHILDREN]}


def read_private_json(path, limit=65536):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, "rb") as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise ValueError("invalid private report")
        body = stream.read(limit + 1)
    if len(body) > limit:
        raise ValueError("oversized report")
    return json.loads(body, object_pairs_hook=contract.unique_object)


def validate_lifecycle(value, expected, final, earliest_ns=0):
    if type(value) is not dict or set(value) != LIFECYCLE_KEYS or type(value["schema_version"]) is not int or value["schema_version"] != 1 or value["clock"] != "linux_clock_monotonic":
        raise ValueError("invalid client lifecycle")
    identity = value["identity"]
    if type(identity) is not dict or set(identity) != IDENTITY_KEYS or any(identity.get(key) != item for key, item in expected.items()) or not isinstance(identity.get("isolation_sha256"), str) or not DIGEST.fullmatch(identity["isolation_sha256"]):
        raise ValueError("substituted client identity")
    for name in ("pid", "start_ticks", "started_ns"):
        if type(value[name]) is not int or not 0 < value[name] < (1 << 63):
            raise ValueError("invalid client identity or clock")
    if value["started_ns"] < earliest_ns:
        raise ValueError("stale client clock")
    if type(value["reaped_ns"]) is not int or type(value["exit_code"]) is not int:
        raise ValueError("invalid reap")
    if final:
        if not value["started_ns"] < value["reaped_ns"] <= time.monotonic_ns() or not -255 <= value["exit_code"] <= 255:
            raise ValueError("invalid reap interval")
    elif value["reaped_ns"] != 0 or value["exit_code"] != -1 or value["started_ns"] > time.monotonic_ns():
        raise ValueError("not a start receipt")
    return value


def live_client(value, owned_group, executable):
    # This reads only bounded kernel identity/state, never argv/environ/paths
    # from a model or remote source. The actual ELF must be the reviewed client.
    pid = value["pid"]
    if not owned_group or pid == owned_group:
        return False
    try:
        with open(f"/proc/{pid}/stat", "rb") as stream:
            body = stream.read(16385)
        if len(body) > 16384:
            return False
        fields = body[body.rindex(b")") + 1:].split()
        info = os.stat(f"/proc/{pid}/exe")
        return (fields[0] in (b"R", b"S", b"D", b"I", b"T") and int(fields[19]) == value["start_ticks"]
                and int(fields[2]) == owned_group and int(fields[3]) == owned_group
                and (info.st_dev, info.st_ino) == executable)
    except (OSError, ValueError, IndexError):
        return False


def pidfd_available():
    if not hasattr(os, "pidfd_open"):
        return False
    try:
        fd=os.pidfd_open(os.getpid(),0)
        os.close(fd)
        return True
    except OSError:
        return False


def observe_bracket(starts, groups, executable):
    a, b = starts
    if a["pid"] == b["pid"] or a["identity"]["isolation_sha256"] == b["identity"]["isolation_sha256"]:
        return None
    handles=[]
    try:
        if not hasattr(os,"pidfd_open"):
            return None
        handles=[os.pidfd_open(a["pid"],0)]
        handles.append(os.pidfd_open(b["pid"],0))
        poll=select.poll()
        for fd in handles: poll.register(fd,select.POLLIN | select.POLLHUP | select.POLLERR)
        begin = time.monotonic_ns()
        # Kernel lifetime handles bracket the A -> B -> A state/identity reads.
        # Exited/reused PIDs cannot pass merely by replaying interval metadata.
        if not poll.poll(0) and live_client(a, groups[0], executable) and live_client(b, groups[1], executable) and live_client(a, groups[0], executable) and not poll.poll(0):
            end = time.monotonic_ns()
            if max(a["started_ns"], b["started_ns"]) <= begin < end:
                return {"clock": "linux_clock_monotonic", "kind": "owned_client_pidfd_A_B_A_live_bracket",
                        "begin_ns": begin, "end_ns": end}
    except OSError:
        return None
    finally:
        for fd in handles: os.close(fd)
    return None


def public_lifecycle(value):
    # Native PIDs/start-ticks and private paths are never public benchmark rows.
    process = hashlib.sha256((value["identity"]["attempt_id"] + ":" + str(value["pid"]) + ":" + str(value["start_ticks"])).encode()).hexdigest()
    return {"clock": value["clock"], "identity": value["identity"], "process_identity_sha256": process,
            "started_ns": value["started_ns"], "reaped_ns": value["reaped_ns"], "exit_code": value["exit_code"]}


def run_pair(driver, args, arm, repetition, environment, capabilities, temporary, cancel_event=None, cohort=None):
    """Only the exact two frozen read-only children; no arbitrary job queue."""
    wall_begin=time.monotonic_ns()
    cancel = cancel_event or threading.Event()
    if cancel.is_set():
        return not_run_pair(arm,repetition,'cancelled_before_launch',True)
    if not pidfd_available():
        return not_run_pair(arm,repetition,'host_pidfd_unavailable')
    cohort = capture_cohort(args,driver) if cohort is None else cohort
    if not cohort_unchanged(cohort,args,driver):
        row=not_run_pair(arm,repetition,'cohort_inputs_changed');row['cohort_pins_stable']=False
        return row
    batch = temporary / secrets.token_hex(16); batch.mkdir(mode=0o700)
    executable_info = args.codex.stat(); executable = (executable_info.st_dev, executable_info.st_ino)
    binary = args.baseline if arm == "A" else args.candidate
    client_digest = cohort['files']['client']
    jobs, groups = [], [None, None]
    for scenario, fixture in contract.CHILDREN:
        private = batch / scenario; private.mkdir(mode=0o700)
        control = private / "host-control"; control.mkdir(mode=0o700)
        client_tmp = private / "client-tmp"; client_tmp.mkdir(mode=0o700)
        attempt = secrets.token_hex(16)
        expected = {"attempt_id": attempt, "scenario": scenario, "arm": arm,
                    "fixture_sha256": capabilities["fixtures"][fixture], "harness_sha256": capabilities["harness_sha256"],
                    "composition_sha256": cohort['composition_sha256'], "client_sha256": client_digest}
        env = dict(environment, TMPDIR=str(client_tmp.resolve()), IVOAI_ECONOMIC_RUNTIME_ARM=arm,
                   IVOAI_GRAFT_PREFETCH_OUTPUT=str((control / "row.json").resolve()),
                   IVOAI_CONCURRENT_CLIENT_OBSERVATION="1", IVOAI_CONCURRENT_ATTEMPT_ID=attempt,
                   IVOAI_CONCURRENT_CLIENT_SHA256=expected["client_sha256"],
                   IVOAI_CONCURRENT_CLIENT_START=str((control / "client-start.json").resolve()))
        jobs.append((scenario, control, expected, env))
    started = time.monotonic_ns(); futures = []; starts = [None, None]; bracket = None
    interrupted = False
    with ThreadPoolExecutor(max_workers=2) as executor:
        try:
            for index, (scenario, private, _, env) in enumerate(jobs):
                futures.append(executor.submit(driver.run_attempt,
                    [str(binary.resolve()), "-test.run=^TestLivePrimaryCodePrefetchBenchmark/" + scenario + "/", "-test.timeout=4m"],
                    env, private / "resources.json", sample_processes=args.sample_processes,
                    on_start=lambda pid, index=index: groups.__setitem__(index, pid), cancel_event=cancel))
            while not all(future.done() for future in futures):
                for index, (_, private, expected, _) in enumerate(jobs):
                    if starts[index] is None:
                        try:
                            starts[index] = validate_lifecycle(read_private_json(private / "client-start.json", 4096), expected, False, started)
                        except (OSError, ValueError, UnicodeError, RecursionError):
                            pass  # Partial/absent receipts never become overlap proof.
                if bracket is None and all(starts) and all(groups):
                    bracket = observe_bracket(starts, groups, executable)
                time.sleep(0.01)
        except BaseException:
            interrupted = True
            cancel.set()
        # All owned attempts reap before parsing/returning, including cancellation.
        results = []
        for future in futures:
            try:
                results.append(future.result())
            except Exception:
                results.append((127, {}))
        while len(results) < 2:
            results.append((130, {}))
    ended = time.monotonic_ns(); children = []; life = []
    for index, ((scenario, private, expected, _), (code, resources)) in enumerate(zip(jobs, results)):
        row = driver.read_evidence(private / "row.json", scenario, arm, repetition, code,
                                   driver.sha256(args.distribution / "files.json"), capabilities)
        row.update(resources)
        try:
            raw = read_private_json(private / "row.json")
            if type(raw) is not list or len(raw) != 1 or type(raw[0]) is not dict:
                raise ValueError("missing child")
            value = validate_lifecycle(raw[0].get("ClientLifecycle"), expected, True, started)
            if value["reaped_ns"]>ended: raise ValueError("reap outside batch")
            start = starts[index]
            if start is None or any(value[key] != start[key] for key in ("schema_version", "clock", "identity", "pid", "start_ticks", "started_ns")):
                raise ValueError("substituted lifecycle")
            row["ClientLifecycle"] = public_lifecycle(value)
            row["Correct"] = row["Correct"] and value["exit_code"] == 0
            life.append(value)
        except (OSError, ValueError, UnicodeError, RecursionError, TypeError):
            row.update(Correct=False, MeasurementValid=False, Error="client_lifecycle_missing_invalid_or_substituted")
            life.append(None)
        children.append(row)
        # Raw PIDs and kernel identity exist only in transient private IPC.
        # Public rows retain their opaque digest, never the raw receipt.
        (private / "row.json").unlink(missing_ok=True)
        (private / "client-start.json").unlink(missing_ok=True)
    overlap = (bracket is not None and all(life) and all(value["started_ns"] <= bracket["begin_ns"] < bracket["end_ns"] <= value["reaped_ns"] for value in life))
    stable = cohort_unchanged(cohort,args,driver)
    child_sum = sum(row["TotalMS"] for row in children) if all(row.get("MeasurementValid") and type(row.get("TotalMS")) is int for row in children) else None
    wall_end=time.monotonic_ns()
    return {"schema_version": 1, "scenario": "concurrent_distinct_projects", "scenario_status": "UNSUPPORTED",
            "scope": "experimental_observed_two_primary_clients", "arm": arm, "repetition": repetition,
            "correct": bool(overlap and stable and not interrupted and not cancel.is_set() and all(row["Correct"] for row in children)),
            "client_overlap": "OBSERVED_CLIENT_LIFECYCLE" if overlap else "UNVERIFIED",
            "remote_inference_overlap": "NOT_ATTESTED", "overlap_receipt": bracket if overlap else None,
            "batch_wall_ms": (wall_end - wall_begin) // 1000000,
            "child_total_ms_sum": child_sum, "executable_pins_stable": stable, "cohort_pins_stable":stable,
            "batch_peak_rss_bytes": None, "children": children, "cancelled": interrupted or cancel.is_set()}


def run(args, driver):
    args.output.mkdir(mode=0o700, parents=True, exist_ok=False)
    temporary = args.output / "tmp"; temporary.mkdir(mode=0o700)
    try:
        cohort=capture_cohort(args,driver)
        expected = catalog.expected_capabilities(args.harness)
        expected.update(schema_version=4, client_lifecycle_sha256=contract.client_lifecycle_digest())
        discovery = {arm: driver.discover_capabilities(binary, temporary / ("discovery-" + arm), expected)
                     for arm, binary in (("A", args.baseline), ("B", args.candidate))}
        oracle = contract.preflight(args.oracle_go)
    except (OSError,ValueError,UnicodeError,RecursionError):
        driver.write_json(args.output/'preflight.json',{"scenario_status":"UNSUPPORTED","execution_admission":"BLOCKED","reason":"frozen_source_or_oracle_unavailable","client_execution":"NOT_RUN","provider_attempts":0})
        driver.write_json(args.output/'runtime-comparison.json',[])
        return 1
    inputs = (args.baseline, args.candidate, args.codex, args.node, args.sandbox_binary)
    pidfd = sys.platform.startswith("linux") and pidfd_available()
    admitted = (pidfd and cohort_unchanged(cohort,args,driver) and all(p.is_file() and os.access(p, os.X_OK) for p in inputs)
                and driver.sha256(args.distribution / "files.json") is not None
                and all(row["status"] == "READY" for row in discovery.values())
                and oracle["preflight_status"] == "OFFLINE_ORACLES_VERIFIED")
    driver.write_json(args.output / "preflight.json", {"schema_version": 1, "scenario_status": "UNSUPPORTED",
        "execution_admission": "EXPERIMENTAL_PROBE_AVAILABLE" if admitted else "BLOCKED", "discovery": discovery,
        "offline_oracles": oracle, "pidfd_available":pidfd, "provider_attempts": 0, "client_execution": "NOT_RUN"})
    driver.write_json(args.output / "manifest.json", {"schema_version": 1, "scope": "experimental_concurrent_clients",
        "scenario_status": "UNSUPPORTED", "driver_sha256": cohort['files']['driver'],
        "global_economic_gate": "INCOMPLETE",
        "composite_driver_sha256": cohort['files']['composite_driver'], "contract_sha256": cohort['composition_sha256'],
        "client_lifecycle_sha256": cohort['observer_sha256'], "harness_sha256": cohort['files']['harness'],
        "baseline_sha256": cohort['files']['baseline'], "candidate_sha256": cohort['files']['candidate'],
        "client_sha256": cohort['files']['client'], "cohort_initial":cohort, "repetitions": args.repetitions,
        "provider_cache_equivalence": "UNVERIFIED", "remote_inference_overlap": "NOT_ATTESTED"})
    if not admitted or args.preflight_only:
        driver.write_json(args.output / "runtime-comparison.json", [])
        stable=cohort_unchanged(cohort,args,driver)
        driver.write_json(args.output/'cohort-final.json',{'cohort_pins_stable':stable,'scenario_status':'UNSUPPORTED','client_execution':'NOT_RUN','preflight_verified':admitted and stable})
        return int(not admitted or not stable)
    environment = driver.benchmark_environment()
    environment.update(GOMAXPROCS="2", IVOAI_GRAFT_PREFETCH_LIVE="1", IVOAI_LIVE_CODEX_PATH=str(args.codex.resolve()),
        IVOAI_GRAFT_TEST_NODE=str(args.node.resolve()), IVOAI_GRAFT_TEST_DIST=str(args.distribution.resolve()),
        IVOAI_GRAFT_SMOKE_BINARY=str(args.sandbox_binary.resolve()))
    rows = []
    changed=False
    for repetition in range(args.repetitions):
        for arm in (("A", "B") if repetition % 2 == 0 else ("B", "A")):
            batch_begin=time.monotonic_ns()
            changed=changed or not cohort_unchanged(cohort,args,driver)
            if changed:
                row=not_run_pair(arm,repetition,'cohort_inputs_changed');row['cohort_pins_stable']=False
            else:
                row = run_pair(driver, args, arm, repetition, environment, expected, temporary,cohort=cohort)
                changed=row.get('cohort_pins_stable') is False or not cohort_unchanged(cohort,args,driver)
                if changed:
                    row.update(correct=False,cohort_pins_stable=False,reason='cohort_inputs_changed')
            if row.get('batch_wall_ms') is not None:
                row['batch_wall_ms']=(time.monotonic_ns()-batch_begin)//1000000
            rows.append(row)
            driver.write_json(args.output / "runtime-comparison.json", rows)
            if row["cancelled"]:
                driver.write_json(args.output/'cohort-final.json',{'cohort_pins_stable':cohort_unchanged(cohort,args,driver),'scenario_status':'UNSUPPORTED','correct':False,'cancelled':True})
                return 130
    stable=not changed and cohort_unchanged(cohort,args,driver)
    driver.write_json(args.output/'cohort-final.json',{'cohort_pins_stable':stable,'scenario_status':'UNSUPPORTED','correct':stable and all(row['correct'] for row in rows)})
    return int(not stable or any(not row["correct"] for row in rows))
