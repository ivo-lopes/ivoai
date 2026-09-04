# Desenvolvimento

## Requisitos

- A versão do Go declarada em `go.mod` para comandos diretos de desenvolvimento.
  `./install.sh` inicializa temporariamente a toolchain exata revisada quando o Go
  estiver ausente ou for muito antigo.
- Git
- Bash e ShellCheck
- Python 3 para empacotamento determinístico da Web skill
- Docker apenas para testes de integração das dependências do servidor

Para um checkout privado, autentique o Git e clone por SSH:

```sh
git clone git@github.com:ivo-lopes/ivoai.git
cd ivoai
./install.sh
ivoai setup
```

## Verificações locais

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

A CI também executa `govulncheck ./...`. Os testes usam diretórios XDG temporários,
processos executáveis falsos e servidores `httptest`. Eles não acessam credenciais de
desenvolvedor do Codex ou do Claude. Os packages do servidor continuam testáveis sem
root, systemd, Docker ou um processo Qdrant ativo.

O harness do scheduler automático usa adapters falsos do Codex/Claude, controle de
ciclo de vida falso do Ruflo e registros sintéticos de cotas/capabilities. Seu teste
de DAG temporal executa simultaneamente dois workers independentes com atraso e
verifica que tarefas dependentes não comecem antecipadamente. Os testes de roteamento
cobrem os quatro níveis de pontuação, pesos normalizados, esgotamento de cota por
provider/modelo, pressão de cota, esforço incompatível, capabilities indisponíveis,
rejeição de trabalho duplicado e seleção econômica entre primary e worker. Nenhum
teste normal invoca um LLM real.

O harness de atualização constrói a tag `v0.5.0` real e o candidato atual. Um helper
vinculado dentro do módulo v0.5.0 arquivado executa download, verificação de checksum,
extração, sondagem de versão e promoção atômica reais do updater publicado contra um
servidor local de releases. Em seguida, o candidato executa setup/Doctor e seu
`update --rollback` real e compatível com o legado; o harness valida a leitura da
v0.5.0, a preservação de campos desconhecidos e marcadores pertencentes aos providers
e a atualização após o rollback. O mesmo harness repete a promoção histórica, a
bridge de setup simples, o rollback, o Doctor do servidor antigo e a atualização
após rollback em uma instalação somente servidor; ele também verifica que a bridge
não crie configuração de cliente. Testes unitários de transação cobrem o novo updater
com journal, modo servidor e migração injetada, Doctor, interrupção e falhas de
tamanho, disco, caminho e ownership. A CI faz checkout de todo o histórico para que
a tag histórica esteja disponível.

## Cobertura da instalação na CI

O job `install-smoke` executa em containers Ubuntu 22.04, Ubuntu 24.04 e Debian 12.
O `scripts/install-smoke.sh` isola `HOME` e todos os diretórios XDG, habilita
`IVOAI_TEST_MODE` e verifica duas execuções do instalador, duas execuções do setup do
cliente, duas do setup do servidor, saída de status e doctor, estabilidade de
ownership, permissões e idempotência. O modo de teste usa fixtures locais de
componentes e ignora o provisionamento de Docker/systemd; portanto, esse job valida o
contrato da CLI e do sistema de arquivos sem contatar contas de usuário nem hosts de
componentes upstream.

O job `real-client-install` usa a mesma matriz de três distribuições sem
`IVOAI_TEST_MODE`. Ele instala o binário ivoai a partir do código-fonte em checkout,
executa duas vezes o setup dos componentes reais e fixados do cliente como usuário de
teste sem privilégios e então verifica status, saída do doctor legível por pessoas e
saída JSON do doctor. Ele não autentica contas reais do ChatGPT, Claude ou servidor
ivoai e não exercita o arquivo público da release do GitHub, pois o binário ivoai vem
do checkout.

O job `real-server-deployment` executa em um host Ubuntu 24.04. Ele instala o binário
ivoai em checkout, provisiona duas vezes o servidor Docker/systemd, executa server
doctor, cria um código de enrollment de curta duração e exercita backup e restauração.
Essa verificação de implantação não consome o código de enrollment em um cliente
separado nem usa credenciais do proprietário.

O job `cross-build` compila o binário para Linux amd64 e arm64. Juntos, esses jobs
complementam os testes unitários, de race, vet, formatação, ShellCheck, build e
vulnerabilidades do job `quality`.

## Release

Tags de versionamento semântico acionam o workflow de release. Ele verifica os gates
de qualidade, constrói arquivos para Linux amd64 e arm64, empacota a Web skill
`ivoai-memory-context`, verifica o ZIP, gera `checksums.txt` para cada artefato e
publica uma release no GitHub. O script de empacotamento grava metadados ZIP fixos
para que uma entrada idêntica da skill produza um arquivo idêntico. Não crie uma tag
em um commit antes que verificações unitárias, de race, vet, cross-build,
idempotência de instalação, implantação e segurança sejam aprovadas.
