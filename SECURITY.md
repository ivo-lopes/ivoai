# Security Policy

## Supported versions

Security fixes are provided for the current released minor version. Before v1.0,
users should update to the newest patch release with `ivoai update`.

## Reporting a vulnerability

Use GitHub's private vulnerability reporting for this repository. Do not open a
public issue containing credentials, enrollment codes, private documents, exploit
details, or production logs.

Include the affected version, component, reproducible steps, impact, and any proposed
mitigation. Reports are acknowledged as soon as practical; disclosure timing is
coordinated after a fix and release are available.

## System and review scope

Security review covers ivoai-owned Go code, installers, deployment definitions,
configuration writes, enrollment/authentication, context ingestion, backup/restore,
and dependency pinning. Third-party agent and infrastructure behavior should also be
reported upstream, but ivoai will assess whether defensive changes are needed here.

The client runs on a user's workstation. The optional gateway is the only intended
Internet-facing server surface. Qdrant, embeddings, ai-memory administration, and
service-control interfaces are loopback-only or private. Protected assets include
vendor credentials, ivoai client tokens, memory, ingested corpus content, connector
configuration, and host service integrity.

## Threat model and trust boundaries

Treat server URLs, HTTP responses, enrollment input, connector paths and documents,
archive contents, subprocess output, and MCP parameters as attacker-controlled. A
connected server is authenticated but is not trusted to control the client host.
Retrieved RAG text is data and cannot authorize commands. Local root and a fully
compromised host are outside ivoai's protection boundary.

## Security invariants

- Vendor authentication stays in the official Codex and Claude clients.
- Secrets never enter main configuration, argv, URLs, diagnostics, or logs.
- Enrollment codes are high-entropy, short-lived, digest-only, transactional, and
  single-use; issued client credentials are scoped, hashed server-side, and revocable.
- Non-loopback server connections require valid TLS and same-origin redirects.
- Authorization is checked before every protected gateway or MCP operation.
- Connector, archive, and filesystem paths remain within explicit managed roots;
  symlink, traversal, special-file, oversized-input, and unsafe overwrite cases fail.
- Agent-facing context methods are read-only. No endpoint accepts a host command,
  executable path, arbitrary file write, or shell expression.
- Optional memory, context, Headroom, and Ruflo failures cannot prevent a direct
  Codex or Claude launch; authorization failures never fail open.
- All production dependency versions and integrity strategies are pinned.
- Session state contains operational metadata only: never prompts, worker results,
  tokens, headers, cookies, environment dumps, or provider keys.
- The local orchestration MCP is stdio-only, bound to one unpredictable active
  session ID, accepts only known Codex/Claude executors, and enforces bounded input,
  output and concurrency.
- PID actions require the Linux process start marker to match, preventing stale PID
  reuse from terminating an unrelated process.
- Ruflo receives a clean provider-free environment and only opaque lifecycle IDs;
  it never receives delegation prompts or worker responses.

## Reportable findings and severity context

Report reachable violations of these invariants, including credential disclosure,
authentication or scope bypass, enrollment replay, arbitrary command/file execution,
path escape, unsafe archive extraction, cross-origin token forwarding, public database
exposure, persistent prompt-injection-to-control flow, or supply-chain verification
bypass. Severity depends on realistic reachability, required privileges, exposed data,
and whether the default deployment is affected.

For the session control plane, also report unsafe executable selection, symlink or
permission bypass in session state, PID-reuse termination, provider environment
leakage, prompt/result persistence, unbounded worker creation, orphan processes, or
remote exposure of `ivoai-orchestrator`.

## Out of scope and known limitations

Do not report a third-party upstream defect with no reachable ivoai impact, expected
errors caused only by unsupported platforms, denial of service requiring existing
root control, or prompt content that remains inert untrusted data. A local root user
can read or alter service data and binaries. TLS termination and certificate lifecycle
may be provided by an administrator-managed reverse proxy; ivoai still requires normal
client certificate validation. Ruflo direct provider execution is intentionally
disabled in the default profile until it works without separate PAYG credentials.

The design and operational controls are detailed in [docs/security.md](docs/security.md).

## Automatic orchestration and quota telemetry

Automatic routing uses only official subscription clients. Codex telemetry is read
through its documented app-server protocol; Claude telemetry is received through a
private session-local structured statusline. ivoai never extracts provider OAuth
credentials, reuses internal tokens, scrapes authenticated Web UI, or enables a PAYG
fallback. Known provider-key and base-URL environment variables are stripped from
quota probes and workers. Ruflo provider execution remains disabled.

Quota inputs are untrusted. Payloads, lines, stderr, metadata, and caches are bounded;
terminal control characters, secret-shaped fields, symlinks, invalid percentages,
and unknown providers are rejected or redacted. Concurrent cache writes are locked.
Unknown telemetry is not interpreted as exhausted. Network errors do not cause
automatic quota failover.

Automatic checkpoints reject credentials and complete prompts/responses. Failover
handoffs include only this bounded checkpoint and bounded Git status/diff statistics;
they never run destructive Git cleanup. PID plus Linux process-start identity guards
signal delivery, cancelled primary processes are reaped, and a two-failover ceiling
prevents Codex/Claude ping-pong.
