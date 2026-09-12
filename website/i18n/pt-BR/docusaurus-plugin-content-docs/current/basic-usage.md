# Uso básico

```bash
ivoai codex
ivoai claude
ivoai opencode
ivoai status
ivoai doctor
```

`ivoai codex` abre a TUI nativa Codex com admissão controlada pelo IVOAI e primary Codex preferencial.
`ivoai opencode` abre o OpenCode gerenciado. Ambos compartilham Prompt Gate, DAG,
aprovação, scheduler e policies; workers Claude continuam elegíveis.

`ivoai codex --direct` e `ivoai opencode --direct` preservam os clientes oficiais
sem orquestração. `ivoai claude` permanece igual. `ivoai auto` é alias deprecated
de `ivoai opencode`, com aviso apenas em stderr.
Veja [Frontends orquestrados](orchestrated-frontends.md).
