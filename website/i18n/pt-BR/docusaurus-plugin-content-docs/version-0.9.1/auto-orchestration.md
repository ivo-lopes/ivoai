# Orquestração automática

`ivoai auto` é o modo conversacional ciente de quota. Seu frontend interativo é uma
TUI OpenCode gerenciada e privada. O IVOAI permanece como control plane e invoca a CLI
oficial do Codex ou Claude Code como executor selecionado; inference nunca é enviada
pelo Ruflo ou por uma credencial de provider copiada.

## Início e ownership da conversa

```sh
ivoai auto
ivoai auto --planner codex
ivoai auto --planner claude
```

Sem flag, o prompt mostra quota de assinatura em cache e usa o planner configurado
(`codex` por default) quando Enter é pressionado. OpenCode é o frontend enquanto o
executor oficial selecionado é planner/primary. O IVOAI é:

- proprietário lógico da sessão e autoridade de executor/quota;
- fonte dos limites de policy, skill, knowledge e WorkingContext;
- único caminho autoritativo de dispatch;
- consolidador de resultados limitados dos workers.

O IVOAI mantém autoridade final sobre a economicidade da delegação e sobre a seleção
de quota, provider, modelo e effort.

OpenCode recebe apenas uma capability efêmera de provider loopback e mostra o status do
IVOAI pelo plugin TUI correspondente à versão. Codex recebe instruções developer locais
à sessão por um override `-c` process-scoped. Claude recebe a mesma policy por seu
`--append-system-prompt-file` oficial; um arquivo `--settings` privado instala um comando
de statusline somente para aquele processo. A configuração persistente do usuário para
Codex e Claude não é reescrita.

## Seleção de modelo e reasoning

O model picker do OpenCode gerenciado é o único seletor interativo de uma conversa AUTO.
Ele expõe `IVOAI Automatic Orchestration`, além dos modelos e variantes de reasoning
reportados pelos clients oficiais instalados. `Ctrl+P` abre o model picker nativo e
`Ctrl+T` alterna somente as variantes de reasoning compatíveis com o modelo selecionado.

Com `IVOAI Automatic Orchestration`, a policy de quota e capability escolhe executor e
modelo default do client. Selecionar uma entrada explícita do Codex ou Claude fixa aquele
executor e modelo para o turno: uma escolha explícita indisponível falha claramente em
vez de trocar de provider silenciosamente. O footer e o painel `/ivoai` mostram modo de
seleção, executor, modelo efetivo e nível de reasoning. Esses campos não sensíveis são
armazenados no mapping de sessão OpenCode ↔ executor e restaurados quando uma conversa
compatível é retomada.

Seleção de modelo não é login no provider. O modo gerenciado continua usando as CLIs
oficiais Codex e Claude Code e suas sessões de assinatura existentes; ele não habilita
OpenCode `/connect`, copia credenciais nem exige uma API key do provider.

As mesmas instruções locais à sessão definem um gate rígido de fontes de pesquisa. Para
qualquer pesquisa ou verificação externa, o primary tenta `ivoai-memory` primeiro e
`ivoai-context` depois, antes de usar busca web, browser ou outro connector externo.
Resultados vazios, indisponíveis, insuficientes ou desatualizados permitem a etapa web;
trabalho autocontido não dispara consultas artificiais. Adapters dos workers recebem a
mesma policy pelos mecanismos oficiais de instrução process-scoped.

## Sequência de inicialização

1. Criar um registro de sessão privado somente com metadata.
2. Testar Codex e Claude sem API keys do provider.
3. Resolver o provider solicitado pelo gate de quota/capability.
4. Se ele tiver hard limit confirmado, selecionar alternativa elegível e registrar o
   startup failover. Se ambos estiverem esgotados ou não autenticados, entrar em
   `BLOCKED` sem iniciar primary ou worker.
5. Verificar o safe mode do Ruflo e inicializar um swarm real sem provider.
6. Registrar a tarefa opaca de lifecycle do primary.
7. Iniciar o backend OpenCode autenticado em `127.0.0.1`, anexar a TUI com tema IVOAI e
   rotear prompts ao executor oficial selecionado pela bridge local.
8. Anexar os MCPs `ivoai-orchestrator` e knowledge locais à sessão àquele executor
   oficial por sua configuração process-local, usando exatamente um CompressionProvider
   compatível.

Nenhuma credencial `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, OpenCode `/connect` ou outra
PAYG é aceita como fallback. A autenticação Codex e Claude permanece nos clients
oficiais. A bridge vê saída estruturada da CLI, nunca arquivos de token ou credencial.
Variáveis de ambiente com chaves do provider são removidas dos probes de quota e workers.
Ruflo mantém `provider_execution=false` e não recebe prompts/resultados.

## Planejamento do primeiro turno e conhecimento compartilhado

A primeira solicitação substancial tem protocolo imposto, não uma convenção opcional de
prompt:

1. tentar exatamente uma consulta limitada em `ivoai-memory` e depois uma em
   `ivoai-context` antes de qualquer consulta Web;
2. salvar um SharedContextBrief sem segredos e limitado à sessão por
   `orchestration_bootstrap`;
3. inspecionar quota e estado de capability do runtime;
4. decompor trabalho não sobreposto em um DAG de dependências e fornecer sete sinais
   limitados de tarefa;
5. chamar `orchestration_plan`, que calcula scores, tiers de execução, delegação
   econômica e profiles;
6. enfileirar trabalho independente com `orchestration_spawn_batch`, continuar trabalho
   útil do primary e então aguardar por notificação, não polling;
7. validar resultados, escalonar somente com evidência, sintetizar e criar checkpoint.

O conteúdo do brief fica apenas em arquivo runtime privado. O JSON da sessão armazena
hash, timestamp, health das fontes e quantidade de referências. Workers recebem o mesmo
brief, evitando repetir a consulta inicial de Memory/Context. Eles podem fazer consulta
adicional quando o brief limitado realmente não tiver um detalhe necessário. Turnos
posteriores relacionados usam delta planning; uma mudança material de objetivo ou
projeto atualiza o brief.

## Scheduling e delegação do DAG

`orchestration_delegate` continua disponível para delegação síncrona compatível com
versões anteriores. O protocolo automático default usa `orchestration_plan`,
`orchestration_spawn`/`orchestration_spawn_batch` assíncronos, `orchestration_wait` e
`orchestration_primary_complete`. Tarefas são limitadas a 12 e workers a três (dois por
default). Dependências desconhecidas, ciclos, trabalho duplicado, labels inseguros,
campos arbitrários ou scores fora do intervalo são rejeitados.

O IVOAI calcula o capability score e o mapeia para LIGHT, BALANCED, STRONG ou MAX. Ele
compara separadamente o ganho de paralelismo/qualidade com o overhead de startup do
worker e transferência de contexto. Uma tarefa trivial fica no `primary` mesmo quando
um modelo pede delegação. Workers com dependências satisfeitas iniciam simultaneamente e
retornam IDs imediatamente; tarefas dependentes ficam na fila até todos os pré-requisitos
terminarem.

A saída exata do worker é persistida primeiro no WorkingContext ArtifactStore privado. O
primary recebe apenas um `WorkerResult` limitado com resumo, findings, `StateDelta`
consultivo e referências opacas à evidência. A saída completa nunca é interpolada
automaticamente em instrução do primary, SharedContextBrief, checkpoint, handoff ou JSON
da sessão.

Todo worker passa pelo router de quota e capability. Clients oficiais executam inference
(`codex exec` ou `claude --print`); workers Codex usam sandbox somente leitura e
allowlists MCP de leitura, workers Claude usam configuração MCP process-scoped rígida e
plan mode com tools de mutação desabilitadas, e Ruflo registra somente estado opaco de
lifecycle. Quando a evidência limitada é insuficiente, o primary pode recuperar
explicitamente um artifact exato ou intervalo de bytes validado pelo MCP orchestrator
local. Referências pertencem à sessão e sobrevivem a failover Codex/Claude sem copiar
bodies. Budgets de armazenamento e prompt são separados: o primeiro preserva evidência,
enquanto o segundo limita apenas contexto automático. Consulte
[Scheduler automático e roteamento de modelos](auto-scheduler.md) e
[WorkingContext](working-context.md).

## Escalonamento progressivo

O trabalho começa no menor profile suficiente. O primary só pode chamar
`orchestration_escalate` depois de um resultado concluído ou com falha e deve fornecer
motivo baseado em evidência. Cada chamada avança exatamente um tier. Effort incompatível
volta ao default do client sem claim falsa de capability; um modelo exato esgotado leva
à consideração de outro modelo/provider suficiente antes de bloquear a tarefa.

## Checkpoints e failover

Quando checkpoints automáticos estão habilitados, o primary recebe instrução para salvar
um resumo sem segredos e limitado após trabalho materialmente concluído. Ele contém
objetivo, decisões, trabalho concluído, nomes de arquivos alterados, verificações
importantes, trabalho pendente, blockers e próximo passo. Nunca contém prompt, response,
transcript, credencial, header ou resposta de autenticação do provider completos.
Conteúdo com formato de segredo e bytes de controle do terminal são rejeitados. O
checkpoint runtime privado fornece ao supervisor um limite imediato de failover; hooks
normais do ai-memory fornecem continuidade operacional durável em torno da mesma sessão
oficial do agent.

Se o provider ativo reportar hard limit de assinatura, o supervisor:

1. bloqueia novo trabalho para o provider e marca o cache de quota;
2. encerra e coleta somente o process group registrado do primary;
3. verifica novamente o provider alternativo;
4. carrega o último checkpoint ou cria fallback explícito de interrupção;
5. lê metadata limitada de `git status` e diff-stat sem alterar a worktree;
6. reinicia a execução pela CLI oficial alternativa com o handoff, enquanto a mesma UI
   OpenCode permanece anexada;
7. registra primary atual, motivo, horário, fase e quantidade de failovers.

O loop automático para depois de dois failovers consecutivos sem novo checkpoint de
sucesso. Falhas de rede não são classificadas como esgotamento de quota. Pressão da
context window é uma métrica separada e nunca marca a quota de assinatura como esgotada.

## Observabilidade e estado

Use um segundo terminal:

```sh
ivoai monitor --watch
ivoai monitor --session <session-id> --json
```

O monitor renderiza janelas Codex de 5 horas e semanais pela duração oficial, uma linha
provider-wide individual sem inventar sua cadência e qualquer outra duração rolling,
como 1h ou 1d. Ele renderiza linhas Claude Code de 5 horas e semanais. Adiciona buckets
de contexto e específicos de modelo, além de horários de reset, somente quando há dado
autoritativo. Reporta fonte e horário de observação, primary atual/inicial, failovers,
disponibilidade de checkpoint, health/quantidade de referências do bootstrap, DAG de
tarefas, score, tier, provenance de model/effort, modo de execução, dependências,
duração, uso do Headroom, workers, Ruflo, context, ai-memory, estado do server e últimos
eventos limitados e sem segredos do control plane. `status` lê o cache de quota limitado
enquanto executa probes curtos e paralelos de health de Server e Ruflo; não faz probe
pesado de quota do provider. `doctor` executa verificações ativas mais profundas de
capability. Probes de version/help do Headroom determinam somente instalação e
compatibilidade, não validação de launch interativo.

O JSON da sessão fica abaixo de `$XDG_STATE_HOME/ivoai/sessions`; o cache de quota fica
abaixo de `$XDG_STATE_HOME/ivoai/quota`. Diretórios são `0700`, arquivos são `0600`,
escritas são atômicas e writers concorrentes de quota usam advisory file lock.

Várias sessões automáticas ou explícitas podem executar ao mesmo tempo. IDs de sessão,
diretórios runtime, marcadores de PID/start do primary, sockets locais do orchestrator e
homes do Ruflo são independentes; portanto, operações de cleanup e stop não selecionam
outra sessão live por recência. O ai-memory compartilhado é comum por design;
observações de lifecycle usam o nome principal do repositório para que Codex e Claude
concordem mesmo quando aliases de cwd são diferentes.

## Isolamento de falhas

- Indisponibilidade de Server, context ou ai-memory não interrompe o primary oficial.
- Falha do Headroom usa o fallback existente para o client Direct.
- Falha do Ruflo interrompe a orquestração automática, mas não afeta `ivoai codex` ou
  `ivoai claude`.
- Quota pending, não exposta e stale são estados distintos e nunca viram `0%`.
- Dois providers confirmadamente esgotados produzem estado waiting/blocked limitado; o
  ivoai não tenta para sempre nem ativa inference PAYG.
- Falha do WorkingContext não ativa fallback de prompt com saída bruta: o resultado
  estruturado é explicitamente degradado e o primary oficial permanece utilizável sem
  evidência externa do worker.
