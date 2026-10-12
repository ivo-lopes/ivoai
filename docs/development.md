# Development

## Requirements

- Go version declared in `go.mod` for direct development commands. `./install.sh`
  bootstraps the exact reviewed toolchain temporarily when Go is absent or too old.
- Git
- Bash and ShellCheck
- Python 3 for deterministic Web skill packaging
- Docker only for server dependency integration tests

For a private checkout, authenticate Git and clone by SSH:

```sh
git clone git@github.com:ivo-lopes/ivoai.git
cd ivoai
./install.sh
ivoai setup
```

## Local checks

```sh
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
shellcheck install.sh scripts/*.sh
scripts/install-smoke.sh
scripts/upgrade-matrix.sh
scripts/package-skill.sh
unzip -t dist/ivoai-memory-context.zip
go build ./cmd/ivoai
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/ivoai
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/ivoai
```

CI also runs `govulncheck ./...`. Tests use temporary XDG directories, fake
executable processes, and `httptest` servers. They do not access developer Codex or
Claude credentials. Server packages remain testable without root, systemd, Docker,
or a live Qdrant process.

The automatic scheduler harness uses fake Codex/Claude adapters, fake Ruflo lifecycle
control, and synthetic quota/capability registries. Its temporal DAG test runs two
independent delayed workers together and verifies that dependent tasks do not start
early. Routing tests cover the four score tiers, normalized weights, provider/model
quota exhaustion, quota pressure, unsupported effort, unavailable capabilities,
duplicate-work rejection, and economic primary-vs-worker selection. No normal test
invokes a real LLM.

The upgrade harness builds the actual `v0.5.0` tag and the current candidate.
A helper linked inside the archived v0.5.0 module runs the published updater's
real download, checksum, extraction, version probe, and atomic promotion against
a local release server. The candidate then performs setup/Doctor and its real
legacy-compatible `update --rollback`; the harness validates v0.5.0 readability,
unknown-field and provider-owned marker preservation, and update-after-rollback.
The same harness repeats the historical promotion, plain-setup bridge, rollback,
old-server Doctor, and update-after-rollback against a server-only installation;
it also asserts that the bridge does not create client configuration.
Transaction unit tests cover the new journaled updater, server mode and injected
migration, Doctor, interruption, size, disk, path and ownership failures. CI
checks out full history so the historical tag is available.

## Installation coverage in CI

### Managed OpenCode regression matrix

`scripts/test-managed-opencode.sh` uses the pinned OpenCode artifact with local
provider/MCP fixtures. It tests native executor discovery and execution through
the same frontend/bridge, controlled HTTP/SSE, permissions, attach, keyboard, and
live resize. It does not require personal provider credentials.

`node --test internal/opencodebridge/presentation.test.mjs` compares deterministic
goldens of the presentation functions imported by the managed TUI: identity,
permissions, auth/quota, runtime, servers, restricted/degraded states, and
60/100/160-column rows. Go goldens cover model/variant metadata and sanitized errors.
Normal tests never rewrite baselines. After reviewing an intentional change, use
`UPDATE_GOLDENS=1` with the corresponding tests; CI refuses that update mode.
Timestamps, ports, and session IDs are not presentation inputs. State, model,
reasoning, permission mode, health, and selection semantics are never normalized.

The `install-smoke` job runs in Ubuntu 22.04, Ubuntu 24.04, and Debian 12
containers. `scripts/install-smoke.sh` isolates `HOME` and every XDG directory,
enables `IVOAI_TEST_MODE`, and checks two installer runs, two client setup runs,
two server setup runs, status and doctor output, ownership stability, permissions,
and idempotency. Test mode uses local component fixtures and skips Docker/systemd
provisioning, so this job validates the CLI and filesystem contract without
contacting user accounts or upstream component hosts.

The `real-client-install` job uses the same three-distribution matrix without
`IVOAI_TEST_MODE`. It installs the ivoai binary from the checked-out source, runs
the real pinned client component setup twice as an unprivileged test user, and then
checks status, human-readable doctor output, and JSON doctor output. It does not
authenticate real ChatGPT, Claude, or ivoai server accounts, and it does not
exercise the public GitHub release archive because the ivoai binary comes from the
checkout.

The `real-server-deployment` job runs on an Ubuntu 24.04 host. It installs the
checked-out ivoai binary, provisions the Docker/systemd server twice, runs server
doctor, creates a short-lived enrollment code, and exercises backup and restore.
This deployment check does not consume the enrollment code from a separate client
or use owner credentials.

The `cross-build` job compiles the binary for Linux amd64 and arm64. Together,
these jobs complement the unit, race, vet, formatting, ShellCheck, build, and
vulnerability checks in the `quality` job.

## Release

Semantic-version tags drive the release workflow. It verifies quality gates, builds
Linux amd64 and arm64 archives, packages the `ivoai-memory-context` Web skill, verifies
the ZIP, generates `checksums.txt` for every artifact, and publishes a GitHub release.
The package script writes fixed ZIP metadata so identical skill input produces an
identical archive. Do not tag a commit until unit, race, vet, cross-build,
install-idempotency, deployment, and security checks pass.

## P1/P2 validation cost and source of evidence

For the 2026-10-12 backlog, use `GOMAXPROCS=2` and `go test -p 1` on a constrained
host, with `TMPDIR` on disk. Do not launch several full suites or rebuild the
managed frontend concurrently. The test layers are intentionally different:

| Layer | Scope / cost | Provider access |
| --- | --- | --- |
| Focused Go tests | Profiles, hook ownership/repair, scope continuity, event replay and status; seconds to tens of seconds | Fixtures only |
| Node presentation/events and Python harness tests | Bounded stream, reconnect, keyboard and consumed-output assertions; seconds | None |
| Full Go / race / vet | Shared configuration, session store and lifecycle changes; minutes, higher RAM for race | Fixtures; opt-in live tests skipped |
| Managed OpenCode / native Codex scripts | Reviewed real binaries, HTTP/TUI contracts; minutes | Local simulated provider, no login |
| `TestLiveNativeAUTOArtifact` | Opt-in real Codex inference and terminal continuity; minutes, consumes account quota | Codex only; no Claude component in isolated state |
| Install/upgrade smoke | Synthetic private HOME/XDG, component and persistent-state recovery | Local fixtures, no production migration |

The live artifact smoke accepts `IVOAI_NATIVE_SMOKE_CONTINUITY=1` and, with
`IVOAI_NATIVE_SMOKE_FRONTEND=codex`, `IVOAI_NATIVE_SMOKE_SWITCH=1`. The latter
verifies idle frontend interruption/reopening and switching to managed OpenCode
without changing the provider. It does **not** prove recovery of an interrupted
approved DAG. Use only checksum-verified reviewed artifacts for optional
`IVOAI_NATIVE_SMOKE_CODEX_PATH`/`IVOAI_NATIVE_SMOKE_OPENCODE_PATH` overrides.
No credentials or personal configuration are copied. The personal Codex config
is guarded by a before/after digest.

Release-train evidence belongs with existing IVOAI-136; public capability
acceptance follows IVOAI-135/146. Never mark provider acceptance complete from a
fixture test, nor treat executable rollback as database migration rollback.

`IVOAI_NATIVE_SMOKE_RECOVERY=1` adds a separately opt-in real approved readonly
interruption/recovery smoke. It waits for a two-task host checkpoint with completed
and pending work, interrupts only the verified owned frontend, runs `session
recover`, requires fresh plan approval, then checks integration, task preservation,
unchanged repository and provider identity. Linux pidfd support and an actual
interruption window are required; missing evidence fails without editing checkpoints.
