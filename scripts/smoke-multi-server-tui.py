#!/usr/bin/env python3
"""Credential-free public-binary TUI smoke. Never uses operator connections."""
import argparse
import fcntl
import http.server
import json
import os
import pathlib
import pty
import select
import struct
import subprocess
import tempfile
import termios
import threading
import time

CODE = "ivoai-enroll_0123456789abcdef_fixture-not-a-real-code"


def fixture(token, reject=False):
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_GET(self):
            if self.path == "/.well-known/ivoai":
                self.reply({"protocol_version": 1, "health_endpoint": "/health", "ready_endpoint": "/ready",
                            "context_mcp_endpoint": "/context", "memory_mcp_endpoint": "/memory",
                            "memory_hooks_endpoint": "/hooks", "enrollment_endpoint": "/enroll",
                            "features": {"context": True, "memory": True}})
            else:
                self.reply({"status": "healthy" if self.path == "/health" else "ready"})

        def do_POST(self):
            if self.path == "/enroll":
                if reject or self.headers.get("Authorization") != "Ivoai-Enrollment " + CODE:
                    self.reply({}, 401)
                else:
                    self.reply({"token": token, "client_id": "fixture", "scopes": ["memory:read", "context:read"]}, 201)
            elif self.headers.get("Authorization") == "Bearer " + token:
                self.reply({"jsonrpc": "2.0", "id": 1, "result": {"tools": []}})
            else:
                self.server.crossover = True
                self.reply({}, 401)

        def reply(self, value, status=200):
            body = json.dumps(value).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.crossover = False
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server, f"http://127.0.0.1:{server.server_port}"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary")
    args = parser.parse_args()
    binary = str(pathlib.Path(args.binary).resolve(strict=True))
    fixtures = [fixture("fixture-private-a"), fixture("fixture-private-b"), fixture("fixture-private-c", True)]
    try:
        with tempfile.TemporaryDirectory(prefix="ivoai-multiserver-smoke-") as directory:
            root = pathlib.Path(directory)
            env = {**os.environ, "HOME": str(root), "IVOAI_TEST_MODE": "1", "NO_COLOR": "1", "TERM": "dumb"}
            for kind in ("CONFIG", "DATA", "STATE", "CACHE"):
                env[f"XDG_{kind}_HOME"] = str(root / kind.lower())
            transcript = []

            def run(command, stdin="", expect=0):
                result = subprocess.run([binary, *command], input=stdin, text=True, capture_output=True, env=env, cwd=root, timeout=45)
                transcript.append(result.stdout + result.stderr)
                assert (result.returncode == 0) == (expect == 0), "unexpected command result (raw output withheld)"
                return result.stdout

            def menu(actions):
                return run([], "4\n6\n" + actions + "0\n0\n0\n")

            def add(alias, url):
                return f"2\n{alias}\n{alias}\n{url}\n{CODE}\n"

            def profiles():
                return {p["alias"]: p for p in json.loads(run(["connect", "server", "list", "--json"]))}

            run(["config", "set", "memory.enabled", "false"])
            assert profiles() == {}
            menu(add("company-a", fixtures[0][1]) + add("company-b", fixtures[1][1]))
            before = profiles()
            assert set(before) == {"company-a", "company-b"}
            assert before["company-a"]["server_id"] != before["company-b"]["server_id"]
            menu("3\n1\n0\n4\n1\n0\n")
            menu("3\n2\n0\n")
            assert not profiles()["company-a"]["enabled"] and profiles()["company-b"] == before["company-b"]
            menu("3\n2\n0\n4\n5\nREMOVE company-b\n")
            assert profiles() == {"company-a": before["company-a"]}
            menu(add("company-b", fixtures[1][1]))
            stable = profiles()
            secret_path = root / "config/ivoai/secrets.json"
            secrets_before = secret_path.read_bytes()
            menu(add("company-c", fixtures[2][1]))
            menu("2\ncompany-a\n")
            assert profiles() == stable and secret_path.read_bytes() == secrets_before
            menu(add("default", fixtures[0][1]))
            assert set(profiles()) == {"company-a", "company-b", "default"}
            assert secret_path.stat().st_mode & 0o777 == 0o600

            # Real keyboard/hidden-input surface: add one profile through a PTY.
            master, slave = pty.openpty()
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
            process = subprocess.Popen([binary], stdin=slave, stdout=slave, stderr=slave,
                                       env={**env, "TERM": "xterm-256color"}, cwd=root, start_new_session=True)
            os.close(slave)
            pending = bytearray()
            captured = bytearray()

            def wait_for(marker):
                deadline = time.monotonic() + 15
                while marker not in pending:
                    assert time.monotonic() < deadline, "PTY landmark timeout: " + marker.decode() + " (raw output withheld)"
                    ready, _, _ = select.select([master], [], [], 0.2)
                    if ready:
                        chunk = os.read(master, 65536)
                        pending.extend(chunk)
                        captured.extend(chunk)
                pending.clear()

            try:
                wait_for(b"Personal AI runtime")
                os.write(master, b"jjj\r")
                wait_for(b"\r\nConnections\r\n")
                os.write(master, b"jjj\r")
                wait_for(b"\r\nIVOAI Servers\r\n")
                os.write(master, b"j\r")
                for marker, answer in [(b"Server alias:", b"lab"), (b"Knowledge purpose", b"personal"),
                                       (b"Server URL:", fixtures[0][1].encode()), (b"Enrollment code (hidden):", CODE.encode())]:
                    wait_for(marker)
                    os.write(master, answer + b"\n")
                wait_for(b"Press Enter")
                os.write(master, b"\r")
                wait_for(b"\r\nIVOAI Servers\r\n")
                os.write(master, b"q")
                wait_for(b"Press Enter")
                os.write(master, b"\r")
                wait_for(b"\r\nConnections\r\n")
                os.write(master, b"q")
                wait_for(b"Personal AI runtime")
                os.write(master, b"q")
                assert process.wait(timeout=10) == 0
            finally:
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=3)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=3)
                os.close(master)
            transcript.append(captured.decode(errors="replace"))
            assert profiles()["lab"]["purpose"] == "personal"
            public = "\n".join(transcript) + (root / "config/ivoai/config.toml").read_text()
            for secret in (CODE, "fixture-private-a", "fixture-private-b", "fixture-private-c"):
                assert secret not in public, "secret exposed (value withheld)"
            assert not any(s.crossover for s, _ in fixtures), "credential crossover"
            print("MULTISERVER_TUI_SMOKE=PASS\nPERSISTENCE=PASS\nFAILED_ENROLLMENT_NO_OVERWRITE=PASS\nSELECTIVE_REMOVE=PASS\nLEGACY_DEFAULT=PASS\nPTY_HIDDEN_INPUT=PASS\nSECRET_STORE_MODE=0600\nSECRET_LEAK_COUNT=0\nTOKEN_CROSSOVER_COUNT=0\nPROFILE_OVERWRITE_COUNT=0")
    finally:
        for server, _ in fixtures:
            server.shutdown()
            server.server_close()


if __name__ == "__main__":
    main()
