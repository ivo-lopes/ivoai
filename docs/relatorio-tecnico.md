# Relatório técnico do ivoai

## 1. Objetivo e princípios

ivoai é um único binário Go com modos client e server. O cliente é host-first: Git e
projeto local são opcionais. O setup inicial não depende de conexão externa, login de
provedor ou chave PAYG.

Princípios arquiteturais:

- componentes opcionais não derrubam Codex ou Claude;
- configuração estruturada e secrets separados;
- subprocessos recebem argv, contexto, timeout e sinais;
- versões e integridade são centralizadas no manifesto;
- o servidor publica um único origin HTTPS;
- bancos e serviços internos não são APIs públicas;
- contexto recuperado é dado não confiável;
- administração remota possui escopo mínimo e nunca equivale a shell.

## 2. Visão de sistema

```text
CLIENTE
  ivoai CLI/menu
    ├── config, state, ownership e secrets XDG
    ├── registry MCP e hooks ai-memory
    ├── Ruflo safe profile
    └── Headroom ── Codex / Claude
             └───── fallback direto
          │ HTTPS + bearer client-scoped
          ▼
SERVIDOR
  ivoai gateway
    ├── discovery, health, readiness e enrollment
    ├── context MCP ── context ── embeddings ── Qdrant
    ├── memory MCP/hook proxy ── ai-memory
    └── remote admin read-only
```

## 3. CLI e interface interativa

O dispatcher de subcommands permanece a interface estável para automação. Sem
argumentos, o pacote terminal constrói um catálogo hierárquico sobre as mesmas funções
de aplicação e server runner.

A interface usa ANSI e `golang.org/x/term`, sem framework TUI adicional. Em TTY ela
ativa raw mode somente durante seleção, renderiza lettering block/shadow e badges não
sensíveis, interpreta setas, `j`/`k`, Enter, Esc e `q`, e restaura o terminal antes de
prompts, operações e agentes externos.

Sem TTY, `TERM=dumb` ou por pipe, utiliza seleção numerada. `NO_COLOR` remove códigos
ANSI e `IVOAI_ASCII=1` força caracteres ASCII.

O menu cobre comandos públicos. Entry points internos de systemd, como `gateway
serve` e `context serve`, não são apresentados. Operações destrutivas exigem frase de
confirmação exata. Itens incompatíveis permanecem visíveis com a razão.

O snapshot do menu é tipado e contém somente readiness e estados booleanos; nenhum
token, enrollment code ou conteúdo bruto de secret chega à camada visual.

## 4. Progresso e I/O

O indicador central apresenta spinner e tempo decorrido em TTY, frames ASCII quando
Unicode não está disponível, mensagens de início/fim fora de TTY e formatação de
barra/bytes para transferências com tamanho conhecido.

Progresso usa stderr. stdout permanece reservado para resultados, inclusive JSON.
`doctor --json` não habilita animação. Antes de login oficial ou agente interativo, a
linha animada é encerrada para entregar o terminal ao subprocesso.

Downloads específicos, como o plugin Docker Compose, reportam bytes e percentual a
partir de `Content-Length`. Health demorado de containers emite heartbeats com elapsed
time e comando seguro de diagnóstico.

## 5. Configuração e persistência client-side

```text
~/.config/ivoai/       configuração e secrets
~/.local/share/ivoai/  binários gerenciados, hooks e assets
~/.local/state/ivoai/  estado operacional e ownership
~/.cache/ivoai/        downloads e cache
```

Diretórios privados usam `0700`; secrets usam `0600`. O TOML principal registra
settings, conexões e MCPs, mas não credenciais. O ownership manifest separa componentes
preexistentes daqueles instalados pelo ivoai para permitir uninstall não destrutivo.

## 6. Instalação e componentes

`install.sh` detecta Linux e arquitetura, baixa releases e valida checksums. Em source
checkout autenticado, utiliza Go compatível ou baixa temporariamente o toolchain
pinado e validado, compila e remove os temporários.

`ivoai setup` garante layout/config/secrets, instala componentes pinados, configura
Ruflo seguro, reconcilia hooks/MCPs e atualiza state/ownership. O manifesto central
registra versão, fonte, arquitetura, estratégia e integridade.

## 7. Provedores e execução de agentes

ChatGPT/Codex delega para `codex login`; Claude delega para `claude auth login`.
Detecção e validação usam comandos oficiais. ivoai não lê cookies nem armazena tokens
dos provedores.

```text
ivoai codex|claude
  ├── carrega ambiente permitido e credencial server-scoped
  ├── verifica Headroom habilitado, saudável e compatível
  ├── executa Headroom -> agente
  └── fallback direto se o preflight falhar
```

Depois de iniciado o wrapper, seu exit code é preservado; não existe retry silencioso
que poderia abrir uma segunda sessão.

## 8. Memória e orquestração

ai-memory é a memória operacional persistente e cross-session. Hooks de Codex e
Claude são idempotentes e falham no caminho allow/zero-exit quando o servidor está
offline. Ao conectar um servidor, endpoints e credencial são reconciliados.

O gateway substitui a credencial client-scoped por token privado antes de encaminhar
rotas permitidas ao ai-memory. O Host público não é propagado. Health usa `tools/list`
MCP autenticado, pois ai-memory não publica `/health` HTTP.

Ruflo cuida de workflows, agentes, coordenação e estado temporário. Provider execution
PAYG e memória durável Ruflo permanecem desabilitados por padrão.

## 9. Contexto/RAG

```text
filesystem/git connector
  -> normalização
  -> filtro sensível
  -> chunking
  -> embeddings locais
  -> collection Qdrant versionada
  -> search/read/recent/health MCP read-only
```

O serviço é saudável com zero documentos e conectores. Administração e ingestão usam
comandos separados; documentos não ganham capacidade de executar ações. Qdrant é um
índice reconstruível a partir do corpus autoritativo.

## 10. Servidor e infraestrutura

```text
/etc/ivoai/              configuração
/etc/ivoai/secrets/      secrets 0600
/var/lib/ivoai/          dados persistentes separados
/var/lib/ivoai/backups/  backups
/run/ivoai/              estado efêmero
/opt/ivoai/              assets
```

`ivoai-gateway.service` e `ivoai-context.service` usam usuários não-login distintos e
hardening systemd. `ivoai-dependencies.service` controla Compose. Qdrant, embeddings e
ai-memory usam imagens por digest e portas limitadas a loopback ou rede interna.

## 11. Protocolo, discovery e enrollment

`GET /.well-known/ivoai` anuncia protocol version, versão server, health/readiness,
MCPs e features não sensíveis. O client recusa redirects cross-origin, TLS inválido,
servidor unhealthy ou protocol major incompatível.

Enrollment:

1. admin cria código CSPRNG com TTL e escopos;
2. servidor guarda somente hash;
3. cliente envia authorization e metadados proxy-resilientes;
4. consumo atômico invalida o código;
5. servidor emite bearer client-scoped;
6. cliente persiste o secret e registra MCPs/hooks.

State e lock de enrollment são `0600` e pertencem ao gateway. Falhas internas de
estado retornam indisponibilidade sem se mascararem como credencial inválida.

## 12. Segurança e ameaças

- TLS Web PKI ou direto; proxy remoto exige CIDR confiável e HTTPS encaminhado.
- Redirects cross-origin são recusados.
- Bodies, concorrência e tempos HTTP são limitados.
- Authorization, cookies, API keys e tokens são redigidos.
- Arquivos sensíveis têm proteção contra symlink e permissões estritas.
- Downloads e extrações são limitados e validados.
- Subprocessos usam argv estruturado.
- Context MCP e remote admin não executam comandos arbitrários.
- Backends possuem credenciais internas distintas.

Falhas de Headroom, Ruflo, memória, contexto ou server remoto degradam somente a
função relacionada. Codex e Claude continuam disponíveis diretamente.

## 13. Backup, restore e atualização

Backup inclui configuração necessária, catálogo/contexto, corpus, memória e metadados
de reconstrução. Restore valida o archive, interrompe serviços necessários e reinicia
de forma controlada; o menu exige confirmação `RESTORE`.

Update é explícito, preserva config/secrets, executa doctor e retém o binário anterior
para rollback quando possível.

## 14. Qualidade e entrega

CI executa gofmt, testes, race detector, vet, builds Linux amd64/arm64, ShellCheck,
govulncheck e smoke tests em Ubuntu 22.04, Ubuntu 24.04 e Debian 12. O menu acrescenta
testes de largura, cor, teclas, fallback, indisponibilidade, confirmação, progresso e
inventário de ações públicas.

## 15. Referências internas

- [Arquitetura](architecture.md)
- [Cliente](client.md)
- [Servidor](server.md)
- [Conexões](connections.md)
- [Segurança](security.md)
- [Desenvolvimento](development.md)
- [Troubleshooting](troubleshooting.md)

