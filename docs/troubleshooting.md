# Troubleshooting

## Update was interrupted or failed

Do not delete the update directory or reinstall over it. Re-run `ivoai update` (or
`sudo ivoai update` on a server); the updater detects an active private journal and
restores the pre-update snapshot before attempting a new candidate. To explicitly
return to the last compatible transaction, run:

```sh
ivoai update --rollback
```

For server installations use `sudo`. Run the matching Doctor afterward. A message
about a corrupted journal is a fail-closed condition: preserve the update directory
for diagnosis and collect `ivoai doctor --inventory --json`; do not hand-edit the
journal or provider authentication files. Candidate compatibility can be checked
without committing managed changes using `ivoai update --dry-run`. The command
stages and executes the checksum-verified candidate for bounded compatibility
probes, so use the same release-channel trust decision as a real update.

## Automatic mode does not start the selected client

Run `ivoai doctor` and inspect the **Automatic Orchestration** section. A provider
must use first-party subscription authentication; PAYG API-key sessions are not an
automatic-routing fallback. `ivoai status` intentionally reads cached quota only,
while Doctor performs the active probes.

If Codex shows a probe error, confirm that the managed client supports
`codex app-server --stdio` and that `codex login status` succeeds. If Claude is
authenticated but 5-hour/weekly quota says `awaiting first response`, start one
automatic Claude conversation and complete a turn so its official statusline can
publish telemetry. `N/A / not exposed` means a structured response was observed but
omitted that field; `stale` means the displayed percentage is an older observation.
Claude monthly is not assumed.

Codex quota rows are classified by the duration returned by the official app-server.
`Codex 5h: N/A / not exposed` is not an error and does not block routing. If quota
from a previous account remains visible after a login performed directly in the
provider CLI, run `ivoai connect chatgpt` or `ivoai connect claude` once: the command
is the supported authentication boundary that invalidates and reprobes only that
provider without copying or hashing credentials.

## Headroom proxy appears but the Codex/Claude Code TUI does not open

IvoAI 0.4.0 could place `headroom wrap` in a new process group while leaving the
terminal foreground group assigned to IvoAI. When Headroom or the official client
read stdin, the kernel suspended that background group with `SIGTTIN`.

Inspect the live process tree with `ps -o pid,ppid,pgid,tpgid,stat,args`. Affected
children have `PGID != TPGID` and `STAT` containing `T`. Update to the fixed patch
release. Fixed versions keep the interactive stack in the existing foreground group,
restore terminal modes on exit, and preserve the official client's exit code.

Headroom starts its proxy in a detached session and may reuse a healthy proxy on port
8787. Do not use `pkill headroom`: a proxy can be shared or pre-existing. Headroom's
own wrapper tracks clients with PID/start-identity markers and cleans up a proxy it
created after the last wrapper exits normally.

## Automatic session switched providers

Use `ivoai monitor --watch` or `ivoai session show --json <session-id>`. A recorded
failover includes current primary, time, and a non-secret reason. Hard quota triggers
fallback; a network error does not. The alternate receives the last confirmed
checkpoint and bounded Git status/diff-stat, but ivoai never resets, checks out, or
cleans the working tree.

If both providers are exhausted, the session stops in `BLOCKED` or
`WAITING_FOR_QUOTA`; no worker and no PAYG provider is started. Wait for the displayed
reset, authenticate an eligible subscription client, then start a new automatic
session. The supervisor refuses more than two consecutive automatic failovers.

## Claude statusline customization

Automatic mode passes a private `--settings` file only to the launched Claude
process. It does not edit the user's persistent statusline. The automatic statusline
is needed for structured model/context/rate-limit capture. Outside that process the
user's normal Claude settings remain unchanged.

Start with:

```sh
ivoai status
ivoai doctor
```

On a server, use:

```sh
sudo ivoai server doctor
sudo ivoai server status
```

`ivoai doctor --json` is suitable for automation and never includes secret values.
Do not bypass checksum, TLS, ownership, symlink, or credential checks to make an
installation continue. If a host was installed before one of the fixes below, update
to a release that contains the fix or reinstall from an authenticated source checkout,
then rerun the idempotent setup.

## Installation and first server setup

This section records failure modes observed while bringing clean and previously
configured Linux hosts through the real ivoai installation flow. Prefer these checks
before manually editing generated Compose or systemd files.

### Public installer cannot download or verify ivoai

The public installer downloads a release archive plus `checksums.txt` and refuses to
install an asset whose checksum does not match. If `Download ivoai release` returns a
404, verify that a GitHub Release exists for the requested version and contains both
the platform archive and checksum file. Do not disable verification or substitute an
unreviewed binary.

For an authenticated source checkout, run:

```sh
./install.sh
```

The source installer uses a compatible system Go when available. If Go is absent or
older than the version declared in `go.mod`, ivoai downloads the pinned official Go
toolchain for the current architecture, verifies its SHA-256, uses temporary build and
module caches, and removes the toolchain afterward. A checksum mismatch, unsupported
architecture, or `no reviewed Go toolchain is pinned` error should be treated as a
hard stop rather than worked around.

If the destination already contains an unrelated `ivoai` executable or symlink, the
installer intentionally refuses to replace it. Move or remove that path only after
confirming it is not a third-party or separately managed installation.

### Docker is missing or too old for server setup

The server requires Docker Engine 28.0.0 or newer and Docker Compose 2.33.1 or
newer. Debian 12's `docker.io` 20.10 package is not sufficient: it predates the
gateway-priority support that routes the embedding container through its temporary
model-download network.

Check both versions:

```sh
docker version --format 'Engine server: {{.Server.Version}}'
docker compose version --short
```

Install or upgrade the Engine through Docker's official
[Debian](https://docs.docker.com/engine/install/debian/) or
[Ubuntu](https://docs.docker.com/engine/install/ubuntu/) instructions, then rerun
`sudo ivoai setup --mode server`. ivoai deliberately does not replace an existing
Engine because the host may run unrelated containers.

When the Engine is compatible, ivoai installs the pinned, checksum-verified official
Docker Compose CLI plugin at `/usr/local/lib/docker/cli-plugins/docker-compose` if
Compose is absent or older than 2.33.1. The download is roughly 49 MiB, has a bounded
30-minute window, and reports received bytes every 10 seconds.

If setup reports a pre-existing incompatible plugin at that path, ivoai preserves it
instead of overwriting third-party software. Inspect the file, move it aside only if
you intentionally want ivoai to manage that location, then rerun setup. Interrupting
the managed download is safe; the incomplete temporary file is removed.

### Server setup appears to hang while dependencies initialize

A first clean server setup may spend several minutes inside:

```text
Starting server dependencies; waiting for container health checks...
```

This is not necessarily a hang. Setup periodically prints elapsed time and the safe
inspection command:

```sh
sudo docker compose -f /etc/ivoai/compose.yaml ps
```

For additional diagnostics:

```sh
sudo ivoai server logs ivoai-dependencies.service
sudo docker compose -f /etc/ivoai/compose.yaml logs --tail=100 qdrant embeddings ai-memory
```

Do not repeatedly restart the stack while the first embedding model is still being
downloaded; doing so restarts the health window and can make a slow bootstrap look
like a persistent failure.

### First embedding initialization takes more than five minutes

The pinned local embedding model is downloaded on the first clean setup. CPU-only
hosts and slow Internet links can exceed five minutes before Text Embeddings Inference
becomes healthy. Current releases give that container a ten-minute startup grace
period; successful health probes end the grace period immediately. Later starts reuse
the persistent model cache.

If the container is still unhealthy after the grace period, inspect:

```sh
sudo docker compose -f /etc/ivoai/compose.yaml ps embeddings
sudo docker compose -f /etc/ivoai/compose.yaml logs --tail=150 embeddings
```

A download or DNS/connectivity error is different from slow initialization and should
be diagnosed as a network problem rather than by increasing health timeouts again.

### Embeddings cannot download the model on a clean host

The embeddings container intentionally has a temporary egress network only for model
bootstrap. It is attached to the internal network, the loopback-publication network,
and `ivoai-model-download`; the model-download network is given the preferred gateway
while the model is being fetched. Once embeddings are healthy, systemd disconnects
that download route.

If logs show that the model registry is unreachable during first bootstrap, inspect
the container networks before changing firewall policy:

```sh
sudo docker inspect ivoai-embeddings --format '{{json .NetworkSettings.Networks}}'
sudo docker network inspect ivoai-model-download
```

On an up-to-date installation the download network must be attached during bootstrap.
Do not permanently add broad Internet egress to `ivoai-internal` as a workaround.

### Rerunning setup fails with `too many levels of symbolic links`

Older setup logic recursively changed ownership inside application-created model
caches. Hugging Face caches legitimately use symlinks, which could produce errors such
as:

```text
open service-owned entry config.json: too many levels of symbolic links
```

Current setup changes ownership only on managed mount roots and does not recursively
walk normal application caches. Update/reinstall ivoai and rerun the idempotent setup.
Do not replace cache symlinks with regular files or use a blanket recursive `chown` as
a workaround.

### Qdrant fails with permission errors under `/qdrant`

Older layouts could leave the non-root Qdrant process unable to create its
initialization marker or temporary snapshot data. Typical paths include:

```text
/qdrant/.qdrant-initialized
/qdrant/snapshots/tmp
```

Current setup uses separate service-owned host mounts for storage, snapshot workspace,
and initialization state while keeping the container non-root. Update/reinstall ivoai
and rerun server setup rather than making the whole container filesystem writable.

### Containers are healthy but Context reports `127.0.0.1:6333: connection refused`

Docker can omit or invalidate host port publication when containers are attached only
to an internal network, and disconnecting the network used for publication can remove
the effective loopback binding. Current releases keep `ivoai-host-publish` attached
with IP masquerading disabled; only the temporary model-download route is disconnected
after embeddings become healthy.

Check the local bindings:

```sh
sudo ss -ltnp | grep -E ':(6333|8080|49374|7744)\b'
sudo docker network inspect ivoai-host-publish
sudo docker compose -f /etc/ivoai/compose.yaml ps
```

Do not manually disconnect `ivoai-host-publish`; it is required for the host-side
loopback mappings while still providing no backend masqueraded egress.

### ai-memory is reported unhealthy although its container is running

ai-memory does not expose the generic `/health` endpoint that older diagnostics
expected. Current ivoai health checks authenticate to the ai-memory MCP endpoint and
perform a bounded `tools/list` request. The gateway also rewrites the upstream Host
header and replaces the client credential with the private backend credential before
proxying allowed memory routes.

Use:

```sh
sudo ivoai server memory status
sudo ivoai server doctor
sudo docker compose -f /etc/ivoai/compose.yaml ps ai-memory
sudo ivoai server logs ivoai-gateway.service
```

If an older installation reports ai-memory unavailable while the container is healthy,
update/reinstall before changing ai-memory authentication or Host allowlists manually.
Context and the basic agents remain independently usable while memory is degraded.

### Reverse proxy returns HTTP 502 after server setup

A `502` means the reverse proxy cannot reach the ivoai gateway. First make local
health pass:

```sh
sudo ivoai server doctor
```

The gateway listens on loopback by default, which is correct only for a reverse proxy
running on the same host. For a proxy on another host or container, explicitly bind
the gateway to the server's private address and trust only the proxy's source CIDR:

```sh
sudo ivoai server gateway configure \
  --public-url https://ai.example.com \
  --listen 192.0.2.10:7744 \
  --trusted-proxy 192.0.2.20/32
```

Use the real private addresses. Do not use `0.0.0.0/0` as a trusted proxy range.
Requests in trusted-proxy mode must also carry `X-Forwarded-Proto: https`.

### Enrollment works locally but is rejected through a proxy

Enrollment rejection responses are deliberately uniform. Current clients transport
the one-time code in a dedicated Authorization scheme and carry non-secret client
metadata separately, making enrollment resilient to proxies that rewrite request
bodies. The gateway accepts the legacy body form only for rolling compatibility and
rejects ambiguous requests that provide both transports.

Inspect only the safe gateway audit metadata:

```sh
sudo ivoai server logs ivoai-gateway.service
```

The audit can distinguish malformed or mismatched enrollment input, unauthorized
scopes, state availability problems, or a request routed to a different gateway
instance without logging the code, verifier, issued client token, or client name. If
the one-time code was consumed or expired, create a new one rather than attempting to
recover or reuse it.

### Restore finishes but services restart with permission errors

A validated backup is restored as root-owned regular files. Current releases reapply
the dedicated service ownership to the restored context, corpus, and memory trees
before restarting the stack, while normal setup still avoids recursively traversing
application caches.

If a host restored with an older build starts failing on read/write permissions,
upgrade before repeating the restore. Do not use `chown -R /var/lib/ivoai`: model and
application caches can contain legitimate symlinks, and broad ownership changes weaken
the separation between gateway, context, and dependency services.

## Interactive menu rendering

- Use `NO_COLOR=1 ivoai` when ANSI colors are not supported.
- Use `IVOAI_ASCII=1 ivoai` when the terminal font cannot render block characters;
  the header falls back to the plain text `ivoai`, never alternate lettering.
- Piped input and `TERM=dumb` intentionally select the numbered fallback.
- Raw terminal state is restored on normal errors, EOF, Esc, `q`, and cancellation.
- Animated progress goes to stderr. Redirect stdout independently when consuming
  command or JSON output.
- Resize is detected while the menu is open. If an intermediary SSH client does not
  propagate `SIGWINCH`, reopen the menu after resizing or set correct `COLUMNS` and
  `LINES` values.
- Very short terminals intentionally hide descriptions and use a scrolling viewport;
  the position indicator shows undisplayed items.
- If an older build exits when Up/Down is pressed, update and reinstall ivoai. Current
  releases decode escape sequences already buffered by the terminal instead of
  misclassifying an arrow key as a standalone Esc.

## Agent is installed but not connected

This is expected immediately after setup. Run `ivoai connect chatgpt` or
`ivoai connect claude`. Browser authentication is owned by the official client. If an
unrelated `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` is present, it may take precedence
inside the vendor CLI; ivoai does not modify the user's shell environment.

## Headroom fails

Headroom is a deprecated compatibility provider, not the default. Doctor still
reports its health during the observation window. Select Direct explicitly with
`ivoai config set compression.provider direct`, or restore the Caveman default with
`ivoai config set compression.provider caveman`. Memory and server state remain
independent.

## Caveman falls back to Direct

This is expected when Caveman is unavailable, authoritative Memory/Context requires
a byte-exact path, or OpenCode uses a native subscription-only provider unsupported
by the pinned proxy. `ivoai status` shows configured/default and effective providers;
`ivoai doctor --json` adds the bounded fallback reason. IVOAI does not require a
PAYG key and does not retry an already-started executor.

## Server is unreachable

Codex and Claude Code remain usable. Check the base URL, certificate hostname,
`/.well-known/ivoai`, `/health`, and `/ready`. A protocol mismatch must be resolved by
updating either side; ivoai will not persist a partially compatible connection.

## Setup cannot install a component

The error identifies the component and leaves other components intact. Confirm the
OS/architecture is listed in the manifest, that HTTPS access to the upstream release
host is available, and rerun `ivoai setup`. Repeated setup does not duplicate hooks or
replace pre-existing tools. For installer, Docker, Qdrant, embeddings, and first-server
failures, use the installation checklist above before making manual changes.

## Server services do not start

Run `ivoai server doctor`, then `ivoai server logs`. Verify Docker and Compose for
dependencies and use `systemctl status ivoai-gateway ivoai-context`. Diagnostics
redact authentication material, but review output before sharing it externally.

## ChatGPT or Claude Web cannot connect to `/mcp`

Verify the public paths without supplying a secret:

```sh
curl -i https://ai.example.com/.well-known/ivoai
curl -i https://ai.example.com/.well-known/oauth-authorization-server
curl -i https://ai.example.com/.well-known/oauth-protected-resource
```

A `502` means the proxy cannot reach port 7744. A proxy-generated `401` or `403`
usually means an NPM Access List, Basic Auth, WAF rule, or another authentication
layer is intercepting OAuth; remove that extra challenge from this host. Preserve the
`Authorization` header and `X-Forwarded-Proto: https`, disable proxy buffering for
Streamable HTTP, and allow a long read timeout.

If OAuth reports an invalid redirect, remove the connector and add it again so its
current redirect URI is dynamically registered. Never broaden redirect matching.
If the activation code expired or was already consumed, create another with
`ivoai server web-access create`; do not place the code in the URL or logs.

Use `ivoai server web-access list` to confirm the grant is active and scoped. Revoke
the entry and reconnect if refresh-token rotation was interrupted. Memory-tool
failures do not imply that context is unavailable; check `ivoai server memory status`
and `ivoai server context status` separately.

## Interactive menu is diagonal or does not fit the terminal

Update and reinstall ivoai if menu rows appear as a staircase, start farther to the
right on every line, or leave large blank regions. Older builds wrote bare newlines
after enabling raw terminal mode. Current builds emit the required carriage returns,
re-read width and height after resize, paginate vertically, and never render beyond
the detected column count. `TERM=dumb` remains available as a numbered fallback.

## `ivoai connect claude` appears to do nothing

Update and reinstall ivoai before retrying. Older builds moved the official Claude
login into a background process group, which allowed the operating system to suspend
it when it read from the terminal. Current builds keep Claude Code in the foreground,
show the authentication preflight, and explicitly request the official Claude
subscription flow:

```sh
ivoai connect claude
```

Complete the browser flow or open the URL printed by Claude Code. Verify the result
with `ivoai doctor`. ivoai does not read or store Claude credentials. If login still
fails, run `claude auth status` to check the official client independently and make
sure the terminal permits interactive input.

## Ruflo is installed but an orchestrated session is refused

Run `ivoai setup` and then `ivoai doctor`. Orchestrated mode requires the current
private wrapper and exact safe-profile version, a provider-free health probe, a real
swarm ID and successful primary lifecycle registration. A profile edited by hand,
an older allowlist, `provider_execution=true`, durable Ruflo memory, or a failing
`ruflo swarm init/status` causes an explicit refusal. Direct commands remain usable:

```sh
ivoai codex
ivoai claude
```

Do not add provider keys to work around the gate. Ruflo is a coordination layer, not
the inference provider.

## Worker failed to launch

Doctor checks `codex exec --help` and `claude --help` without performing inference.
Repair components with `ivoai setup`, verify each official login separately, and
inspect the session with `ivoai monitor --session <id>`. Only managed or discovered
Codex/Claude component paths are accepted; an executable path cannot be supplied by
a delegation prompt. A Headroom start failure retries the official worker directly,
while an already-started wrapper's failure is reported normally.

## Model is `unknown`

This is a safe result, not a health failure. ivoai reports a model only when verified
by runtime evidence, supplied with `--model`/`-m`, or found in a supported official
client configuration. It never derives a model from the CLI version, account plan or
binary name. Pass an explicit official model argument if deterministic provenance is
needed.

## Monitor shows a stale session

Run `ivoai session show <id>` and then `ivoai session stop <id>`. ivoai sends a signal
only when both PID and Linux process-start marker match. If no owned process exists,
the stale lifecycle is finalized as failed rather than risking an unrelated recycled
PID. Session JSON under the XDG state directory may be retained as non-sensitive
history; do not edit it manually.

## Recovering an orphan worker

Normal primary shutdown cancels the bridge context, terminates owned worker process
groups, closes Ruflo lifecycle tasks and removes the private runtime directory. If
the host lost power or ivoai itself was killed, use `ivoai session stop <id>` after
restart. A mismatched process marker is deliberately not killed. Check the official
client independently only if an actual matching process remains.

## ai-memory or Context is degraded during a session

These services are independent of inference. The monitor reports `ready`, `degraded`
or `disabled`, but Codex, Claude and bounded workers continue. Use `ivoai memory
configure`, `ivoai doctor`, and the server context/memory status commands to repair
the integration. Ruflo never receives a copy of the context corpus or becomes a
durable memory fallback.

If a fact written by Claude is not recalled by Codex, first check chronology: the
write must complete before the query. Then run `ivoai doctor` and confirm both
`ivoai-memory` registrations use the same server. IvoAI-managed primaries use
`memory_read_page(query=...)` as the first bounded memory lookup and make at most one
`memory_query` fallback. For research tasks they then attempt `context_search` before
any external web source, even when memory was useful. Explicit writes use one
canonical page and one verification instead of duplicating a fact across scopes.
Lifecycle hooks use the main Git
repository as their project scope, so the same checkout reached through `/home/...`,
`/mnt/...`, a subdirectory or a linked worktree does not create separate hook buckets.
Context cannot accept conversational writes; it contains only documents ingested
through configured connectors.

If the memory tool visibly finds the correct page but its body ends before the
answer, inspect whether an older IvoAI launch used lossy compression. Headroom
0.36.0 can compress Codex Code Mode `custom_tool_call_output` frames and omit exact
trailing text. Current IvoAI launches bypass any compression provider whenever
authoritative `ivoai-memory` or `ivoai-context` is active and print the reason; start a new `ivoai codex`,
`ivoai claude`, session, or automatic run after updating. Existing processes keep
the launch policy they started with and are not killed or rewritten during update.

For registered remote IvoAI services, memory and Context reads receive narrowly
scoped, process-local Codex approval overrides. If `memory_query` still says that an
approval is required, confirm the launch went through the current `ivoai codex`,
session, or automatic command rather than invoking an older Codex binary directly.
Memory writes are intentionally not auto-approved.

## Multiple servers and knowledge selection

Inspect profiles independently:

```sh
ivoai connect server list
ivoai connect server show mindsite
ivoai connect server test mindsite
ivoai doctor
```

If ivoai reports that multiple knowledge purposes are connected, launch with an
explicit source such as `ivoai codex --knowledge-source mindsite`. Repeat the flag
only when cross-purpose read federation is intentional. An ambiguous Memory write is
rejected; restart the operation with exactly one destination rather than disconnecting
the other organization.

A partial federated response means at least one selected source timed out, returned
HTTP failure, exceeded the response limit or returned malformed JSON-RPC. The result
keeps per-source attribution. Test each alias separately. One failed profile does not
delete another, and `ivoai disconnect server mindsite` leaves Voicecorp intact.

The MCP names inside two concurrent agent processes may both be `ivoai-memory` and
`ivoai-context`; this is expected. Each points to a different session-local loopback
router and local capability. Do not compare the names to infer the upstream. Check
the session's selected aliases and `ivoai connect server list`; no global agent
configuration is rewritten when another session starts.

If Codex reports that `codex-code-mode-host` is missing, update ivoai. Current
setup installs the separate, version-matched official companion release asset beside
managed Codex and verifies its reviewed SHA-256. A launch is refused if that managed
companion is absent or mismatched, because disabling it also makes this Codex version
fail closed for MCP tools. Do not copy an unverified executable from another install
into the managed bin directory.

On WSL, VPN and split-horizon DNS setups, the first resolver attempt can take about
five seconds. Current status and Doctor probes allow that resolver window before
declaring the server unreachable. A real failure remains bounded and does not block
the official clients.

## Automatic scheduler is degraded

Run `ivoai doctor` and inspect **Automatic Scheduler**, **Parallel Worker Runtime**,
the two model-routing rows, effort support, and Shared Knowledge Bootstrap. A model
router can be `degraded` while the normal official TUI remains usable. Re-run
`ivoai setup` if a managed client is missing; capability discovery is refreshed
automatically when its version changes.

If a planned task remains `queued`, use `ivoai monitor --watch` and inspect its
`depends` row. Complete the primary-owned prerequisite or wait for its workers. A
task shown with `mode=primary` and “worker overhead is not lower” is intentional, not
a scheduler failure.

If bootstrap is degraded, check the Memory and Context rows independently. AUTO may
continue without them for self-contained work, but it reports the knowledge gap. Do
not copy memory/context contents into session JSON or Ruflo as a workaround. A later
materially different objective triggers a new bounded bootstrap; related follow-ups
reuse the existing brief with delta planning.

If effort says `unsupported`, IvoAI deliberately lets the official client select its
default. It never guesses an effort or model. A model-specific exhausted quota blocks
only that model; an all-provider block requires confirmed authoritative exhaustion or
missing eligible subscription authentication.
