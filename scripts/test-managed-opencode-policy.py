#!/usr/bin/env python3
"""Real pinned executable policy contracts; run only inside the offline launcher."""
import base64
import contextlib
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


def run(binary, root):
    if os.environ.get("IVOAI_OPENCODE_PRIVATE_NETWORK") != "1":
        raise RuntimeError("offline launcher required")
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    outcomes = []
    for case in ("managed_local", "managed_package", "managed_remote", "managed_foreign_file", "managed_symlink", "upstream_local_control"):
        work = root / case
        work.mkdir(mode=0o700)
        home = work / "home"
        config_dir = home / "config" / "opencode"
        config_dir.mkdir(mode=0o700, parents=True)
        assets = work / "assets"
        assets.mkdir(mode=0o700)
        plugin = assets / "server-plugin.mjs"
        plugin.write_text("export const IVOAISessionBridge = async () => ({})\n")
        plugin.chmod(0o600)
        spec = plugin.as_uri()
        if case == "managed_package":
            spec = "@ivoai-fixture/must-never-resolve@1.0.0"
        elif case == "managed_remote":
            spec = "https://127.0.0.1:1/must-never-fetch.mjs"
        elif case == "managed_foreign_file":
            foreign = work / "foreign.mjs"
            foreign.write_text("throw new Error('MUST_NOT_EXECUTE')")
            spec = foreign.as_uri()
        elif case == "managed_symlink":
            target = work / "target.mjs"
            target.write_text("throw new Error('MUST_NOT_EXECUTE')")
            plugin.unlink()
            plugin.symlink_to(target)
        release = threading.Event()
        hits = []

        class Registry(BaseHTTPRequestHandler):
            def do_GET(self):
                hits.append(self.path)
                release.wait(10)
                self.send_response(404)
                self.send_header("Content-Type", "application/json")
                self.end_headers()
                with contextlib.suppress(BrokenPipeError):
                    self.wfile.write(b'{"error":"isolated fixture; installs forbidden"}')

            def log_message(self, *_):
                pass

        registry = ThreadingHTTPServer(("127.0.0.1", 0), Registry)
        threading.Thread(target=registry.serve_forever, daemon=True).start()
        (config_dir / ".npmrc").write_text(f"registry=http://127.0.0.1:{registry.server_port}\nfetch-retries=0\nfetch-timeout=12000\naudit=false\nfund=false\n")
        cfg = assets / "opencode.json"
        cfg.write_text(json.dumps({"autoupdate": False, "share": "disabled", "plugin": [spec], "enabled_providers": ["ivoai"], "model": "ivoai/auto", "provider": {"ivoai": {"npm": "@ai-sdk/openai-compatible", "options": {"baseURL": "http://127.0.0.1:1/v1", "apiKey": "disposable-fixture"}, "models": {"auto": {"name": "fixture"}}}}}))
        cfg.chmod(0o600)
        env = {"PATH": "/usr/bin:/bin", "HOME": str(home), "TMPDIR": "/tmp", "LANG": "C.UTF-8", "XDG_CONFIG_HOME": str(home / "config"), "XDG_DATA_HOME": str(home / "data"), "XDG_STATE_HOME": str(home / "state"), "XDG_CACHE_HOME": str(home / "cache"), "OPENCODE_CONFIG": str(cfg), "OPENCODE_DISABLE_PROJECT_CONFIG": "1", "OPENCODE_DISABLE_AUTOUPDATE": "1", "OPENCODE_DISABLE_MODELS_FETCH": "1", "OPENCODE_DISABLE_LSP_DOWNLOAD": "1", "OPENCODE_DISABLE_EXTERNAL_SKILLS": "1", "OPENCODE_DISABLE_DEFAULT_PLUGINS": "1", "OPENCODE_SERVER_USERNAME": "ivoai", "OPENCODE_SERVER_PASSWORD": "disposable-fixture"}
        if case != "upstream_local_control":
            env.update(IVOAI_MANAGED_LOCAL_PLUGINS="1", IVOAI_MANAGED_ASSETS_DIR=str(assets))
        process_log = work / "process.log"
        stream = process_log.open("wb")
        proc = subprocess.Popen([str(binary), "serve", "--hostname", "127.0.0.1", "--port", "0", "--print-logs"], cwd=work, env=env, stdout=subprocess.PIPE, stderr=stream, start_new_session=True)
        endpoint = ""
        ready = threading.Event()

        def read_listener():
            nonlocal endpoint
            for line in proc.stdout:
                stream.write(line)
                if line.startswith(b"opencode server listening on http://127.0.0.1:"):
                    endpoint = line.decode().strip().split(" on ", 1)[1]
                    ready.set()

        reader = threading.Thread(target=read_listener, daemon=True)
        reader.start()

        def request(route, timeout=2):
            req = urllib.request.Request(endpoint + route, headers={"Authorization": "Basic " + base64.b64encode(b"ivoai:disposable-fixture").decode()})
            try:
                with urllib.request.urlopen(req, timeout=timeout) as response:
                    return response.status, response.read(65536)
            except urllib.error.HTTPError as exc:
                return exc.code, exc.read(65536)

        started = time.monotonic()
        try:
            assert ready.wait(20), "listener did not become ready"
            assert request("/global/health")[0] == 200
            route = "/config/providers?directory=" + urllib.parse.quote(str(work))
            if case == "upstream_local_control":
                timed_out = False
                try:
                    request(route)
                except TimeoutError:
                    timed_out = True
                assert timed_out and len(hits) == 1, "unflagged control did not demonstrate one dependency wait"
                release.set()
                status, _ = request(route, 15)
                assert status == 200, "unflagged upstream behavior did not resume after registry failure"
            elif case == "managed_local":
                status, body = request(route)
                assert status == 200 and b'"ivoai"' in body, "managed local plugin/catalog did not load"
                time.sleep(.25)
                assert not hits, "managed runtime contacted package registry"
            else:
                status, body = request(route)
                stream.flush()
                assert status == 500 and b"IVOAI_MANAGED_PLUGIN_DENIED" in process_log.read_bytes(), f"unreviewed plugin refusal missing: status={status}"
                assert not hits, "refused plugin contacted package registry"
            assert not list(home.rglob("node_modules")), "runtime created an npm dependency tree"
            outcomes.append({"case": case, "status": "PASS", "registry_requests": len(hits), "elapsed_ms": round((time.monotonic() - started) * 1000), "provider_inference": False})
        finally:
            release.set()
            if proc.poll() is None:
                os.killpg(proc.pid, signal.SIGTERM)
                try:
                    proc.wait(3)
                except subprocess.TimeoutExpired:
                    os.killpg(proc.pid, signal.SIGKILL)
                    proc.wait()
            reader.join(2)
            registry.shutdown()
            registry.server_close()
            stream.close()
    return {"schema_version": 1, "scope": "real_pinned_binary_private_network_no_inference", "uid": os.geteuid(), "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(), "results": outcomes}


if __name__ == "__main__":
    print(json.dumps(run(Path(sys.argv[1]), Path(sys.argv[2])), indent=2))
