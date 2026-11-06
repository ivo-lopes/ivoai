# Client

## Menu interativo

**Launch Orchestration** oferece **Codex Orchestrated** e sessões explícitas
**Codex Direct** / **Claude Direct**. As entradas OpenCode estão desabilitadas e
apresentam a migração, sem iniciar sessões. O mesmo Core controla admissão,
permissões e agendamento gerenciados. Consulte [frontends](orchestrated-frontends.md).

**Connections → IVOAI Servers** gerencia profiles independentes. Adicione um
servidor e selecione seu alias para testar, habilitar/desabilitar, editar, fazer
re-enrollment ou remover. Veja o fluxo completo em [Conexões](connections.md).


Execute `ivoai` sem argumentos para abrir o menu completo. Em um TTY, use Cima/Baixo
(ou `j`/`k`), Enter, Esc e `q`. O menu restaura o modo cooked do terminal antes de prompts
e antes de iniciar Codex ou Claude. Operações destrutivas exigem uma frase exata de
confirmação.

Quando stdin/stdout não são terminais, o ivoai exibe um fallback numerado. Defina
`NO_COLOR=1` para desabilitar cores ANSI ou `IVOAI_ASCII=1` para evitar lettering Unicode.
Subcomandos continuam sendo a interface estável para automação. O progresso é escrito em
stderr para que stdout e `doctor --json` permaneçam legíveis por máquina.

O renderer lê largura e altura em cada frame e reage a `SIGWINCH`. Terminais largos mostram
lettering completo com block/shadow, badges de prontidão e descrições. Terminais médios usam
banner reduzido e descrições compactas. Terminais pequenos usam wordmark de uma linha, viewport
limitada pela altura e indicador de posição. Badges quebram linha e labels são truncados pela
largura exibida das células Unicode; a interface não depende de terminal fixo de 80 colunas.

Telas interativas para pessoas usam a mesma paleta semântica: cyan para trabalho ativo, violet
para títulos, green para sucesso, yellow para resultados degradados e red para falhas. Menu
principal, todos os submenus e todos os comandos para pessoas usam o mesmo lettering adaptativo
do ivoai e mostram a versão do binário logo abaixo. Lettering, decoração da versão, animação do
cursor e cor ficam deliberadamente ausentes de saída redirecionada ou legível por máquina.

## Apresentação do instalador

`install.sh` usa o mesmo banner responsivo do ivoai que a CLI e reporta versão exata instalada,
plataforma e arquitetura detectadas, destino da instalação e fases. Transferências com tamanho
conhecido usam barra de bytes/percentual; checksum, extração, build do source e registro usam
spinner com tempo decorrido. Se uma etapa falhar, o instalador interrompe a animação, mostra o
log relacionado em um bloco de erro legível e não deixa download temporário parcial.

No sucesso, ele reporta o path instalado e o próximo comando. Um usuário normal é direcionado a
`ivoai setup`; uma instalação server como root, a `ivoai setup --mode server`. A animação vira
automaticamente texto simples periódico quando stderr não é um terminal compatível.

## Arquivos e ownership

O ivoai segue a XDG Base Directory Specification:

| Finalidade | Default |
| --- | --- |
| Configuração | `$XDG_CONFIG_HOME/ivoai` ou `~/.config/ivoai` |
| Dados e assets gerenciados | `$XDG_DATA_HOME/ivoai` ou `~/.local/share/ivoai` |
| Estado e manifest de ownership | `$XDG_STATE_HOME/ivoai` ou `~/.local/state/ivoai` |
| Cache | `$XDG_CACHE_HOME/ivoai` ou `~/.cache/ivoai` |

Diretórios com estado privado usam modo `0700`; arquivos secretos usam `0600`. O TOML principal
contém status e preferências, não bearer tokens.

`ivoai setup` é idempotente. Ele registra se cada executável já existia ou foi instalado pelo
ivoai. `ivoai uninstall` remove somente arquivos e binários gerenciados; não remove logins de
terceiros nem tools preexistentes.

## Componentes

Versões e fontes de instalação são centralizadas em `manifest/components.yaml`. O setup verifica
a plataforma, baixa assets pinados, verifica os dados de integridade revisados, instala wrappers
gerenciados e reporta falhas independentes. Headroom usa constraints com hash lock específicas da
arquitetura; Ruflo usa seu lockfile npm completo. Updates são explícitos por `ivoai update`. O
updater verifica o candidate, cria snapshot privado de arquivos pertencentes ao IVOAI, aplica
migrations ordenadas pertencentes ao target, promove o binário e executa Doctor. Uma falha restaura
binário e config/state/ownership compatíveis. Use `ivoai update --dry-run` para um plano de
compatibilidade sem commit; ele ainda executa os probes limitados de preflight do candidate
verificado por checksum. Use `ivoai update --rollback` para a última transação. Consulte o
[contrato de compatibilidade de produção](production-compatibility.md).

O estado desconectado saudável é:

```text
ivoai          ready
Codex          installed / not connected
Claude Code    installed / not connected
OpenCode       removed / retained metadata only
Headroom       ready
Compression    default caveman / effective caveman
Caveman        installed / managed
ai-memory      installed / not connected
Ruflo          ready / provider execution disabled
Server         not-connected

Overall: READY — external connections pending
```

Se metadata ou integridade revisada do compressor estiver indisponível, a
compressão mostra `default caveman / effective direct / fallback=caveman_unavailable`.
Esse fallback opcional é separado da ausência de clientes obrigatórios ou de um
setup Core degradado.

## Launch do agent

`ivoai codex` abre o terminal oficial Codex pela façade gerenciada do IVOAI;
`ivoai auto` é seu alias de compatibilidade. `ivoai codex --direct` usa
explicitamente o cliente direto, sem orquestração de turnos gerenciada. O Primary
adaptativo é experimental e fica desabilitado por padrão:
`IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1` habilita somente o caminho de runtime e
plataforma revisados durante certificação controlada. A opção não autoriza
versões ou plataformas arbitrárias.

`ivoai claude` usa a TUI oficial direta. Claude headless gerenciado exige
`IVOAI_EXPERIMENTAL_CLAUDE_HEADLESS=1` e `ivoai claude run --prompt ...`.
O host ACP experimental do Antigravity exige `IVOAI_EXPERIMENTAL_ANTIGRAVITY=1`,
`ivoai antigravity run --prompt ...` e o binário `agy` 1.5.2 revisado em Linux
amd64. Esses hosts headless não controlam as TUIs nativas. A certificação de
inferência Claude continua pendente de uma conta autorizada do usuário.

O launch OpenCode gerenciado e o setup legado explícito retornam erros de
migração. Dados pessoais e perfis anteriores permanecem intactos para rollback.

Codex e Claude podem selecionar um purpose de knowledge registrado sem alterar a config
global do agent:

```sh
ivoai codex --knowledge-source mindsite
ivoai claude --knowledge-source voicecorp
ivoai auto --knowledge-source mindsite
```

Sem `--knowledge-source`, todas as fontes conectadas e habilitadas participam da federation de
leitura limitada. Repita `--knowledge-source` ou passe um valor separado por vírgula para restringir
a sessão exatamente àquele subconjunto. Federation automática nunca transmite escritas Memory;
um destino ambíguo falha explicitamente. O agent recebe somente endpoints MCP privados em loopback
e uma capability local de curta duração. Credenciais upstream permanecem dentro do ivoai. Consulte
[Fontes de conhecimento multi-server](multi-server.md).

Codex e Claude solicitam Caveman por default. Overrides explícitos `direct` ou `headroom` legado
são preservados. Se o provider selecionado estiver indisponível, unhealthy ou incompatível no
preflight, o ivoai inicia diretamente o agent oficial. Quando um processo wrapper selecionado
inicia, seu exit status é propagado em vez de ocultado. Hooks de memory e context são best effort e
não podem bloquear o launch.

Quando `ivoai-memory` ou `ivoai-context` autoritativo estiver ativo para as fontes selecionadas da
sessão, o launcher inicia deliberadamente o client oficial de forma direta, independentemente de
Caveman ou Headroom ter sido solicitado. Um provider lossy pode encurtar resultados exatos de
custom tools antes que o modelo os veja, inclusive o fim exato de uma página de memória. O launch
mostra esse bypass e a metadata observada da sessão reporta o bypass provider-neutral.
O componente Headroom e sua escolha explícita são mantidos, mas o wrapper gerenciado
de Codex/Claude fica `unavailable_by_policy`: o wrapper pinado pode alterar a
configuração MCP do cliente, sem alternativa segura com recuperação exata verificada.
O IVOAI usa Direct uma vez, antes de sondar ou executar o wrapper, e preserva a escolha.

`ivoai opencode`, inclusive com `--direct`, é recusado antes de despachar o
executor. `ivoai setup --legacy-opencode` também recusa a instalação. Para usar
uma instalação upstream independente, execute-a fora do IVOAI; o launcher
gerenciado não a adota, troca seu provider nem inicia um processo OpenCode como
fallback.

Todo primary gerenciado pelo ivoai recebe o mesmo contrato de conhecimento compartilhado. Qualquer
tarefa que exija pesquisa, levantamento de fatos, informação atual ou verificação externa usa a
ordem fixa `ivoai-memory` → `ivoai-context` somente leitura → fontes web/externas. As duas etapas do
ivoai são tentadas antes da primeira consulta externa, inclusive em perguntas aparentemente gerais
ou sensíveis ao tempo. Resultados internos vazios, indisponíveis, insuficientes ou stale permitem
pesquisa web; tarefas autocontidas não disparam consultas artificiais. Uma solicitação explícita
para lembrar informação é gravada por `memory_write_page` e verificada com `memory_read_page`.
Context é um índice RAG alimentado por connector, não um store de escrita conversacional; portanto,
um agent não pode afirmar que gravou um fato do chat no Context.

A mesma policy process-scoped chega aos workers Codex e Claude pelos contratos
existentes do Core. Codex é o frontend nativo gerenciado; nenhum frontend mantém
outro scheduler ou banco de memória. Tipos serializados e histórico OpenCode
preservados não restauram execução, e texto recuperado não recebe autoridade de
instrução.

Codex trata tools MCP sem annotations somente leitura de forma conservadora. Para servers IVOAI
remotos realmente registrados e habilitados, o launcher adiciona overrides `approve` process-local
somente para `memory_query`, `memory_read_page` e as quatro tools Context somente leitura. Isso
mantém leituras headless funcionais enquanto escritas e exclusões Memory e tools MCP não relacionadas
continuam seguindo a policy normal de aprovação do usuário. Nenhum server MCP ausente é sintetizado
por esses overrides.

OpenAI publica `codex-code-mode-host` como asset versionado separado. O IVOAI instala o archive
revisado junto ao Codex gerenciado e verifica seu SHA-256. O tool router estável do Codex falha de
forma fechada quando o companion está ausente, inclusive para chamadas MCP; portanto o ivoai recusa
um launch gerenciado sem tools e direciona o usuário a `ivoai setup`. Instalações Codex gerenciadas
externamente permanecem responsáveis por seu próprio companion da versão correspondente.

## Session control plane

O menu interativo contém **Session Control** para inspeção de sessões nativas,
resume/recovery compatíveis, handoff confirmado e stop seguro. Inícios diretos
Codex e Claude permanecem explícitos; a orquestração nativa gerenciada usa Codex.
O host Claude headless experimental é outro comando, não um modo da TUI Claude.
A automação usa as mesmas operações de sessão:

```sh
ivoai session start --executor codex --mode direct
ivoai session start --executor claude --mode direct
ivoai session list --json
ivoai session show --json <session-id>
ivoai session resume <session-id>
ivoai session recover <session-id>
ivoai session handoff <session-id> --to claude --confirm
ivoai session stop <session-id>
ivoai monitor --watch
```

Sessões Direct adicionam metadata e monitoramento sem DAG. Codex gerenciado usa
o protocolo de planejamento, a aprovação de planos e o scheduler de workers
existentes no Core. Com a flag adaptativa experimental habilitada, turnos
somente leitura elegíveis podem terminar sem DAG. O perfil de coordenação Ruflo
sem provider não é outro scheduler. Resume, recovery e handoff permanecem
limitados às identidades elegíveis e aos modos de cliente compatíveis; um
planner/handoff Claude gerenciado sem suporte é recusado, sem abrir um host
headless ou outra TUI. Consulte
[Conversation Continuity](conversation-continuity.md).

O JSON da sessão é estado XDG privado e não contém prompt, response ou credencial. A saída do modelo
é rotulada `runtime_verified`, `argument`, `configured` ou `unknown`; o último valor é usado
intencionalmente em vez de adivinhação. Consulte
[Controle e orquestração de sessões](orchestration.md).

Vários primaries podem executar simultaneamente, inclusive duas sessões Codex e uma Claude. Cada
um tem ID de sessão aleatório, marcador PID/start e diretório runtime privado independentes.
Metadata de sessão e updates de quota usam file lock e escrita atômica; interromper ou limpar uma
sessão alcança somente seus processos e runtime registrados. Memory é compartilhada apenas dentro
da fonte/purpose explicitamente selecionada. Sessões Voicecorp e Mindsite concorrentes têm routers
loopback e capabilities separados, enquanto estado transitório do Ruflo e a bridge orchestrator
local à sessão permanecem isolados. Metadata da sessão armazena aliases selecionados, nunca
credenciais.

## Modo de conversa automático

`ivoai codex` e `ivoai auto` abrem o mesmo frontend nativo oficial Codex.
O Core mantém ownership da sessão, seleção de provider e autoridade de escrita
única. O Primary adaptativo continua desabilitado por padrão até certificação e
rollout controlados: `IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1` habilita somente
sessões revisadas. Prompts naturais não exigem critérios formais de aceite.
Sob essa flag, consultas simples e somente leitura elegíveis podem terminar no
Primary; tarefas complexas escalam para validação de plano, aprovações e scheduler
de workers existentes. `--planner opencode` é recusado; histórico legado não é
convertido em nova execução.

O Core controla agendamento de DAG, grants isolados de workers, verificações de
quota/capability e continuidade limitada. O perfil seguro de coordenação Ruflo
continua sem provider; ele não substitui o scheduler nem controla memória durável.
Nenhum worker ou benefício econômico é presumido para pedidos simples. O gate
econômico global continua incompleto. Execute `ivoai monitor --watch` em outro
terminal para ver primary, provenance do modelo, workers, fonte/freshness/reset
da quota e health de services. Campos indisponíveis mantêm `N/A / not exposed`;
antes de uma resposta Claude, sua quota pode mostrar `awaiting first response`,
e observações antigas continuam `stale`.

A seleção de knowledge e os limites da capability local passam aos workers e
handoffs compatíveis. Transições de cliente sem suporte são recusadas; isso não
promete failover transparente entre as TUIs Codex↔Claude. Um handoff nunca amplia
o conjunto de fontes nem copia credenciais upstream.

Executar `ivoai connect chatgpt` ou `ivoai connect claude` invalida o cache
reconstruível de quota do provider antes da autenticação oficial e faz novo probe
depois. Essa limpeza de cache não copia nem inspeciona credenciais do provider.

Consulte [Orquestração automática](auto-orchestration.md),
[Scheduler automático](auto-scheduler.md) e [Roteamento por quota](quota-routing.md).

`ivoai status` usa verificações live limitadas para cada profile Server e para o profile seguro do
Ruflo. Reporta alias, purpose, protocol, features limitadas, credencial configurada/não configurada,
redundancy group e priority sem imprimir segredo. Config armazenada nunca é rotulada healthy por si
só. Uma fonte indisponível não remove outra; Codex e Claude locais permanecem disponíveis. A
instalação/compatibilidade do Headroom é reportada separadamente da validação de launch interativo.

## Identidade do projeto

O ivoai é host-first. Fora de um projeto, memory usa identidade estável normalizada do host em vez
de derivar projeto de todo diretório de trabalho. `ivoai project init` cria marcador local explícito
dentro de um repositório Git e substitui a identidade do host para aquela árvore.
