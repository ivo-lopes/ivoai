#!/usr/bin/env python3
"""Verify managed frontend packaging never requests a private npm SDK version."""
import json
from pathlib import Path
import re
import sys


def verify(source):
    version = json.loads((source / "packages/opencode/package.json").read_text())["version"]
    assert re.fullmatch(r"\d+\.\d+\.\d+", version), "upstream SDK version must be exact"
    for relative in ("config/config.ts", "config/tui.ts"):
        body = (source / "packages/opencode/src" / relative).read_text()
        expressions = re.findall(r'name: "@opencode-ai/plugin",\s*version: ([^\n]+)', body)
        assert len(expressions) == 1, "SDK installation contract changed"
        # Local upstream development retains its local-resolution behavior. A
        # published private build must request the reviewed public SDK version.
        expected = f'InstallationLocal ? undefined : "{version}",'
        assert expressions[0] == expected, f"{relative}: SDK version aliases private binary version"
    print("OPENCODE_PUBLIC_PLUGIN_VERSION=PASS")


if __name__ == "__main__":
    verify(Path(sys.argv[1]))
