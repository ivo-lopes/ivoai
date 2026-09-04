# Client

## Menu interativo

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
OpenCode       ready / managed frontend
Headroom       ready
Compression    default caveman / effective caveman
Caveman        installed / managed
ai-memory      installed / not connected
Ruflo          ready / provider execution disabled
Server         not-connected

Overall: READY — external connections pending
```

## Launch do agent

`ivoai codex` e `ivoai claude` abrem suas interfaces Direct oficiais. `ivoai opencode`, assim como
`ivoai auto`, abre uma TUI OpenCode pinada e anexada ao control plane privado do IVOAI. O IVOAI
roteia turnos para a CLI oficial Codex ou Claude Code e reutiliza o login que já pertence àquele
client sem lê-lo ou copiá-lo. Um overlay OpenCode gerenciado e privado desabilita config de projeto,
sharing e auto-update; a configuração global e o store de provider do usuário permanecem intactos.

Sessões Codex e Claude podem selecionar um purpose de knowledge registrado sem alterar a config
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
mostra esse bypass e a metadata observada da sessão reporta o bypass provider-neutral; Headroom
permanece disponível como provider explícito temporário de compatibilidade.

Para uma sessão OpenCode standalone explícita, OpenCode mantém seu provider nativo somente de
assinatura. Quando o runtime Caveman pinado não consegue fazer proxy desse provider, o preflight
seleciona Direct antes do launch; o IVOAI não solicita API key, troca provider nem inicia OpenCode
duas vezes. AUTO usa a bridge IVOAI e os contratos de executor Codex/Claude descritos acima.

Todo primary gerenciado pelo ivoai recebe o mesmo contrato de conhecimento compartilhado. Qualquer
tarefa que exija pesquisa, levantamento de fatos, informação atual ou verificação externa usa a
ordem fixa `ivoai-memory` → `ivoai-context` somente leitura → fontes web/externas. As duas etapas do
ivoai são tentadas antes da primeira consulta externa, inclusive em perguntas aparentemente gerais
ou sensíveis ao tempo. Resultados internos vazios, indisponíveis, insuficientes ou stale permitem
pesquisa web; tarefas autocontidas não disparam consultas artificiais. Uma solicitação explícita
para lembrar informação é gravada por `memory_write_page` e verificada com `memory_read_page`.
Context é um índice RAG alimentado por connector, não um store de escrita conversacional; portanto,
um agent não pode afirmar que gravou um fato do chat no Context.

A mesma policy process-scoped é injetada em workers Codex e Claude. OpenCode é o frontend AUTO, não
um worker consultivo nem um segundo scheduler. O executor OpenCode standalone continua disponível
explicitamente, sem modificar config pertencente ao usuário nem tratar texto recuperado como
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

O menu interativo contém **Session Control**, com escolhas diretas e orquestradas para os dois
clients oficiais, listagem de sessões, monitoramento e stop seguro. As mesmas operações estão
disponíveis para automação:

```sh
ivoai session start --executor codex --mode direct
ivoai session start --executor claude --mode orchestrated
ivoai session list --json
ivoai session show --json <session-id>
ivoai session stop <session-id>
ivoai monitor --watch
```

Sessões Direct adicionam metadata e monitoramento, mas não inicializam Ruflo. Sessões orquestradas
exigem profile seguro do Ruflo verificado, inicializam e verificam um swarm real, registram o primary
e injetam o MCP `ivoai-orchestrator` local. O MCP delega tarefas limitadas aos modos não interativos
oficiais Codex/Claude. O default são dois workers concorrentes e o máximo rígido são três.

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

`ivoai auto` abre o frontend OpenCode gerenciado e mostra estado limitado de executor, quota e
knowledge pertencente ao IVOAI. A preferência de planner configurada seleciona o executor oficial
inicial; `--planner codex` e `--planner claude` fazem override para a sessão. Codex/Claude nunca
assumem a tela. OpenCode fornece a UI enquanto IVOAI mantém ownership da sessão, seleção de provider
e autoridade single-writer.

O modo automático inicia o mesmo control plane Ruflo sem provider das sessões orquestradas explícitas,
injeta o MCP orchestration local à sessão e adiciona bootstrap único de Memory/Context, brief
compartilhado privado, score objetivo de tarefas, delegação econômica, roteamento de model/effort
ciente de quota/capability, workers DAG realmente assíncronos e checkpoints limitados de
continuidade. Trabalho trivial fica no primary; trabalho independente valioso executa
simultaneamente. Execute `ivoai monitor --watch` em um segundo terminal para ver primary atual,
provenance do modelo, quantidade de failovers, estado de worker, fonte/freshness/reset da quota e
health de services. Antes da primeira resposta do Claude, suas linhas de quota dizem
`awaiting first response`; campos não compatíveis dizem `N/A / not exposed`, e observações antigas
são marcadas `stale`. Mensalidade do Claude não é fabricada.

A seleção de knowledge permanece fixa durante failover Codex↔Claude e workers herdam os mesmos
endpoints locais à sessão. Failover nunca expande o conjunto de fontes nem copia credencial upstream
para um handoff.

Executar `ivoai connect chatgpt` ou `ivoai connect claude` invalida o cache de quota reconstruível
do provider selecionado antes do fluxo de autenticação oficial e executa novo probe depois. Isso
impede que um hard limit de conta anterior contamine novo contexto de autenticação sem ler
credenciais do provider.

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
