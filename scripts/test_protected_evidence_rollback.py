"""Adversarial parser fixtures only; these do not count as a real rollback run."""

import importlib.util
import json
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location("rollback_validator", Path(__file__).with_name("validate-protected-evidence-rollback.py"))
VALIDATOR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VALIDATOR)


def fixture():
    return [
        {"Action": "run", "Package": VALIDATOR.PACKAGE, "Test": VALIDATOR.TEST},
        {"Action": "output", "Package": VALIDATOR.PACKAGE, "Test": VALIDATOR.TEST, "Output": VALIDATOR.MARKER},
        {"Action": "pass", "Package": VALIDATOR.PACKAGE, "Test": VALIDATOR.TEST},
        {"Action": "pass", "Package": VALIDATOR.PACKAGE},
    ]


def encode(rows):
    return "\n".join(json.dumps(row) for row in rows)


class RollbackProofParserTests(unittest.TestCase):
    def test_accepts_only_complete_named_proof_fixture(self):
        VALIDATOR.validate(encode(fixture()))

    def test_rejects_empty_missing_skipped_failed_or_duplicate_named_proof(self):
        cases = [[], [fixture()[-1]], fixture()[:-1], fixture()[1:], fixture() + [fixture()[2]], fixture() + [fixture()[-1]]]
        for action in ("skip", "fail"):
            rows = fixture()
            rows[2]["Action"] = action
            cases.append(rows)
        rows = fixture()
        rows[0], rows[2] = rows[2], rows[0]
        cases.append(rows)
        for rows in cases:
            with self.subTest(rows=rows), self.assertRaises(ValueError):
                VALIDATOR.validate(encode(rows))

    def test_rejects_wrong_package_test_marker_or_json(self):
        for field, value in (("Package", "other/module"), ("Test", "UnrelatedTest"), ("Output", "PASS")):
            rows = fixture()
            rows[1][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                VALIDATOR.validate(encode(rows))
        for text in ("not JSON", "[]", "null"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                VALIDATOR.validate(text)

    def test_rejects_duplicate_marker_or_foreign_skip(self):
        for extra in (fixture()[1], {"Action": "skip", "Package": VALIDATOR.PACKAGE, "Test": "UnexpectedSkip"}):
            with self.subTest(extra=extra), self.assertRaises(ValueError):
                VALIDATOR.validate(encode(fixture() + [extra]))


if __name__ == "__main__":
    unittest.main()
