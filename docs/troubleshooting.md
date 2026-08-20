# Troubleshooting

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
