# Remote MCP for ChatGPT and Claude

Validated against official product documentation on **2026-09-03**. Product UI,
plan availability, and administrative policy may change; recheck the linked official
pages before an organizational rollout.

IVOAI exposes a unified remote Streamable HTTP MCP endpoint at:

```text
https://ai.example.com/mcp
```

The endpoint supports MCP initialize, tool discovery, JSON or SSE responses, and
OAuth 2.1 authorization with PKCE. Do not put tokens in the URL. Create a bounded,
one-time browser activation code on the server:

```bash
sudo ivoai server web-access create --ttl 10m
```

## ChatGPT Web

OpenAI currently documents full MCP apps and developer mode for Business and
Enterprise/Edu workspaces; Pro can connect read/fetch MCPs in developer mode. An
admin enables developer mode, then an authorized user uses **Settings → Apps →
Create**, provides the HTTPS MCP endpoint and authentication, selects **Scan Tools**,
completes OAuth, and creates the draft app. When IVOAI's authorization page opens,
review the requested scopes and enter the one-time activation code there. Select the
app from the chat tools menu for the message that needs it. Admins/owners publish
reviewed apps for a workspace.

ChatGPT connects to remote MCP servers. For a private/on-prem server that should not
be publicly exposed, use OpenAI's supported Secure MCP Tunnel rather than an ad-hoc
public bypass. See the [official OpenAI developer-mode and MCP app guide](https://help.openai.com/en/articles/12584461-developer-mode-and-full-mcp-connectors-in-chatgpt).

To revoke access, disconnect/remove the app in ChatGPT and revoke the corresponding
IVOAI grant:

```bash
sudo ivoai server web-access list
sudo ivoai server web-access revoke <id>
```

## Claude Web

Anthropic currently documents custom remote MCP connectors for Free, Pro, Max, Team,
and Enterprise; Free is limited to one. For Team/Enterprise, an Owner or Primary
Owner adds the remote URL under **Organization settings → Connectors → Add → Custom
→ Web**. Members then use **Customize → Connectors** and select **Connect**. For an
individual plan, use **Customize → Connectors → + → Add custom connector**. Complete
OAuth by reviewing the requested scopes and entering the one-time IVOAI activation
code, then enable or disable the connector per conversation from the `+` menu.

Remote connector traffic originates from Anthropic's cloud. The endpoint therefore
must be reachable from the documented network ranges, or the firewall must allow the
current Anthropic IP ranges. See the [official Anthropic custom connector guide](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp).

Remove the connector under **Customize → Connectors**, and revoke its IVOAI grant.
Do not configure a shared static bearer or provider API key.

## Nginx Proxy Manager

The external reverse proxy terminates TLS and forwards the public `/mcp` endpoint to
the IVOAI gateway according to [Server](server.md). The documentation site is a
separate HTTP listener:

```text
Public docs host: docs.example.com
Forward scheme:  http
Forward host:    IVOAI_SERVER_LAN_IP
Forward port:    7780
WebSockets:      off
TLS:             terminated by Nginx Proxy Manager
```

Restrict the docs port at the firewall to the proxy network/IP when possible.

## Conformance and troubleshooting

MCP POST requests use `Content-Type: application/json` and accept both
`application/json` and `text/event-stream`. IVOAI parses media ranges, whitespace,
q-values and valid content-type parameters rather than comparing literal strings.
An HTTP 406 means the peer rejected content negotiation; confirm the exact `/mcp`
endpoint, current client version, reverse-proxy header forwarding, and the dual
`Accept` values. Never include `Authorization` values in diagnostics.

Run `sudo ivoai server doctor` and a safe read-only tool scan. Expected gates are
initialize success, tools/list success, and a safe `context_health` or `memory_status`
call. A 401 means authentication is missing. Scope denial on `/mcp` is returned as a
standard MCP tool error; the enrolled client API uses HTTP 403 for an authenticated
credential with insufficient scope. A 406 means transport negotiation is
incompatible.
