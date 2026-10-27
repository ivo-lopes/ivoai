#!/usr/bin/env python3
"""Exercise smoke assertions without starting a provider or native frontend."""
import ast
import copy
from pathlib import Path
import unittest


class IntakeFixtureTests(unittest.TestCase):
    def test_oversized_fixture_is_complete_utf8_paste_before_submission(self):
        tree = ast.parse(Path(__file__).with_name("smoke-native-auto.py").read_text())
        nodes = [n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == "send_oversized_fixture"]
        self.assertEqual(len(nodes), 1)
        output = bytearray()
        def write(_fd, payload):
            # A real PTY may accept only part of a chunk; the fixture must not
            # silently shrink below the backend's rejection threshold.
            count = min(317, len(payload))
            output.extend(payload[:count])
            return count
        namespace = {"master": 1, "os": type("PTY", (), {"write": staticmethod(write)}),
                     "send": lambda text: output.extend(text.encode()), "pump": lambda _: None}
        exec(compile(ast.Module(body=nodes, type_ignores=[]), "intake-fixture", "exec"), namespace)
        namespace["send_oversized_fixture"]()
        self.assertTrue(output.startswith(b"\x1b[200~"))
        self.assertTrue(output.endswith(b"\x1b[201~\r"))
        payload = output[len(b"\x1b[200~"):-len(b"\x1b[201~\r")]
        self.assertGreater(len(payload), 128 << 10)
        self.assertEqual(payload.decode("utf-8"), "a" * ((128 << 10) + 1))


class NativeComposerTests(unittest.TestCase):
    def checker(self):
        tree = ast.parse(Path(__file__).with_name("smoke-native-auto.py").read_text())
        nodes = [n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name == "codex_composer_ready"]
        self.assertEqual(len(nodes), 1, "native composer predicate missing")
        namespace = {}
        exec(compile(ast.Module(body=nodes, type_ignores=[]), "composer", "exec"), namespace)
        return namespace["codex_composer_ready"]

    def test_reviewed_native_landmarks_without_old_footer(self):
        # Only allowlisted landmarks from startup diagnostics, no transcript.
        self.assertTrue(self.checker()("OpenAI Codex /model › shortcuts"))

    def test_resumed_composer_omits_initial_model_hint(self):
        self.assertTrue(self.checker()("OpenAI Codex › shortcuts"))

    def test_picker_and_admission_dialogs_are_not_composers(self):
        check = self.checker()
        for dialog in ["Resume a previous session", "Sign in with ChatGPT", "Do you trust"]:
            self.assertFalse(check("OpenAI Codex /model › shortcuts " + dialog))

    def test_banner_or_partial_landmarks_are_not_ready(self):
        check = self.checker()
        for text in ["OpenAI Codex", "OpenAI Codex /model", "shortcuts", "› /model shortcuts"]:
            self.assertFalse(check(text), text)
        self.assertTrue(check("100% context left"))
        self.assertTrue(check("? for shortcuts"))


class ContinuityOutputTests(unittest.TestCase):
    def recall_assertion(self):
        source = Path(__file__).with_name("smoke-native-auto.py").read_text()
        tree = ast.parse(source)
        for node in ast.walk(tree):
            if (isinstance(node, ast.Assert) and isinstance(node.msg, ast.Constant)
                    and node.msg.value == "NATIVE_CONVERSATION_FACT_NOT_RECALLED"):
                return compile(ast.fix_missing_locations(ast.Module(body=[node], type_ignores=[])), "recall-assertion", "exec")
        self.fail("continuity recall assertion missing")

    def test_answer_already_consumed_by_completion_wait(self):
        # The preceding wait_for pumped the answer before observing completion.
        exec(self.recall_assertion(), {"history": "assistant: fixture-1", "pump": lambda _: ""})

    def test_missing_answer_still_fails(self):
        with self.assertRaisesRegex(AssertionError, "NATIVE_CONVERSATION_FACT_NOT_RECALLED"):
            exec(self.recall_assertion(), {"history": "assistant: unknown", "pump": lambda _: ""})


class RecoverySafetyTests(unittest.TestCase):
    def checker(self):
        tree = ast.parse(Path(__file__).with_name("smoke-native-auto.py").read_text())
        nodes = [node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == "readonly_recovery_snapshot"]
        self.assertEqual(len(nodes), 1, "readonly checkpoint guard missing")
        namespace = {}
        exec(compile(ast.fix_missing_locations(ast.Module(body=nodes, type_ignores=[])), "recovery-guard", "exec"), namespace)
        return namespace["readonly_recovery_snapshot"]

    def fixture(self):
        value = {"plan_id": "plan_real", "working_directory": "/fixture", "workers": [],
                 "tasks": [{"id": "read", "state": "completed"}, {"id": "verify", "state": "primary"}]}
        checkpoint = {"recovery": {"approved": True, "integrated": False, "directory": "/fixture",
                     "plan": {"id": "plan_real", "tasks": [
                       {"id": "read", "role": "research", "execution_mode": "primary"},
                       {"id": "verify", "role": "research", "execution_mode": "primary", "dependencies": ["read"]}]}}}
        return value, checkpoint

    def test_partial_approved_readonly_execution_is_recoverable(self):
        value, checkpoint = self.fixture()
        self.assertEqual(self.checker()(value, checkpoint, True), (["read"], ["verify"]))

    def test_unsafe_or_unproven_execution_is_never_interrupted(self):
        check = self.checker()
        value, checkpoint = self.fixture()
        for field, replacement in [("write_paths", ["file"]), ("allowed_mcp_tools", {"remote": ["read"]}),
                                   ("allowed_mcps", ["remote"]), ("execution_mode", "worker")]:
            unsafe = copy.deepcopy(checkpoint)
            unsafe["recovery"]["plan"]["tasks"][1][field] = replacement
            self.assertIsNone(check(value, unsafe, True), field)
        for field, replacement in [("approved", False), ("integrated", True), ("directory", "/other")]:
            unsafe = copy.deepcopy(checkpoint)
            unsafe["recovery"][field] = replacement
            self.assertIsNone(check(value, unsafe, True), field)
        value["tasks"][1]["state"] = "completed"
        self.assertIsNone(check(value, checkpoint, True))
        value["tasks"][1]["state"] = "primary"
        value["workers"] = [{"id": "unexpected"}]
        self.assertIsNone(check(value, checkpoint, True))


if __name__ == "__main__":
    unittest.main()
