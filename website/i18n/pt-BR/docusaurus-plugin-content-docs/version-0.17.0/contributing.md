# Contribuindo

Mantenha as alterações pequenas, preserve instalações externas e autenticação, use os
caminhos transacionais existentes de armazenamento e cadeia de suprimentos e adicione
testes de regressão no limite do chamador. Antes de enviar:

```bash
gofmt -w .
go test ./...
go test -race ./...
go vet ./...
shellcheck install.sh scripts/*.sh
cd website && corepack pnpm install --frozen-lockfile && corepack pnpm build
```
