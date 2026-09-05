# Two-production canary rollout

No production deployment is authorized by this document. It defines the later
operator-controlled rollout after the compatibility release candidate and both
read-only inventories are approved.

## Release gates

The candidate cannot enter production until all of these are green:

1. canonical v0.5.0 fixtures load with the candidate;
2. the real v0.5.0 binary/candidate/old-binary matrix passes;
3. normal, no-op, sequential, interrupted and failed-validation migrations pass;
4. checksum/candidate/permission/path/symlink failures change no managed data;
5. automatic and repeated rollback restore exact compatible state;
6. unknown TOML fields and external component ownership survive;
7. client and server setup/status/Doctor regressions pass;
8. gofmt, unit, race, vet, ShellCheck and govulncheck pass in CI;
9. the two sanitized live inventories have no unexplained incompatibility;
10. a tested rollback command and observation owner are assigned.

## Rollout order

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

Never update both installations at once. A failure or unexpected divergence on
PROD-1 stops rollout; preserve the transaction journal, run rollback, run Doctor,
and collect sanitized diagnostics. Provider login stores and unmanaged tools must
not change. A migration declared irreversible blocks rollout before promotion.

## Future architecture rollout policy (IVOAI-54)

OpenCode, OpenViking, Caveman, NativeOrchestrator and a Skill Control Plane must
arrive additively. Each requires a coexistence release, disabled/shadow behavior,
canary evidence, default promotion, and an observation release before legacy code
can be removed. Headroom and Ruflo remain in this compatibility release.
