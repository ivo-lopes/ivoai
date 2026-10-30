#!/usr/bin/env python3
"""Reviewable one-shot metadata recipe. No execution during implementation."""
import argparse
import hashlib
import json
import os
import pathlib
import signal
import stat
import subprocess
import tempfile

GO_SHA = "5f5eb6d0b2fb6aa0bb5d5f8c5f1c944e0de955c925f0111af92a9d8f79680b5b"
PINS = {"codex": "628e35487d888a28f0427fc37501da529dd3d81fcbad7a2c3421c1e8670d92e1", "codex-code-mode-host": "ec71830690a7c0024916ee0c99fbdc635bc6e747ff665ea4f03a8389bbcb6b30"}
ALLOW = "HOME CODEX_HOME PATH LANG TZ TERM TMPDIR LC_ALL LC_CTYPE LC_COLLATE LC_MESSAGES LC_MONETARY LC_NUMERIC LC_TIME LC_PAPER LC_NAME LC_ADDRESS LC_TELEPHONE LC_MEASUREMENT LC_IDENTIFICATION XDG_CONFIG_HOME XDG_DATA_HOME XDG_CACHE_HOME XDG_STATE_HOME XDG_RUNTIME_DIR DBUS_SESSION_BUS_ADDRESS".split()
PASS = "PASS_CLIENT_REPORTED_CHATGPT_CANONICAL_ROUTE_DURING_PROBE"
BOOL_FIELDS = set("ClientReported OwnedReaped NativeWaitZero ContextActive FixtureUnchanged PrimaryIdentityPreserved NetworkNamespacePreserved PIDContained AuthenticatedPositiveCertified DriverModelPromptIssued ExclusiveInteractiveCredentialCertified BillingCertified CohortConnected AtomicSnapshotCertified".split())
REASONS = set("ACCEPTED_UNQUALIFIED SCOPE_DENIED PROXY_POLICY_UNKNOWN TRANSPORT_UNAVAILABLE RPC_INVALID RPC_ERROR RESPONSE_BOUNDS CONFIG_DENIED REQUIREMENTS_DENIED AUTH_DENIED ROUTE_DENIED CANCELED CLEANUP_FAILED UNKNOWN".split())

def client_environment(environ):
    return {key: environ[key] for key in ALLOW if key in environ}

def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as file:
        if os.fstat(file.fileno()).st_size > 512 << 20:
            raise RuntimeError("EXECUTABLE_BOUND")
        for block in iter(lambda: file.read(1 << 20), b""):
            value.update(block)
    return value.hexdigest()

def exclusive(path, text):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as file:
        file.write(text)

def unique(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise RuntimeError("DUPLICATE_RECEIPT_KEY")
        value[key] = item
    return value

def closed_receipt(body):
    if len(body) > 8192:
        raise RuntimeError("RECEIPT_BOUND")
    value = json.loads(body, object_pairs_hook=unique)
    if type(value) is not dict or set(value) != BOOL_FIELDS | {"Outcome", "Reason", "Tokens"}:
        raise RuntimeError("RECEIPT_SCHEMA")
    if value["Outcome"] not in (PASS, "STOP_METADATA_QUALIFICATION_NOT_MET", "SYNTHETIC_DATA_ONLY_NOT_CERTIFIED") or value["Reason"] not in REASONS or value["Tokens"] != "NOT_APPLICABLE_NO_MODEL_PROMPT_ISSUED":
        raise RuntimeError("RECEIPT_ENUM")
    if any(type(value[key]) is not bool for key in BOOL_FIELDS):
        raise RuntimeError("RECEIPT_TYPE")
    if any(value[key] for key in ("DriverModelPromptIssued", "ExclusiveInteractiveCredentialCertified", "BillingCertified", "CohortConnected", "AtomicSnapshotCertified")):
        raise RuntimeError("RECEIPT_BROADER_CLAIM_REFUSED")
    if value["AuthenticatedPositiveCertified"] != (value["Outcome"] == PASS):
        raise RuntimeError("RECEIPT_CERTIFICATION_MISMATCH")
    if value["Outcome"] == PASS and (value["Reason"] != "ACCEPTED_UNQUALIFIED" or not all(value[key] for key in ("ClientReported", "OwnedReaped", "NativeWaitZero", "ContextActive", "FixtureUnchanged", "PrimaryIdentityPreserved", "NetworkNamespacePreserved", "PIDContained"))):
        raise RuntimeError("RECEIPT_PREDICATES_MISSING")
    return value

def read_closed(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as file:
        info = os.fstat(file.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o600 or info.st_size > 8192:
            raise RuntimeError("RECEIPT_FILE")
        return closed_receipt(file.read(8193))

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--execute-reviewed-once", action="store_true")
    parser.add_argument("--filtered-environment-permitted", action="store_true")
    parser.add_argument("--source", required=True, type=pathlib.Path)
    parser.add_argument("--expected-source", required=True)
    parser.add_argument("--binary-dir", required=True, type=pathlib.Path)
    parser.add_argument("--go", required=True, type=pathlib.Path)
    parser.add_argument("--module-cache", required=True, type=pathlib.Path)
    args = parser.parse_args()
    if not args.execute_reviewed_once or not args.filtered_environment_permitted:
        parser.error("EXPLICIT_REVIEWED_ONCE_AND_ENVIRONMENT_FLAGS_REQUIRED; this recipe does not grant authorization")
    client_env = client_environment(os.environ)
    if not client_env.get("HOME") or not pathlib.Path(client_env["HOME"]).is_absolute():
        raise SystemExit("CLIENT_HOME_ENVELOPE_UNAVAILABLE")
    root = pathlib.Path(tempfile.mkdtemp(prefix="ivoai-subscription-auth-metadata-", dir="/var/tmp"))
    for name in ("build-home", "go-cache", "go-path", "scratch"):
        (root / name).mkdir(mode=0o700)
    summary = {"Outcome": "NOT_EXECUTED", "OfficialClientStartsMaximum": 1, "AuthenticatedPositiveCertified": False, "CohortConnected": False, "DriverModelPromptIssued": False}
    try:
        source, binary_dir, go = args.source, args.binary_dir, args.go
        if any(not path.is_absolute() or path.resolve() != path for path in (source, binary_dir, go)):
            raise RuntimeError("INPUT_PATH")
        git_env = {"PATH": "/usr/bin:/bin", "HOME": str(root / "build-home")}
        def git(*tail):
            result = subprocess.run(["/usr/bin/git", "--no-optional-locks", *tail], cwd=source, env=git_env, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=15, check=True)
            if len(result.stdout) > 65536:
                raise RuntimeError("SOURCE_METADATA_BOUND")
            return result.stdout
        revision = git("rev-parse", "HEAD").decode().strip()
        if revision != args.expected_source or git("status", "--porcelain"):
            raise RuntimeError("SOURCE_PIN")
        if digest(go) != GO_SHA:
            raise RuntimeError("GO_PIN")
        for name, expected in PINS.items():
            path = binary_dir / name
            if path.is_symlink() or not stat.S_ISREG(path.stat().st_mode) or digest(path) != expected:
                raise RuntimeError("CLIENT_PIN")
        summary["SourceCommit"] = revision
        module_cache = args.module_cache
        if not module_cache.is_absolute() or not module_cache.is_dir():
            raise RuntimeError("LOCAL_MODULE_CACHE_UNAVAILABLE")
        build_env = {"PATH": str(go.parent) + ":/usr/bin:/bin", "HOME": str(root / "build-home"), "GOCACHE": str(root / "go-cache"), "GOPATH": str(root / "go-path"), "GOMODCACHE": str(module_cache), "TMPDIR": str(root / "scratch"), "GOTOOLCHAIN": "local", "GOENV": "off", "GOWORK": "off", "GOPROXY": "off", "GOSUMDB": "off", "CGO_ENABLED": "0", "GOFLAGS": ""}
        build = ["/usr/bin/bwrap", "--die-with-parent", "--unshare-all", "--uid", str(os.getuid()), "--gid", str(os.getgid()), "--ro-bind", "/", "/", "--bind", str(root), str(root), "--proc", "/proc", "--dev", "/dev", "--chdir", str(source), str(go), "test", "-c", "-buildvcs=false", "-o", str(root / "app.test"), "./internal/app"]
        built = subprocess.run(build, env=build_env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=180)
        if built.returncode or git("status", "--porcelain") or git("rev-parse", "HEAD").decode().strip() != revision:
            raise RuntimeError("SOURCE_BUILD")
        summary["TestELFSHA256"] = digest(root / "app.test")
        # This phase shares normal host filesystem/network paths. Only PID/user
        # containment is added; auth/config/keyring remain client-owned in place.
        runtime_env = dict(client_env)
        runtime_env.update({"IVOAI_SUBSCRIPTION_AUTH_METADATA_20261029": "REVIEWED_ONE_METADATA_PROBE", "IVOAI_SUBSCRIPTION_HOST_POLICY": "FILTERED_ENV_PERMITTED", "IVOAI_SUBSCRIPTION_PRIVATE_OUTPUT": str(root), "IVOAI_SUBSCRIPTION_REVIEWED_CODEX": str(binary_dir / "codex"), "IVOAI_SUBSCRIPTION_OUTER_UID": str(os.getuid()), "IVOAI_SUBSCRIPTION_OUTER_GID": str(os.getgid()), "IVOAI_SUBSCRIPTION_OUTER_NETNS": os.readlink("/proc/self/ns/net"), "IVOAI_SUBSCRIPTION_OUTER_PIDNS": os.readlink("/proc/self/ns/pid")})
        command = ["/usr/bin/bwrap", "--die-with-parent", "--new-session", "--unshare-user", "--unshare-pid", "--uid", str(os.getuid()), "--gid", str(os.getgid()), "--dev-bind", "/", "/", "--proc", "/proc", "--clearenv"]
        for key, value in runtime_env.items():
            command.extend(["--setenv", key, value])
        command.extend([str(root / "app.test"), "-test.run=^TestLiveSubscriptionClientReportedRoute$", "-test.count=1", "-test.timeout=30s"])
        child = subprocess.Popen(command, env={"PATH": "/usr/bin:/bin"}, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
        timed_out = False
        try:
            try:
                code = child.wait(timeout=35)
            except subprocess.TimeoutExpired:
                timed_out = True
                os.killpg(child.pid, signal.SIGTERM)
                try:
                    code = child.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    code = child.wait(timeout=3)
        finally:
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=3)
        summary["OuterExitZero"] = code == 0
        receipt = root / "qualification.json"
        if receipt.exists():
            value = read_closed(receipt)
            summary["Qualification"] = value
            passed = not timed_out and code == 0 and value["Outcome"] == PASS and value["AuthenticatedPositiveCertified"] and all(value[key] for key in ("ClientReported", "OwnedReaped", "NativeWaitZero", "ContextActive", "FixtureUnchanged", "PrimaryIdentityPreserved", "NetworkNamespacePreserved", "PIDContained")) and not any(value[key] for key in ("DriverModelPromptIssued", "ExclusiveInteractiveCredentialCertified", "BillingCertified", "CohortConnected", "AtomicSnapshotCertified"))
            summary["Outcome"] = PASS if passed else "STOP_METADATA_QUALIFICATION_NOT_MET"
            summary["AuthenticatedPositiveCertified"] = passed
        else:
            summary["Outcome"] = "STOP_ENVELOPE_OR_EXECUTION_NOT_MET"
        if timed_out:
            summary["Outcome"] = "STOP_OWNED_TIMEOUT"
            summary["AuthenticatedPositiveCertified"] = False
    except Exception:
        summary["Outcome"] = "STOP_PREPARATION_OR_ENVELOPE_NOT_MET"
    exclusive(root / "result.json", json.dumps(summary, indent=2) + "\n")
    print(str(root / "result.json"))
    return 0 if summary["Outcome"] == PASS else 1

if __name__ == "__main__":
    raise SystemExit(main())
