"""Pure recipe checks: no main, subprocess, client or personal-state operation."""
import pathlib
import types
import unittest

recipe = types.ModuleType("reviewed_recipe")
recipe.__file__ = str(pathlib.Path(__file__).with_name("run_authenticated_once.py"))
exec(compile(pathlib.Path(recipe.__file__).read_text(), recipe.__file__, "exec"), recipe.__dict__)

class RecipeContract(unittest.TestCase):
    def test_shared_environment_values_preserved_without_provider_injection(self):
        values = {key: "synthetic-private-canary" for key in recipe.ALLOW}
        values.update({key: "refuse-private-canary" for key in ("OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN", "LD_PRELOAD", "PYTHONPATH", "GIT_DIR", "GOWORK", "HTTPS_PROXY", "SSL_CERT_FILE")})
        actual = recipe.client_environment(values)
        self.assertEqual(actual, {key: "synthetic-private-canary" for key in recipe.ALLOW})
        self.assertEqual(actual["CODEX_HOME"], values["CODEX_HOME"])
        self.assertEqual(actual["DBUS_SESSION_BUS_ADDRESS"], values["DBUS_SESSION_BUS_ADDRESS"])

    def value(self):
        result = {key: False for key in recipe.BOOL_FIELDS}
        result.update(Outcome="SYNTHETIC_DATA_ONLY_NOT_CERTIFIED", Reason="UNKNOWN", Tokens="NOT_APPLICABLE_NO_MODEL_PROMPT_ISSUED")
        return result

    def test_closed_receipt(self):
        import json
        value = self.value()
        self.assertEqual(recipe.closed_receipt(json.dumps(value).encode()), value)
        self.assertFalse(value["AuthenticatedPositiveCertified"])

    def test_free_string_denied(self):
        import json
        value = self.value(); value["Reason"] = "synthetic-private-canary"
        with self.assertRaises(RuntimeError): recipe.closed_receipt(json.dumps(value).encode())

    def test_synthetic_certification_denied(self):
        import json
        value = self.value(); value["AuthenticatedPositiveCertified"] = True
        with self.assertRaises(RuntimeError): recipe.closed_receipt(json.dumps(value).encode())

    def test_broader_claim_denied(self):
        import json
        value = self.value(); value["BillingCertified"] = True
        with self.assertRaises(RuntimeError): recipe.closed_receipt(json.dumps(value).encode())

    def test_duplicate_denied(self):
        with self.assertRaises(RuntimeError): recipe.closed_receipt(b'{"Reason":"UNKNOWN","Reason":"UNKNOWN"}')

    def test_type_and_bound_denied(self):
        import json
        value = self.value(); value["ClientReported"] = 1
        with self.assertRaises(RuntimeError): recipe.closed_receipt(json.dumps(value).encode())
        with self.assertRaises(RuntimeError): recipe.closed_receipt(b" " * 8193)

if __name__ == "__main__": unittest.main()
