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
