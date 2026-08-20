# Development

## Requirements

- Go version declared in `go.mod`
- Git
- Bash and ShellCheck
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
go build ./cmd/ivoai
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/ivoai
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/ivoai
```

CI also runs `govulncheck ./...`. Tests use temporary XDG directories, fake
executable processes, and `httptest` servers. They do not access developer Codex or
Claude credentials. Server packages remain testable without root, systemd, Docker,
or a live Qdrant process.

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
Linux amd64 and arm64 archives, generates `checksums.txt`, and publishes a GitHub
release. Do not tag a commit until unit, race, vet, cross-build,
install-idempotency, deployment, and security checks pass.
