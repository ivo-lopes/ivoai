# Uso básico

```bash
ivoai codex
ivoai claude
ivoai auto
ivoai status
ivoai doctor
```

`ivoai codex` abre a TUI oficial com admissão pelo Core IVOAI. `ivoai auto` é um
alias para esse mesmo caminho. Prompts naturais não precisam de cabeçalhos formais
de aceite. O Primary adaptativo sem DAG é experimental e opt-in, OFF por padrão;
o planejamento/scheduler existente e todas as autorizações continuam disponíveis.

`ivoai codex --direct` e `ivoai claude` usam os clientes oficiais diretos.
Os hosts headless possuem flags explícitas próprias e não significam controle
transparente da TUI. `ivoai opencode`, inclusive `--direct`, retorna erro de
migração e preserva todos os dados existentes do usuário.

Veja [Frontends orquestrados](orchestrated-frontends.md) para prompts e decisões.
