# Troubleshooting

## Interactive menu rendering

- Use `NO_COLOR=1 ivoai` when ANSI colors are not supported.
- Use `IVOAI_ASCII=1 ivoai` when the terminal font cannot render block characters.
- Piped input and `TERM=dumb` intentionally select the numbered fallback.
- Raw terminal state is restored on normal errors, EOF, Esc, `q`, and cancellation.
- Animated progress goes to stderr. Redirect stdout independently when consuming
  command or JSON output.

Start with:

```sh
ivoai status
ivoai doctor
```

`ivoai doctor --json` is suitable for automation and never includes secret values.

## Agent is installed but not connected

This is expected immediately after setup. Run `ivoai connect chatgpt` or
`ivoai connect claude`. Browser authentication is owned by the official client. If an
unrelated `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` is present, it may take precedence
inside the vendor CLI; ivoai does not modify the user's shell environment.

## Headroom fails

Doctor reports Headroom health separately for each agent. Disable it through
`ivoai config set headroom.enabled false`, or use the direct-agent fallback selected by
the launcher. Memory and server state remain independent.

## Server is unreachable

Codex and Claude Code remain usable. Check the base URL, certificate hostname,
`/.well-known/ivoai`, `/health`, and `/ready`. A protocol mismatch must be resolved by
updating either side; ivoai will not persist a partially compatible connection.

## Setup cannot install a component

The error identifies the component and leaves other components intact. Confirm the
OS/architecture is listed in the manifest, that HTTPS access to the upstream release
host is available, and rerun `ivoai setup`. Repeated setup does not duplicate hooks or
replace pre-existing tools.

## Server services do not start

Run `ivoai server doctor`, then `ivoai server logs`. Verify Docker and Compose for
dependencies and use `systemctl status ivoai-gateway ivoai-context`. Diagnostics
redact authentication material, but review output before sharing it externally.

On Debian 12, `docker-compose-v2` and `docker-compose-plugin` may be absent from
the configured repositories; this is supported. Re-run `ivoai setup --mode server`:
ivoai installs the pinned, checksum-verified official Compose CLI plugin. If setup
reports a pre-existing incompatible plugin at
`/usr/local/lib/docker/cli-plugins/docker-compose`, ivoai preserves it instead
of overwriting third-party software; move that file aside deliberately, then retry.
During this approximately 49 MB download, setup reports received bytes every 10
seconds. Interrupting the process is safe: the incomplete temporary file is removed,
and the idempotent setup can be run again.

If an older setup reports `open service-owned entry config.json: too many levels of
symbolic links`, update and reinstall ivoai before rerunning setup. Hugging Face model
caches intentionally use symlinks; current ivoai preserves them and does not recursively
change ownership inside container-managed data.

If Qdrant reports permission errors for `/qdrant/.qdrant-initialized` or
`/qdrant/snapshots/tmp`, update and reinstall ivoai. Current setup mounts separate
service-owned init and snapshot paths while keeping the image root filesystem
non-writable and the Qdrant process non-root.

If containers are healthy but Context reports `127.0.0.1:6333: connection refused`,
update and reinstall ivoai. Some Docker versions silently omit published ports for a
container attached only to an internal network. Current setup establishes all three
loopback bindings through a transient network, verifies service stability, and then
removes dependency egress.

An HTTP 502 from a reverse proxy means the gateway is not reachable from that proxy.
First make `ivoai server doctor` pass. For a proxy on another host, configure the
gateway with its private listen address and the proxy's narrow source CIDR using
`--trusted-proxy`; loopback is reachable only from the ivoai server itself.

Enrollment rejection responses remain deliberately uniform. The gateway journal
records only safe correlation metadata: enrollment ID, input length, format validity,
proxy peer, result, and broad rejection reason. It never records the enrollment code,
its verifier, a client token, or the client name. Use
`ivoai server logs ivoai-gateway.service` to distinguish a malformed/mismatched code,
an unauthorized scope, or a request routed to another gateway instance.
