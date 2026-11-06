# Uso avançado

Use `ivoai session start` para um ciclo de vida explícito direto ou orquestrado, repita
`--knowledge-source` para um subconjunto restritivo de fontes e use `ivoai monitor --watch`
para metadata limitada do ciclo de vida.

```bash
ivoai session start --executor codex --mode orchestrated
ivoai auto --planner claude --knowledge-source research
ivoai auto --knowledge-source company-a --knowledge-source company-b
```

Consulte [Orquestração automática](auto-orchestration.md), [WorkingContext](working-context.md)
e [conhecimento multi-server](multi-server.md).
