# Frontends orquestrados

Os clientes gerenciados compartilham um Core: admissão, sessões, permissões,
protocolo de planejamento e scheduler. O cliente nativo apresenta entrada, saída
e aprovações; não mantém outro roteador econômico ou banco de memória.

## Comandos

| Comando | Admissão e execução |
| --- | --- |
| `ivoai codex` | TUI oficial Codex com admissão do Core |
| `ivoai auto` | Alias de compatibilidade para Codex gerenciado |
| `ivoai codex --direct` | Cliente direto, sem orquestração de turnos gerenciada |
| `ivoai claude` | Cliente oficial Claude direto |
| `ivoai claude run --prompt ...` | Host headless gerenciado experimental |
| `ivoai antigravity run --prompt ...` | Host ACP gerenciado experimental |
| `ivoai opencode` ou `--legacy-opencode` | Erro explícito de remoção/migração, sem instalar ou executar |

Coloque `--direct` antes de `--`. O histórico OpenCode continua legível para
inspeção e rollback; resume/recovery não redirecionam nem reproduzem tarefas
legadas. Configurações, binários, credenciais e históricos pessoais permanecem
intactos. Consulte [executores](executors.md).

## Terminal Codex

Digite um pedido natural no composer oficial, por exemplo:

```text
Explain how authentication works in this project.
```

Cabeçalhos formais de aceite são opcionais. Integridade, autorização, escopo e
isolamento permanecem obrigatórios. O Primary adaptativo é experimental e fica
desabilitado por padrão até certificação e rollout controlados. Com
`IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1`, sessões elegíveis revisadas podem terminar
consultas somente leitura no Primary sem DAG ou workers.
Trabalhos complexos usam o protocolo de planejamento existente, validação de
plano, aprovações e workers isolados. O modo simples não concede escrita nem
execução privilegiada.

O seletor usa modelos/reasoning verificados em runtime. Aprovar um plano e mudar
o executor são decisões separadas. Composer, navegação, seleção e apresentação
continuam nativos; não existe pré-composer IVOAI. A façade admite cada turno antes
da execução oficial e recusa métodos/configurações sem suporte. Direct é uma
operação separada e explícita, nunca fallback depois de uma solicitação admitida.

Versões, plataformas e rollout exigem as evidências reais da release. Um teste de
startup ou provedor sintético não certifica inferência. O Codex home privado por
projeto guarda histórico nativo sem copiar autenticação. `/resume` e
`ivoai session resume` usam os mappings existentes. Claims duráveis, auditoria e
histórico de planos concluídos preservam identidade e prevenção de replay.
Histórico falho, cancelado, estrangeiro ou incompleto não recupera autoridade ao
sair de uma projeção limitada de metadados.

## Evidências e workers

Quando habilitado explicitamente por `IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY=1`, o
Evidence Gateway atende consultas CodeIntel nativas somente leitura. Consultas repetidas podem reutilizar o resultado tipado
exato no mesmo processo, após novas verificações do recibo. Mudanças no código,
permissões, proprietário, worktree ou runtime invalidam reutilização/recuperação.
Operações de índice/diagnóstico nunca são reproduzidas do cache. Cache do Gateway
e cache de tokens reportado pelo provedor são métricas distintas; não há economia
global presumida.

Tarefas complexas usam o mesmo scheduler Core e workers existentes. O perfil de
coordenação Ruflo sem provider não introduz outro scheduler. Nenhum frontend
concede novos grants ou contorna aprovações. Memory e Context continuam recursos
separados, com seus próprios contratos. Conteúdo externo é evidência não confiável,
sem autoridade para dar instruções de execução. Nomes de pacotes e leitores de
contratos antigos não reativam o OpenCode.

## Limites dos clientes e diagnóstico

`ivoai claude` permanece como TUI oficial direta. Claude headless gerenciado usa
`IVOAI_EXPERIMENTAL_CLAUDE_HEADLESS=1`, desabilitado por padrão, e o runtime
Claude Code 2.1.318 revisado em Linux amd64. Sua certificação de inferência está pendente
de uma conta autorizada do usuário; testes offline não a comprovam.

A flag `IVOAI_EXPERIMENTAL_ANTIGRAVITY=1`, desabilitada por padrão, habilita o host
ACP `agy` 1.5.2 revisado em Linux amd64. A identidade exata do binário continua
obrigatória. Protocolos headless não oferecem controle transparente da TUI,
paridade universal de clientes ou retomada entre processos. Não substitua a
autenticação oficial existente por provedor falso ou API paga. Planner/handoff ainda recusados permanecem indisponíveis
até que seu contrato nativo seja implementado e certificado.

`ivoai monitor --watch` e o painel administrativo mostram sessões, workers,
quotas, evidências e aprovações sanitizados pelo mesmo backend. O painel não é
outro chat. Cancelar ou revogar um turno não dispara replay ou fallback automático.
Falha do frontend ou da validação final continua falha mesmo com exit zero do filho.
