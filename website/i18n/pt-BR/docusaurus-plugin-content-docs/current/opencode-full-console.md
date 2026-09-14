# OpenCode Full Console

`ivoai opencode` é frontend do mesmo core de `ivoai codex`, não um segundo
scheduler ou registry. `ivoai auto` permanece alias deprecated; direct é explícito.

## Console operacional

`/ivoai` abre a console com scroll; Escape devolve o foco ao composer. O estado
é atualizado aproximadamente a cada segundo, sem requisições sobrepostas.
São exibidos Prompt Gate, plano/DAG e dependências, workers, provider/modelo/
reasoning, quota, worktrees isoladas/integradas, ferramentas MCP autorizadas por
task, Skills/Ponytail, Memory/Context por purpose e saúde dos hooks gerenciados.
Estado desconhecido/stale não é apresentado como saudável. Commit de worker
não equivale a integração: a metadata muda após a integração efetiva.

O core fornece snapshot mais seu ring de eventos tipados, limitado a 128 itens.
Sequências sobrevivem ao restart e permitem identificar lacunas na janela.
Não é transcript nem audit log completo. A console não persiste outra cópia:
não recebe prompt bruto, resposta de provider, resultado de worker, reasoning
privado, corpo de checkpoint, credential, environment ou stderr bruto.

Aprovações de plano, mudança material de routing e MCP continuam nos diálogos
nativos. Permission mode full não aprova plano nem amplia grants.

## Sessões e modelos

`/ivoai-sessions` lista sessões lógicas do projeto atual e retoma conversas
OpenCode elegíveis já mapeadas, com confirmação. Não troca durante turno ativo,
não repete o último prompt e preserva IVOAI/native ID. O OpenCode continua dono
do histórico frontend. Transições de modo/provider e mappings ausentes são
encaminhados ao Session Control de `ivoai`, sem criar conversa silenciosamente.
A TUI principal mantém inspect/resume/recover/handoff/stop e adoção explícita Codex.

`/ivoai-models` mostra catálogo observado na sessão, source e opções de reasoning.
Use o seletor nativo para override explícito do primary. A admissão do turno
revalida disponibilidade/quota: catálogo não garante provider continuamente
online. Modelo explícito indisponível falha sem fallback silencioso. Workers
continuam independentes, pelo Economic Router mínimo suficiente.

## Perfis

Na command palette OpenCode ou em `ivoai` → Orchestration Policies:

| Perfil | Policy existente |
| --- | --- |
| economic | Até dois workers, sem escritas paralelas, escalation progressiva |
| balanced | Cap automático, escritas isoladas paralelas, pesos padrão |
| quality | Até quatro workers, escritas isoladas, maior peso de risco/verificação |
| custom | Preserva valores; edição individual no mesmo menu |

CLI:

```sh
ivoai config set orchestration.auto.automation_profile economic
```

Efeito na próxima sessão, nunca nos workers em execução. Todos os presets
mantêm threshold de quota 10%, telemetry de quota e aprovação de plano. Overrides
de provider/modelo/effort e knowledge são preservados. Nenhum model ID é inventado.
Quality ajusta pesos de routing, não acceptance nem outro motor de verificação.
Editar uma policy individual marca custom, sem segundo config stack.
O menu custom inclui peso de verificação no routing (0–100), sem desativar
validação obrigatória ou acceptance.

## Hooks e segurança

A palette oferece validate/repair de wiring comprovadamente IVOAI-owned, sempre
com confirmação. Hooks pessoais são preservados; nenhum lifecycle é executado
como teste. Falha/degradação não vira success. Diagnóstico bounded em
`ivoai memory hooks status` e `ivoai doctor`.

Endpoints de controle usam o bridge loopback autenticado e ações fixas, nunca
shell, keys arbitrárias ou credentials. Veja [continuidade](conversation-continuity.md)
e [desenvolvimento em equipe](team-development.md).
