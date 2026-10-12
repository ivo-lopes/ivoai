#!/usr/bin/env python3
"""Offline contract tests; never execute an upstream installer or CLI."""
import base64
import hashlib
import importlib.util
import io
import json
import pathlib
import tarfile
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]

class GraftBuildTests(unittest.TestCase):
    def setUp(self):
        helper = ROOT / "patches/graft/build.py"
        self.assertTrue(helper.is_file(), "reviewed build helper is missing")
        spec = importlib.util.spec_from_file_location("graft_build", helper)
        self.build = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.build)

    def test_build_rejects_unpinned_archive_before_dependency_execution(self):
        import os, subprocess
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            source = root / "wrong.tgz"; source.write_bytes(b"untrusted")
            result = subprocess.run([str(ROOT/"scripts/build-managed-graft.sh"), str(root/"out")], env={**os.environ, "GRAFT_SOURCE_ARCHIVE": str(source)}, capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Pinned archive checksum mismatch", result.stderr)
            self.assertEqual(list((root/"out").iterdir()), [])

    def test_version_probe_only_accepts_version_and_survives_archive(self):
        import subprocess
        probe = ROOT/"patches/graft/graft-runtime"
        ok = subprocess.run(["/bin/sh", str(probe), "--version"], capture_output=True, text=True)
        self.assertEqual((ok.returncode, ok.stdout), (0, "0.21.1-ivoai.1\n"))
        for args in ([], ["init"], ["--version", "extra"], ["mcp"]):
            result = subprocess.run(["/bin/sh", str(probe), *args], capture_output=True, text=True)
            self.assertEqual((result.returncode, result.stdout, result.stderr), (2, "", ""))
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp); payload = root/"payload"; payload.mkdir()
            (payload/"graft-runtime").write_bytes(probe.read_bytes())
            archive = root/"runtime.tgz"; self.build.archive_payload(payload, archive)
            with tarfile.open(archive) as tar:
                self.assertEqual(tar.getmember("graft-runtime").mode, 0o755)

    def test_runtime_payload_omits_compile_only_sources_but_keeps_licenses_and_node_types(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            files = ["dist/index.js", "dist/index.js.map", "dist/index.d.ts", "node_modules/sdk/index.mjs", "node_modules/sdk/index.d.mts", "node_modules/tree-sitter-kotlin/src/parser.c", "node_modules/tree-sitter-kotlin/src/node-types.json", "node_modules/tree-sitter-kotlin/LICENSE"]
            for name in files:
                p = root/name; p.parent.mkdir(parents=True, exist_ok=True); p.write_text("fixture")
            self.build.clean_native_payload(root)
            for name in files:
                wanted = name.endswith((".js", ".mjs", ".json", "LICENSE"))
                self.assertEqual((root/name).exists(), wanted, name)

    def dependency(self):
        return {"version": "1.0.0", "resolved": "https://registry.npmjs.org/example/-/example-1.0.0.tgz", "integrity": "sha512-" + base64.b64encode(bytes(64)).decode(), "license": "MIT"}

    def test_lock_requires_registry_integrity_and_license(self):
        lock = {"lockfileVersion": 3, "packages": {"": {"version": "0.21.1"}, "node_modules/example": self.dependency()}}
        self.assertEqual(len(self.build.validate_lock(lock)), 1)
        for field, value in (("integrity", "sha512-bad"), ("resolved", "https://evil.example/pkg.tgz"), ("license", "")):
            with self.subTest(field=field):
                bad = json.loads(json.dumps(lock)); bad["packages"]["node_modules/example"][field] = value
                with self.assertRaises(ValueError): self.build.validate_lock(bad)

    def test_source_extraction_rejects_traversal_and_links(self):
        for name, kind in (("../escape", tarfile.REGTYPE), ("root/alias", tarfile.SYMTYPE), ("/absolute", tarfile.REGTYPE)):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                archive = pathlib.Path(tmp) / "source.tgz"
                with tarfile.open(archive, "w:gz") as tar:
                    member = tarfile.TarInfo(name); member.type = kind; member.linkname = "/etc/passwd" if kind == tarfile.SYMTYPE else ""
                    tar.addfile(member, io.BytesIO())
                with self.assertRaises(ValueError): self.build.extract_source(archive, pathlib.Path(tmp)/"out")

    def test_archive_is_reproducible_and_contains_integrity_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp); payload = root / "payload"; payload.mkdir()
            (payload/"LICENSE").write_text("MIT\n"); (payload/"dist").mkdir(); (payload/"dist/index.js").write_text("export {};\n")
            self.build.write_file_manifest(payload)
            first = root/"one.tgz"; second = root/"two.tgz"
            self.build.archive_payload(payload, first)
            import os
            os.chmod(payload/"dist/index.js", 0o600); os.utime(payload/"LICENSE", (999,999))
            self.build.archive_payload(payload, second)
            self.assertEqual(first.read_bytes(), second.read_bytes())
            listing = json.loads((payload/"files.json").read_text())
            self.assertEqual(listing["dist/index.js"]["sha256"], hashlib.sha256(b"export {};\n").hexdigest())

    def test_runtime_prune_drops_build_paths_and_foreign_prebuilds(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp); addon = root/"node_modules/tree-sitter-kotlin"
            (addon/"build/Release").mkdir(parents=True); (addon/"build/Release/addon.node").write_bytes(b"ELF fixture")
            (addon/"build/Makefile").write_text("/private/build/root")
            (addon/"prebuilds/linux-arm64").mkdir(parents=True); (addon/"prebuilds/linux-arm64/addon.node").write_bytes(b"foreign")
            self.build.clean_native_payload(root)
            self.assertTrue((addon/"build/Release/addon.node").is_file())
            self.assertFalse((addon/"build/Makefile").exists()); self.assertFalse((addon/"prebuilds").exists())

if __name__ == "__main__": unittest.main()
