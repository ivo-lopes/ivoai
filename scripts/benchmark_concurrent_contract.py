"""Opt-in offline composition preflight. Never starts a client or provider."""
import argparse
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import stat
import sys

import benchmark_catalog as catalog

CONTRACT = catalog.DATA / "concurrent-projects.json"
CHILDREN = (("implementation", "go-core"), ("dependencies", "python-deps"))


@dataclass(frozen=True)
class Child:
    scenario: str
    fixture: str


@dataclass(frozen=True)
class Composition:
    schema_version: int
    scenario: str
    execution: str
    max_clients: int
    client_overlap_required: bool
    children: tuple[Child, ...]


def unique_object(pairs):
    value = {}
    for name, item in pairs:
        if name in value:
            raise ValueError("duplicate_composition_field")
        value[name] = item
    return value


def load_contract(body=None):
    body = CONTRACT.read_bytes() if body is None else body
    if len(body) > 4096:
        raise ValueError("oversized_composition")
    value = json.loads(body, object_pairs_hook=unique_object)
    expected = {"schema_version": 1, "scenario": "concurrent_distinct_projects", "execution": "read_only",
                "max_clients": 2, "client_overlap_required": True,
                "children": [{"scenario": scenario, "fixture": fixture} for scenario, fixture in CHILDREN]}
    # JSON equality via encoding also rejects Python's bool/int equivalence.
    if json.dumps(value, sort_keys=True) != json.dumps(expected, sort_keys=True):
        raise ValueError("invalid_composition")
    cases = {s.id: s for s in catalog.load_catalog()}
    for scenario, fixture in CHILDREN:
        child = cases[scenario]
        if not child.runner_supported or child.fixture != fixture or child.execution != "read_only":
            raise ValueError("invalid_composition_child")
    return Composition(**{**value, "children": tuple(Child(**child) for child in value["children"])})


def contract_digest():
    composition = load_contract()
    parts = [("concurrent-projects.json", CONTRACT.read_bytes()),
             ("concurrent.go", (catalog.ROOT / "internal/benchmarkfixtures/concurrent.go").read_bytes()),
             ("catalog_sha256", catalog.digest(catalog.CATALOG).encode()),
             ("fixture_contract_sha256", catalog.contract_digest().encode())]
    parts += [(child.scenario, catalog.fixture_digest(child.fixture).encode()) for child in composition.children]
    result = hashlib.sha256()
    for name, body in parts:
        result.update(name.encode() + b"\0" + body + b"\0")
    return result.hexdigest()


def client_lifecycle_digest():
    result = hashlib.sha256()
    for name in ("client_lifecycle.go", "client_lifecycle_linux.go", "client_lifecycle_other.go"):
        result.update(name.encode() + b"\0" + (catalog.ROOT / "internal/benchmarkfixtures" / name).read_bytes() + b"\0")
    return result.hexdigest()


def source_pins(go):
    return {"preflight_sha256": catalog.digest(Path(__file__)),
            "oracle_runner_sha256": catalog.digest(Path(catalog.__file__)),
            "go_sha256": catalog.digest(go) if go and go.is_file() else None,
            "python_sha256": catalog.digest(Path(sys.executable))}


def preflight(go=None):
    composition = load_contract()
    digest, pins = contract_digest(), source_pins(go)
    # This is explicitly the two existing executable fixture oracles, not two
    # agent clients. Source-pinned future execution must validate its own rows.
    rows = catalog.verify_fixtures(go=go, fixtures=tuple(child.fixture for child in composition.children))
    children = []
    verified = type(rows) is list and len(rows) == 2
    for index, child in enumerate(composition.children):
        row = rows[index] if type(rows) is list and index < len(rows) and type(rows[index]) is dict else {}
        identity_ok = row.get("fixture") == child.fixture and row.get("fixture_sha256") == catalog.fixture_digest(child.fixture) and row.get("scope") == "offline_synthetic_program_oracle" and type(row.get("provider_attempts")) is int and row["provider_attempts"] == 0
        success = identity_ok and row.get("status") == "VERIFIED" and row.get("correct") is True and type(row.get("exit_code")) is int and row["exit_code"] == 0
        verified = verified and success
        children.append({"scenario": child.scenario, **row} if identity_ok else
                        {"scenario": child.scenario, "fixture": child.fixture, "status": "BLOCKED", "correct": False,
                         "reason": "oracle_report_missing_or_invalid", "provider_attempts": 0})
    stable = digest == contract_digest() and pins == source_pins(go)
    return {"schema_version": 1, "scope": "offline_composition_contract_and_fixture_oracles",
            "scenario": composition.scenario, "scenario_status": "UNSUPPORTED",
            "reason": "client_overlap_executor_not_implemented", "client_execution": "NOT_RUN",
            "client_overlap": "NOT_RUN", "provider_attempts": 0, "client_attempts": 0,
            "global_economic_gate": "INCOMPLETE", "contract_sha256": digest,
            "source_pins_stable": stable,
            "preflight_status": "OFFLINE_ORACLES_VERIFIED" if verified and stable else "BLOCKED",
            "children": children,
            "source_pins": pins}


def write_private(path, report):
    parent = path.parent.stat()
    if not path.is_absolute() or not stat.S_ISDIR(parent.st_mode) or parent.st_uid != os.getuid() or stat.S_IMODE(parent.st_mode) != 0o700:
        raise ValueError("new_output_in_owned_private_directory_required")
    descriptor = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w") as stream:
        json.dump(report, stream, indent=2, allow_nan=False)
        stream.write("\n")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--preflight-offline", action="store_true", required=True)
    parser.add_argument("--go", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args(argv)
    if args.go and (not args.go.is_absolute() or not args.go.is_file() or not os.access(args.go, os.X_OK)):
        parser.error("absolute reviewed Go executable required")
    report = preflight(args.go)
    write_private(args.output, report)
    # Exit zero verifies offline oracles only; scenario_status stays UNSUPPORTED.
    return int(report["preflight_status"] != "OFFLINE_ORACLES_VERIFIED")


if __name__ == "__main__":
    raise SystemExit(main())
