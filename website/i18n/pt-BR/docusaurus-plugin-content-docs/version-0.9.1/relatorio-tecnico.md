# Relatório técnico do ivoai

## Orquestração automática eficiente (v0.5.0)

`ivoai auto` adiciona um supervisor ao Session Control Plane. O primary selecionado
continua sendo a TUI oficial do Codex ou Claude Code, simultaneamente planner,
conversation owner e consolidador. O Quota Manager usa
`account/rateLimits/read` no Codex app-server e o payload estruturado de `statusLine`
do Claude, normaliza context/session/weekly/monthly/model-scoped sem confundi-los e
persiste somente telemetria não sensível em cache privado, atômico e bloqueado por
arquivo.

O gate é aplicado no startup, periodicamente no primary, antes e depois de cada
worker e antes de failover. Ruflo mantém `provider_execution=false` e registra apenas
IDs opacos. Checkpoints locais limitados e livres de segredos dão a fronteira
imediata do handoff; hooks do ai-memory mantêm continuidade operacional durável. O
supervisor preserva o worktree, sinaliza e aguarda o grupo de processo correto e
limita a dois failovers consecutivos. Métricas ausentes continuam desconhecidas e
erros de rede não são promovidos a exaustão. Detalhes e contratos estão em
`docs/auto-orchestration.md` e `docs/quota-routing.md`.

O primeiro pedido substantivo acrescenta um protocolo determinístico: lookup bounded
em ai-memory, lookup bounded em Context, SharedContextBrief privado, análise,
decomposição, DAG, scoring, resolução de capability/quota, dispatch paralelo,
validação, síntese e checkpoint. O conteúdo do brief fica no runtime `0700/0600`; o
JSON de sessão mantém somente timestamp, estados das fontes, número de referências e
hash.

Cada task fornece `complexity`, `risk`, `reasoning_depth`, `context_breadth`,
`verification_need`, `parallel_value` e `latency_sensitivity` em `0..100`. O IvoAI
calcula o capability score com pesos 30/25/20/10/15 e resolve LIGHT (0–24), BALANCED
(25–49), STRONG (50–74) ou MAX (75–100). Uma função separada compara benefício de
paralelismo/qualidade com overhead de startup/contexto e pode manter a task no
primary mesmo quando o planner solicitou delegação.

O Capability Registry consulta `codex app-server`/`model/list` para modelos e efforts
estruturados. Para Claude, apenas os efforts expostos pelo `--help` oficial são
usados; sem catálogo estruturado, o model fica `client-default`. O router seleciona
o menor perfil suficiente, aplica quota provider/model-specific e pressão de quota,
e nunca inventa nome ou suporte. Effort não suportado vira
`effort_source=unsupported` e não é enviado.

`orchestration_spawn` e `orchestration_spawn_batch` são assíncronos. O scheduler usa
notificações, limita o DAG a 12 tasks, respeita dependências e ocupa no máximo dois
workers por padrão (hard cap três). Prompts/resultados permanecem na memória do
bridge; Ruflo recebe IDs opacos. Codex worker usa sandbox read-only e Claude worker
usa permission mode `plan` com ferramentas de escrita desabilitadas. Escalada avança
um tier por vez e exige motivo. A especificação completa está em
`docs/auto-scheduler.md`.

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
    ├── OAuth 2.1 + Web MCP unificado
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

O renderer não fixa as dimensões na abertura. Ele consulta largura e altura a cada
frame e recebe `SIGWINCH`, recalculando banner, badges, descrições e viewport. O modo
amplo é usado a partir de `90x24`, o intermediário entre `60x18` e `89x23`, e o
compacto abaixo disso. Cálculos usam células visuais Unicode e removem ANSI antes de
medir; badges quebram linha e listas altas são paginadas, evitando overflow.

A apresentação semântica é compartilhada pelos comandos humanos: cyan/violeta para
estrutura e progresso, verde para sucesso, amarelo para warning e vermelho para erro.
O lettering completo aparece no instalador e na entrada principal; telas internas
usam wordmark compacto. JSON, pipes, `NO_COLOR`, `TERM=dumb` e CI não recebem códigos
ANSI ou animação.

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

O shell installer implementa a mesma máquina visual antes que o binário exista. Cada
fase possui início, conclusão e erro; downloads conhecidos exibem barra, enquanto
checksum, extração e build exibem spinner. Saída detalhada temporariamente capturada é
reapresentada se a operação falhar. A conclusão informa caminho instalado e diferencia
o próximo passo client (`ivoai setup`) do server (`ivoai setup --mode server`).

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

## 12. OAuth e MCP para aplicações Web

O endpoint público `/mcp` usa Streamable HTTP através do SDK Go oficial do MCP,
pinado. Ele preserva os endpoints nativos usados pelo desktop, mas oferece a ChatGPT
Web e Claude Web um único catálogo agregado. A sessão negocia a versão do protocolo,
publica schemas de entrada/saída, annotations de segurança e respostas em
`structuredContent` com fallback textual.

Ferramentas de contexto são read-only. A memória é separada por scopes:

| Ferramentas | Scope |
| --- | --- |
| `context_search`, `context_get_document`, `context_recent`, `context_health` | `context:read` |
| `memory_query`, `memory_recent`, `memory_read_page`, `memory_status` | `memory:read` |
| `memory_write_page`, `memory_feedback` | `memory:write` |
| `memory_delete_page` | `memory:delete` + confirmação do path normalizado |

O facade não repassa ferramentas upstream desconhecidas. Self-routing, sweeps,
auto-improvement, manutenção, provider execution e shell remoto ficam fora do
catálogo. A indisponibilidade de ai-memory retorna erro apenas nas ferramentas de
memória.

OAuth 2.1 utiliza Authorization Code com PKCE S256, metadata de authorization server
e protected resource, dynamic client registration, redirect exato, consentimento e
revogação. Authorization codes duram cinco minutos, access tokens uma hora, e refresh
tokens rotativos 30 dias. Um código de ativação one-time criado por
`ivoai server web-access create` autoriza o navegador sem uma base de senhas.

O estado OAuth é owner-only, bloqueado entre processos e escrito atomicamente.
Códigos e tokens são armazenados apenas como hashes; o valor é entregue somente ao
participante do fluxo. Rotação invalida o refresh anterior, e revogação encerra toda
a família. Origin, redirect, PKCE, scopes e confirmação destrutiva são validados no
gateway.

Para proxy reverso, somente a origem HTTPS é pública. Nginx Proxy Manager preserva
`Authorization`, `Host` e `X-Forwarded-Proto`, desabilita buffering do Streamable HTTP
e não adiciona Basic Auth ou Access List sobre OAuth. Qdrant, TEI e ai-memory
continuam inacessíveis externamente.

## 13. Distribuição da skill

`skills/ivoai-memory-context/SKILL.md` instrui o modelo a usar sempre a ordem de
pesquisa `memória → Context → web`: ambos os serviços internos são tentados antes da
primeira pesquisa externa, inclusive para fatos gerais ou atuais. Resultado vazio,
indisponível, insuficiente ou desatualizado permite consultar a web. RAG e memória
são tratados como dados não confiáveis. Escrita exige pedido explícito; delete exige
uma confirmação separada que nomeie o path normalizado.

O MCP publica uma fotografia dessa skill por `skills/list`, `skills/get` e
`resources/read`, com URI `skill://` e digest. O workflow de release também produz
`ivoai-memory-context.zip`, contendo a pasta da skill na raiz, para importação no
Claude Web. A importação não garante invocação em cada turno: tool selection permanece
uma decisão do produto Web.

## 14. Segurança e ameaças

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

## 15. Backup, restore e atualização

Backup inclui configuração necessária, catálogo/contexto, corpus, memória e metadados
de reconstrução. Restore valida o archive, interrompe serviços necessários e reinicia
de forma controlada; o menu exige confirmação `RESTORE`.

Update é explícito e transacional. Antes da promoção, valida release, SHA-256,
archive, versão do candidato e contrato de schemas; então cria snapshot privado
somente de arquivos pertencentes ao IVOAI. O binário capaz de recuperar o journal
é promovido atomicamente, aplica migrations ordenadas e reversíveis, reconcilia o
runtime e então o Doctor valida o resultado.
Falha em migration, setup ou Doctor restaura executável, config, state, ownership e
metadados de componentes compatíveis. Campos TOML desconhecidos são preservados por
merge entre documento bruto e projeção tipada. `--dry-run` baixa, valida e executa
o candidato verificado apenas para preflight, sem commit de estado gerenciado; preconditions de cada migration são aplicadas
dentro da transação real. O
`--rollback` é idempotente. A matriz CI constrói a tag real v0.5.0 e prova
o core real do updater v0.5.0 → candidate → rollback → v0.5.0 → candidate sem
tocar autenticação de providers. Stores dinâmicos do server exigirão participante
quiesced explícito antes de qualquer futura alteração de schema.

## 16. Qualidade e entrega

CI executa gofmt, testes, race detector, vet, builds Linux amd64/arm64, ShellCheck,
govulncheck e smoke tests em Ubuntu 22.04, Ubuntu 24.04 e Debian 12. O menu acrescenta
testes de largura, cor, teclas, fallback, indisponibilidade, confirmação, progresso e
inventário de ações públicas.

O MCP acrescenta testes de negociação, schemas, skills, isolamento de falhas e
autorização por tool. OAuth cobre PKCE, DCR, redirect malicioso, expiração, consumo
one-time, rotação, revogação, CSRF, scopes e ausência de secrets nos logs. O release
valida o ZIP da skill e inclui seu checksum junto aos binários.

## 17. Session Control Plane

O domínio `internal/session` persiste metadados operacionais de forma atômica sob XDG
state, com IDs aleatórios, locks no-follow e identidade de processo composta por PID
e start time do kernel. Os estados são `starting`, `running`, `degraded`, `stopping`,
`completed` e `failed`. Prompt, resultado, environment e secrets não pertencem ao
schema.

No modo direct, `internal/app` chama o mesmo `agents.Runtime` usado pelos entrypoints
históricos, apenas observando PID e uso real de Headroom. No modo orchestrated,
`orchestration.ControlPlane` valida o perfil seguro, executa e confirma um swarm real
e registra o primary como task opaca antes do launch. O MCP local
`ivoai-orchestrator` oferece status, agentes, delegate, result e cancel somente por
stdio e somente enquanto a sessão está ativa.

`internal/workers` encapsula `codex exec --json --output-last-message` e
`claude --print --output-format json`. Executáveis vêm exclusivamente do component
state, o ambiente exclui keys PAYG, o task é limitado a 32 KiB e o resultado a 1 MiB.
Headroom é usado após probe e seu uso efetivo fica no worker metadata. Ruflo recebe
apenas IDs e lifecycle; respostas ficam em memória no bridge. A concorrência padrão é
dois e o hard limit é três.

O monitor possui saída humana responsiva e JSON sem ANSI. Proveniência de modelo
segue `runtime_verified > argument > configured > unknown`; a implementação atual
não promove saída textual comum a runtime verified. Detalhes operacionais estão em
[orchestration.md](orchestration.md).

## 18. Referências internas

- [Arquitetura](architecture.md)
- [Cliente](client.md)
- [Servidor](server.md)
- [Conexões](connections.md)
- [Segurança](security.md)
- [Desenvolvimento](development.md)
- [Troubleshooting](troubleshooting.md)
