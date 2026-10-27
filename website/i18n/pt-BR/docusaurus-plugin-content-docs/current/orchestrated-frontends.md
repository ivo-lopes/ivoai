# Frontends orquestrados

## Comandos

| Comando | Admissão e execução |
| --- | --- |
| `ivoai codex` | TUI nativa Codex; admissão IVOAI; primary Codex preferencial |
| `ivoai opencode` | TUI OpenCode gerenciada; primary resolvido pela policy |
| `ivoai auto` | Alias deprecated de `ivoai opencode` durante v0.10.x |
| `ivoai codex --direct` | Cliente oficial Codex, sem Prompt Gate/DAG |
| `ivoai opencode --direct` | OpenCode standalone, sem Prompt Gate/DAG |
| `ivoai claude` | Interface direta opcional existente |

Coloque `--direct` antes de `--`; argumentos depois de `--` pertencem ao cliente
oficial. `session start --executor codex|opencode --mode direct` continua disponível;
`--mode orchestrated` entra no core nativo compartilhado.

## Terminal Codex

Digite o primeiro prompt diretamente no composer oficial Codex. Use seus
controles multilinha nativos e Enter para enviar:

```text
Read VERSION in the current directory and report its value.
Acceptance: return only the version; do not modify files.
```

O seletor nativo de modelo/reasoning escolhe um primary Codex verificado em
runtime; workers mantêm routing independente. Planos e decisões de quota usam
diálogos nativos, não `/approve`. Composer, navegação, scroll, seleção e interrupção
continuam upstream. Não há pré-composer IVOAI.

O [protocolo oficial App Server](https://learn.chatgpt.com/docs/app-server) conecta
a TUI a uma façade IVOAI autenticada e process-local. Ela admite `turn/start`
antes de encaminhar ao App Server oficial. Um adapter Responses privado retorna
a síntese do core compartilhado, nunca tool calls nativos. Shell/exec, review,
steering, escrita de configuração e métodos desconhecidos não contornam o gate.
Use `--direct` para essas operações upstream fora da orquestração.

Codex 0.162.1 e 0.154.0 foram testados. O transporte App Server é experimental no
upstream; a integração local é pinada e testada. O frontend usa um Codex home
privado e persistente por projeto, sem copiar credenciais. Workers mantêm
autenticação oficial. O picker nativo `/resume` e `ivoai session resume` usam os
mesmos mappings de [continuidade](conversation-continuity.md). Alterações de
configuração global não são expostas pela façade. IVOAI guarda metadados
sanitizados, não um segundo store de transcripts.

A rejeição de um prompt não inicia worker nem execução substantiva. Títulos de critérios de aceite são opcionais: linguagem natural informal e limitada usa a avaliação compartilhada de intake. A política existente de DAG, grants e validação de conclusão continua em vigor; esta entrega não introduz execução sem DAG. Full não aprova planos. Immediate start
permanece configuração explícita compartilhada.

## Um control plane

Ambos usam a mesma admissão e scheduler: purpose-auto, Memory/Context limitado,
DAG, aprovação, Economic Router, concorrência por host, Skills/Ponytail por worker,
MCP allowlists, worktrees, integração, verificação do aceite e síntese do primary.
Após aprovação, o scheduler enfileira os nós delegados automaticamente.

Codex é primary forte preferencial em `ivoai codex`; workers Claude permanecem
elegíveis. Trocar primary exige decisão de routing separada. Em 10% de quota,
a conservação propõe economia nos workers, sem trocar primary silenciosamente.
Overrides explícitos indisponíveis falham de forma fechada. Login Claude é opcional.

Status distingue `frontend`, `orchestration_mode`, `primary_provider` e executor,
modelo e reasoning por worker. Campos legados `mode` e `primary_executor` continuam.
Progresso contém apenas metadados, não transcripts ou stores de credenciais.

## Compatibilidade e recuperação

Upgrade preserva profiles, secret refs, MCP, permission mode, pack, Ponytail e
policies, sem re-enrollment ou novo login. Config `auto.*` não é renomeada.
Registros do novo frontend Codex usam um namespace privado no mesmo store de sessões:
binários antigos os ignoram no rollback, e reapply restaura acesso.

O aviso do alias aparece uma vez em stderr, nunca em stdout. Se OpenCode falhar,
o IVOAI não abre uma TUI direta sem gate. Use `ivoai codex` para orquestração
controlada ou escolha `--direct` explicitamente. Sandbox, Skill Gate, MCP
deny-by-default e isolamento de purpose permanecem vigentes.

## Eventos operacionais e perfis de automação

O painel OpenCode gerenciado assina o endpoint SSE local autenticado `/events`.
As observações da versão 1 incluem sequência monotônica por sessão e, quando
aplicável, identificadores de plano, tarefa, turno e tentativa. São retidos apenas
metadados sanitizados: 128 observações por sessão. A reconexão retoma pelo cursor;
lacunas de histórico ou troca de sessão reiniciam explicitamente a visualização.
Consumidores lentos são limitados e desconectados. Heartbeat e reconexão indicam
estado real, sem progresso inventado. Notificações do sistema de arquivos observam
escritas de outros processos IVOAI. A leitura de status utiliza observações de
quota em cache, sem autenticar provedores em nome do painel. Informações ausentes,
desatualizadas e com erro permanecem distinguíveis.

Configuração → **Automation Profiles** oferece `economic`, `balanced`, `intensive`,
`review`, `development`, `research` e `diagnostic`. Cada perfil mostra os valores
atuais e propostos antes da confirmação. Equivalentes na CLI:

```sh
ivoai config profile list
ivoai config profile show
ivoai config profile preview economic
ivoai config profile apply economic
ivoai config profile restore
```

Os perfis afetam sessões futuras e alteram apenas concorrência, limite de workers,
escritas paralelas/otimização e pesos de roteamento. Preferências de provedor e
modelo, permissões, aprovações, autenticação e servidores permanecem intactos.
São limites máximos: host, quota e DAG aprovado continuam determinando a
concorrência útil. A regra existente de modelo forte para o primary é preservada.
Um recibo privado e atômico guarda os valores anteriores permitidos. Reaplicar é
idempotente; restaurar recusa sobrescrever personalizações posteriores. `show`
distingue perfis, herança e personalização. Esse rollback restaura configuração,
não histórico do provedor ou bancos de dados dos servidores.
