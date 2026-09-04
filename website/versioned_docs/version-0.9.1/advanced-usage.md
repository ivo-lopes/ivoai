# Advanced usage

Use `ivoai session start` for an explicit direct or orchestrated lifecycle, repeat
`--knowledge-source` for a restrictive source subset, and use `ivoai monitor --watch`
for bounded lifecycle metadata.

```bash
ivoai session start --executor codex --mode orchestrated
ivoai auto --planner claude --knowledge-source research
ivoai auto --knowledge-source company-a --knowledge-source company-b
```

See [Automatic orchestration](auto-orchestration.md), [WorkingContext](working-context.md),
and [multi-server knowledge](multi-server.md).
