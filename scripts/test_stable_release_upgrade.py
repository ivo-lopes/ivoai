"""Offline controls for the real-Core qualification harness, never inference."""
import http.client
import importlib.util
import io
import json
import os
from pathlib import Path
import ssl
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("stable_upgrade", Path(__file__).with_name("stable_release_upgrade.py"))
GATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GATE)


class StableReleaseUpgradeControls(unittest.TestCase):
    def test_future_release_cannot_reuse_an_unreviewed_baseline_pair(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = root / "output"
            with patch.object(GATE.platform, "system", return_value="Linux"), patch.object(GATE.platform, "machine", return_value="x86_64"), patch.object(GATE, "verified_download", side_effect=AssertionError("network must not start")):
                with self.assertRaisesRegex(GATE.GateFailure, "UNREVIEWED_BASELINE_CANDIDATE_PAIR"):
                    GATE.main(["--baseline-core", str(root / "baseline"), "--candidate-dir", str(root / "candidate"), "--version", "0.18.0", "--output", str(output)])
            self.assertFalse(output.exists())

    def test_baseline_catalog_is_frozen_and_candidate_pair_is_explicit(self):
        repo=Path(__file__).resolve().parents[1]
        self.assertEqual(GATE.BASELINE,"0.16.0")
        self.assertEqual(GATE.QUALIFIED_CANDIDATE,"0.17.0")
        for arch in ('amd64','arm64'):
            values=GATE.baseline_setup_assets(repo,arch)
            self.assertTrue(values)
            self.assertTrue(any('rust-v0.162.1' in x['URL'] for x in values))
            self.assertFalse(any('opencode' in x['URL'] for x in values))
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(GATE.GateFailure): GATE.baseline_setup_assets(Path(directory),'amd64')

    def test_unexpected_command_output_stays_in_bounded_private_diagnostic(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            values = GATE.environment(root)
            with self.assertRaises(GATE.GateFailure):
                GATE.run([sys.executable, "-c", "print('synthetic-output-marker'); raise SystemExit(7)"], values)
            private = root / "private-command-failure.log"
            self.assertEqual(private.stat().st_mode & 0o777, 0o600)
            self.assertIn(b"synthetic-output-marker", private.read_bytes())
            self.assertNotIn("synthetic-output-marker", json.dumps(GATE.LAST_COMMAND))
            self.assertEqual(GATE.LAST_COMMAND["exit_code"], 7)

    def test_archive_does_not_accept_links_paths_or_additional_members(self):
        for name, kind, extra in [("../ivoai", tarfile.REGTYPE, False), ("ivoai", tarfile.SYMTYPE, False), ("ivoai", tarfile.REGTYPE, True)]:
            with self.subTest(name=name, kind=kind, extra=extra), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                archive = root / "candidate.tgz"
                with tarfile.open(archive, "w:gz") as stream:
                    item = tarfile.TarInfo(name)
                    item.type = kind
                    item.size = 1 if kind == tarfile.REGTYPE else 0
                    item.linkname = "/etc/passwd" if kind == tarfile.SYMTYPE else ""
                    stream.addfile(item, io.BytesIO(b"x"))
                    if extra:
                        item = tarfile.TarInfo("unexpected")
                        stream.addfile(item, io.BytesIO())
                with self.assertRaises(GATE.GateFailure):
                    GATE.extract_core(archive, root / "core")
                self.assertFalse((root.parent / "ivoai").exists())

    def test_environment_and_downloads_cannot_inherit_auth_or_arbitrary_hosts(self):
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {"OPENAI_API_KEY": "private-marker", "HTTPS_PROXY": "http://unexpected.invalid"}):
            root = Path(directory)
            values = GATE.environment(root)
            self.assertNotIn("OPENAI_API_KEY", values)
            self.assertNotIn("HTTPS_PROXY", values)
            self.assertNotIn("IVOAI_TEST_MODE", values)
            self.assertEqual(values["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"], "1")
            with patch.object(GATE.urllib.request, "build_opener", side_effect=AssertionError("network must not start")):
                for url in ["http://github.com/asset", "https://unexpected.invalid/asset", "https://user:pass@github.com/asset", "https://github.com/asset?token=private"]:
                    with self.subTest(url=url), self.assertRaises(GATE.GateFailure):
                        GATE.verified_download(url, "a" * 64, root)

    def test_tls_fixture_serves_only_readonly_allowlisted_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", str(root / "fixture.key"), "-out", str(root / "fixture.pem"), "-days", "1", "-subj", "/CN=Upgrade fixture", "-addext", "basicConstraints=critical,CA:TRUE", "-addext", "subjectAltName=DNS:github.com,DNS:api.github.com"], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            archive = root / "ivoai_linux_amd64.tar.gz"
            archive.write_bytes(b"verified fixture artifact")
            context = ssl.create_default_context(cafile=str(root / "fixture.pem"))
            with GATE.GitHubFixture(root, {}, archive, "0.16.0") as server:
                def request(host, path):
                    connection = http.client.HTTPSConnection("127.0.0.1", server.server.server_port, context=context, timeout=3)
                    connection.set_tunnel(host, 443)
                    try:
                        connection.request("GET", path)
                        response = connection.getresponse()
                        return response.status, response.read()
                    finally:
                        connection.close()
                status, body = request("api.github.com", "/repos/ivo-lopes/ivoai/releases/latest")
                self.assertEqual(status, 200)
                self.assertEqual(json.loads(body)["tag_name"], "v0.16.0")
                server.mode = "wrong_version"
                status, body = request("github.com", "/ivo-lopes/ivoai/releases/download/v99.99.99/" + archive.name)
                self.assertEqual((status, body), (200, archive.read_bytes()))
                server.mode = "corrupt"
                status, body = request("github.com", "/ivo-lopes/ivoai/releases/download/v0.16.0/" + archive.name)
                self.assertEqual(status, 200)
                self.assertNotEqual(body, archive.read_bytes())
                with self.assertRaises(OSError):
                    request("api.openai.com", "/v1/responses")
                self.assertEqual(server.denied, 1)
                server.reject("unmapped_route", "api.github.com", "/repos/ivo-lopes/ivoai/releases/latest?token=private-marker")
                diagnostic = json.dumps(server.denials)
                self.assertNotIn("private-marker", diagnostic)
                self.assertNotIn("?token=", diagnostic)
                self.assertEqual(server.denials[-1]["public_host"], "api.github.com")
                self.assertNotIn("public_path", server.denials[-1])
                server.phase = "verify_upgraded_graft_provenance"
                with patch.object(server, "tls") as tls:
                    with self.assertRaises(OSError):
                        request("chatgpt.com", "/")
                    tls.wrap_socket.assert_not_called()
                self.assertEqual(server.denied, 3)
                self.assertEqual(server.unexpected_requests, 2)
                self.assertEqual(server.diagnostic_blocks, {"verify_upgraded_graft_provenance": 1})
                self.assertEqual(server.denials[-1]["during_phase"], "verify_upgraded_graft_provenance")
                server.phase = "preparation"
                with self.assertRaises(OSError):
                    request("chatgpt.com", "/")
                self.assertEqual(server.denied, 4)
                self.assertEqual(server.unexpected_requests, 3)
                self.assertNotIn("chatgpt.com", GATE.FIXTURE_HOSTS)
                self.assertNotIn("chatgpt.com", GATE.DOWNLOAD_HOSTS)


if __name__ == "__main__":
    unittest.main()
