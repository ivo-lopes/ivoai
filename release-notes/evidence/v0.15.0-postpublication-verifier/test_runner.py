import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).parent
spec = importlib.util.spec_from_file_location("private_runner", HERE / "verify_release.py")
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=HERE)
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.assets = self.root / "assets"
        self.assets.mkdir()
        self.hashes = {}
        entries = []
        for name in sorted(r.v.EXPECTED):
            path = self.assets / name
            path.write_bytes(name.encode())
            digest = r.v.digest(path)
            self.hashes[name] = digest
            entries.append({"name": name, "size": path.stat().st_size, "digest": "sha256:" + digest})
        self.api = {"id": 427082748, "tag_name": "v0.14.0", "published_at": "2026-10-28T19:10:11Z", "draft": False, "prerelease": False, "assets": entries}
        self.path = self.root / "api.json"
        r.configure("v0.14.0", 427082748, "2026-10-28T19:10:11Z")

    def check_api(self):
        self.path.write_text(json.dumps(self.api))
        return r.v.api_hashes(self.path, self.hashes, self.assets)

    def test_positive_expected_metadata_and_digest(self):
        self.assertEqual(len(self.check_api()["verified_sha256"]), 12)

    def test_version_identity_publication_and_stable_mismatch(self):
        for key, bad in (("tag_name", "v0.15.0"), ("id", 1), ("id", True), ("published_at", "2026-10-28T19:10:12Z"), ("draft", True), ("prerelease", True)):
            with self.subTest(key=key, bad=bad):
                old = self.api[key]
                self.api[key] = bad
                with self.assertRaises(ValueError):
                    self.check_api()
                self.api[key] = old

    def test_wrong_digest_rejected(self):
        self.api["assets"][0]["digest"] = "sha256:" + "0" * 64
        with self.assertRaisesRegex(ValueError, "digest mismatch"):
            self.check_api()

    def test_missing_digest_remains_unknown(self):
        self.api["assets"][0].pop("digest")
        result = self.check_api()
        self.assertEqual(len(result["verified_sha256"]), 11)
        self.assertEqual(len(result["missing_sha256"]), 1)

    def test_api_size_bound_before_parser(self):
        self.path.write_bytes(b"x" * ((2 << 20) + 1))
        with mock.patch.object(r.v, "unique_json", side_effect=AssertionError("parser reached")):
            with self.assertRaisesRegex(ValueError, "invalid asset size"):
                r.v.api_hashes(self.path, self.hashes, self.assets)

    def test_saved_api_required(self):
        with self.assertRaisesRegex(ValueError, "required"):
            r.v.api_hashes(None, self.hashes, self.assets)

    def test_duplicate_api_key_rejected(self):
        self.path.write_text('{"id":1,"id":427082748}')
        with self.assertRaisesRegex(ValueError, "duplicate JSON key"):
            r.v.api_hashes(self.path, self.hashes, self.assets)

    def test_expected_fields_bounded_and_canonical(self):
        cases = [("v0.15", 1, "2026-10-28T19:10:11Z"), ("v0.15.0-rc1", 1, "2026-10-28T19:10:11Z"), ("v0.15.0/../", 1, "2026-10-28T19:10:11Z"), ("v0.15.0", True, "2026-10-28T19:10:11Z"), ("v0.15.0", 0, "2026-10-28T19:10:11Z"), ("v0.15.0", 1 << 63, "2026-10-28T19:10:11Z"), ("v0.15.0", 1, "2026-02-31T19:10:11Z"), ("v0.15.0", 1, "2026-10-28T19:10:11Zx")]
        for args in cases:
            with self.subTest(args=args), self.assertRaises((ValueError, TypeError)):
                r.configure(*args)

    def test_all_limits_and_reader_bytes_preserved(self):
        self.assertEqual(r.v.MAX_CORE_BYTES, 256 << 20)
        self.assertEqual(r.v.MAX_GZIP_BYTES, (256 << 20) + 65536)
        self.assertEqual(r.v.MAX_ZIP_BYTES, 512 << 10)
        self.assertEqual(r.v.MiB, 1 << 20)
        self.assertEqual(r.v.digest(HERE / "verify_release014.py"), r.BASELINE_SHA256)
        self.assertEqual(r.v.digest(HERE / "buildinfo_reader.go"), r.HELPER_SHA256)


if __name__ == "__main__":
    unittest.main(verbosity=2)
