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

Codex 0.153.4 e 0.154.0 foram testados. O transporte App Server é experimental no
upstream; a integração local é pinada e testada. O frontend usa um Codex home
efêmero, sem copiar credenciais. Workers mantêm autenticação oficial. Navegação
da conversa funciona durante a sessão; resume/fork persistentes e alterações de
configuração global não são expostos pela façade. IVOAI guarda metadados
sanitizados, não um segundo store de transcripts.

Prompt insuficiente não inicia worker nem execução substantiva. Critérios de aceite
são obrigatórios nos dois frontends. Full não aprova planos. Immediate start
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
