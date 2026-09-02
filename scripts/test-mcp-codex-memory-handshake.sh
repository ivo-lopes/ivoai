#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
go_binary="${GO_BINARY:-go}"

if [[ "${1:-}" != "--live" ]]; then
  cd "$repo_root"
  "$go_binary" test ./internal/knowledgerouter ./internal/connections \
    -run 'TestRouterForwardsStreamableHTTPNegotiationHeaders|TestRouterValidatesClientAcceptMediaRanges|TestRPCResponseContentTypes|TestProbeMCPUsesStreamableHTTPNegotiation' \
    -count=1
  printf 'MCP Codex memory handshake (hermetic): PASS\n'
  exit 0
fi

ivoai_binary="${IVOAI_BINARY:-ivoai}"
knowledge_source="${IVOAI_MCP_TEST_SOURCE:-default}"
timeout_seconds="${IVOAI_MCP_TEST_TIMEOUT:-180}"
case_root="$(mktemp -d "${TMPDIR:-/tmp}/ivoai-mcp-codex-memory.XXXXXXXX")"

cleanup() {
  rm -rf -- "$case_root"
}
trap cleanup EXIT

fail() {
  printf 'MCP Codex memory handshake (live): FAIL (%s)\n' "$1" >&2
  exit 1
}

command -v "$ivoai_binary" >/dev/null 2>&1 || [[ -x "$ivoai_binary" ]] || fail "ivoai executable unavailable"
command -v codex >/dev/null 2>&1 || fail "Codex executable unavailable"

set +e
timeout "${timeout_seconds}s" "$ivoai_binary" codex \
  --knowledge-source "$knowledge_source" -- \
  exec --ephemeral --json --skip-git-repo-check --approve-for-me \
  -C "$case_root" \
  'Use the ivoai-memory MCP server. Call exactly one read-only memory_status tool, then respond with exactly MCP_MEMORY_OK if and only if the tool succeeded. Do not call shell commands and do not write memory.' \
  >"$case_root/stdout.jsonl" 2>"$case_root/stderr.log"
exit_code=$?
set -e

[[ "$exit_code" -eq 0 ]] || fail "Codex exited with status $exit_code"
if grep -Eiq 'HTTP 406|upstream HTTP 406|-32022' "$case_root/stdout.jsonl" "$case_root/stderr.log"; then
  fail "HTTP 406 or JSON-RPC -32022 observed"
fi
if grep -Eiq 'MCP startup incomplete|failed to start.*ivoai-memory' "$case_root/stdout.jsonl" "$case_root/stderr.log"; then
  fail "ivoai-memory startup warning observed"
fi
grep -F '"type":"mcp_tool_call"' "$case_root/stdout.jsonl" \
  | grep -F '"server":"ivoai-memory"' \
  | grep -F '"tool":"memory_status"' \
  | grep -Fq '"status":"completed"' \
  || fail "memory_status did not complete"
grep -Fq '"text":"MCP_MEMORY_OK"' "$case_root/stdout.jsonl" \
  || fail "Codex did not confirm the Memory smoke"

printf 'CODEX_IVOAI_MEMORY_MCP=PASS\n'
printf 'MCP_INITIALIZE=PASS\n'
printf 'MCP_TOOLS_DISCOVERY=PASS\n'
printf 'MCP_MEMORY_SMOKE=PASS\n'
printf 'UPSTREAM_HTTP_406_COUNT=0\n'
printf 'CODEX_STARTUP_MCP_WARNING_COUNT=0\n'
