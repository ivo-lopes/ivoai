# Connections

All connection changes are performed by ivoai commands; manual edits to agent config,
MCP files, hooks, shell profiles, or token files are not required.

## ChatGPT / Codex

`ivoai connect chatgpt` verifies Codex, runs `codex login status`, and invokes the
official `codex login` browser flow only when needed. A final official status check is
required before ivoai records the connection. ivoai does not read or copy Codex token
files. `ivoai disconnect chatgpt` changes ivoai state but deliberately does not log
the user out of Codex.

## Claude / Claude Code

`ivoai connect claude` uses `claude auth status` and the official
`claude auth login` flow. Claude Pro, Max, Team, and Enterprise subscription login is
supported; an Anthropic API key is not the default. Credentials remain under Claude
Code's control. Disconnecting ivoai does not remove the official client login.

## ivoai server

Interactive connection asks for a base HTTPS URL and enrollment code. Automation can
provide the URL by flag and the code through standard input, avoiding shell history.
For example:

```sh
printf '%s\n' "$IVOAI_ENROLLMENT_CODE" | \
  ivoai connect server --url https://ai.example.com --code-stdin
```

`--enrollment-code` is also supported for constrained automation, but standard input
is preferred because command arguments may be visible in process listings and shell
history.

The client:

1. validates the URL and TLS certificate;
2. reads `/.well-known/ivoai` and checks protocol 1, health, and feature endpoints;
3. consumes the one-time enrollment code;
4. saves the client-scoped secret with mode `0600`, records the connection, and
   updates the internal MCP registry;
5. probes the context MCP and any advertised memory MCP with the new credential,
   reporting failures as warnings without discarding the consumed enrollment;
6. configures ai-memory hooks best-effort, reporting a degraded integration without
   discarding the valid server connection.

HTTP is rejected for non-loopback servers. Disconnect deletes only ivoai's scoped
credential and managed registry entries.

## MCP registry

Context and memory are modeled as entries in a shared internal MCP registry rather
than embedded per-agent special cases. The following commands manage additional
HTTP MCP registry entries without editing configuration files:

```sh
ivoai connect mcp list
ivoai connect mcp add example https://mcp.example.com
ivoai connect mcp remove example
```

These commands manage ivoai's registry; agent-specific rendering remains an edge
adapter rather than a separate source of truth.
