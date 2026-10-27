"""Offline archive admission tests; never execute package code or installers."""
import gzip
import base64
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch


BUILDER_PATH = Path(__file__).resolve().parents[1] / "patches/zod3-compat/build.py"
SPEC = importlib.util.spec_from_file_location("zod_compat_builder", BUILDER_PATH)
BUILDER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILDER)


def archive_bytes(entries):
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w:gz") as archive:
        for item, content in entries:
            archive.addfile(item, io.BytesIO(content))
    return buffer.getvalue()


def regular(name, content=b""):
    item = tarfile.TarInfo(name)
    item.size = len(content)
    return item, content


class ExtractionTests(unittest.TestCase):
    def reject_without_writes(self, body, message=None):
        with tempfile.TemporaryDirectory(prefix="ivoai-zod-extract-") as directory:
            destination = Path(directory)
            with self.assertRaisesRegex(ValueError, message or ".*"):
                BUILDER.extract_regular(body, destination)
            self.assertEqual(list(destination.iterdir()), [])

    def test_regular_payload_preserves_bytes(self):
        body = archive_bytes([regular("package/nested/input.js", b"export const answer = 42;\n")])
        with tempfile.TemporaryDirectory(prefix="ivoai-zod-extract-") as directory:
            destination = Path(directory)
            BUILDER.extract_regular(body, destination)
            self.assertEqual((destination / "package/nested/input.js").read_bytes(), b"export const answer = 42;\n")

    def test_unsafe_names_rejected_before_any_write(self):
        for name in ("/absolute", "../outside", "package/../../outside", "other/input.js"):
            with self.subTest(name=name):
                body = archive_bytes([regular("package/valid.js", b"safe"), regular(name)])
                self.reject_without_writes(body, "invalid archive path")

    def test_links_and_special_files_rejected_before_any_write(self):
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.FIFOTYPE, tarfile.CHRTYPE):
            with self.subTest(kind=kind):
                item = tarfile.TarInfo("package/unsafe")
                item.type = kind
                item.linkname = "../../outside"
                body = archive_bytes([regular("package/valid.js", b"safe"), (item, b"")])
                self.reject_without_writes(body, "unexpected archive entry")

    def test_normalized_duplicates_rejected_before_any_write(self):
        for name in ("package/input.js", "package/./input.js", "package//input.js"):
            with self.subTest(name=name):
                body = archive_bytes([regular("package/input.js", b"first"), regular(name, b"second")])
                self.reject_without_writes(body, "invalid archive path")

    def test_directory_and_file_aliases_cannot_overwrite_each_other(self):
        directory = tarfile.TarInfo("package/nested/")
        directory.type = tarfile.DIRTYPE
        self.reject_without_writes(archive_bytes([(directory, b""), regular("package/./nested")]), "invalid archive path")

    def test_oversized_header_rejected_before_reading_missing_payload(self):
        item = tarfile.TarInfo("package/oversized")
        item.size = BUILDER.MAX_MEMBER_BYTES + 1
        # No payload follows the header. An eager archive scan would raise a
        # tar truncation error instead of enforcing the declared-size budget.
        self.reject_without_writes(gzip.compress(item.tobuf()), "unexpected archive entry")

    def test_total_budget_checked_before_advancing_to_missing_payload(self):
        first, content = regular("package/first", b"1234")
        second = tarfile.TarInfo("package/second")
        second.size = 4
        raw = first.tobuf() + content + b"\0" * (512 - len(content)) + second.tobuf()
        with patch.object(BUILDER, "MAX_ARCHIVE_BYTES", 7):
            self.reject_without_writes(gzip.compress(raw), "archive byte budget exceeded")

    def test_entry_budget_checked_before_advancing_to_missing_payload(self):
        first, _ = regular("package/first")
        second = tarfile.TarInfo("package/second")
        second.size = 1
        with patch.object(BUILDER, "MAX_ARCHIVE_MEMBERS", 1):
            self.reject_without_writes(gzip.compress(first.tobuf() + second.tobuf()), "archive entry budget exceeded")


class FixtureIntegrityTests(unittest.TestCase):
    def test_digest_checks_survive_python_optimization(self):
        fixture_script = Path(__file__).with_name("test-zod3-compat.sh").read_bytes()
        body = b"synthetic mismatched archive; must never reach extraction"
        for algorithm in ("SHA-256", "SHA-512"):
            with self.subTest(algorithm=algorithm), tempfile.TemporaryDirectory(prefix="ivoai-zod-integrity-") as directory:
                root = Path(directory)
                (root / "scripts").mkdir()
                (root / "patches/zod3-compat").mkdir(parents=True)
                (root / "internal/components/assets").mkdir(parents=True)
                script = root / "scripts/test-zod3-compat.sh"
                script.write_bytes(fixture_script)
                archive = root / "internal/components/assets/synthetic.tgz"
                archive.write_bytes(body)
                provenance = {
                    "archive_file": archive.name,
                    "archive_sha256": hashlib.sha256(body).hexdigest(),
                    "archive_integrity": "sha512-" + base64.b64encode(hashlib.sha512(body).digest()).decode(),
                }
                provenance["archive_sha256" if algorithm == "SHA-256" else "archive_integrity"] = "incorrect"
                (root / "patches/zod3-compat/provenance.json").write_text(json.dumps(provenance))
                result = subprocess.run(["bash", str(script)], env={**os.environ, "PYTHONOPTIMIZE": "1"},
                                        capture_output=True, text=True, timeout=10, check=False)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("fork archive " + algorithm + " mismatch", result.stderr)


if __name__ == "__main__":
    unittest.main()
