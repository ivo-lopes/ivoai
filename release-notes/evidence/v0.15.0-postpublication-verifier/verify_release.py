#!/usr/bin/env python3
"""Private offline runner over the immutable bounded v0.14 verifier. No network or downloaded code execution."""
import argparse
import datetime
import hashlib
import importlib.util
from pathlib import Path
import re
import sys

HERE = Path(__file__).resolve().parent
BASELINE_SHA256 = "15c166f204ddaaf776a0b5a891dcb5fba06fc37bd82e9a39d81bd30e49763976"
HELPER_SHA256 = "ed9718c5e503d11c70ebbc25fe887ce7ae5abf578faadbe450e334d0503e0eb1"
for name, expected in (("verify_release014.py", BASELINE_SHA256), ("buildinfo_reader.go", HELPER_SHA256)):
    if hashlib.sha256((HERE / name).read_bytes()).hexdigest() != expected:
        raise RuntimeError("immutable verifier source identity mismatch")
spec = importlib.util.spec_from_file_location("bounded_release_baseline", HERE / "verify_release014.py")
v = importlib.util.module_from_spec(spec)
spec.loader.exec_module(v)
_original_api = v.api_hashes
_original_run = v.run


def configure(tag, release_id, published_at):
    v.require(re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag) is not None, "expected stable release tag required")
    v.require(type(release_id) is int and 0 < release_id < (1 << 63), "expected positive release ID required")
    v.require(isinstance(published_at, str) and re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z", published_at) is not None, "expected canonical UTC publication time required")
    datetime.datetime.strptime(published_at, "%Y-%m-%dT%H:%M:%SZ")
    v.TAG = tag

    def bound_api(path, hashes, assets):
        v.require(path is not None, "saved release API metadata is required")
        v.regular(path, 2 << 20)
        api = v.unique_json(path.read_text())
        v.require(type(api.get("id")) is int and api["id"] == release_id, "API release identity mismatch")
        v.require(api.get("published_at") == published_at, "API publication time mismatch")
        v.require(api.get("prerelease") is False, "stable release cannot be a prerelease")
        return _original_api(path, hashes, assets)

    def bound_run(args, report):
        report["private_runner_sha256"] = v.digest(Path(__file__))
        report["immutable_baseline_sha256"] = BASELINE_SHA256
        report["release_expectations"] = {"tag": tag, "release_id": release_id, "published_at": published_at, "prerelease": False}
        return _original_run(args, report)

    v.api_hashes = bound_api
    v.run = bound_run


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--expected-tag", required=True)
    p.add_argument("--expected-release-id", required=True, type=int)
    p.add_argument("--expected-published-at", required=True)
    p.add_argument("--assets", required=True)
    p.add_argument("--source-tree", required=True)
    p.add_argument("--source-commit", required=True)
    p.add_argument("--api-json", required=True)
    p.add_argument("--output", required=True, help="new private output directory; must not exist")
    p.add_argument("--go", default="go")
    args = p.parse_args(argv)
    try:
        configure(args.expected_tag, args.expected_release_id, args.expected_published_at)
    except (ValueError, TypeError) as error:
        p.error(str(error))
    forwarded = [sys.argv[0]]
    for key in ("assets", "source_tree", "source_commit", "api_json", "output", "go"):
        forwarded.extend(("--" + key.replace("_", "-"), getattr(args, key)))
    previous = sys.argv
    try:
        sys.argv = forwarded
        return v.main()
    finally:
        sys.argv = previous


if __name__ == "__main__":
    raise SystemExit(main())
