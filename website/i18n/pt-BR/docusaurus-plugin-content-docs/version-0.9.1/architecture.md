# Arquitetura do ivoai

Status: base de implementação para v0.5.0. As decisões e os dados upstream foram
validados até 2026-08-25. As versões exatas fixadas estão em `manifest/components.yaml`; este
documento explica por que elas existem e como as peças se encaixam.

## Objetivos e limites

O ivoai é um único binário Go com modos cliente e servidor. Um cliente é útil imediatamente
após a configuração, sem conexão externa e sem chave de API com cobrança por uso. ChatGPT,
Claude e um servidor ivoai são conexões deliberadas após a configuração, todas gerenciadas
por comandos CLI.

Falhas na configuração opcional de memória, contexto, compressão e orquestração são isoladas da
inicialização do agente local. Uma falha na verificação prévia de compressão seleciona o agente direto. Depois que um processo
wrapper inicia, seu status de saída é propagado para evitar o risco de duplicar uma
sessão interativa.

O núcleo é neutro em relação à organização. Ele não contém domínios de empresas, endereços privados,
caminhos de usuários nem conectores pré-configurados. Git é opcional; o modo de projeto é uma
substituição explícita, não um pré-requisito.

## Plano de conhecimento com múltiplos servidores

O caminho de conhecimento no cliente é neutro em relação ao provedor. Sessões sem filtros selecionam todos
os servidores habilitados; flags `--knowledge-source` repetidas formam um subconjunto restritivo
local à sessão:

```text
managed session (Codex / Claude / OpenCode-first AUTO workers)
        │ local capability, selected sources
        ▼
per-session loopback KnowledgeRouter
        │
        ├── ServerPool purpose A ── primary/standby redundancy
        └── ServerPool purpose B ── independent Memory/Context
```

`ServerProfile` registra um ID opaco, alias, finalidade, grupo de redundância opcional,
prioridade, protocolo, endpoints e recursos. As credenciais são armazenadas separadamente por
ID opaco. O pool agrupa réplicas equivalentes de forma determinística, preservando
os limites de finalidade. Nenhum nome de empresa aparece na lógica central de roteamento.

O roteador vincula um listener efêmero em `127.0.0.1` para cada sessão. Ele emite uma
capability local aleatória de curta duração, mantém as credenciais upstream no processo,
rejeita redirecionamentos entre origens e encaminha cada credencial apenas ao
perfil correspondente. Codex recebe argumentos MCP locais ao processo e Claude, um arquivo MCP
privado de runtime. AUTO usa o mesmo roteador durante o failover; os workers herdam endpoints locais.
Nenhuma reescrita global mutável de MCP é necessária, portanto sessões com finalidades diferentes podem executar
simultaneamente.

Respostas autoritativas de uma única fonte são preservadas. Sessões sem filtros federam
todas as fontes habilitadas; seletores explícitos restringem esse conjunto. Leituras federadas de `tools/call`
são distribuídas simultaneamente com prazos por fonte e mesclam de forma determinística
os resultados com atribuição de origem. As requisições são limitadas a 4 MiB e as respostas upstream,
a 16 MiB. Escritas exigem exatamente um destino lógico; uma escrita com redundância ocorre
apenas no primário e não é repetida após um efeito colateral incerto. O failover de leitura é
limitado ao mesmo grupo de finalidade/redundância e usa estado de circuito limitado.

Este roteador não é MemoryBackend, ContextBackend, WorkingContext nem replicação.
ai-memory continua sendo a memória operacional durável no servidor selecionado; Context continua sendo
o conhecimento privado somente leitura; ArtifactStore continua sendo a evidência transitória exata. Consulte
[Fontes de conhecimento com múltiplos servidores](multi-server.md).

## Contratos substituíveis do núcleo

Os limites de runtime em `internal/core` são deliberadamente pequenos e não fazem parte
dos esquemas persistidos de configuração, estado, propriedade, componente ou servidor. Eles fornecem
identidades neutras em relação ao provedor, suporte a capacidades (`supported`, `unsupported`,
`unknown` ou `not_exposed`), integridade, compatibilidade, proveniência, ciclo de vida e
metadados explícitos de fallback. Instalado, disponível e íntegro são fatos separados;
a ausência de evidência de sondagem permanece desconhecida, em vez de ser inferida a partir do nome de um provedor ou
modelo.

Os adaptadores atuais são:

- `CodexExecutor` e `ClaudeExecutor` sobre seus runtimes CLI oficiais, além de um
  `OpenCodeExecutor` independente. AUTO usa OpenCode como frontend gerenciado e roteia
  a execução pelos contratos Codex/Claude; o executor OpenCode independente
  continua permitindo apenas execução direta. Eles expõem início de sessão e cancelamento limitado.
- `AIMemoryBackend` sobre o configurador de cliente ai-memory existente. As chamadas de conteúdo de memória
  permanecem atrás do limite MCP autenticado, em vez de expandir esta
  interface de ciclo de vida.
- `LegacyQdrantContextBackend` sobre o catálogo, o embedding local e o serviço de contexto
  Qdrant existentes. O contrato voltado ao agente permanece somente leitura; a ingestão continua sendo uma
  composição administrativa.
- `HeadroomCompressionProvider` sobre a sondagem e a invocação do wrapper existentes. A
  política de fidelidade IVOAI ainda decide quando resultados exatos de Memory/Context exigem um
  bypass, e o cliente oficial continua sendo o fallback direto.
- [CompressionProvider: Caveman, Headroom e bypass direto](compression-provider.md)
  registra o padrão Caveman e o limite de migração. Os provedores são mutuamente
  exclusivos; Direct continua sendo o fallback de segurança, e Headroom explícito continua sendo um
  caminho temporário de compatibilidade.
- [Canário do Caveman e avaliação de fidelidade](caveman-canary.md) define o
  corpus determinístico, os critérios de exatidão byte a byte, os testes smoke opcionais com artefatos fixados e as
  evidências usadas para aprovar Caveman como padrão, mantendo o fallback Direct.
- `RufloOrchestratorAdapter` sobre o plano de controle existente com perfil seguro. Ele expõe
  apenas swarm efêmero e coordenação opaca de ciclo de vida; agendamento, roteamento,
  inferência, cota e memória durável continuam sob responsabilidade do IVOAI.

`SkillSource`, `SkillRegistry` e `ToolProvider` atualmente definem apenas contratos básicos de identidade,
sondagem e descoberta. Nenhum novo pacote de skills ou provedor de ferramentas é carregado por esta
base. Doctor constrói uma matriz de componentes exclusiva de runtime a partir do estado existente e de
sondagens oficiais, para que futuras seleções e fallbacks possam explicar qual implementação está
ativa e por quê. A matriz nunca é gravada na persistência de sessão/configuração e não
exige migração.

## Visão do sistema

```text
CLIENT                                      SERVER

ivoai CLI/wizard                       one public HTTPS origin
  |                                      |
  +-- configuration + ownership          +-- /.well-known/ivoai
  +-- connection registry                +-- /health and /ready
  +-- official agent authentication      +-- enrollment/control API
  +-- fail-safe memory hooks              +-- context MCP (read-only)
  +-- session control plane               +-- ai-memory MCP
  |     +-- direct observability
  |     +-- AUTO bootstrap + DAG scheduler
  |     `-- WorkingContext + private ArtifactStore
  |                                      |
  +-- Headroom -- Codex/Claude            +-- Web MCP + OAuth 2.1
        `------ direct fallback           +-- context service
                                         |     +-- connectors/ingestion
                                         |     +-- local embeddings
                                         |     `-- Qdrant (internal)
                                         `-- ai-memory (internal)
```

## Plano de controle automático sensível à cota

`ivoai auto` adiciona um supervisor e um agendador determinístico sobre o plano de controle
de sessão existente. Uma TUI OpenCode com versão fixada é o frontend interativo; ela não é um segundo
agendador e nunca se torna a responsável autoritativa pelas escritas. A TUI se conecta a um
servidor OpenCode autenticado em uma porta aleatória de `127.0.0.1`. Uma ponte de provedor
IVOAI privada em outra porta loopback aleatória invoca a CLI oficial Codex ou Claude
selecionada, para que cada cliente mantenha sua própria autenticação de assinatura. Nenhum token do fornecedor,
cookie, refresh token ou arquivo de credenciais é lido ou copiado para o OpenCode.

O plugin do servidor OpenCode anexa IDs não secretos de sessão e mensagem do frontend às
requisições da ponte. IVOAI persiste um mapeamento limitado, vinculado ao diretório de trabalho e ao escopo de conhecimento,
desse ID para IDs separados de conversa da CLI oficial por executor.
Isso oferece suporte a turnos subsequentes, failover e recuperação após reinicialização sem armazenar
corpos de conversa nem cruzar escopos de conhecimento. IVOAI continua sendo o planejador, proprietário lógico da sessão, seletor de executor,
autoridade de cota, autoridade de política e consolidador de resultados. Seu primeiro turno substantivo
tenta Memory e depois Context exatamente uma vez, cria um
SharedContextBrief privado e limitado, valida um DAG de no máximo 12 tarefas e envia sinais numéricos de tarefas
ao IVOAI. IVOAI — e não OpenCode — calcula pontuações de capacidade, escolhe o menor
perfil de modelo/esforço suficiente e verificado em runtime, rejeita delegações antieconômicas e
executa de forma assíncrona workers consultivos cujas dependências já estão prontas.
Um gerenciador de cota independente controla a admissão do primário inicial e de cada worker;
Ruflo continua sendo um coordenador efêmero de ciclo de vida sem provedor. A cota do Codex vem
do método JSON-RPC oficial do app-server, enquanto a cota do Claude vem de um
payload estruturado de statusline local à sessão. A pressão sobre a janela de contexto é modelada
separadamente da cota de assinatura.

O supervisor controla a identidade do processo e o failover limitado. Ele persiste apenas metadados
de sessão, snapshots normalizados de cota e checkpoints sem segredos. Ao confirmar um
limite rígido, ele encerra e recolhe o grupo de processos do executor atual, sonda novamente o alternativo e
continua no mesmo frontend OpenCode com uma transferência limitada. Um teto de dois failovers
evita ciclos de pingue-pongue. A sobreposição gerenciada desabilita o compartilhamento,
a atualização automática e a configuração de projeto do OpenCode; executar OpenCode fora do IVOAI não é afetado. Consulte
[Orquestração automática](auto-orchestration.md) e
[Roteamento por cota de assinatura](quota-routing.md). A pontuação, a descoberta de capacidades e
a API assíncrona são especificadas em [Agendador automático](auto-scheduler.md).

Apenas o gateway é acessível externamente. Qdrant, o runtime de embedding, a administração
do ai-memory e o gerenciamento de serviços permanecem em uma rede Compose privada ou em um socket
loopback. O servidor não expõe shell nem endpoint de comandos arbitrários.

## Arquitetura do cliente

O instalador público baixa um arquivo de release e verifica seu checksum publicado.
Quando invocado a partir de um checkout de código-fonte autenticado, ele primeiro usa uma toolchain
Go compatível do sistema. Se Go estiver ausente ou for mais antigo que a diretiva `go.mod`, ele baixa o
arquivo Go revisado para linux/amd64 ou linux/arm64, verifica o SHA-256 oficial fixado
e compila com caches temporários de módulos e build. A toolchain e os caches são removidos
quando a instalação termina; nenhuma instalação global de Go ou alteração no gerenciador de pacotes é
necessária.

### Arquivos e propriedade

O cliente segue a XDG Base Directory Specification:

- configuração: `$XDG_CONFIG_HOME/ivoai` ou `~/.config/ivoai`, modo `0700`;
- dados persistentes da aplicação: `$XDG_DATA_HOME/ivoai` ou `~/.local/share/ivoai`;
- estado operacional e manifesto de propriedade: `$XDG_STATE_HOME/ivoai` ou `~/.local/state/ivoai`;
- cache/downloads: `$XDG_CACHE_HOME/ivoai` ou `~/.cache/ivoai`;
- segredos: `$XDG_CONFIG_HOME/ivoai/secrets.json` (ou `~/.config/ivoai/secrets.json`), modo `0600`, dentro do diretório de configuração com modo `0700`.

O TOML principal contém apenas status de conexão e configurações não secretas. O manifesto de
propriedade registra os executáveis dos componentes como `managed` ou `pre_existing`; todos
os binários, hooks e runtimes de componentes gerenciados pelo ivoai ficam sob a raiz de dados
XDG do ivoai. A desinstalação remove as raízes XDG do ivoai e o registro do instalador, preservando
executáveis de terceiros preexistentes e a autenticação dos fornecedores.
As integrações usam o mecanismo de configuração suportado por cada cliente de terceiros e
preservam configurações não relacionadas.

O catálogo de instalação compilado no binário espelha o manifesto central revisado
e seleciona artefatos exatos por sistema operacional/arquitetura. Os arquivos obtidos diretamente são verificados contra
valores SHA-256 revisados antes da extração limitada. Headroom inclui
locks de restrições com hashes específicos por arquitetura e permite apenas wheels. Ruflo inclui um
lock npm v3 completo e instala com `npm ci` e scripts de ciclo de vida desabilitados; toda
dependência do registro tem um valor de integridade. Ambos os instaladores executam com um ambiente
mínimo que exclui credenciais de usuários e provedores.

Os inicializadores gerenciados usam substituição atômica. Os downloads têm limites de tamanho e timeouts.
`latest` nunca é um alvo de instalação de componente. As atualizações são explícitas por meio de
`ivoai update` e usam uma sondagem de compatibilidade versionada, snapshots privados
de arquivos exatos, um registro ordenado de migrações reversíveis, promoção atômica e
Doctor após a atualização. O rollback restaura tanto o executável anterior quanto a
persistência compatível pertencente ao IVOAI. Campos TOML desconhecidos são mantidos por meio de uma
mesclagem do documento bruto com uma projeção tipada. A invariante completa está documentada em
[production-compatibility.md](production-compatibility.md).

O Codex gerenciado inclui o `codex-code-mode-host` da mesma versão, publicado como um
artefato oficial de release separado. Ambos os arquivos específicos por arquitetura têm
checksums fixados independentemente. O host não tem comando de versão, portanto sua identidade de compatibilidade é
a versão de release revisada registrada no estado gerenciado; um
componente complementar ausente ou incompatível faz a configuração/inicialização falhar, em vez de remover silenciosamente toda a superfície de ferramentas.

### Conexões e credenciais oficiais

`ivoai connect chatgpt` delega a autenticação a `codex login`;
`codex login status` é a sondagem de status suportada. A documentação oficial da OpenAI
afirma que o login por assinatura do ChatGPT é suportado e abre um navegador. O ivoai nunca
lê nem armazena a credencial retornada. Fonte: [Autenticação do ChatGPT](https://learn.chatgpt.com/docs/auth).

`ivoai connect claude` verifica `claude auth status`, inicia o fluxo oficial
de navegador `claude auth login` quando necessário e valida o status novamente. A Anthropic
documenta o acesso por assinaturas Pro, Max, Team e Enterprise sem chave de API.
Fonte: [Autenticação do Claude Code](https://code.claude.com/docs/en/authentication).

A versão exata fixada do Claude gerenciado é 2.1.228 porque o canal `stable` do registro da Anthropic
apontava para ela na data da validação, enquanto `latest` era 2.1.237. A Anthropic descreve
stable como atrasado e filtrado contra regressões. O ivoai instala esse artefato revisado e
altera sua versão gerenciada fixada apenas durante uma configuração ou atualização explícita. O comportamento implementado
dentro do executável upstream do Claude continua regido pela Anthropic.

O ivoai não inspeciona `~/.codex/auth.json`, cookies do Claude nem tokens OAuth. Ele
não solicita, intermedeia, copia nem registra credenciais. Uma instalação e um
login oficiais preexistentes continuam sob propriedade do usuário.

### Inicialização do agente e isolamento de falhas

A seleção de fontes de pesquisa é centralizada em `internal/knowledgepolicy`. Primários
interativos, sessões automáticas e workers em segundo plano recebem o mesmo contrato
local ao processo: memória primeiro, Context em segundo e fontes web externas apenas depois, quando
o conhecimento interno estiver indisponível, insuficiente, desatualizado ou quando for solicitada
verificação independente. A inspeção da árvore de trabalho local e tarefas completamente especificadas pelo
usuário não criam chamadas artificiais de pesquisa. Esta é uma invariante da camada de instruções;
os clientes oficiais continuam controlando suas ferramentas e sua autenticação.

O inicializador usa argv estruturado e encaminhamento de sinais. Clientes interativos herdam
o grupo de processos em primeiro plano do ivoai; criar um novo grupo sem `tcsetpgrp`
suspenderia as leituras do terminal com `SIGTTIN`. Assim, os sinais de interrupção,
suspensão, continuação e redimensionamento do terminal e do controle de tarefas do shell alcançam toda a pilha interativa, enquanto
o cancelamento de contexto usa uma sequência limitada de TERM a KILL. A árvore de decisão normal é:

```text
requested agent
  -> ivoai-memory or ivoai-context MCP active?
       yes -> official agent [argv...]
       no  -> requested CompressionProvider healthy and compatible?
                caveman  -> managed Caveman proxy -> official agent
                headroom -> headroom wrap <agent> [argv...]
                direct or failed preflight -> official agent [argv...]
```

Headroom 0.36.0 está obsoleto, mas é mantido durante a janela de observação do Caveman.
Ele fornece oficialmente `wrap codex` e `wrap claude` e é instalado
em um ambiente de ferramentas isolado usando uv 0.12.5 e o runtime CPython
3.13.15 gerenciado exato sob a raiz de dados do ivoai. Uma verificação prévia indisponível, sem integridade ou incompatível
seleciona o agente direto. Depois que um processo wrapper compatível inicia,
o ivoai propaga seu status de saída; ele não tenta novamente, de forma silenciosa, uma sessão
interativa que falhou. O ivoai não reescreve os aliases do usuário nem substitui inicializadores de terceiros.
Headroom 0.36.0 pode aplicar compressão com perdas a itens
`custom_tool_call_output` do Codex Code Mode sem associá-los de forma confiável ao nome da
ferramenta MCP de origem. Por isso, IvoAI contorna qualquer provedor de compressão com perdas sempre que
conhecimento compartilhado autoritativo estiver ativo na sessão. Essa política neutra em relação ao provedor
preserva resultados exatos de Memory e Context; a telemetria da sessão registra o
provedor solicitado, o caminho Direct efetivo e o motivo limitado.
Fontes: [CLI do Headroom](https://headroomlabs-ai.github.io/headroom/cli/),
[Headroom v0.36.0](https://github.com/headroomlabs-ai/headroom/releases/tag/v0.36.0),
[Issue 940 do Headroom](https://github.com/headroomlabs-ai/headroom/issues/940),
[uv 0.12.5](https://github.com/astral-sh/uv/releases/tag/0.12.5) e
[Python 3.13.15](https://www.python.org/downloads/release/python-31315/).

### Plano de controle de sessão

Os metadados de sessão são um domínio explícito sob o diretório de estado XDG. IDs aleatórios,
arquivos privados atômicos e marcadores de início de PID do Linux permitem monitorar o ciclo de vida sem
criar outro armazenamento de conversas. Prompts, resultados, tokens e ambientes brutos
são excluídos. Sessões diretas chamam o mesmo runtime interativo que `ivoai codex` e
`ivoai claude`; Ruflo não é acionado.

Primários simultâneos não compartilham estado de ciclo de vida: cada sessão tem seu próprio registro,
identidade de processo e subárvore de runtime, e cada comando Ruflo executa com essa subárvore
tanto como diretório de trabalho quanto como `HOME` isolado. Os armazenamentos de sessão e cota serializam
mutações entre processos com locks consultivos e publicam arquivos completos por substituição
atômica. As páginas compartilhadas de `ai-memory` são a exceção deliberada: elas formam o
plano comum de conhecimento durável usado por todos os clientes oficiais.

Sessões orquestradas têm um critério rigoroso de admissão: o perfil seguro deve corresponder à sua
lista revisada de ferramentas permitidas, a execução de provedores e a memória durável do Ruflo devem ser false, Ruflo deve
passar por um comando de versão/integridade, um swarm hierárquico real deve retornar um ID verificável
e uma tarefa primária opaca deve ser registrada antes de o cliente oficial iniciar.
Ruflo recebe um ambiente limpo com `RUFLO_PROVIDER=ivoai-disabled` e
`CLAUDE_FLOW_MEMORY_BACKEND=memory`.

O MCP stdio local `ivoai-orchestrator` é injetado apenas durante a vida útil desse
primário. Ele mapeia a delegação para executáveis oficiais confiáveis do Codex/Claude e mapeia
IDs opacos de ciclo de vida para comandos de tarefas do Ruflo. As evidências exatas dos workers são gravadas primeiro
no WorkingContext ArtifactStore privado e transitório. A ponte fornece ao primário
apenas um `WorkerResult` limitado e neutro em relação ao provedor (resumo, descobertas, um
`StateDelta` consultivo e `ResultRef`s opacos). O estado da sessão armazena apenas essas referências
limitadas, nunca os corpos dos workers. Ferramentas locais explícitas somente leitura recuperam um
artefato exato ou um intervalo limitado após validar propriedade, TTL, tamanho e digest. A
concorrência dos workers tem valor padrão de dois e limite rígido de três. A ponte não é registrada
no gateway público do servidor nem roteada por ele. Consulte [orchestration.md](orchestration.md)
e [WorkingContext](working-context.md).

Planos automáticos adicionam ao estado da sessão IDs de tarefas contendo apenas metadados, dependências, pontuações, níveis, origens
dos perfis, estado de execução, duração e motivos de escalonamento. Instruções completas
de tarefas, conteúdo de SharedContextBrief, respostas de workers, credenciais e
ambientes nunca são enviados ao Ruflo. Respostas exatas permanecem privadas no
ArtifactStore transitório; metadados de sessão e transferências de failover contêm apenas ResultRefs. Os workers
Codex executam em sandbox somente leitura e desabilitam MCPs herdados, exceto as ferramentas de leitura gerenciadas
de Memory/Context. Os workers Claude usam configuração MCP estrita com escopo de processo e modo
de permissão plan com ferramentas de mutação desabilitadas. Apenas o primário aplica alterações.

ai-memory 1.29.0 é a camada de memória operacional durável. Seus binários nativos
versionados e pacote de hooks oferecem suporte a Codex e Claude Code e publicam checksums
por plataforma. Os hooks enfileiram com latência limitada e tratam falhas de rede, autenticação e
serviço como não fatais; a falha de um hook resulta no caminho de permissão/saída zero
apropriado ao host. Conectar um servidor reescreve apenas os metadados de endpoint
de memória pertencentes ao ivoai. A release upstream autoritativa é
[ai-memory v1.29.0](https://github.com/akitaonrails/ai-memory/releases/tag/v1.29.0); seu changelog foi
revisado porque ela é mais recente que o mínimo de 1.28.1 nos requisitos do produto.
IvoAI instala hooks de ciclo de vida com a estratégia de projeto `repo-root` do ai-memory. A resolução
de projeto, portanto, usa o nome do repositório Git principal entre aliases de caminhos Linux/WSL,
subdiretórios e worktrees vinculadas, em vez de dividir observações pela forma como o processo
cliente representa o diretório atual. Consultas MCP explícitas ainda incluem o
escopo global de acordo com a semântica normal de consulta do ai-memory.

Ruflo 3.38.12 é instalado para workflows, coordenação e skills. O ivoai registra um
perfil MCP de privilégio mínimo contendo apenas ferramentas de coordenação, memória
temporária local ao processo e um wrapper que remove variáveis de credenciais de provedores PAYG
suportados antes de Ruflo iniciar. A execução de provedores e a memória durável do Ruflo permanecem
desabilitadas; Codex e Claude continuam executando por meio de seus clientes oficiais
autenticados por assinatura. As issues upstream
[#2356](https://github.com/ruvnet/ruflo/issues/2356) e
[#2962](https://github.com/ruvnet/ruflo/issues/2962) permaneciam abertas e mostravam execução
direta selecionando provedores configurados separadamente, portanto esses caminhos são deliberadamente
excluídos do perfil padrão.

### Identidade e registro MCP

O registro de conexões modela entradas MCP uniformemente por ID estável, transporte,
URL/argv, escopos, propriedade e status de habilitação. Context e memória usam o mesmo
registro que os MCPs adicionados pelo usuário; renderizadores específicos de cada agente são adaptadores de borda, não
fontes de verdade separadas.

A identidade padrão do cliente ivoai fora de um projeto explicitamente inicializado é
`host:<normalized-hostname>`. Ela é independente do diretório atual, portanto `/etc`,
`/opt` e `/var/lib` não se tornam projetos acidentais. `ivoai project init`
cria um ID determinístico a partir da raiz Git absoluta e um marcador local `.ivoai.toml`
que substitui a identidade do host. Simplesmente entrar em um repositório Git não
altera silenciosamente a identidade do ivoai. O escopo de ciclo de vida do ai-memory é uma questão separada e
usa a estratégia estável do repositório principal descrita acima.

## Arquitetura do servidor

### Runtime e persistência

Os hosts iniciais suportados são Ubuntu 22.04+, Ubuntu 24.04+ e Debian 12 em amd64 ou
arm64. Os processos de gateway e contexto do ivoai são serviços systemd executados como
usuários distintos sem login, `ivoai-gateway` e `ivoai-context`, no grupo compartilhado
`ivoai`. As unidades usam `Restart=on-failure`, `NoNewPrivileges=yes`, uma
`UMask` restritiva, remoção de capabilities, visualizações ocultas de processos, diretórios temporários privados
e listas de permissão de escrita limitadas aos próprios caminhos de dados. Os contêineres de backend usam a
identidade separada `ivoai`, sem login.

Estrutura:

```text
/etc/ivoai/                 non-secret server configuration
/etc/ivoai/secrets/         0700 directory, 0600 secret files and managed TLS copies
/var/lib/ivoai/context/     metadata, normalized corpus and ingestion state
/var/lib/ivoai/memory/      ai-memory authoritative data
/var/lib/ivoai/qdrant/      rebuildable vector index
/var/lib/ivoai/qdrant-snapshots/ Qdrant snapshot workspace
/var/lib/ivoai/qdrant-init/ Qdrant non-secret initialization marker
/var/lib/ivoai/models/      pinned embedding model snapshot
/var/lib/ivoai/enrollment/  hashed enrollment/client credential records
/var/lib/ivoai/backups/     versioned backup archives
/run/ivoai/                 sockets/PIDs and other ephemeral state
```

O stdout e o stderr dos serviços vão para journald por padrão. Os logs e
diagnósticos do servidor renderizados pela CLI passam pelo mecanismo de ocultação do ivoai para formas rotuladas de autorização, códigos de cadastro
e formatos comuns de chaves de API. Os handlers de autenticação não registram corpos de requisição nem
valores de credenciais; os operadores ainda precisam revisar a saída do journald antes de compartilhá-la.

Docker Compose é reservado para Qdrant, Text Embeddings Inference e ai-memory. Cada
imagem é referenciada por digest imutável, está conectada a uma rede interna e
não tem porta no host, a menos que uma porta de compatibilidade restrita a loopback seja inevitável. O ivoai gerencia
o projeto Compose e os volumes de forma idempotente. Os próprios serviços Go do ivoai continuam sendo executáveis
systemd comuns e não exigem uma imagem privada do ivoai. A configuração exige Docker Engine
28.0.0 ou mais recente porque a prioridade determinística de gateway faz parte do limite de saída de rede
do backend; ela valida o daemon antes de gravar os artefatos do servidor e não substitui
a instalação do Engine do operador. Quando um Compose compatível não está empacotado, o ivoai
instala o plugin CLI Docker Compose 5.5.0 revisado para amd64 ou arm64 a partir de sua
release oficial, verifica o SHA-256 do manifesto e nunca seleciona um artefato flutuante
`latest`. As cópias do certificado e da chave
de TLS direto são arquivos `0600` pertencentes ao serviço em `/etc/ivoai/secrets/tls`. Depois que systemd
carrega seus arquivos de ambiente de Qdrant e embedding, o sandbox do serviço de contexto torna
toda a árvore de segredos, o estado de cadastro e o estado de memória inacessíveis. O
gateway tem uma lista de negação separada para dados de memória, Qdrant, modelos e
corpus no sistema de arquivos que estejam fora de suas responsabilidades de plano de controle.

### Protocolo versão 1

A resposta pública de descoberta não contém informações sensíveis. O gateway aplica
`Cache-Control: no-store` de forma consistente às respostas de descoberta e da API:

```json
{
  "protocol_version": 1,
  "server_version": "0.1.0",
  "public_base_url": "https://ai.example.com",
  "health_endpoint": "/health",
  "ready_endpoint": "/ready",
  "enrollment_endpoint": "/v1/enroll",
  "context_mcp_endpoint": "/v1/mcp/context",
  "memory_mcp_endpoint": "/v1/memory/mcp",
  "memory_hooks_endpoint": "/v1/memory",
  "web_mcp_endpoint": "/mcp",
  "oauth_authorization_server_metadata": "/.well-known/oauth-authorization-server",
  "features": {"context": true, "memory": true, "memory_hooks": true, "web_mcp": true, "oauth_pkce": true, "remote_admin_read_only": true}
}
```

O gateway expõe essa resposta em `GET /.well-known/ivoai`. `/health` indica que o processo
está ativo e não depende de conectores opcionais. `/ready` exige um
serviço de contexto íntegro; uma dependência ai-memory indisponível é informada como `ready_degraded`
para que o contexto continue utilizável.

Antes de consumir um código de cadastro, o cliente exige TLS válido segundo a Web PKI, um
servidor íntegro ou pronto em estado degradado e a versão principal 1 do protocolo. Após o cadastro, a
credencial entregue uma única vez e o registro MCP são persistidos antes das sondagens de contexto e memória.
Falhas nas sondagens tornam-se avisos explícitos de degradação, de modo que um código consumido nunca
deixe a credencial emitida sem persistência. Redirecionamentos para outra origem são rejeitados durante a
descoberta e o cadastro. Texto sem criptografia é aceito apenas em loopback para desenvolvimento ou
atrás de um proxy reverso no mesmo host que termina TLS. Um proxy reverso remoto exige um
CIDR de origem explícito e a imposição de HTTPS encaminhado; outros listeners fora de loopback
exigem configuração de TLS direto.

Os endpoints usam corpos JSON limitados e timeouts de requisição, leitura, escrita e inatividade
no nível do servidor. Endpoints MCP aceitam JSON-RPC sobre HTTP e exigem autenticação bearer.
Alterações de protocolo que preservam a versão 1 são aditivas; remover ou alterar um campo
ou ferramenta exige uma nova versão principal do protocolo. Migrações de banco de dados e de coleções Qdrant
têm suas próprias versões de esquema monotonicamente crescentes.

### Cadastro e autorização

`ivoai server enrollment create` gera 256 bits secretos a partir do CSPRNG do sistema
operacional e exibe uma única vez o código de uso único em base64url. Como esses códigos aleatórios de alta entropia
resistem a tentativas de adivinhação offline, o servidor armazena apenas um hash SHA-256, junto
com o ID, horário de criação, expiração, escopos e timestamps de consumo/revogação. O
TTL padrão é de 10 minutos.

Os códigos são comparados em tempo constante. As mutações adquirem um lock exclusivo de arquivo do sistema operacional, e uma
troca bem-sucedida substitui atomicamente o arquivo de estado depois de marcar o código como
consumido. Isso impede que processos independentes da CLI e do gateway consumam ou
sobrescrevam o mesmo estado simultaneamente. Códigos e credenciais retornadas nunca são
colocados em exemplos de argv, URLs ou logs; há suporte a entrada padrão e a um prompt
interativo sem eco.

A troca retorna uma única vez uma credencial bearer aleatória com escopo de cliente. Apenas seu hash e
metadados são armazenados no servidor, no backend local de cadastro acessível somente ao proprietário em
`/var/lib/ivoai/enrollment/state.json`; o cliente armazena o valor em um arquivo
de segredos `0600`. Os escopos iniciais são `context:read`, `memory:read`, `memory:write`, `status:read`,
`doctor:read` e `connector:read`. O cadastro nunca implica permissão para
mutação administrativa. A revogação é imediata. Códigos inválidos, expirados, consumidos e revogados
compartilham um único erro externo uniforme, para que o estado do código não seja divulgado.

### Web MCP e OAuth

ChatGPT Web e Claude Web usam um limite separado de integração pública. `/mcp` é um
endpoint MCP Streamable HTTP construído sobre o MCP Go SDK oficial com versão fixada. Ele agrega
contexto e memória por trás de uma única URL de conector, preservando as rotas MCP existentes dos clientes
nativos. `initialize` anuncia instruções do servidor, capacidades de ferramentas
e a extensão limitada de skills; as ferramentas retornam `structuredContent` tipado mais uma representação
textual para compatibilidade.

A superfície de ferramentas Web é intencionalmente mais restrita que a dos serviços subjacentes:

- `context_search`, `context_get_document`, `context_recent` e `context_health`
  são somente leitura e rotulam os documentos recuperados como dados não confiáveis;
- `memory_query`, `memory_recent`, `memory_read_page` e `memory_status` exigem
  `memory:read`;
- `memory_write_page` e `memory_feedback` exigem `memory:write`;
- `memory_delete_page` exige `memory:delete`, um caminho de página normalizado e confirmação
  explícita na chamada.

Manutenção, autoaperfeiçoamento, orquestração de transferências, ferramentas upstream arbitrárias e
comandos do host não são publicados por essa fachada. A perda de ai-memory degrada as ferramentas
de memória sem tornar indisponíveis as ferramentas de contexto ou a verificação de atividade do gateway.

OAuth segue Authorization Code com PKCE S256. Metadados do recurso protegido e do servidor
de autorização oferecem suporte à descoberta de conectores Web; o registro dinâmico de clientes aceita
apenas URIs de redirecionamento HTTPS validadas (além do caso de desenvolvimento em loopback
explicitamente suportado). Os códigos de autorização duram cinco minutos, os access tokens, uma hora, e
os refresh tokens rotativos, 30 dias. O fluxo de consentimento no navegador exige adicionalmente um
código de ativação de uso único e curta duração criado pelo administrador local, evitando um
novo banco de dados de senhas.

A persistência OAuth no servidor armazena apenas hashes de códigos de ativação, códigos
de autorização, access tokens e refresh tokens. As mutações usam locks e são atômicas. Os escopos são
verificados novamente no limite da ferramenta. A validação de origem, a verificação de PKCE, a ida e volta de `state`,
a correspondência exata de redirecionamento, a rotação de tokens e a revogação limitam a substituição
de tokens e ataques de autorização entre sites.

O valor `resource` da RFC 8707 é a URL pública canônica de `/mcp`. Ele é preservado em
códigos de autorização, access tokens, refresh tokens rotativos e em cada requisição
MCP autenticada, para que as credenciais não possam ser reutilizadas contra um público diferente.

### Entrega de skills por MCP

O `skills/ivoai-memory-context/SKILL.md` do repositório também está disponível por meio de
`skills/list`, `skills/get` e `resources/read`. O recurso anunciado usa uma
URI `skill://` e um digest para que clientes compatíveis possam importar um snapshot fixo. Uma release
também publica o mesmo diretório como `ivoai-memory-context.zip` para importação como
Skill personalizada do Claude. As instruções de inicialização MCP, as descrições das ferramentas de leitura e a skill
declaram a mesma ordem `memory → Context → web`. Importar instruções não pode
garantir tecnicamente que um modelo Web invoque uma ferramenta em todos os turnos; a plataforma
mantém o controle final da seleção de ferramentas.

A administração remota é uma lista explícita de operações tipadas permitidas, como status,
resumo do doctor e lista de conectores. Não há parâmetro de comando do host,
parâmetro de caminho de executável, shell, primitiva de escrita de arquivo nem endpoint de proxy genérico.

## Context e RAG

O núcleo de contexto permanece íntegro com zero conectores e zero documentos. Um conector
implementa descoberta e leituras em streaming para uma interface de documento normalizado; ele
não pode escrever no sistema de arquivos do servidor fora de sua origem configurada. Os adaptadores v0.1 são:

- `filesystem`: uma raiz local configurada; a travessia de diretórios ignora nomes inseguros e entradas de symlink;
- `git`: um checkout local existente enumerado com `git ls-files` limitado e depois filtrado pelas mesmas regras de documentos.

Futuros adaptadores Drive, S3, Notion, HTTP, GitHub e MCP genérico implementam a mesma
interface sem alterar a divisão em chunks, o armazenamento ou as ferramentas do agente. As credenciais dos conectores
são segredos separados com escopo definido.

Pipeline:

```text
connector -> canonical document -> sensitive/binary filter -> deterministic chunks
          -> local embedding -> versioned Qdrant collection -> read-only MCP tools
```

A normalização registra um ID de documento estável, origem, caminho relativo, timestamps e
metadados do conector. O filtro exclui arquivos de segredos, credenciais, estado de nuvem e
chaves; conteúdo binário; arquivos não regulares; estruturas internas ocultas de VCS; e arquivos acima de 8 MiB.
A enumeração Git desabilita hooks e programas fsmonitor controlados pelo repositório.

A ingestão abre a raiz do conector e cada componente do caminho com semântica no-follow
no nível do sistema operacional, rejeita travessia e arquivos especiais, lê pelo descritor verificado
e impõe cotas agregadas de documentos, bytes e chunks. A substituição do catálogo é
feita em lotes. A reingestão reconcilia documentos que desapareceram, enquanto a remoção de um conector
elimina as entradas de catálogo e os vetores Qdrant dessa origem antes de excluir sua entrada
no registro.

Os IDs dos chunks derivam do ID do documento, do índice do chunk e do texto do chunk. A política padrão
usa até 1,200 pontos de código Unicode com sobreposição de aproximadamente 150 pontos de código e
prefere limites de nova linha ou palavra. A reingestão é determinística, mas Qdrant
atualmente exclui os pontos antigos de um documento antes de enviar sua substituição. Uma falha
no envio, portanto, exige outra execução de ingestão.

A coleção Qdrant é `ivoai-context-v1-d384`; Qdrant 1.19.0 usa o digest fixado da imagem
upstream sem privilégios e de múltiplas arquiteturas. Seu mapeamento no host é
restrito a loopback (`127.0.0.1:6333`) e exige uma chave de API gerada. Embeddings e
ai-memory usam credenciais geradas separadas. Docker usa uma rede transitória de publicação
para estabelecer os vínculos de loopback e uma rede separada de download de modelos com
a prioridade de gateway mais alta definida explicitamente. Depois que o modelo de embedding está íntegro, systemd
desconecta a rota de download. A rede de publicação em loopback permanece conectada para
que os mapeamentos no host continuem válidos, mas desabilita IP masquerading e, portanto, não fornece
saída de rede ao backend. O índice é um cache; documentos de origem,
metadados normalizados e definições de conectores são autoritativos. Uma coleção pode
ser excluída e reconstruída de forma determinística.

As ferramentas do agente são `context_search`, `context_get_document`, `context_recent` e
`context_health`. Elas são somente leitura e retornam metadados de origem. As contagens de resultados de busca e de recentes
são limitadas a 100; `context_recent` omite os corpos dos documentos; e
`context_get_document` retorna no máximo um documento, sujeito ao limite de ingestão de 8 MiB.
A ingestão e a administração de conectores usam comandos autenticados e
rotas de API separados.

O texto dos documentos é dado não confiável. As descrições e os resultados das ferramentas o rotulam dessa forma e
nunca encaminham comandos fornecidos por documentos para instaladores, shells, configuração
de conectores ou decisões de autorização.

### Decisão sobre embedding local

Text Embeddings Inference (TEI) 1.9.3 foi escolhido em vez de uma API de nuvem porque é
Apache-2.0, mantido pela Hugging Face, expõe uma pequena API HTTP, suporta contêineres
CPU e documenta tanto x86_64 quanto aarch64. A imagem amd64 e a imagem upstream
arm64 `cpu-arm64-sha-30507cb` são fixadas individualmente por digest imutável de índice OCI;
nenhuma arquitetura usa uma tag mutável em runtime. Fontes:
[TEI v1.9.3](https://github.com/huggingface/text-embeddings-inference/releases/tag/v1.9.3) e
[TEI](https://github.com/huggingface/text-embeddings-inference).

O modelo padrão é `intfloat/multilingual-e5-small` na revisão imutável
`614241f622f53c4eeff9890bdc4f31cfecc418b3`, com seu hash safetensors registrado no
manifesto. Ele tem licença MIT, 384 dimensões, suporta 94 idiomas, tem limite de 512 tokens
e é adequado para recuperação em português/inglês priorizando CPU, com consumo moderado
de recursos. Os prefixos obrigatórios `query:` e `passage:` do E5 são aplicados centralmente, e
o nome da coleção Qdrant inclui a versão dimensional/do esquema. Fonte do modelo:
[Revisão fixada de multilingual-e5-small](https://huggingface.co/intfloat/multilingual-e5-small/tree/614241f622f53c4eeff9890bdc4f31cfecc418b3).

TEI foi preferido ao embedding diretamente dentro do gateway Go porque o isolamento
de processos limita o impacto de falhas do modelo nativo e permite verificações de integridade, reinicializações e
limites de recursos independentes. FastEmbed 0.8.0 é uma alternativa futura plausível com menor consumo de recursos,
mas adicionaria um runtime Python dentro do processo de contexto ou outro serviço
feito sob medida sem melhorar o limite HTTP atual.

## Backup e restauração

Um backup grava um manifesto versionado e inclui a configuração do servidor com valores
secretos excluídos, metadados de contexto e o corpus autoritativo normalizado, definições
de conectores sem credenciais, dados persistentes do ai-memory e metadados de reconstrução
do índice. O estado de cadastro e de credenciais de clientes é excluído. Os dados do Qdrant são
reconstruíveis e nunca são a única fonte recuperável.

Os arquivos são criados sob um diretório de backup com modo `0700`, usando um nome temporário.
Eles rejeitam symlinks e arquivos especiais e são finalizados atomicamente. A CLI para
os serviços gerenciados de gateway, contexto e dependências antes do backup ou da restauração e
os inicia novamente depois.

A restauração prepara e valida o formato, os tamanhos limitados das entradas e os caminhos seguros; rejeita
links e travessia; e então grava arquivos individuais atomicamente, sem restaurar
segredos. Ela mescla os dados nas raízes gerenciadas e não remove arquivos obsoletos. A restauração aceita
apenas um caminho local absoluto explícito para o arquivo e não é exposta pela API
remota.

## Invariantes de segurança

Os limites de skills neutros em relação ao provedor são implementados por uma base separada e versionada
de Skill Control Plane. Registro, indexação apenas de metadados, resolução de dependências e
conflitos, política de negação por padrão e preparação genérica da cadeia de suprimentos
sem execução estão documentados em [skill-control-plane.md](skill-control-plane.md). A
base é aditiva: o caminho atual de sessão não exige um registro
preenchido, e nenhuma skill externa pode assumir autoridade de orquestrador.

- Segredos nunca entram no TOML principal, em URLs, diagnósticos ou logs. O ivoai não injeta
  segredos armazenados no argv de subprocessos; o cadastro usa entrada padrão ou um prompt
  sem eco por padrão.
- Subprocessos de instaladores e sondagens usam argv estruturado, contextos limitados e ambientes
  mínimos. Subprocessos de agentes interativos usam argv estruturado e encaminhamento
  de sinais, mantendo o ambiente compatível do usuário exigido pelos clientes
  dos fornecedores.
- A validação HTTPS está ativada por padrão; nenhuma opção de produção insecure-skip-verify é persistida.
- Todas as camadas opcionais têm timeouts limitados ou circuit breakers. Em caso de falha, elas permitem prosseguir apenas
  para iniciar o agente local oficial, nunca para autorização.
- Dados de conectores e RAG não são confiáveis e não podem invocar ferramentas nem alterar configurações.
- Qdrant e as superfícies administrativas internas não são públicos.
- O cadastro é de uso único, tem curta duração, usa locks entre processos e é persistido atomicamente.
  As credenciais de clientes são armazenadas como hashes no servidor, têm escopo definido e são revogáveis.
- Escritas atômicas rejeitam propriedade, modos e destinos de symlink inseguros.
- Doctor oculta valores e informa apenas permissões, versões, acessibilidade, TLS e compatibilidade de protocolo.

## Decisões upstream e incertezas conhecidas

| Componente | Versão fixada | Decisão e incerteza em 2026-08-23 |
|---|---:|---|
| Codex CLI + code-mode host | 0.148.0 | Artefatos oficiais da mesma release; `codex login` suporta assinaturas do ChatGPT, e o host com checksum separado preserva a superfície gerenciada de ferramentas/MCP. |
| Claude Code | 2.1.228 | Canal oficial stable; latest era 2.1.237. Binário externo proprietário sujeito aos termos da Anthropic. |
| Headroom | 0.36.0 | Release atual do PyPI/GitHub com wheels amd64/arm64; a integração de evolução rápida exige uma sondagem smoke na configuração e fallback direto. |
| uv / CPython | 0.12.5 / 3.13.15 | Par exato de instalador/runtime privado para Headroom, com restrições incorporadas com hashes específicos por arquitetura. |
| ai-memory | 1.29.0 | Lançado recentemente na data da validação, com changelog e hashes por arquitetura revisados. O tempo limitado de observação em uso é mitigado pela versão exata fixada, pelo timeout dos hooks e pelo isolamento de falhas. |
| Ruflo | 3.38.12 | Fixação exata de integridade npm. Inferência direta/roteamento de provedores continua inadequado ao padrão sem PAYG e está desabilitado. Os nomes do pacote/repositório mudaram ao longo do tempo, portanto atualizações exigem revisão de proveniência. |
| Qdrant | 1.19.0 | Release Apache-2.0 atual e digest OCI de múltiplas arquiteturas; apenas interno. |
| TEI | 1.9.3 | Serviço HTTP CPU maduro; as imagens upstream amd64 e arm64 são fixadas independentemente por digest de índice OCI. |
| multilingual-e5-small | `614241f…` | Revisão imutável do modelo MIT; 384 dimensões/94 idiomas. A qualidade da recuperação deve ser medida no corpus do usuário antes de alterar modelos. |

Nenhuma conta externa, servidor privado ou credencial real de proprietário foi usada durante esta
descoberta. Não se presume compatibilidade com releases upstream futuras. Uma release do ivoai
que altere uma versão fixada deve revisar esta tabela e o manifesto somente depois que
os testes automatizados de instalação, auth-status, wrapper e isolamento de falhas passarem.
