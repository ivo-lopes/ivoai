# Contributing

Keep changes small, preserve external installations and authentication, use the
existing transactional storage/supply-chain paths, and add regression tests at the
caller boundary. Before submitting:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
shellcheck install.sh scripts/*.sh
cd website && corepack pnpm install --frozen-lockfile && corepack pnpm build
```
