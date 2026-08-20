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

## Reportable findings and severity context

Report reachable violations of these invariants, including credential disclosure,
authentication or scope bypass, enrollment replay, arbitrary command/file execution,
path escape, unsafe archive extraction, cross-origin token forwarding, public database
exposure, persistent prompt-injection-to-control flow, or supply-chain verification
bypass. Severity depends on realistic reachability, required privileges, exposed data,
and whether the default deployment is affected.

## Out of scope and known limitations

Do not report a third-party upstream defect with no reachable ivoai impact, expected
errors caused only by unsupported platforms, denial of service requiring existing
root control, or prompt content that remains inert untrusted data. A local root user
can read or alter service data and binaries. TLS termination and certificate lifecycle
may be provided by an administrator-managed reverse proxy; ivoai still requires normal
client certificate validation. Ruflo direct provider execution is intentionally
disabled in the default profile until it works without separate PAYG credentials.

The design and operational controls are detailed in [docs/security.md](docs/security.md).
