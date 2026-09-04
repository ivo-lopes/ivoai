# Rollout canário em duas produções

Este documento não autoriza nenhum deployment em produção. Ele define o rollout posterior,
controlado pelo operador, depois da aprovação do release candidate de compatibilidade e dos dois
inventários somente leitura.

## Gates da release

O candidate não pode entrar em produção até que todos estes itens estejam verdes:

1. as fixtures canônicas da v0.5.0 carregam com o candidate;
2. a matriz binário real v0.5.0/candidate/binário antigo passa;
3. as migrations normal, no-op, sequencial, interrompida e com falha de validação passam;
4. falhas de checksum/candidate/permissão/path/symlink não alteram dados gerenciados;
5. rollback automático e repetido restaura o estado compatível exato;
6. campos TOML desconhecidos e ownership de componentes externos sobrevivem;
7. as regressões de setup/status/Doctor do client e server passam;
8. gofmt, unit, race, vet, ShellCheck e govulncheck passam no CI;
9. os dois inventários live sanitizados não têm incompatibilidade sem explicação;
10. um comando de rollback testado e um responsável pela observação estão definidos.

## Ordem do rollout

```text
candidate + green hermetic matrix
  -> release candidate
  -> PROD-1 canary
  -> setup/status/Doctor/service health/smoke
  -> observation window
  -> explicit GO or ivoai update --rollback
  -> PROD-2
  -> the same health and smoke checks
  -> close rollout
```

Nunca atualize as duas instalações ao mesmo tempo. Uma falha ou divergência inesperada em
PROD-1 interrompe o rollout; preserve o transaction journal, execute rollback, execute Doctor e
colete diagnósticos sanitizados. Stores de login de provider e tools não gerenciadas não podem
mudar. Uma migration declarada irreversível bloqueia o rollout antes da promoção.

## Política de rollout da arquitetura futura (IVOAI-54)

OpenCode, OpenViking, Caveman, NativeOrchestrator e um Skill Control Plane devem chegar de forma
aditiva. Cada um exige uma release de coexistência, comportamento disabled/shadow, evidência de
canário, promoção a default e uma release de observação antes que código legado possa ser
removido. Headroom e Ruflo permanecem nesta release de compatibilidade.
