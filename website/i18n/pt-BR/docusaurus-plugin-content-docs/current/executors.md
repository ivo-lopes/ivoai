# Executores

## Codex nativo

`ivoai codex` abre a TUI oficial por uma façade App Server do IVOAI. O mesmo Core
admite prompts naturais, preserva permissões e aprova planos. Com a opção
adaptativa certificada habilitada, consultas simples podem terminar sem DAG;
trabalhos complexos usam o scheduler e workers existentes. `ivoai auto` é um alias
desse caminho. `ivoai codex --direct` mantém o cliente direto, sem orquestração de
turnos gerenciada. Consulte [frontends](orchestrated-frontends.md).

A autenticação pertence ao cliente oficial. IVOAI não copia credenciais de
assinatura nem introduz fallback silencioso para API paga. Versões, plataformas
e flags certificadas estão nas notas da release; teste offline não equivale a
inferência oficial.

## Claude Code e Antigravity

`ivoai claude` inicia o cliente oficial. Os comandos experimentais gerenciados
`ivoai claude run --prompt ...` e `ivoai antigravity run --prompt ...` compartilham
admissão, isolamento, permissões e protocolo de orquestração do Core. Execução
headless não significa controle transparente da TUI nativa. Inferência Claude
exige a conta autorizada do próprio usuário.

## OpenCode gerenciado removido

`ivoai opencode`, seu modo `--direct`, novas sessões, seleção de planner,
troca de frontend e `ivoai setup --legacy-opencode` retornam mensagens explícitas
de migração antes de instalar ou executar. Nenhum catálogo pode reativar o
componente. Doctor mostra somente metadados preservados, sem verificar ou
executar o binário. As ações antigas do painel apresentam a migração.

Sessões e checkpoints legados continuam legíveis como histórico, sem replay ou
redirecionamento automático de tarefas. Novas execuções precisam de uma sessão
nativa admitida e contexto revisado. Os contratos serializados e a chave histórica
`opencode.permission_mode` permanecem compatíveis; esta chave controla as
aprovações do Core e não restaura OpenCode nem concede escrita a workers.

Nenhum executável, configuração, credencial, perfil ou histórico pessoal do
OpenCode é apagado, modificado ou adotado. O usuário pode usar sua instalação
upstream separadamente. Rollback usa o Core/perfil anterior e os assets originais
verificados; não reconstrua tags antigas, troque assets publicados nem remova
namespaces ou locks para forçar uma retomada. Consulte [rollback](update-rollback.md).
