# Orquestração automática

Codex gerenciado e seu alias `ivoai auto` usam a mesma admissão e o mesmo scheduler
do Core. OpenCode gerenciado foi removido. A autenticação pertence aos clientes
oficiais, sem cópia de credenciais ou fallback automático para API paga.

## Início e ownership da conversa

```sh
ivoai codex
ivoai auto
```

O intake aceita linguagem natural UTF-8 limitada, sem cabeçalhos formais de aceite.
Integridade, escopo, proprietário e permissões continuam obrigatórios. O Primary
adaptativo (`IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY`) é experimental e OFF por padrão.
Quando habilitado explicitamente em runtime revisado, uma consulta somente leitura
pode terminar sem DAG. Trabalho complexo usa o protocolo, validação, aprovações e
workers isolados existentes; o modo simples não concede escrita.

## Modelo e raciocínio

O picker Codex usa modelos/efforts Codex verificados. Um catálogo compartilhado não
comprova controle Claude por essa TUI. Escolhas explícitas indisponíveis falham.
Hosts headless Claude/Antigravity têm flags próprias, OFF por padrão, sem controle
transparente da TUI nativa. Campos reportados de quota/cache/modelo são separados
de valores desconhecidos ou inferidos.

## Admissão e fontes

O Core valida frontend/runtime antes de criar trabalho e admite cada turno de novo.
Nenhum renderer/plugin OpenCode ou alteração de configuração pessoal é necessário.
Um turno admitido não é repetido por fallback para cliente direto sem gate.
Purpose routing limita fontes relevantes; pesquisa tenta Memory e Context na
ordem existente antes de fontes externas. Conteúdo recuperado é dado não confiável,
sem autoridade para executar instruções. Brief e referências exatas não duplicam
Memory durável nem Context documental. Reutilização CodeIntel no Gateway também
é opt-in e OFF por padrão, com recuperação exata, owner e geração da fonte; não
é cache universal nem comprovação de economia global.

## Planejamento, workers e escalonamento

O Core valida escopo, aceite da tarefa, dependências, recursos e aprovação. O
scheduler existente usa workers quando úteis. Workers de leitura preservam essa
política; escritores aprovados usam worktrees isoladas e integração verificada.
Resultados são validados antes da síntese. Nenhum frontend cria outro scheduler ou
concede ferramentas ao escolher uma estratégia. Escalonamento exige capabilities
e quotas verificadas; mudanças materiais de executor têm consentimento próprio.
Telemetria ausente é desconhecida, não quota zero/ilimitada. Compressão não destrói
evidência autoritativa e provedores não são encadeados.

## Continuidade, falhas e cancelamento

Retomadas elegíveis mantêm identidade, histórico exato de planos e claims duráveis.
Histórico falho, cancelado, estrangeiro ou incompleto não recupera autoridade por
compactação de uma projeção. NoReplay/NoAutomaticFailover permanecem obrigatórios.
Uma quota não autoriza repetir mutação incerta nem transformar handoff TUI recusado
em execução headless. Recuperar um DAG interrompido é uma operação explícita,
revisada; resume normal não repete o prompt anterior.

## Observabilidade

```sh
ivoai monitor --watch
ivoai monitor --session <session-id> --json
```

O mesmo backend administrativo mostra quotas, modelos, workers, evidências, grants
e diagnósticos sanitizados, sem transcripts, credenciais ou raciocínio oculto.
Exit zero nativo não apaga falha na validação do Core. Hits do Gateway, cache do
provedor, uso reportado, processos amostrados e tempo total têm escopos separados.
O gate econômico permanece INCOMPLETE até comprovação em condições equivalentes.
Revogação, recibo fechado, troca de owner/worktree e resultados incertos recusam
recuperação ou execução; não causam replay. Direct e handoff são explícitos.

## Políticas

```sh
ivoai config set orchestration.auto.plan_execution approve
ivoai config set orchestration.auto.knowledge_routing purpose-auto
ivoai config set orchestration.auto.concurrency auto
ivoai config set orchestration.auto.worker_cap 2
ivoai config set orchestration.auto.parallel_writes true
ivoai config set orchestration.auto.provider_preference auto
ivoai config set orchestration.auto.low_quota_threshold 10
```

Essas opções não habilitam flags experimentais. Planos imediatos exigem escolha
explícita e preservam validação, permissões e sandbox. A chave compatível
`opencode.permission_mode` controla o Core; não restaura o renderer removido.
Escritas aprovadas continuam isoladas e validadas. Evidências e falhas são
preservadas. Dados pessoais OpenCode, perfis antigos e assets históricos permanecem
intactos para rollback explícito segundo seu contrato original.

[Referência](orchestrated-frontends.md)

[Referência](auto-scheduler.md)

[Referência](working-context.md)

[Referência](conversation-continuity.md)

[Referência](connections.md)
