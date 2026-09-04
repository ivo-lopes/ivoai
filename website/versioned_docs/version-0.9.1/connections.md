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

Interactive connection asks for a base HTTPS URL and enrollment code. Named profiles
keep independent purposes and credentials. Automation can provide the URL by flag
and the code through standard input, avoiding shell history. For example:

```sh
printf '%s\n' "$IVOAI_ENROLLMENT_CODE" | ivoai connect server add mindsite \
  --url https://ai.example.com --purpose mindsite --code-stdin
ivoai connect server list
ivoai connect server show mindsite
ivoai connect server test mindsite
```

The legacy `ivoai connect server --url ... --code-stdin` remains supported and maps
to the `default` profile. `ivoai disconnect server <alias>` removes only that
profile and credential; `ivoai disconnect server --all` is the explicit bulk form.

`--enrollment-code` is also supported for constrained automation, but standard input
is preferred because command arguments may be visible in process listings and shell
history.

The client:

1. validates the URL and TLS certificate;
2. reads `/.well-known/ivoai` and checks protocol 1, health, and feature endpoints;
3. consumes the one-time enrollment code;
4. saves the client-scoped secret with mode `0600`, keyed by an opaque server ID,
   and records the profile without globally exposing that upstream;
5. probes the context MCP and any advertised memory MCP with the new credential,
   reporting failures as warnings without discarding the consumed enrollment;
6. configures generic ai-memory hooks best-effort; each managed session later binds
   them to its private loopback knowledge router.

Current clients carry the one-time code in the `Authorization` header using the
ivoai enrollment scheme; the JSON body contains only client identity and requested
scopes. The gateway accepts the legacy JSON field for rolling upgrades, but rejects
requests that ambiguously provide both transports.

HTTP is rejected for non-loopback servers. Reconnecting one alias leaves all other
profiles untouched, and a failed enrollment cannot remove an existing profile.
See [Multi-server knowledge sources](multi-server.md) for purpose isolation,
explicit federation, redundancy and concurrent sessions.

## MCP registry

Additional user HTTP MCPs remain in the shared internal registry. IVOAI server
Memory/Context are different: selected sources are rendered through a per-session
loopback router, so connecting several upstreams does not expose all of them to every
agent. The following commands manage additional registry entries:

```sh
ivoai connect mcp list
ivoai connect mcp add example https://mcp.example.com
ivoai connect mcp remove example
```

These commands manage ivoai's registry; agent-specific rendering remains an edge
adapter rather than a separate source of truth.

## ChatGPT Web and Claude Web

Web products connect directly to the server's unified remote MCP; they do not use the
desktop enrollment credential. Prerequisites are a publicly reachable HTTPS origin,
a passing `ivoai server doctor`, and reverse-proxy access to the OAuth and `/mcp`
routes.

Create a short-lived browser activation code on the server:

```sh
sudo ivoai server web-access create --ttl 10m
```

In ChatGPT Web, enable developer mode for custom connectors when required by the
workspace, add a connector, and enter `https://ai.example.com/mcp`. In Claude Web,
add a custom connector with the same URL. Both services discover ivoai's OAuth 2.1
metadata and open the browser authorization flow. Review the requested scopes and
enter the activation code there; do not put it in the connector URL or a custom
header.

The connector may request these scopes:

| Scope | Capability |
| --- | --- |
| `context:read` | Search and read untrusted indexed context |
| `memory:read` | Query, list, and read memory pages |
| `memory:write` | Write pages and submit memory feedback |
| `memory:delete` | Delete a confirmed normalized page path |

The default activation code permits the three non-destructive scopes. Request
`memory:delete` explicitly only when deletion is needed, or generate a narrower
grant for read-only use. Access and refresh tokens remain owned by the Web connector;
ivoai stores only token hashes and revocation metadata.

ChatGPT-compatible MCP discovery advertises the bundled
`ivoai-memory-context` skill. For Claude Web, download
`ivoai-memory-context.zip` from the matching ivoai release and import it as a custom
Skill. The instructions ask the model to check ivoai before project-dependent
answers and before every web/external research operation, in the order memory →
Context → web. MCP initialization and tool descriptions advertise the same order.
No remote MCP or skill format can guarantee a tool call on every model turn because
the Web product retains final tool-selection control.

Provider references: [connect an MCP server to ChatGPT](https://developers.openai.com/plugins/deploy/connect-chatgpt),
[use custom connectors in Claude](https://support.claude.com/en/articles/11176164-use-connectors-to-extend-claude-s-capabilities),
and [Claude custom Skills](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview).
