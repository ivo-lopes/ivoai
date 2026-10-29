#!/usr/bin/env python3
"""Frozen proposal only. No official process until explicit parent review/authorization."""
import argparse, hashlib, json, os, pathlib, signal, stat, subprocess, tempfile
GO_SHA = "5f5eb6d0b2fb6aa0bb5d5f8c5f1c944e0de955c925f0111af92a9d8f79680b5b"
PINS = {"codex": "628e35487d888a28f0427fc37501da529dd3d81fcbad7a2c3421c1e8670d92e1", "codex-code-mode-host": "ec71830690a7c0024916ee0c99fbdc635bc6e747ff665ea4f03a8389bbcb6b30"}
def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            value.update(chunk)
    return value.hexdigest()
def write(path, body, mode=0o600):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, mode)
    with os.fdopen(fd, "w") as f: f.write(body)
def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--execute-reviewed-once", action="store_true")
    parser.add_argument("--source", required=True, type=pathlib.Path)
    parser.add_argument("--expected-source", required=True)
    parser.add_argument("--binary-dir", required=True, type=pathlib.Path)
    parser.add_argument("--go", required=True, type=pathlib.Path)
    args = parser.parse_args()
    if not args.execute_reviewed_once:
        parser.error("REVIEWED_EXECUTION_FLAG_REQUIRED; this file is a proposal, not authorization")
    source, binary_dir, go = args.source, args.binary_dir, args.go
    if any(not p.is_absolute() or p.resolve() != p for p in (source, binary_dir, go)):
        raise SystemExit("INPUT_PATH_REFUSED")
    clean = subprocess.run(["git", "status", "--porcelain"], cwd=source, capture_output=True, check=True).stdout
    if clean: raise SystemExit("SOURCE_NOT_CLEAN")
    if digest(go) != GO_SHA: raise SystemExit("GO_PIN_REFUSED")
    revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=source, capture_output=True, check=True).stdout.decode().strip()
    if revision != args.expected_source: raise SystemExit("SOURCE_PIN_REFUSED")
    for name, expected in PINS.items():
        f = binary_dir / name
        if f.is_symlink() or not stat.S_ISREG(f.stat().st_mode) or f.stat().st_size > 512 << 20 or digest(f) != expected:
            raise SystemExit("PIN_REFUSED")
    root = pathlib.Path(tempfile.mkdtemp(prefix="ivoai-subscription-noauth-", dir="/var/tmp"))
    for name in ("case", "fixture", "reviewed", "case/home", "case/codehome", "case/repo", "case/effects"):
        (root / name).mkdir(mode=0o700)
    summary = {"result": "NOT_EXECUTED", "source_commit": revision, "official_client_starts_maximum": 1, "authenticated_positive_certified": False}
    try:
        for name in PINS:
            fd = os.open(root / "reviewed" / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o500)
            with (binary_dir / name).open("rb") as src, os.fdopen(fd, "wb") as dst:
                for chunk in iter(lambda: src.read(1 << 20), b""): dst.write(chunk)
            if digest(root / "reviewed" / name) != PINS[name]: raise RuntimeError("COPY_INTEGRITY_FAILED")
        write(root / "fixture/host-netns", os.readlink("/proc/self/ns/net") + "\n")
        write(root / "fixture/mcp-tripwire.sh", '#!/bin/sh\ncase "$1" in user|system) ;; *) exit 9 ;; esac\numask 077\nprintf started > "/case/effects/$1.started"\nexit 7\n', 0o500)
        write(root / "case/codehome/config.toml", 'model_provider="synthetic_custom"\n[model_providers.synthetic_custom]\nname="Synthetic unused route"\nbase_url="http://127.0.0.1:34567/v1"\nwire_api="responses"\nrequires_openai_auth=false\n[mcp_servers.synthetic_user]\ncommand="/bin/sh"\nargs=["/fixture/mcp-tripwire.sh","user"]\n')
        write(root / "fixture/system-config.toml", '[mcp_servers.synthetic_system]\ncommand="/bin/sh"\nargs=["/fixture/mcp-tripwire.sh","system"]\n')
        write(root / "case/repo/README", "Disposable subscription route qualification; no prompt.\n")
        build_env = {"PATH": str(go.parent) + ":/usr/bin:/bin", "HOME": os.environ.get("HOME", ""), "GOTOOLCHAIN": "local", "GOENV": "off", "GOPROXY": "off", "GOSUMDB": "off", "CGO_ENABLED": "0", "GOFLAGS": ""}
        built = subprocess.run([str(go), "test", "-c", "-o", str(root / "reviewed/app.test"), "./internal/app"], cwd=source, env=build_env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=120)
        if built.returncode: raise RuntimeError("BUILD_FAILED")
        if subprocess.run(["git", "status", "--porcelain"], cwd=source, capture_output=True, check=True).stdout: raise RuntimeError("SOURCE_CHANGED")
        after_revision = subprocess.run(["git", "rev-parse", "HEAD"], cwd=source, capture_output=True, check=True).stdout.decode().strip()
        if after_revision != revision: raise RuntimeError("SOURCE_CHANGED")
        summary["test_ELF_sha256"] = digest(root / "reviewed/app.test")
        command = ["/usr/bin/bwrap", "--die-with-parent", "--new-session", "--unshare-all", "--clearenv", "--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--ro-bind", "/lib", "/lib", "--ro-bind-try", "/lib64", "/lib64", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--dir", "/etc", "--dir", "/etc/codex", "--ro-bind", str(root / "fixture/system-config.toml"), "/etc/codex/config.toml", "--ro-bind", str(root / "fixture"), "/fixture", "--bind", str(root / "case"), "/case", "--ro-bind", str(root / "case/codehome/config.toml"), "/case/codehome/config.toml", "--ro-bind", str(root / "reviewed"), "/reviewed", "--chdir", "/case/repo", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "HOME", "/case/home", "--setenv", "CODEX_HOME", "/case/codehome", "--setenv", "LANG", "C.UTF-8", "--setenv", "IVOAI_SUBSCRIPTION_OFFLINE_NOAUTH_20261029", "REVIEWED_ONE_NEGATIVE", "/reviewed/app.test", "-test.run=^TestLiveSubscriptionStdioNoAuthQualification$", "-test.count=1", "-test.timeout=30s"]
        write(root / "namespace-argv.json", json.dumps(command, indent=2) + "\n")
        child = subprocess.Popen(command, env={"PATH": "/usr/bin:/bin"}, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
        try:
            try:
                code = child.wait(timeout=35)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGTERM)
                try: code = child.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    os.killpg(child.pid, signal.SIGKILL)
                    code = child.wait(timeout=3)
                summary["result"] = "FAIL_OWNED_TIMEOUT"
        finally:
            if child.poll() is None:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=3)
        summary["namespace_exit_zero"] = code == 0
        receipt = root / "case/qualification.json"
        if receipt.exists():
            def unique(pairs):
                value = {}
                for k, v in pairs:
                    if k in value: raise RuntimeError("DUPLICATE_RECEIPT_KEY")
                    value[k] = v
                return value
            fd = os.open(receipt, os.O_RDONLY | os.O_NOFOLLOW)
            with os.fdopen(fd, "rb") as f:
                metadata = os.fstat(f.fileno())
                if not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.getuid() or stat.S_IMODE(metadata.st_mode) != 0o600 or metadata.st_size > 8192: raise RuntimeError("RECEIPT_FILE_REFUSED")
                body = f.read(8193)
                if len(body) > 8192: raise RuntimeError("RECEIPT_BOUND")
                value = json.loads(body, object_pairs_hook=unique)
            expected = {"Outcome", "Reason", "SourceConstructorConfigObserved", "UserMCPDeclared", "SystemMCPDeclared", "NoMCPTripwire", "NoAPIRouteTripwire", "NoAuthFile", "OwnedReaped", "NativeWaitZero", "AuthenticatedPositiveCertified"}
            if set(value) != expected: raise RuntimeError("RECEIPT_SCHEMA_REFUSED")
            if value["Outcome"] not in ("PASS_NOAUTH_NEGATIVE_NOT_AUTHENTICATED", "FAIL_NOAUTH_QUALIFICATION"): raise RuntimeError("RECEIPT_ENUM_REFUSED")
            if value["Reason"] not in ("ACCEPTED_UNQUALIFIED", "SCOPE_DENIED", "PROXY_POLICY_UNKNOWN", "TRANSPORT_UNAVAILABLE", "RPC_INVALID", "RPC_ERROR", "RESPONSE_BOUNDS", "CONFIG_DENIED", "REQUIREMENTS_DENIED", "AUTH_DENIED", "ROUTE_DENIED", "CANCELED", "CLEANUP_FAILED", "UNKNOWN"): raise RuntimeError("RECEIPT_ENUM_REFUSED")
            if any(type(v) is not bool for k, v in value.items() if k not in ("Outcome", "Reason")): raise RuntimeError("RECEIPT_TYPE_REFUSED")
            summary["qualification"] = value
            if summary["result"] != "FAIL_OWNED_TIMEOUT": summary["result"] = "PASS_NOAUTH_NEGATIVE_ONLY" if code == 0 and value["Outcome"] == "PASS_NOAUTH_NEGATIVE_NOT_AUTHENTICATED" else "FAIL_QUALIFICATION"
        elif summary["result"] != "FAIL_OWNED_TIMEOUT": summary["result"] = "FAIL_EARLY_ENVELOPE_OR_STARTUP"
    except Exception:
        summary["result"] = "FAIL_PREPARATION_OR_CONTROL"
    write(root / "result.json", json.dumps(summary, indent=2) + "\n")
    print(str(root / "result.json"))
    return 0 if summary["result"] == "PASS_NOAUTH_NEGATIVE_ONLY" else 1
if __name__ == "__main__": raise SystemExit(main())
