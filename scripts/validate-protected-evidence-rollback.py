#!/usr/bin/env python3
"""Validate one actual Go JSON proof; parser fixtures are not integration proof."""

import json
from pathlib import Path
import sys

PACKAGE = "github.com/ivo-lopes/ivoai/internal/workingcontext"
TEST = "TestLiveProtectedArtifactsDoNotPoisonLegacyCoreRollback"
MARKER = "LEGACY_CORE_ROLLBACK=PASS old_full_and_range_denied legacy_put_read_gc_worked protected_bytes_preserved; old_inventory_excludes_protected_disk"


def validate(text):
    rows = [json.loads(line) for line in text.splitlines() if line.strip()]
    if not rows or any(not isinstance(row, dict) for row in rows):
        raise ValueError("missing or invalid Go JSON events")
    if any(row.get("Action") in ("fail", "skip") for row in rows):
        raise ValueError("failed or skipped proof event")
    for row in rows:
        if row.get("Package") not in (None, PACKAGE):
            raise ValueError("unexpected proof package")
        if row.get("Test") not in (None, TEST):
            raise ValueError("unexpected proof test")
    test_events = [row.get("Action") for row in rows if row.get("Package") == PACKAGE and row.get("Test") == TEST]
    if test_events.count("run") != 1 or test_events.count("pass") != 1:
        raise ValueError("required named test must run and pass exactly once")
    if test_events.index("run") > test_events.index("pass"):
        raise ValueError("test passed before running")
    package_passes = [row for row in rows if row.get("Package") == PACKAGE and not row.get("Test") and row.get("Action") == "pass"]
    if len(package_passes) != 1:
        raise ValueError("missing or duplicate package success")
    markers = [row for row in rows if row.get("Package") == PACKAGE and row.get("Test") == TEST and row.get("Action") == "output" and MARKER in row.get("Output", "")]
    if len(markers) != 1:
        raise ValueError("missing or duplicate old-Core contract evidence")


def main():
    if len(sys.argv) != 2:
        raise ValueError("one Go JSON proof file required")
    validate(Path(sys.argv[1]).read_text())
    print("PROTECTED_EVIDENCE_ROLLBACK_GATE=PASS named_test=1 skips=0")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError) as exc:
        print(f"PROTECTED_EVIDENCE_ROLLBACK_GATE=FAIL {exc}", file=sys.stderr)
        sys.exit(1)
