#!/usr/bin/env python3
"""Real published-Core upgrade qualification, with a private GitHub fixture.

Preparation installs only reviewed components in disposable HOME/XDG roots.
The actual upgrade/rollback runs without external networking, using real CLI
binaries, real component archives, production migrations/setup/doctor and a
loopback HTTPS fixture. No TEST_MODE, personal config, auth or inference is used.

Zero external traffic does not mean zero attempted connections. Official Codex
diagnostics may attempt chatgpt.com discovery: the fixture always refuses that
CONNECT before TLS, counts it separately only during known diagnostic phases,
and still fails every other denied request. Catalog/quota availability is not
certified by this gate. Historical failed receipts are never rewritten.
"""
import argparse
import hashlib
import http.server
import json
import os
from pathlib import Path
import platform
import re
import shutil
import ssl
import subprocess
import tarfile
import tempfile
import threading
import time
import tomllib
import urllib.parse
import urllib.request

BASELINE = "0.16.0"
# Advance this reviewed pair and its published hashes for each stable release.
QUALIFIED_CANDIDATE = "0.17.0"
BASELINE_BINARIES = {
    "amd64": "a933d4d313930066409c77682a8cc26b574b5c259174e1cb68fac6b890c9d2ef",
    "arm64": "f7cf98e224f202ebc6e218fc26a8cc83d7a4cb25cb66182b67683008b197f999",
}
BASELINE_METADATA = {
    "amd64": "6fc5af54dd963e8d0af58b6a8dc051a52fef9e0447ef25630da669f394b6f6d7",
    "arm64": "ea612e7e9e8c5e352f78a91fe3c597dec2530eb023b00346087c24a91390a106",
}
BOOTSTRAP = {
    "amd64": [("https://nodejs.org/dist/v26.11.1/node-v26.11.1-linux-x64.tar.gz", "15fc043a51a5783f2f4b634561091658f5a7f0e3db03f997dca3a8033a33e45b"),
              ("https://github.com/astral-sh/uv/releases/download/0.13.0/uv-x86_64-unknown-linux-gnu.tar.gz", "1468ebd5a5541121837c5a2817b9972ba6090fa6caa3d142620850a47fb75154")],
    "arm64": [("https://nodejs.org/dist/v26.11.1/node-v26.11.1-linux-arm64.tar.gz", "170454fb022f5e9b5f6cc76c25fa32ef5100895be44eb04e92f9780591491bbf"),
              ("https://github.com/astral-sh/uv/releases/download/0.13.0/uv-aarch64-unknown-linux-gnu.tar.gz", "3ccfb6af6e242433eb552f7d9676abd5412c6595c497e990d9c8cb7b5bd4d2c3")],
}
DOWNLOAD_HOSTS = {"github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "nodejs.org", "registry.npmjs.org"}
FIXTURE_HOSTS = {"github.com", "api.github.com", "nodejs.org"}
# Official CLI diagnostic startup can attempt provider discovery without auth.
# These CONNECTs remain denied before TLS. No provider endpoint is served.
DIAGNOSTIC_PHASES = {"real_cli_upgrade_migrate_setup_doctor", "verify_upgraded_graft_provenance", "real_cli_rollback", "verify_rolled_back_graft_provenance", "real_cli_update_after_rollback"}
MAX_ARCHIVE = 512 << 20
LAST_COMMAND = {}


class GateFailure(Exception):
    pass


def require(condition, reason):
    if not condition:
        raise GateFailure(reason)


def digest(path):
    h = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def verified_download(url, expected, directory):
    require(re.fullmatch(r"[a-f0-9]{64}", expected), "INVALID_PIN")
    target = directory / expected
    if target.exists():
        require(digest(target) == expected, "CACHE_INTEGRITY_FAILED")
        return target

    class Redirects(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, request, fp, code, message, headers, location):
            destination = urllib.parse.urlparse(location)
            require(destination.scheme == "https" and destination.hostname in DOWNLOAD_HOSTS and not destination.username, "DOWNLOAD_REDIRECT_DENIED")
            return super().redirect_request(request, fp, code, message, headers, location)

    parsed = urllib.parse.urlparse(url)
    require(parsed.scheme == "https" and parsed.hostname in DOWNLOAD_HOSTS and not parsed.username and not parsed.query and not parsed.fragment, "DOWNLOAD_URL_DENIED")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), Redirects())
    with opener.open(url, timeout=90) as response, target.open("xb") as output:
        count = 0
        while block := response.read(1 << 20):
            count += len(block)
            require(count <= MAX_ARCHIVE, "DOWNLOAD_SIZE_EXCEEDED")
            output.write(block)
    require(digest(target) == expected, "DOWNLOADED_INTEGRITY_FAILED")
    return target


BASELINE_SOURCE = "685c5740ec046dcbcf7eeec77c6fd88885975d8d"
BASELINE_SETUP_SHA256 = "c4b4204b9f5295bbe89ec86ddf742ebec1eba3ae5f2d59bccbf68e592fbfc60f"
LEGACY_USER_MARKERS = [".config/opencode/opencode.json", ".local/share/opencode/auth.json",
                       ".local/share/opencode/history/fixture.json", ".local/bin/opencode",
                       ".config/opencode/profiles/previous.json"]

def baseline_setup_assets(repo, arch):
    path = repo/"scripts/release-baselines/v0.16.0/setup-assets.json"
    require(path.is_file() and not path.is_symlink() and digest(path)==BASELINE_SETUP_SHA256,"BASELINE_CATALOG_INTEGRITY_FAILED")
    document = json.loads(path.read_text())
    require(document["source"]==BASELINE_SOURCE and document["catalog_source_sha256"]=="147f8813a5712b75deda6406fb8e7878c1a410030acbfcfcaffb7ab6ff87bbd8", "BASELINE_CATALOG_SOURCE_MISMATCH")
    return document["assets"][arch]

def prime_headroom_cache(work, repo, arch, values, catalog, bootstrap, routes):
    # Only cache/interpreter preparation, with a disposable tool root: the actual
    # candidate Headroom profile and active state remain uninstalled until update.
    require(re.fullmatch(r"3\.[0-9]+\.[0-9]+",bootstrap["python_version"]),"UNREVIEWED_PYTHON_POLICY")
    require(re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:]+Z",bootstrap["exclude_newer"]),"UNREVIEWED_CUTOFF_POLICY")
    uvs=[x for x in bootstrap["assets"] if x["URL"].startswith("https://github.com/astral-sh/uv/releases/download/")]
    wheels=[x for x in catalog if x["URL"].endswith(".whl") and "headroom" in x["URL"]]
    require(len(uvs)==len(wheels)==1,"CACHE_PREPARATION_INPUT_AMBIGUOUS")
    uv=work/"cache-prime-uv"
    with tarfile.open(routes[uvs[0]["URL"]],"r|gz") as archive:
        found=False
        for member in archive:
            if Path(member.name).name!="uv": continue
            require(not found and member.isfile() and 0<member.size<=128<<20,"UV_CACHE_EXECUTABLE_DENIED")
            with archive.extractfile(member) as source,uv.open("xb") as target:
                shutil.copyfileobj(source,target,1<<20)
            found=True
        require(found,"UV_CACHE_EXECUTABLE_UNAVAILABLE")
    uv.chmod(0o700)
    stage=work/"candidate-headroom-cache-prime"
    stage.mkdir(mode=0o700)
    lock=repo/f"internal/components/assets/headroom-{arch}.lock"
    wheel=wheels[0]
    text=lock.read_text()
    require(wheel["URL"] in text and wheel["SHA256"] in text,"CACHE_PREPARATION_LOCK_MISMATCH")
    private_home=work/"data/ivoai/installer-home"
    private_home.mkdir(mode=0o700,parents=True,exist_ok=True)
    environment={"HOME":str(private_home),"PATH":str(uv.parent)+":/usr/bin:/bin",
        "UV_TOOL_DIR":str(stage),"UV_TOOL_BIN_DIR":str(stage/"bin"),
        "UV_PYTHON_INSTALL_DIR":str(work/"data/ivoai/components/python"),
        "UV_PYTHON_PREFERENCE":"only-managed","UV_NO_PROGRESS":"1"}
    argument="headroom-ai[proxy] @ "+wheel["URL"]+"#sha256="+wheel["SHA256"]
    run([uv,"tool","install","--force","--python",bootstrap["python_version"],"--constraints",lock,
        "--no-build","--exclude-newer",bootstrap["exclude_newer"],argument],environment,timeout=1200,cwd=stage)


def extract_core(archive, destination):
    require(0 < archive.stat().st_size <= MAX_ARCHIVE, "CORE_ARCHIVE_SIZE")
    with tarfile.open(archive, "r|gz") as tar:
        entry = tar.next()
        require(entry is not None, "CORE_ARCHIVE_ENTRY_COUNT")
        require(entry.name in {"ivoai", "./ivoai"} and entry.isfile() and 0 < entry.size <= 256 << 20, "CORE_ARCHIVE_ENTRY_DENIED")
        with tar.extractfile(entry) as source, destination.open("xb") as output:
            shutil.copyfileobj(source, output, 1 << 20)
        require(tar.next() is None, "CORE_ARCHIVE_ENTRY_COUNT")
    destination.chmod(0o700)


def environment(work):
    values = {"PATH": "/usr/bin:/bin", "HOME": str(work / "home"), "LANG": "C.UTF-8", "TZ": "UTC", "NO_COLOR": "1", "TERM": "dumb"}
    # Official process-local opt-outs, never personal Claude configuration.
    # Setup removes managed MCP entries even with orchestration disabled.
    values["CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC"] = "1"
    values["DISABLE_AUTOUPDATER"] = "1"
    values["DISABLE_ERROR_REPORTING"] = "1"
    values["DISABLE_TELEMETRY"] = "1"
    for variable, folder in [("XDG_CONFIG_HOME", "config"), ("XDG_DATA_HOME", "data"), ("XDG_STATE_HOME", "state"), ("XDG_CACHE_HOME", "cache"), ("TMPDIR", "tmp"), ("CODEX_HOME", "provider-codex"), ("CLAUDE_CONFIG_DIR", "provider-claude")]:
        values[variable] = str(work / folder)
    for key, value in values.items():
        if key in {"HOME", "TMPDIR", "CODEX_HOME", "CLAUDE_CONFIG_DIR"} or key.startswith("XDG_"):
            Path(value).mkdir(mode=0o700, parents=True, exist_ok=True)
    return values


def run(command, env, timeout=180, expected=0, contains=None, cwd=None):
    # Only bounded synthetic fixture output is inspected in RAM. No command
    # output, argv, environment or client history is included in the receipt.
    # An unexpected failure preserves at most 64 KiB in a separate private log.
    process = subprocess.Popen([str(x) for x in command], env=env, cwd=cwd or env["HOME"], stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, start_new_session=True)
    try:
        output, _ = process.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        import signal
        os.killpg(process.pid, signal.SIGKILL)
        process.communicate()
        raise GateFailure("COMMAND_TIMEOUT")
    require(len(output) <= 8 << 20, "COMMAND_OUTPUT_LIMIT")
    accepted = (process.returncode == 0) if expected == 0 else (process.returncode != 0)
    if not accepted:
        global LAST_COMMAND
        lowered = output.lower()
        terms = [term for term in ["candidate migration", "candidate setup", "post-update doctor", "active managed component provenance does not match resolved source", "deadline exceeded", "context canceled", "timed out", "timeout", "certificate", "checksum", "permission denied", "no such file", "enotfound", "eai_again", "econnrefused", "ruflo", "headroom", "graft", "codex", "claude", "python", "npm", "rollback"] if term.encode() in lowered]
        LAST_COMMAND = {"exit_code": process.returncode, "known_terms": terms}
        # This diagnostic is strictly private: only synthetic HOME/config was
        # supplied. Public receipts contain classifications, never this output.
        private = Path(env["HOME"]).parent / "private-command-failure.log"
        try:
            descriptor = os.open(private, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        except FileExistsError:
            pass  # Preserve the deepest failure, not its enclosing wrapper.
        else:
            with os.fdopen(descriptor, "wb") as stream:
                stream.write(output[:32768] + b"\n[bounded diagnostic tail]\n" + output[-32768:])
        raise GateFailure("COMMAND_EXIT_UNEXPECTED")
    if contains:
        require(contains.encode() in output, "COMMAND_ORACLE_FAILED")
    return output


class GitHubFixture:
    def __init__(self, work, routes, archive, version):
        self.work = work
        self.routes, self.archive, self.version = routes, archive, version
        self.mode, self.denied, self.calls = "normal", 0, 0
        self.phase, self.diagnostic_blocks = "preparation", {}
        self.denials = []
        self.lock = threading.Lock()
        self.sha = digest(archive)
        self.tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        self.tls.load_cert_chain(work / "fixture.pem", work / "fixture.key")
        owner = self

        class Handler(http.server.BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, *_):
                pass

            def do_CONNECT(self):
                if self.path not in {host + ":443" for host in FIXTURE_HOSTS}:
                    owner.reject("connect_host", self.path)
                    self.send_error(403)
                    return
                self.tls_host = self.path[:-4]
                self.send_response(200)
                self.end_headers()
                self.connection = owner.tls.wrap_socket(self.connection, server_side=True)
                self.rfile = self.connection.makefile("rb")
                self.wfile = self.connection.makefile("wb")
                self.close_connection = False
                try:
                    while not self.close_connection:
                        self.handle_one_request()
                finally:
                    self.connection.close()

            def do_GET(self):
                host = getattr(self, "tls_host", "")
                if host not in FIXTURE_HOSTS or self.headers.get("Authorization") or self.headers.get("Cookie"):
                    owner.reject("request_identity", host, self.path)
                    self.send_error(403)
                    return
                url = "https://" + host + self.path
                latest = "https://api.github.com/repos/ivo-lopes/ivoai/releases/latest"
                version = "99.99.99" if owner.mode == "wrong_version" else owner.version
                base = "https://github.com/ivo-lopes/ivoai/releases/download/v" + version + "/"
                if url == latest:
                    body = json.dumps({"tag_name": "v" + version, "html_url": "https://github.com/ivo-lopes/ivoai/releases/tag/v" + version, "body": "Synthetic release metadata; real artifact bytes."}).encode()
                elif url == "https://api.github.com/repos/openai/codex/releases/latest":
                    body = b'{"tag_name":"rust-v0.162.1"}'
                elif url == base + "checksums.txt":
                    body = (owner.sha + "  " + owner.archive.name + "\n").encode()
                elif url == base + owner.archive.name:
                    body = owner.archive.read_bytes()
                    if owner.mode == "corrupt":
                        body = body[:-1] + bytes([body[-1] ^ 1])
                elif url in owner.routes:
                    body = owner.routes[url].read_bytes()
                else:
                    owner.reject("unmapped_route", host, self.path)
                    self.send_error(403)
                    return
                owner.calls += 1
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.send_header("Content-Type", "application/octet-stream")
                self.end_headers()
                self.wfile.write(body)

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True

    def reject(self, reason, host, path=""):
        # No request headers, query, credentials or arbitrary URL text persists.
        event = {"reason": reason, "host_sha256": hashlib.sha256(host.encode()).hexdigest(), "path_sha256": hashlib.sha256(path.encode()).hexdigest()}
        event["during_phase"] = self.phase
        diagnostic = reason == "connect_host" and host == "chatgpt.com:443" and self.phase in DIAGNOSTIC_PHASES
        if diagnostic:
            event["classification"] = "blocked_official_provider_diagnostic"
        public_host = host.removesuffix(":443")
        if public_host in FIXTURE_HOSTS:
            event["public_host"] = public_host
            if re.fullmatch(r"/(?:repos|dist|ivo-lopes|anthropics)/[a-zA-Z0-9/._-]{1,200}", path):
                event["public_path"] = path
        with self.lock:
            self.denied += 1
            if diagnostic:
                self.diagnostic_blocks[self.phase] = self.diagnostic_blocks.get(self.phase, 0) + 1
            if len(self.denials) < 16:
                self.denials.append(event)
            (self.work / "fixture-denials.json").write_text(json.dumps(self.denials))

    @property
    def unexpected_requests(self):
        return self.denied - sum(self.diagnostic_blocks.values())

    def __enter__(self):
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        return self

    def __exit__(self, *_):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def env(self, values, work):
        result = dict(values)
        result.update(HTTPS_PROXY="http://127.0.0.1:" + str(self.server.server_port), HTTP_PROXY="http://127.0.0.1:" + str(self.server.server_port), NO_PROXY="localhost,127.0.0.1", SSL_CERT_FILE=str(work / "fixture.pem"), SSL_CERT_DIR=str(work / "empty-cert-dir"))
        return result


def tree_hash(root):
    h = hashlib.sha256()
    for path in sorted(root.rglob("*")):
        relative = str(path.relative_to(root)).encode()
        if path.is_symlink():
            value = os.readlink(path).encode()
        elif path.is_file():
            value = digest(path).encode()
        else:
            continue
        h.update(relative + b"\0" + value + b"\0")
    return h.hexdigest()


def snapshot(work):
    paths = [work / "install/ivoai", work / "config/ivoai/config.toml", work / "config/ivoai/secrets.json", work / "state/ivoai/ownership.toml", work / "state/ivoai/state.toml"]
    return {str(path.relative_to(work)): (digest(path), path.stat().st_mode & 0o777) for path in paths}


def assert_markers(work):
    for relative in LEGACY_USER_MARKERS:
        target = work/"home"/relative
        require(target.read_bytes()==b"synthetic-user-owned-opencode-marker\n" and target.stat().st_mode&0o777==0o600,"RETAINED_OPENCODE_USER_DATA_CHANGED")
    config = tomllib.loads((work / "config/ivoai/config.toml").read_text())
    require(config["compatibility_fixture"]["future_field"] == "preserve-me", "CONFIG_MARKER_CHANGED")
    secret_path = work / "config/ivoai/secrets.json"
    require(secret_path.stat().st_mode & 0o777 == 0o600, "SECRET_PERMISSIONS_CHANGED")
    secrets = json.loads(secret_path.read_text())
    require(secrets["servers"]["upgrade_fixture"]["token"] == "SYNTHETIC-UPGRADE-MARKER", "SECRET_MARKER_CHANGED")
    ownership = tomllib.loads((work / "state/ivoai/ownership.toml").read_text())
    item = ownership["components"]["ivoai"]
    require(item["managed"] and item["path"] == str(work / "install/ivoai"), "CORE_OWNERSHIP_CHANGED")
    for folder in ["provider-codex", "provider-claude", "provider-opencode"]:
        require((work / folder / "upgrade-marker").read_text() == "synthetic-provider-owned-marker\n", "PROVIDER_MARKER_CHANGED")


def assert_code_identity(core, env, expected_sha256):
    report = json.loads(run([core, "doctor", "--json"], env, timeout=180))
    code = report["code_intelligence"]
    require(code["integrity"] == "verified", "GRAFT_PROVENANCE_NOT_VERIFIED")
    require(code["artifact_sha256"] == expected_sha256, "GRAFT_ARTIFACT_HASH_MISMATCH")


def inside(work, manifest):
    def checkpoint(phase):
        fixture.phase = phase
        (work / "inside-phase.json").write_text(json.dumps({"phase": phase}))
    values = environment(work)
    core = work / "install/ivoai"
    archive = Path(manifest["archive"])
    routes = {url: Path(path) for url, path in manifest["routes"].items()}
    with GitHubFixture(work, routes, archive, manifest["version"]) as fixture:
        env = fixture.env(values, work)
        original = snapshot(work)
        for mode, reason in [("corrupt", "checksum mismatch"), ("wrong_version", "probe failed")]:
            checkpoint("reject_" + mode)
            fixture.mode = mode
            run([core, "update", "--dry-run"], env, expected=1, contains=reason)
            require(snapshot(work) == original, "REJECTED_UPDATE_CHANGED_INSTALLATION")
        fixture.mode = "normal"
        config = work / "config/ivoai/config.toml"
        saved = config.read_bytes()
        config.write_bytes(saved.replace(b"version = 1", b"version = 999", 1))
        incompatible = snapshot(work)
        checkpoint("reject_incompatible_schema")
        run([core, "update", "--dry-run"], env, expected=1, contains="schema 999")
        require(snapshot(work) == incompatible, "INCOMPATIBLE_UPDATE_CHANGED_INSTALLATION")
        config.write_bytes(saved)
        checkpoint("read_only_dry_run")
        run([core, "update", "--dry-run"], env, contains="compatible: no managed changes")
        require(snapshot(work) == original, "DRY_RUN_CHANGED_INSTALLATION")
        profiles = sorted((work / "data/ivoai/components").glob("ruflo-runtime-*"))
        require(len(profiles) == 1, "BASELINE_PROFILE_NOT_PROVEN")
        old_profile, old_profile_hash = profiles[0], tree_hash(profiles[0])
        checkpoint("real_cli_upgrade_migrate_setup_doctor")
        run([core, "update"], env, timeout=900, contains="update complete; transaction")
        require(digest(core) == manifest["candidate_binary_sha256"], "PROMOTED_BINARY_MISMATCH")
        run([core, "version"], env, contains=manifest["version"])
        assert_markers(work)
        require(tree_hash(old_profile) == old_profile_hash, "OLD_PROFILE_CHANGED")
        expected_profiles = {old_profile.name, manifest["candidate_profile"]}
        actual_profiles = {path.name for path in (work / "data/ivoai/components").glob("ruflo-runtime-*")}
        require(actual_profiles == expected_profiles, "CANDIDATE_PROFILE_NOT_INSTALLED")
        require(digest(work / "state/ivoai/updates/ivoai.previous") == manifest["baseline_binary_sha256"], "PREVIOUS_BINARY_NOT_PRESERVED")
        checkpoint("verify_upgraded_graft_provenance")
        assert_code_identity(core, env, manifest["candidate_graft_sha256"])
        after = snapshot(work)
        checkpoint("current_update_idempotent")
        run([core, "update"], env, contains="is current")
        require(snapshot(work) == after, "CURRENT_UPDATE_NOT_IDEMPOTENT")
        checkpoint("real_cli_rollback")
        run([core, "update", "--rollback"], env, timeout=900, contains="rollback complete")
        require(digest(core) == manifest["baseline_binary_sha256"], "BASELINE_BINARY_NOT_RESTORED")
        assert_markers(work)
        require(tree_hash(old_profile) == old_profile_hash, "ROLLBACK_CHANGED_OLD_PROFILE")
        run([core, "version"], env, contains=BASELINE)
        checkpoint("verify_rolled_back_graft_provenance")
        assert_code_identity(core, env, manifest["baseline_graft_sha256"])
        checkpoint("real_cli_update_after_rollback")
        run([core, "update"], env, timeout=900, contains="update complete; transaction")
        require(digest(core) == manifest["candidate_binary_sha256"], "REUPDATE_BINARY_MISMATCH")
        assert_markers(work)
        require(tree_hash(old_profile) == old_profile_hash, "REUPDATE_CHANGED_OLD_PROFILE")
        require(fixture.unexpected_requests == 0, "UNEXPECTED_NETWORK_REQUEST")
        return {"passed": ["corrupt_checksum_refused", "wrong_version_refused", "incompatible_schema_refused", "read_only_dry_run", "real_cli_upgrade_migrate_setup_doctor", "graft_provenance_after_upgrade_and_rollback", "current_update_idempotent", "real_cli_rollback", "real_cli_update_after_rollback", "old_binary_and_profile_preserved", "synthetic_credentials_and_ownership_preserved"], "fixture_requests": fixture.calls, "denied_requests": fixture.denied, "blocked_provider_diagnostics": {"count": sum(fixture.diagnostic_blocks.values()), "by_phase": fixture.diagnostic_blocks}, "unexpected_requests": fixture.unexpected_requests, "external_network_during_upgrade": False, "provider_catalog_and_quota_certified": False}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline-core", type=Path)
    parser.add_argument("--candidate-dir", type=Path)
    parser.add_argument("--version", default="0.17.0")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--inside", type=Path, help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    if args.inside:
        manifest = json.loads((args.inside / "manifest.json").read_text())
        try:
            result = inside(args.inside, manifest)
        except GateFailure as error:
            detail = {"outcome": "FAIL", "failure": str(error), "command": LAST_COMMAND}
            for filename, field in [("inside-phase.json", "scenario"), ("fixture-denials.json", "fixture_denials")]:
                if (args.inside / filename).is_file():
                    detail[field] = json.loads((args.inside / filename).read_text())
            (args.inside / "result.json").write_text(json.dumps(detail))
            raise
        (args.inside / "result.json").write_text(json.dumps(result))
        return
    require(all([args.baseline_core, args.candidate_dir, args.output]), "REQUIRED_ARGUMENT_MISSING")
    require(platform.system() == "Linux", "NATIVE_LINUX_REQUIRED")
    arch = {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine())
    require(arch is not None, "NATIVE_ARCHITECTURE_REQUIRED")
    require(re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", args.version), "STABLE_VERSION_REQUIRED")
    require(args.version == QUALIFIED_CANDIDATE, "UNREVIEWED_BASELINE_CANDIDATE_PAIR")
    candidate_dir = args.candidate_dir.resolve()
    expected_files = [f"ivoai_linux_{arch}.tar.gz"]
    for component in ["graft", "caveman"]:
        expected_files += [f"ivoai-{component}_linux_{arch}.tar.gz", (f"ivoai-caveman_linux_{arch}.metadata.json" if component == "caveman" else f"managed-{component}-{arch}.json")]
    require(all((candidate_dir / name).is_file() and not (candidate_dir / name).is_symlink() for name in expected_files), "CANDIDATE_ARTIFACT_SET_INCOMPLETE")
    require(not any("opencode" in p.name for p in candidate_dir.iterdir()), "LEGACY_CANDIDATE_ASSETS_FORBIDDEN")
    require(not args.output.exists(), "OUTPUT_MUST_BE_NEW")
    args.output.mkdir(mode=0o700, parents=True)
    receipt = {"schema_version": 1, "outcome": "FAIL", "baseline": BASELINE, "candidate": args.version, "architecture": arch, "test_mode": False, "real_cli_binaries": True, "provider_inference": False, "personal_credentials_inherited": False}
    start = time.monotonic()
    repo = Path(__file__).resolve().parents[1]
    work = Path(tempfile.mkdtemp(prefix="ivoai-stable-upgrade-", dir="/var/tmp"))
    def checkpoint(phase):
        receipt["phase"] = phase
        receipt["wall_seconds"] = round(time.monotonic() - start, 3)
        (args.output / "qualification.json").write_text(json.dumps(receipt, indent=2) + "\n")
    try:
        checkpoint("prepare")
        values = environment(work)
        require(args.baseline_core.is_file() and not args.baseline_core.is_symlink() and digest(args.baseline_core) == BASELINE_BINARIES[arch], "PUBLISHED_BASELINE_INTEGRITY_FAILED")
        require(all(shutil.which(tool) for tool in ["go", "openssl", "bwrap"]), "GATE_TOOL_MISSING")
        (work / "install").mkdir(mode=0o700)
        core = work / "install/ivoai"
        shutil.copyfile(args.baseline_core, core)
        core.chmod(0o700)
        require(digest(core) == BASELINE_BINARIES[arch], "COPIED_BASELINE_INTEGRITY_FAILED")
        run([core, "version"], values, contains=BASELINE)
        helper = work / "fixture-helper"
        go_env = dict(values, GOTOOLCHAIN="local", GOCACHE=os.environ.get("GOCACHE", str(Path.home() / ".cache/go-build")), GOPATH=os.environ.get("GOPATH", str(Path.home() / "go")))
        checkpoint("fixture_helper_compile")
        run([shutil.which("go"), "build", "-o", helper, "./scripts/stable-release-upgrade-fixtures"], go_env, timeout=180, cwd=repo)
        run([helper, "initialize"], values)
        run([core, "_register-install"], values)
        config = work / "config/ivoai/config.toml"
        with config.open("a") as stream:
            stream.write('\n[compatibility_fixture]\nfuture_field = "preserve-me"\n')
        for folder in ["provider-codex", "provider-claude", "provider-opencode"]:
            (work / folder).mkdir(mode=0o700,exist_ok=True)
            (work / folder / "upgrade-marker").write_text("synthetic-provider-owned-marker\n")
        for relative in LEGACY_USER_MARKERS:
            target = work / "home" / relative
            target.parent.mkdir(mode=0o700,parents=True,exist_ok=True)
            target.write_bytes(b"synthetic-user-owned-opencode-marker\n")
            target.chmod(0o600)
        downloads = work / "downloads"
        downloads.mkdir(mode=0o700)
        routes = {}
        catalog = json.loads(run([helper, "catalog"], values))
        bootstrap = json.loads(run([helper, "bootstrap"], values))
        checkpoint("reviewed_asset_preparation")
        for asset in catalog + baseline_setup_assets(repo, arch) + bootstrap["assets"]:
            if asset["URL"] in routes:
                require(digest(routes[asset["URL"]]) == asset["SHA256"], "CONFLICTING_BOOTSTRAP_PIN")
            else:
                routes[asset["URL"]] = verified_download(asset["URL"], asset["SHA256"], downloads)
        for url, sha in BOOTSTRAP[arch]:
            routes[url] = verified_download(url, sha, downloads)
        old_meta_url = f"https://github.com/ivo-lopes/ivoai/releases/download/v{BASELINE}/managed-graft-{arch}.json"
        old_meta = json.loads(verified_download(old_meta_url, BASELINE_METADATA[arch], downloads).read_text())
        old_graft_url = f"https://github.com/ivo-lopes/ivoai/releases/download/v{BASELINE}/" + old_meta["archive"]
        routes[old_graft_url] = verified_download(old_graft_url, old_meta["sha256"], downloads)
        archive = candidate_dir / f"ivoai_linux_{arch}.tar.gz"
        candidate_binary = work / "candidate-core"
        extract_core(archive, candidate_binary)
        run([candidate_binary, "version"], values, contains=args.version)
        routes[f"https://github.com/ivo-lopes/ivoai/releases/download/v{args.version}/" + archive.name] = archive
        candidate_graft_sha256 = ""
        for component in ["graft", "caveman"]:
            metadata_name = f"ivoai-caveman_linux_{arch}.metadata.json" if component == "caveman" else f"managed-{component}-{arch}.json"
            metadata = json.loads((candidate_dir / metadata_name).read_text())
            require(metadata["archive"] == f"ivoai-{component}_linux_{arch}.tar.gz", "CANDIDATE_COMPONENT_NAME_DENIED")
            require(re.fullmatch(r"[a-f0-9]{64}", metadata["sha256"]), "CANDIDATE_COMPONENT_HASH_DENIED")
            payload = candidate_dir / metadata["archive"]
            require(payload.is_file() and digest(payload) == metadata["sha256"] and metadata["platform"] == "linux/" + arch, "CANDIDATE_COMPONENT_INTEGRITY_FAILED")
            routes[f"https://github.com/ivo-lopes/ivoai/releases/download/v{args.version}/" + payload.name] = payload
            if component == "graft":
                candidate_graft_sha256 = metadata["sha256"]
        (work / "empty-cert-dir").mkdir()
        run(["/usr/bin/openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", work / "fixture.key", "-out", work / "fixture.pem", "-days", "1", "-subj", "/CN=IVOAI isolated upgrade fixture", "-addext", "basicConstraints=critical,CA:TRUE", "-addext", "subjectAltName=DNS:github.com,DNS:api.github.com,DNS:nodejs.org"], values)
        with GitHubFixture(work, routes, archive, args.version) as fixture:
            # Preparation alone may fetch the fixed Headroom Python graph and
            # pinned npm packages. Its HOME/cache are still wholly disposable.
            checkpoint("real_baseline_component_setup")
            run([core, "setup"], fixture.env(values, work), timeout=1200)
            node = work / "data/ivoai/components/node-26.11.1/bin/node"
            npm = node.parent.parent / "lib/node_modules/npm/bin/npm-cli.js"
            stage = work / "candidate-npm-cache-prime"
            stage.mkdir()
            for name in ["package.json", "package-lock.json", "braces-3.0.4-ivoai.1.tgz", "ivoai-zod3-compat-3.25.76-ivoai.1.tgz"]:
                shutil.copyfile(repo / "internal/components/assets" / name, stage / name)
            npm_env = dict(values, NPM_CONFIG_CACHE=str(work / "cache/ivoai/npm"), RUFLO_PROVIDER="ivoai-disabled", CLAUDE_FLOW_MEMORY_BACKEND="memory")
            checkpoint("real_candidate_profile_cache_preparation")
            run([node, npm, "ci", "--ignore-scripts", "--no-audit", "--no-fund"], npm_env, timeout=600, cwd=stage)
            checkpoint("real_candidate_headroom_cache_preparation")
            prime_headroom_cache(work, repo, arch, values, catalog, bootstrap, routes)
            receipt["preparation_denials"] = fixture.denials
            require(fixture.denied == 0, "PREPARATION_REQUEST_NOT_ALLOWLISTED")
        profile = hashlib.sha256((stage / "package.json").read_bytes() + (stage / "package-lock.json").read_bytes()).hexdigest()
        manifest = {"archive": str(archive), "version": args.version, "routes": {url: str(path) for url, path in routes.items()}, "baseline_binary_sha256": BASELINE_BINARIES[arch], "candidate_binary_sha256": digest(candidate_binary), "candidate_profile": "ruflo-runtime-" + profile, "baseline_graft_sha256": old_meta["sha256"], "candidate_graft_sha256": candidate_graft_sha256}
        (work / "manifest.json").write_text(json.dumps(manifest))
        checkpoint("network_isolated_upgrade")
        command = ["bwrap", "--die-with-parent", "--new-session", "--unshare-user", "--unshare-net", "--unshare-pid", "--clearenv", "--ro-bind", "/usr", "/usr", "--ro-bind", "/bin", "/bin", "--ro-bind", "/lib", "/lib", "--ro-bind-try", "/lib64", "/lib64", "--dir", "/etc", "--ro-bind", "/etc/ssl", "/etc/ssl", "--ro-bind-try", "/etc/ld.so.cache", "/etc/ld.so.cache", "--ro-bind-try", "/etc/nsswitch.conf", "/etc/nsswitch.conf", "--ro-bind-try", "/etc/passwd", "/etc/passwd", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--bind", str(work), str(work), "--ro-bind", str(repo), str(repo), "--ro-bind", str(candidate_dir), str(candidate_dir), "--chdir", str(work), "--setenv", "PATH", "/usr/bin:/bin", "/usr/bin/python3", str(Path(__file__).resolve()), "--inside", str(work)]
        run(command, values, timeout=2400)
        receipt.update(json.loads((work / "result.json").read_text()), phase="completed", outcome="PASS")
    except (GateFailure, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        receipt["failure"] = str(error) if isinstance(error, GateFailure) else type(error).__name__
        if (work / "result.json").is_file():
            receipt["inner_result"] = json.loads((work / "result.json").read_text())
        raise
    finally:
        if (work / "private-command-failure.log").is_file():
            shutil.copyfile(work / "private-command-failure.log", args.output / "private-command-failure.log")
            (args.output / "private-command-failure.log").chmod(0o600)
        receipt["wall_seconds"] = round(time.monotonic() - start, 3)
        (args.output / "qualification.json").write_text(json.dumps(receipt, indent=2) + "\n")
        shutil.rmtree(work)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        # Detail is classified in the bounded receipt. Never print client or
        # package-manager diagnostics, private environment or fixture secrets.
        raise SystemExit("stable release upgrade qualification failed; inspect qualification.json")
