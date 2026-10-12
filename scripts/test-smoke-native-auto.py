#!/usr/bin/env python3
"""Exercise smoke assertions without starting a provider or native frontend."""
import ast
import copy
from pathlib import Path
import unittest


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
