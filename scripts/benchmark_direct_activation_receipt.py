"""Offline cold-activation consistency contract; no launcher or execution grant.

A trusted host runner must bind a new challenge, reviewed executable and sources.
These hashes/booleans are not remote attestation of arbitrary supplied JSON. Old
baselines lacking actual public Direct-only factory qualification are unsupported
by v1; absence cannot be converted into a fabricated passing observation.
"""
from dataclasses import dataclass
import json
import os
from pathlib import Path
import re
import stat

MAX_RECEIPT_BYTES = 16384
DIGEST = re.compile(r"[a-f0-9]{64}\Z")
IDENTITY_KEYS = ("executable_sha256", "harness_sha256", "exporter_sha256",
                 "catalog_sha256", "fixture_sha256", "task_sha256",
                 "prompt_sha256", "expected_sha256", "challenge_sha256")
BOOL_FIELDS = ("actual_factory_checked", "classified", "exact_fidelity", "untrusted",
               "artifact_owner_exact", "exact_read_equal", "wrong_owner_denied", "fixture_unchanged")
COUNTS = ("code_inspections", "direct_inspections", "graft_inspections", "codeintel_cache_hits",
          "gateway_resolves", "gateway_cache_hits", "gateway_cache_misses", "gateway_cache_unknown")
ARM_KEYS = {"arm", "gateway_enabled", "execution_mode", "text_bytes", "text_sha256", *BOOL_FIELDS, *COUNTS}
ROOT_KEYS = {"schema_version", "contract", "scenario", "fixture", "status",
             "supplies_actual_factory_qualification", "cold_only", "warmup_performed",
             "model_client_started", "authenticated", "protected_recovery_enabled", "identity", "arms"}


class ActivationReceiptError(ValueError):
    """Only bounded reason enums; never echo paths, source, config or exceptions."""


@dataclass(frozen=True)
class ExpectedColdActivation:
    scenario: str
    fixture: str
    executable_sha256: str
    harness_sha256: str
    exporter_sha256: str
    catalog_sha256: str
    fixture_sha256: str
    task_sha256: str
    prompt_sha256: str
    expected_sha256: str
    challenge_sha256: str
    gateway_flags: tuple[bool, bool] = (False, True)

    def __post_init__(self):
        if self.scenario != "api" or self.fixture != "go-core":
            raise ActivationReceiptError("UNSUPPORTED_SCENARIO")
        if (type(self.gateway_flags) is not tuple or len(self.gateway_flags) != 2
                or any(type(x) is not bool for x in self.gateway_flags)
                or self.gateway_flags != (False, True)):
            raise ActivationReceiptError("UNSUPPORTED_FLAG_TUPLE")
        if any(type(getattr(self, key)) is not str or not DIGEST.fullmatch(getattr(self, key)) for key in IDENTITY_KEYS):
            raise ActivationReceiptError("INVALID_EXPECTED_IDENTITY")


@dataclass(frozen=True)
class QualifiedColdActivation:
    expected: ExpectedColdActivation
    text_sha256: str
    text_bytes: int
    basis: str = "reviewed_local_receipt_consistency_only"


def validate_receipt(value, expected):
    if type(expected) is not ExpectedColdActivation:
        raise ActivationReceiptError("MISSING_HOST_EXPECTATION")
    if type(value) is not dict or set(value) != ROOT_KEYS:
        raise ActivationReceiptError("RECEIPT_SCHEMA")
    if (type(value["schema_version"]) is not int or value["schema_version"] != 1
            or value["contract"] != "primary_direct_gateway_cold_v1"
            or value["scenario"] != expected.scenario or value["fixture"] != expected.fixture):
        raise ActivationReceiptError("RECEIPT_CONTRACT")
    if value["status"] != "QUALIFIED_COLD_DIRECT_ONLY":
        raise ActivationReceiptError("UNQUALIFIED_STATUS")
    required = {"supplies_actual_factory_qualification": True, "cold_only": True,
                "warmup_performed": False, "model_client_started": False, "authenticated": False,
                "protected_recovery_enabled": False}
    if any(type(value[k]) is not bool or value[k] is not v for k, v in required.items()):
        raise ActivationReceiptError("UNQUALIFIED_FACTORY_OR_SCOPE")
    identity = value["identity"]
    if (type(identity) is not dict or set(identity) != set(IDENTITY_KEYS)
            or any(type(identity[k]) is not str or identity[k] != getattr(expected, k) for k in IDENTITY_KEYS)):
        raise ActivationReceiptError("HOST_IDENTITY_MISMATCH")
    arms = value["arms"]
    if type(arms) is not list or len(arms) != 2:
        raise ActivationReceiptError("ARM_SCHEMA")
    for index, arm in enumerate(arms):
        if type(arm) is not dict or set(arm) != ARM_KEYS:
            raise ActivationReceiptError("ARM_SCHEMA")
        if (arm["arm"] != ("A", "B")[index] or type(arm["gateway_enabled"]) is not bool
                or arm["gateway_enabled"] is not expected.gateway_flags[index]
                or arm["execution_mode"] != "direct_only"):
            raise ActivationReceiptError("ARM_TREATMENT_MISMATCH")
        if any(type(arm[k]) is not bool or arm[k] is not True for k in BOOL_FIELDS):
            raise ActivationReceiptError("INCOMPLETE_ACTUAL_FACTORY_PROOF")
        target = {k: 0 for k in COUNTS}
        target.update(code_inspections=1, direct_inspections=1, gateway_resolves=index, gateway_cache_misses=index)
        if any(type(arm[k]) is not int or arm[k] != target[k] for k in COUNTS):
            raise ActivationReceiptError("COLD_OBSERVATION_MISMATCH")
        if (type(arm["text_bytes"]) is not int or not 1 <= arm["text_bytes"] <= 3072
                or type(arm["text_sha256"]) is not str or not DIGEST.fullmatch(arm["text_sha256"])):
            raise ActivationReceiptError("EXACT_EVIDENCE_BOUNDS")
    if (arms[0]["text_bytes"], arms[0]["text_sha256"]) != (arms[1]["text_bytes"], arms[1]["text_sha256"]):
        raise ActivationReceiptError("EXACT_EVIDENCE_MISMATCH")
    return QualifiedColdActivation(expected, arms[0]["text_sha256"], arms[0]["text_bytes"])


def _unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ActivationReceiptError("DUPLICATE_JSON_KEY")
        value[key] = item
    return value


def read_receipt(path, expected):
    """Read one private new-run receipt; never invokes a client or consumes auth."""
    path = Path(path)
    if not path.is_absolute() or str(path) != os.path.normpath(path):
        raise ActivationReceiptError("RECEIPT_PATH_REFUSED")
    try:
        current = path.parent
        while True:
            info = current.lstat()
            if not stat.S_ISDIR(info.st_mode) or stat.S_ISLNK(info.st_mode):
                raise ActivationReceiptError("RECEIPT_DIRECTORY_REFUSED")
            if current == path.parent and (info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) & 0o077):
                raise ActivationReceiptError("RECEIPT_DIRECTORY_REFUSED")
            if current == current.parent:
                break
            current = current.parent
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, "rb") as stream:
            info = os.fstat(stream.fileno())
            if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid()
                    or stat.S_IMODE(info.st_mode) != 0o600 or info.st_size > MAX_RECEIPT_BYTES):
                raise ActivationReceiptError("RECEIPT_FILE_REFUSED")
            body = stream.read(MAX_RECEIPT_BYTES + 1)
        if len(body) > MAX_RECEIPT_BYTES:
            raise ActivationReceiptError("RECEIPT_BOUNDS")
        value = json.loads(body, object_pairs_hook=_unique_object,
                           parse_constant=lambda _: (_ for _ in ()).throw(ActivationReceiptError("NONFINITE_JSON")))
    except ActivationReceiptError:
        raise
    except (OSError, ValueError, UnicodeError, RecursionError):
        raise ActivationReceiptError("RECEIPT_MISSING_OR_INVALID") from None
    return validate_receipt(value, expected)
