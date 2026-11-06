"""Startup gate controls; these tests do not execute or certify a provider."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SPEC=importlib.util.spec_from_file_location('native_pair',Path(__file__).with_name('verify-native-codex-pair.py'))
GATE=importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GATE)


class NativePairControls(unittest.TestCase):
    def test_reviewed_pairs_have_exact_archive_and_executable_identities(self):
        rows=json.loads((Path(__file__).resolve().parents[1]/'manifest/native-codex-runtime.json').read_text())
        self.assertEqual(len(rows),4)
        self.assertEqual({(r['arch'],r['binary']) for r in rows},
                         {(a,b) for a in ('amd64','arm64') for b in ('codex','codex-code-mode-host')})
        for row in rows:
            self.assertEqual(row['sha256'],row['actual_archive_sha256'])
            self.assertEqual(len(row['binary_sha256']),64)
            self.assertTrue(row['url'].startswith('https://github.com/openai/codex/releases/download/rust-v0.167.0/'))

    def test_fingerprint_rejects_symlinks_and_empty_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            empty=root/'empty'; empty.touch()
            link=root/'link'; link.symlink_to(empty)
            for path in (empty,link):
                with self.assertRaises((ValueError,OSError)): GATE.fingerprint(path)

    def test_unsupported_cpu_is_rejected_before_executable_access(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            emulator=root/'emulator'; emulator.touch()
            with self.assertRaisesRegex(ValueError,'baseline CPU'):
                GATE.main(['--directory',str(root),'--arch','amd64','--emulator',str(emulator),
                           '--cpu','host','--output',str(root/'receipt')])
            self.assertFalse((root/'receipt').exists())
