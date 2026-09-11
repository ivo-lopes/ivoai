# Frontends orquestrados

## Comandos

| Comando | Admissão e execução |
| --- | --- |
| `ivoai codex` | Terminal IVOAI; primary Codex preferencial; orquestração compartilhada |
| `ivoai opencode` | TUI OpenCode gerenciada; primary resolvido pela policy |
| `ivoai auto` | Alias deprecated de `ivoai opencode` durante v0.10.x |
| `ivoai codex --direct` | Cliente oficial Codex, sem Prompt Gate/DAG |
| `ivoai opencode --direct` | OpenCode standalone, sem Prompt Gate/DAG |
| `ivoai claude` | Interface direta opcional existente |

Coloque `--direct` antes de `--`; argumentos depois de `--` pertencem ao cliente
oficial. `session start --executor codex|opencode --mode direct` continua disponível;
`--mode orchestrated` entra no core nativo compartilhado.

## Terminal Codex

Digite um prompt multilinha e termine com `/submit`:

```text
Read VERSION in the current directory and report its value.
Acceptance: return only the version; do not modify files.
/submit
```

`/models` lista o catálogo verificado em runtime. `/model <id>` e
`/reasoning <effort>` selecionam opções suportadas. `/status` mostra metadados;
`/approve` ou `/reject` resolve a decisão exibida; `/cancel` interrompe o turno;
`/exit` fecha o frontend. EOF envia o prompt pendente, mas nunca aprova decisões.
Esta é uma camada IVOAI, não a TUI upstream Codex; `--direct` preserva essa TUI.

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
