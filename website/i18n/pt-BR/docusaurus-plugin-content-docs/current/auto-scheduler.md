# Scheduler automático e roteamento de modelos

`ivoai opencode` usa o DAG nativo do IVOAI e o MCP local `ivoai-orchestrator`.
OpenCode é o frontend; o IVOAI controla admissão, policy, concorrência e integração.
O modo AUTO nativo não depende do lifecycle Ruflo do modo orchestrated legado.

## Admissão e plano

O prompt precisa conter objetivo, entregável e critérios de aceite observáveis.
Headings Markdown não são obrigatórios. Prompts ambíguos ou insuficientes retornam
somente os campos faltantes e aguardam reformulação, sem iniciar workers.

Fluxo: readiness → purpose-auto → Memory/Context selecionados → brief limitado →
DAG → roteamento → aprovação → workers → validação → integração → síntese.

```text
Prompt readiness -> purpose-auto -> selected Memory/Context -> bounded brief
  -> validated DAG -> scores -> quota/capability routing -> plan approval
  -> host/DAG admission -> scoped workers -> result validation
  -> checked integration -> strong primary synthesis
```

O plano tem até 12 tarefas, com papel, aceite local, dependências, contexto,
Skills, MCPs e paths de escrita. Ciclos, IDs inseguros, dependências desconhecidas e
duplicação não justificada são rejeitados. Por padrão, `orchestration_plan` aguarda
a aprovação do usuário. Full permissions não aprova o plano.

Cada worker recebe seu SharedContextBrief: objetivo e aceite locais, restrições,
findings relevantes e ResultRefs. Não recebe automaticamente o prompt completo ou
todo o contexto do primary. Conteúdo recuperado é dado não confiável.

## Economic Router

O score considera complexidade (30%), risco (25%), reasoning (20%), verificação
(15%) e amplitude de contexto (10%). Os tiers são LIGHT (0–24), BALANCED (25–49),
STRONG (50–74) e MAX (75–100). Pesos customizados são normalizados.

```text
score = round((30*complexity + 25*risk + 20*reasoning_depth
             + 15*verification_need + 10*context_breadth) / 100)
```

Workers usam o menor tier suficiente, respeitando a capability do papel. O primary
usa STRONG ou MAX verificado por padrão. Nomes reais de modelos e reasoning vêm do
cliente oficial em runtime, não de uma tabela fixa de nomes. Overrides explícitos
incompatíveis falham fechados. Snapshots locais são diagnóstico, não autoridade
para uma nova descoberta.

Codex e Claude podem participar do mesmo DAG quando elegíveis. Claude é opcional;
ausência de autenticação não bloqueia Codex. Capability de escrita não comprovada
não é concedida. Quota desconhecida não significa ilimitada. O roteamento considera
capability, risco, catálogo, quota e policy; metadata registra requested/effective.

Delegação read-only compara benefício com overhead. Tarefas triviais permanecem no
primary. Escrita sempre passa pelo adapter controlado, mesmo com uma única tarefa.

```text
benefit  = round((45*parallel_value + 20*verification_need + 20*risk
                 + 15*context_breadth) / 100)
overhead = 25 + 20*(100-complexity)/100 + 5*latency_sensitivity/100
```

## Concorrência e isolamento

A admissão considera nós prontos, CPU, RAM disponível, load, pressão de I/O quando
observável, quota e limite do usuário. Há limite absoluto de 12; recursos
desconhecidos degradam conservadoramente. Dependências precisam terminar antes do
início de seus dependentes.

MCPs são deny-by-default por worker. Estar cadastrado não concede acesso. Apenas
Skills e ferramentas selecionadas são projetadas process-local; credentials não
entram no brief, argv ou metadata. Resultados completos ficam no ArtifactStore
transitório privado e são recuperados explicitamente por referências opacas.

Writers aprovados usam branches/worktrees próprias. O IVOAI coleta mudanças dentro
dos paths permitidos e integra em checkout isolado antes de atualizar o primary.
Conflitos preservam evidência e não são resolvidos automaticamente. Se worktrees
não puderem inicializar, writers viram produtores read-only de patches sequenciais:
o IVOAI verifica o diff e aplica sob lease exclusivo. Alterações não suportadas
falham fechadas; nenhum `git init` silencioso é feito.

## Ferramentas e estado

- `orchestration_bootstrap` e `orchestration_capabilities`: contexto limitado e catálogo.
- `orchestration_plan`: validação, roteamento e aprovação.
- `orchestration_spawn` / `orchestration_spawn_batch`: dispatch controlado.
- `orchestration_primary_complete`: conclui tarefa do primary.
- `orchestration_wait`: espera limitada por notificações, sem busy-loop.
- `orchestration_result` e artifact reads: evidência limitada ou exata sob demanda.
- `orchestration_escalate`: elevação de um tier com razão explícita.
- `orchestration_cancel`: cancela worker pertencente à sessão.
- `orchestration_integrate`: integração obrigatória antes da síntese.

Com quota restante de 10% ou menos, a UI pede confirmação de conservação. Uma
mudança material de provider/model/effort pede aprovação própria. Rejeitar mantém a
rota enquanto elegível; não autoriza substituição silenciosa. O primary permanece
forte por padrão.

OpenCode mostra apenas metadata operacional: prontidão, plano/aprovação, tarefas,
workers, executor/model/effort, purposes, MCPs, concorrência e estado de quota.
Não mostra transcripts privados ou chain of thought. Falha ou DAG incompleto não
pode virar resposta final bem-sucedida.

Veja [Orquestração automática](auto-orchestration.md) para configurações na TUI e
CLI, compatibilidade e limitações de seleção por purpose.
