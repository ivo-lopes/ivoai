"""Typed matrix and offline synthetic oracles; never starts an agent/client."""
from dataclasses import dataclass
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / "internal/benchmarkfixtures/data"
CATALOG = DATA / "catalog.json"
TOKEN = re.compile(r"[a-z][a-z0-9_-]*\Z")


@dataclass(frozen=True)
class Scenario:
    id: str
    label: str
    fixture: str
    languages: tuple[str, ...]
    runner_supported: bool
    blocker: str
    task: str
    prompt: str
    expected: dict | None
    structural: str
    execution: str


def load_catalog():
    raw = json.loads(CATALOG.read_bytes())
    if set(raw) != {"schema_version", "scope", "global_economic_gate", "scenarios"} or raw["schema_version"] != 2 or raw["global_economic_gate"] != "INCOMPLETE":
        raise ValueError("invalid_catalog")
    cases = []
    for item in raw["scenarios"]:
        if set(item) != set(Scenario.__annotations__) or type(item["runner_supported"]) is not bool:
            raise ValueError("invalid_scenario_fields")
        for key in ("id", "label", "fixture", "blocker", "task", "prompt", "structural", "execution"):
            if type(item[key]) is not str:
                raise ValueError("invalid_scenario_type")
        if not TOKEN.fullmatch(item["id"]) or item["fixture"] and not TOKEN.fullmatch(item["fixture"]):
            raise ValueError("invalid_scenario_identity")
        if type(item["languages"]) is not list or any(type(x) is not str for x in item["languages"]):
            raise ValueError("invalid_languages")
        if item["structural"] not in ("allowed", "required", "forbidden"):
            raise ValueError("invalid_structural_policy")
        supported = item["runner_supported"]
        if supported and item["execution"] not in ("read_only", "workspace_edit") or not supported and item["execution"] != "unavailable":
            raise ValueError("invalid_execution_contract")
        if supported and (item["blocker"] or not all(item[x] for x in ("task", "prompt", "fixture")) or type(item["expected"]) is not dict):
            raise ValueError("incomplete_execution_contract")
        if not supported and not item["blocker"]:
            raise ValueError("missing_blocker")
        if item["fixture"] and not (DATA / "projects" / item["fixture"]).is_dir():
            raise ValueError("missing_fixture")
        cases.append(Scenario(**{**item, "languages": tuple(item["languages"])}))
    if len(cases) != 16 or len({s.id for s in cases}) != 16:
        raise ValueError("matrix_must_have_16_distinct_scenarios")
    return tuple(cases)


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def fixture_digest(name):
    if not TOKEN.fullmatch(name):
        raise ValueError("invalid_fixture")
    root = DATA / "projects" / name
    result = hashlib.sha256()
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError("unsafe_fixture")
        if path.is_file():
            relative = path.relative_to(root).as_posix()
            relative = relative.removesuffix(".fixture")
            result.update(relative.encode() + b"\0" + path.read_bytes() + b"\0")
    return result.hexdigest()


def contract_digest():
    result = hashlib.sha256()
    for name in ("fixtures.go", "edit_oracle.go", "exec_observation.go"):
        result.update(name.encode() + b"\0" + (ROOT / "internal/benchmarkfixtures" / name).read_bytes() + b"\0")
    return result.hexdigest()


def oracle_identity(scenario, go, node, bwrap):
    tool = go if scenario.id == "small_known_file_correction" else node
    if scenario.execution != "workspace_edit" or not tool or not bwrap:
        return None
    try:
        paths = (Path(tool), Path(bwrap), Path("/usr/bin/prlimit"))
        if any(not p.is_absolute() or not p.is_file() or not os.access(p, os.X_OK) or p.stat().st_size > 512 << 20 for p in paths):
            return None
        result = hashlib.sha256(scenario.id.encode() + b"\0")
        for path in paths:
            result.update(digest(path).encode() + b"\0")
        return result.hexdigest()
    except OSError:
        return None


def expected_capabilities(harness):
    cases = [s for s in load_catalog() if s.runner_supported]
    return {"schema_version": 3, "catalog_sha256": digest(CATALOG),
            "fixture_contract_sha256": contract_digest(),
            "harness_sha256": digest(harness), "supported_scenarios": sorted(s.id for s in cases),
            "fixtures": {s.fixture: fixture_digest(s.fixture) for s in cases}}


def planned_status(scenario):
    if scenario.runner_supported:
        return "NOT_RUN"
    if scenario.id in ("institutional_documentation", "ai_memory_history"):
        return "BLOCKED"
    return "UNSUPPORTED"


def verify_fixtures(go=None, node=None):
    """Execute only reviewed synthetic programs, in disposable private roots.

    Go has no external module dependencies; Python uses its standard library;
    TypeScript uses Node's native stripping. No package manager or installer runs.
    """
    results = []
    cases = {s.id: s for s in load_catalog()}
    with tempfile.TemporaryDirectory(prefix="ivoai-offline-oracles-") as temporary:
        private = Path(temporary)
        environment = {"PATH": "/usr/bin:/bin", "HOME": str(private), "TMPDIR": str(private),
                       "LC_ALL": "C", "TZ": "UTC", "GOTOOLCHAIN": "local", "GOPROXY": "off",
                       "GOSUMDB": "off", "GOENV": "off", "GOCACHE": str(private / "go-cache"),
                       "GOPATH": str(private / "go-path"), "GOMAXPROCS": "2", "PYTHONDONTWRITEBYTECODE": "1"}
        commands = {
            "go-core": ([str(go), "test", "./..."], None) if go else (None, None),
            "python-deps": ([sys.executable, "-I", "-c", 'import ast,json,sys;from pathlib import Path;sys.path.insert(0,str(Path.cwd()));from pricing.totals import total;tree=ast.parse(Path("app/checkout.py").read_text());print(json.dumps({"modules":sorted(n.module for n in ast.walk(tree) if isinstance(n,ast.ImportFrom)),"total":total(100)}))'], cases["dependencies"].expected),
            "monorepo-ts": ([str(node), "packages/shop/src/main.ts"], cases["monorepo"].expected) if node else (None, None),
            "rules-shell": (["/bin/sh", "check.sh"], cases["unsupported_parser"].expected),
            "non-code": ([sys.executable, "-I", "-c", 'import json;from pathlib import Path;x=json.loads(Path("milestones.json").read_text());print(json.dumps({"order":[v["id"] for v in sorted(x,key=lambda v:v["priority"])],"total_days":sum(v["days"] for v in x)}))'], cases["non_code"].expected),
        }
        for fixture, (command, expected) in commands.items():
            row = {"fixture": fixture, "fixture_sha256": fixture_digest(fixture),
                   "scope": "offline_synthetic_program_oracle", "provider_attempts": 0,
                   "status": "BLOCKED", "correct": False}
            if command is None:
                row["reason"] = "explicit_tool_path_required"
                results.append(row)
                continue
            root = private / fixture
            shutil.copytree(DATA / "projects" / fixture, root)
            if fixture == "go-core":
                for path in root.rglob("*.fixture"):
                    path.rename(path.with_name(path.name.removesuffix(".fixture")))
                # Only the offline copy gets this independent executable oracle.
                (root / "mathpkg/offline_oracle_test.go").write_text('package mathpkg_test\nimport("testing";"fixture/mathpkg";"fixture/app";"fixture/service")\nfunc TestIndependentOracle(t *testing.T){if mathpkg.Scale(3)!=6||app.Entry()!=6||service.Process()!=10{t.Fatal("incorrect fixture computation")}}\n')
            try:
                # Reviewed synthetic programs emit bounded tiny output. Suppress
                # stderr and retain only fixed diagnostics and numeric exit code.
                run = subprocess.run(command, cwd=root, env=environment, stdout=subprocess.PIPE,
                                     stderr=subprocess.DEVNULL, timeout=60)
                row["exit_code"] = run.returncode
                correct = run.returncode == 0 and len(run.stdout) <= 4096
                if expected is not None:
                    correct = correct and json.loads(run.stdout) == expected
                row.update(status="VERIFIED" if correct else "FAILED", correct=correct)
            except (OSError, subprocess.TimeoutExpired, ValueError):
                row["reason"] = "oracle_execution_unavailable_or_invalid"
            results.append(row)
    return results


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verify-fixtures", action="store_true")
    parser.add_argument("--go", type=Path)
    parser.add_argument("--node", type=Path)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args(argv)
    cases = load_catalog()
    result = {"scope": "offline_catalog_and_synthetic_fixtures", "global_economic_gate": "INCOMPLETE",
              "provider_attempts": 0, "catalog_sha256": digest(CATALOG),
              "fixture_contract_sha256": contract_digest(),
              "harness_sha256": digest(ROOT / "internal/app/primary_prefetch_live_test.go"),
              "oracle_tools_sha256": {name: digest(path) if path and path.is_file() else None
                                       for name, path in (("go", args.go), ("node", args.node),
                                                          ("python", Path(sys.executable)), ("shell", Path("/bin/sh")))},
              "scenarios": [{"id": s.id, "status": planned_status(s), "blocker": s.blocker,
                             "client_execution": "NOT_RUN"} for s in cases]}
    if args.verify_fixtures:
        result["fixture_oracles"] = verify_fixtures(args.go, args.node)
    body = json.dumps(result, indent=2) + "\n"
    if args.output:
        descriptor = os.open(args.output, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        with os.fdopen(descriptor, "w") as stream:
            stream.write(body)
    else:
        print(body, end="")
    return int(any(row["status"] != "VERIFIED" for row in result.get("fixture_oracles", [])))


if __name__ == "__main__":
    raise SystemExit(main())
