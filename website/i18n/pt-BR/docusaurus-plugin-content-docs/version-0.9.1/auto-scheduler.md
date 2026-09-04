# Scheduler automático e roteamento de modelos

`ivoai auto` usa um policy engine in-process e o MCP local à sessão
`ivoai-orchestrator`. A TUI OpenCode pinada é o frontend; o IVOAI permanece como
proprietário da conversa e único writer autoritativo, e a CLI oficial do Codex ou
Claude Code é o executor selecionado. O scheduler inicia apenas workers consultivos,
limitados e somente leitura por esses mesmos clientes oficiais de assinatura.

## Primeiro turno substancial

O primary segue esta ordem:

```text
Memory lookup -> Context lookup -> SharedContextBrief -> task analysis
  -> validated DAG -> scores -> quota/capability routing -> async dispatch
  -> result validation -> primary synthesis -> bounded checkpoint
```

As primeiras tentativas de Memory e Context acontecem uma vez. `orchestration_bootstrap`
armazena um brief sem segredos de no máximo 64 KiB no diretório runtime privado da
sessão. O JSON da sessão retém apenas timestamp, status das fontes, quantidade de
referências e hash SHA-256. Workers recebem o brief automaticamente e consultam o
conhecimento compartilhado novamente apenas quando falta algum detalhe. Uma mudança
material de objetivo ou projeto exige novo bootstrap; turnos posteriores relacionados
usam delta planning.

Falhas de Memory e Context são independentes. Se uma ou ambas estiverem indisponíveis,
o brief registra a fonte degradada e a sessão pode continuar quando a tarefa ainda é
executável. O material recuperado é sempre dado não confiável.

## Planejamento e delegação econômica

`orchestration_plan` aceita no máximo 12 tarefas. Ele rejeita identificadores inseguros,
dependências desconhecidas, ciclos, texto de tarefa duplicado sem marcador explícito de
verificação independente, campos desconhecidos e scores fora de `0..100`. Ele nunca
aceita executável, comando shell, ambiente, endpoint ou credencial.

O planner fornece sete sinais limitados. O IVOAI calcula capability assim:

```text
score = round((30*complexity + 25*risk + 20*reasoning_depth
             + 15*verification_need + 10*context_breadth) / 100)
```

Pesos não negativos e diferentes do default são normalizados pela soma positiva. Os
tiers são:

| Score | Tier |
| ---: | --- |
| 0–24 | LIGHT |
| 25–49 | BALANCED |
| 50–74 | STRONG |
| 75–100 | MAX |

O planner pode propor delegação, mas o IVOAI tem autoridade final. A decisão
determinística compara:

```text
benefit  = round((45*parallel_value + 20*verification_need + 20*risk
                 + 15*context_breadth) / 100)
overhead = 25 + 20*(100-complexity)/100 + 5*latency_sensitivity/100
```

Um worker é usado somente quando `benefit > overhead`, a execução paralela está
habilitada e o planner marcou o trabalho como delegável. Caso contrário, a tarefa
permanece no primary. Isso mantém uma correção de digitação local enquanto permite
sobrepor trabalhos independentes de inventário, arquitetura e segurança.

## Resolução de capability e profile

A ordem da autoridade de roteamento é quota, registry de capabilities do runtime,
policy configurada, tier exigido e preferência do planner. Nomes de modelos nunca são
inventados.

- Modelos Codex e efforts de reasoning compatíveis vêm da resposta estruturada
  `model/list` do `codex app-server`. O IVOAI passa o modelo selecionado com `--model`
  e o effort verificado pela configuração process-scoped `model_reasoning_effort`.
- Claude Code expõe opções verificadas de effort no help de sua CLI oficial. Ele não
  possui catálogo estruturado equivalente na versão validada do client, portanto o
  IVOAI deixa o modelo vazio e usa o default do client oficial. Um effort verificado é
  passado com `--effort`.
- Se o effort explícito não for compatível, o IVOAI não envia nenhum e registra
  `effort_source=unsupported`; ele nunca rotula o default do client como effort
  confirmado.

A metadata de capability é armazenada em cache XDG privado, indexado pela versão do
client oficial. Update ou mudança de versão invalida o cache. Overrides de profile
vazios significam resolução automática. Um modelo configurado não vazio só é elegível
se existir no catálogo do runtime.

Para um tier exigido, o router seleciona o menor tier suficiente do catálogo. Ele
respeita janelas de quota do modelo exato, tenta outro modelo suficiente e então
considera o provider de assinatura autenticado alternativo. Quando vários profiles
satisfazem o piso de qualidade, a quota restante autoritativa pode preservar o provider
sob maior pressão. Telemetria desconhecida ou com falha não significa quota zero.

## Runtime DAG assíncrono

Sessões automáticas adicionam estes métodos MCP:

- `orchestration_bootstrap` — salva o SharedContextBrief privado;
- `orchestration_capabilities` — inspeciona metadata segura de model/effort do runtime;
- `orchestration_plan` — valida e resolve o DAG;
- `orchestration_spawn` — retorna imediatamente um ID de worker;
- `orchestration_spawn_batch` — enfileira tarefas independentes e inicia trabalho elegível em paralelo;
- `orchestration_primary_complete` — libera dependentes de trabalho do primary;
- `orchestration_wait` — aguarda any/all com timeout limitado e notificações;
- `orchestration_result` — lê um resultado limitado mantido na memória da bridge;
- `orchestration_escalate` — sobe um tier com motivo baseado em evidência;
- `orchestration_cancel` — cancela somente um worker pertencente à sessão.

O scheduler usa dois workers por default e limita rigidamente a concorrência a três.
Dependências precisam terminar antes que uma tarefa enfileirada comece. Prompts e
resultados ficam na memória da bridge, não no JSON da sessão ou no Ruflo. Budgets de
resultado de worker são limitados por tier, e prompts de workers solicitam conclusões,
fatos, evidências, problemas e recomendações em vez de narrativa longa.

Workers Codex executam com `--sandbox read-only`, desabilitam servidores MCP herdados e
permitem somente tools gerenciadas de leitura Memory/Context. Workers Claude usam uma
configuração MCP process-scoped rígida, modo de permissão plan e negações explícitas de
mutação em filesystem e memory. Ambos preservam acesso ao conhecimento compartilhado
pelo ambiente limitado do server. Ruflo recebe somente IDs opacos de lifecycle.

## Escalonamento, observabilidade e limites

O profile inicial é o menor suficiente. Uma tarefa concluída ou com falha pode avançar
apenas um passo (`LIGHT -> BALANCED -> STRONG -> MAX`) e somente com motivo limitado,
como falha de validação, baixa confiança, contexto ausente ou risco reavaliado. Nenhum
retry automático fica oculto do primary.

`ivoai monitor --watch` mostra prontidão do brief, score, tier, provider selecionado,
modelo e fonte, effort e fonte, modo de execução, estado das dependências, duração, uso
do Headroom e quantidade de escalonamentos. O JSON contém apenas essa metadata. Métricas
de tokens e economia do Headroom permanecem indisponíveis enquanto uma fonte oficial
estruturada não as fornecer; o ivoai não as estima.

Headroom 0.36.0 é ignorado sempre que material autoritativo de conhecimento compartilhado
estiver no caminho do primary ou worker, pois sua proteção de tool results não foi
comprovada como segura para essas respostas exatas. Modos Direct do agent, Web MCP,
ai-memory e Context permanecem independentes do scheduler automático.
