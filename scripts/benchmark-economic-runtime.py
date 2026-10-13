#!/usr/bin/env python3
"""Opt-in, provider-consuming comparison of separately compiled App test binaries.

Build the same primary_prefetch_live_test.go harness on v0.12.0 and the candidate.
Both production implementations retain Graft. This isolates automatic retrieval;
it does not replace full frontend, worker or institutional acceptance benchmarks.
Only official executor authentication is used. No prompts/answers are retained.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("baseline", "candidate", "codex", "node", "distribution", "sandbox-binary", "output"):
        parser.add_argument("--" + name, required=True, type=Path)
    parser.add_argument("--repetitions", type=int, default=3, choices=range(1, 6))
    args = parser.parse_args()
    # Refuse to overwrite existing evidence, including failed comparisons.
    args.output.mkdir(mode=0o700, parents=True, exist_ok=False)
    temporary = args.output / "tmp"
    temporary.mkdir(mode=0o700)
    env = {k: v for k, v in os.environ.items() if k not in ("OPENAI_API_KEY", "CODEX_API_KEY")}
    env.update(TMPDIR=str(temporary.resolve()), GOMAXPROCS="2", IVOAI_GRAFT_PREFETCH_LIVE="1",
               IVOAI_LIVE_CODEX_PATH=str(args.codex.resolve()), IVOAI_GRAFT_TEST_NODE=str(args.node.resolve()),
               IVOAI_GRAFT_TEST_DIST=str(args.distribution.resolve()),
               IVOAI_GRAFT_SMOKE_BINARY=str(args.sandbox_binary.resolve()))
    manifest = {"repetitions": args.repetitions, "model_requested": "gpt-6.1-sol", "effort_requested": "low",
                "effective_model_observed": None, "tools_exposed": 0,
                "baseline_test_sha256": hashlib.sha256(args.baseline.read_bytes()).hexdigest(),
                "candidate_test_sha256": hashlib.sha256(args.candidate.read_bytes()).hexdigest()}
    (args.output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    rows = []
    failed = False
    for repetition in range(args.repetitions):
        for index, scenario in enumerate(("api", "implementation", "multi_module_callers")):
            for arm in (("A", "B") if (repetition + index) % 2 == 0 else ("B", "A")):
                output = args.output / f"{repetition}-{scenario}-{arm}.json"
                env.update(IVOAI_ECONOMIC_RUNTIME_ARM=arm, IVOAI_GRAFT_PREFETCH_OUTPUT=str(output.resolve()))
                binary = args.baseline if arm == "A" else args.candidate
                try:
                    completed = subprocess.run([str(binary.resolve()),
                        "-test.run=^TestLivePrimaryCodePrefetchBenchmark/" + scenario + "/",
                        "-test.timeout=4m"], env=env, stdout=subprocess.DEVNULL,
                        stderr=subprocess.DEVNULL, timeout=250, check=False)
                    code = completed.returncode
                except subprocess.TimeoutExpired:
                    code = 124
                batch = json.loads(output.read_text()) if output.exists() else [
                    {"Scenario": scenario, "Arm": arm, "Correct": False, "Error": "no_evidence"}]
                for row in batch:
                    row.update(Repetition=repetition, TestExitCode=code)
                    failed |= code != 0 or not row.get("Correct", False)
                rows.extend(batch)
                (args.output / "runtime-comparison.json").write_text(json.dumps(rows, indent=2) + "\n")
                print(repetition, scenario, arm, "exit", code, flush=True)
    return int(failed)


if __name__ == "__main__":
    raise SystemExit(main())
