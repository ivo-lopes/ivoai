# Orquestração automática

`ivoai codex` e `ivoai opencode` compartilham o mesmo core. O primeiro usa admissão
via App Server IVOAI e TUI nativa Codex; o segundo usa OpenCode.
Veja [Frontends orquestrados](orchestrated-frontends.md). `ivoai auto` é alias
deprecated. Para a TUI oficial sem orquestração, escolha `--direct` explicitamente.

`ivoai opencode` usa o OpenCode pinado como frontend gerenciado. O IVOAI é a
autoridade de prompt, sources, DAG, quota, modelos, workers e integração. Codex,
Claude opcional e Native OpenCode elegível mantêm autenticação oficial própria.
O IVOAI não copia credentials de providers para o OpenCode.

```sh
ivoai codex
ivoai opencode
ivoai opencode --planner codex
ivoai opencode --planner claude
```

## Prompt e plano

AUTO aceita pedidos informais e perguntas sem exigir um modelo de objetivo,
entregável ou critérios de aceite. A entrada aceita texto UTF-8 não vazio de até
128 KiB, incluindo tabulações e quebras de linha LF/CRLF. Texto vazio, malformado,
acima do limite ou com caracteres de controle inseguros é rejeitado antes da
execução com um diagnóstico fixo, que nunca inclui trechos do prompt.

Exemplo: `Qual versão deste projeto?`

Readiness não concede ferramentas, aprova planos nem comprova aceite. O primary
pode esclarecer ambiguidades sem tratar a ausência de headings como uma fronteira
de autorização. O caminho simples somente com primary, sem DAG, permanece pendente;
esta mudança de entrada preserva os gates existentes de plano, quota, permissões
e validação final.

Após readiness, purpose-auto seleciona sources antes de Memory/Context. O primary
gera o menor DAG útil. A UI apresenta plano e pede aprovação por padrão; cancelar
não inicia workers. `plan_execution=immediate` permite início sem essa etapa de
confirmação, mas não remove o gate, o Skill Gate nem a aprovação de mudança material
de routing.

O primary usa modelo STRONG ou MAX comprovado pelo catálogo runtime. Workers usam
o menor tier suficiente para cada papel e risco. Modelo e reasoning explícitos
incompatíveis falham fechados, sem substituição silenciosa.

## Configuração TUI-first

O monitor de uma sessão pode ser consultado em outro terminal:

```sh
ivoai monitor --watch
ivoai monitor --session <session-id> --json
```

Abra `ivoai` e o menu de orquestração. As mesmas escolhas persistentes estão
disponíveis por `ivoai config set` / `ivoai config get`:

| Chave (prefixo `orchestration.auto.`) | Padrão | Escolhas |
| --- | --- | --- |
| `plan_execution` | `approve` | `approve`, `immediate` |
| `knowledge_routing` | `purpose-auto` | `purpose-auto`, `all-enabled`, `explicit-only` |
| `concurrency` | `auto` | `auto`, `sequential` |
| `worker_cap` | `0` (auto) | `0..12` |
| `parallel_writes` | `true` | `true`, `false` |
| `provider_preference` | `auto` | `auto`, `codex`, `claude` |
| `low_quota_threshold` | `10` | percentual validado pela configuração |

```sh
ivoai config set orchestration.auto.plan_execution approve
ivoai config set orchestration.auto.knowledge_routing purpose-auto
ivoai config set orchestration.auto.concurrency auto
ivoai config set orchestration.auto.worker_cap 2
ivoai config set orchestration.auto.parallel_writes true
ivoai config set orchestration.auto.provider_preference auto
ivoai config set orchestration.auto.low_quota_threshold 10
```

Configuração alterada vale para a próxima sessão gerenciada. Aceite obrigatório,
primary forte e MCP deny-by-default são invariantes, não opções para desligar
silenciosamente. O modelo/variant picker do OpenCode continua disponível e mostra
requested/effective.

## Sources e isolamento

Em purpose-auto, a seleção inicial é conservadora e local, por alias/purpose
mencionado. Não é um classificador semântico institucional completo. Ambiguidade
não consulta todos os servidores. Uma instrução negativa de source exclui o
purpose. Use override explícito para desambiguar:

Use `ivoai opencode --knowledge-source company-a`; repita `--knowledge-source` para
selecionar um subset explícito com mais de uma source.

`all-enabled` preserva a federação compatível; `explicit-only` não seleciona
sources implicitamente. Sem necessidade institucional, nenhuma source é consultada.
Seleção por sessão é o teto da seleção por worker. Não há fan-out de writes.
Profiles, identidade estável e secrets continuam no domínio Connections existente.

Workers recebem contexto local e ResultRefs relevantes, não cópia integral do
prompt/brief. MCPs são negados por padrão; acesso selecionado é projetado
process-local com ferramentas permitidas. MCP pessoal arbitrário não é herdado.

## Escrita paralela e falhas

Writers aprovados recebem worktree e branch próprias. Paths permitidos,
dependências e mudanças coletadas são verificados. O primary coordena em read-only;
a integração é feita pelo IVOAI após validação. Nenhum conflito é resolvido
automaticamente. Evidência de worktree com falha fica na área privada de recuperação.

Se a criação inicial de worktrees não for possível, inclusive fora de Git,
a execução degrada explicitamente para patches sequenciais: o worker continua
read-only e o IVOAI verifica/aplica o diff sob lease exclusivo. Paths sensíveis,
symlinks, modos especiais, diffs binários e mudanças fora do escopo são rejeitados.
Nenhum `git init` silencioso é executado.

A concorrência usa DAG, CPU, RAM, load, I/O observável e limite do usuário. Falha
não vira sucesso por causa de encerramento normal da TUI. DAG incompleto não
produz resposta final certificada.

## Quota e frontend

Com quota de 10% ou menos, a UI propõe conservação e pede confirmação. Rejeitar
mantém a rota existente enquanto elegível. Uma mudança material posterior exige
aprovação própria. O primary permanece forte por padrão.

OpenCode mostra readiness, plano/aprovação, contagens de tarefas/workers,
concorrência, executor/model/reasoning, purposes, MCPs e quota. Não mostra
transcripts privados ou chain of thought dos workers.

`opencode.permission_mode=interactive|full` permanece independente: interactive
é o default público; full é opt-in persistente. Full elimina aprovações de
ferramentas configuráveis, não aprova planos nem desativa sandbox/Skill Gate/policy.

Falha do frontend gerenciado é mostrada como degradação explícita. Uma TUI direta
não é anunciada como se ainda oferecesse o mesmo frontend/plano gerenciado.
Claude não autenticado é capability indisponível, não erro global; não é necessário
fazer login para usar um executor elegível.

## Compatibilidade e limites

Upgrade mantém ServerProfiles, secret refs, MCP registry, autenticação oficial e
permission mode. Não exige novo enrollment/login. Novos defaults de orquestração
não alteram configurações pessoais dos executores.

Prompts expandidos de workers permanecem transitórios. O checkpoint privado
preserva um DAG aprovado e limitado para recuperação segura, sem transcript.
[Conversation Continuity](conversation-continuity.md) usa mappings nativos e
identidade comprovada. Skills/Ponytail continuam disponíveis. A UI é operacional
mínima; Full Console e OpenViking permanecem fora desta release.

Veja [Scheduler automático](auto-scheduler.md), [Connections](connections.md) e
[WorkingContext](working-context.md).
