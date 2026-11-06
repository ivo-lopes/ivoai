# Executores

## Codex nativo

`ivoai codex` abre a TUI oficial por uma façade App Server do IVOAI. O mesmo Core
admite prompts naturais, preserva permissões e aprova planos. O Primary
adaptativo é experimental e fica desabilitado por padrão. Durante certificação
controlada, `IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1` permite consultas elegíveis
somente leitura sem DAG nos pares de runtime e plataformas revisados; trabalhos
complexos usam o scheduler e workers existentes. `ivoai auto` é um alias
desse caminho. `ivoai codex --direct` mantém o cliente direto, sem orquestração de
turnos gerenciada. Consulte [frontends](orchestrated-frontends.md).

A autenticação pertence ao cliente oficial. IVOAI não copia credenciais de
assinatura nem introduz fallback silencioso para API paga. Versões, plataformas
e flags certificadas estão nas notas da release; teste offline não equivale a
inferência oficial.

## Claude Code e Antigravity

`ivoai claude` inicia a TUI oficial Claude direta. A execução headless gerenciada
usa `ivoai claude run --prompt ...` com `IVOAI_EXPERIMENTAL_CLAUDE_HEADLESS=1`.
A flag fica desabilitada por padrão; o host atual revisado usa Claude Code 2.1.318 em
Linux amd64. A certificação de inferência oficial continua pendente de uma conta
autorizada do usuário; verificações offline de protocolo não a substituem.

`ivoai antigravity run --prompt ...` exige `IVOAI_EXPERIMENTAL_ANTIGRAVITY=1` e
o binário `agy` 1.5.2 revisado no PATH, atualmente limitado a Linux amd64. Esse
host ACP experimental verifica a identidade exata do executável; uma versão
impressa correspondente não basta. A autenticação oficial existente é usada sem
copiar credenciais de assinatura nem introduzir fallback para API paga.

Esses hosts headless compartilham admissão, ownership, permissões e protocolo de
planejamento do Core. Eles não controlam as TUIs nativas nem prometem paridade
universal. Turnos relacionados em um host vivo são diferentes de retomada entre
processos; planner/handoff ou resume sem suporte continuam sendo recusados.

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
