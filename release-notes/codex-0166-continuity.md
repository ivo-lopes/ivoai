# Codex 0.166.0 and owned adaptive conversation resume

This increment advances the verified managed Codex/Code Mode companion bootstrap
from 0.162.1 to 0.166.0 on Linux amd64 and ARM64. The upstream stable release is
`rust-v0.166.0`, published 2026-10-29. All four downloaded archives match their
upstream API SHA-256 digests; extracted executable identities are retained in the
[evidence](evidence/codex-0166-continuity-20261104/verified-assets.json).
No installer script, personal configuration, credentials or paid API was used.

The adaptive capability remains **OFF by default**. Production admits only an
explicit reviewed executable/companion pair on Linux amd64; the previously
certified 0.162.1 pair remains eligible for explicit rollback. ARM64 bootstrap
archive verification does not certify adaptive ARM64 execution. An arbitrary
version banner, mismatched companion or different digest still fails closed.

```sh
IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1 ivoai codex
IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1 ivoai session resume SESSION_ID
```

An owned eligible read-only conversation can now be reopened in another process.
Each new prompt obtains a fresh receipt; existing lease/account/native-thread,
configuration, state, root identity and source-scope checks continue to apply.
A rejected, failed, cancelled or stale attempt cannot become eligible history.
Completed prompts are never replayed. The resumed frontend retains the effective
model and reasoning effort; unavailable retained selections fail explicitly.
The ordinary approval boundary governs later executor/model picker changes.

Real validation exposed a resolver interaction: when the managed and personal
clients had the same version, normal resolution preferred the personal client.
Its different ownership correctly refused adaptive admission, so a simple query
entered the existing DAG path and waited for plan approval. Epochs 2 and 3 retain
that failure, including the original 110-second completion deadline. With the
explicit adaptive flag, equal-version resolution now retains the healthy,
authenticated registered managed client. A newer personal client still wins;
broken or unauthenticated managed clients cannot override a healthy client.
The default resolver policy is unchanged when the flag is absent. No ownership
or fingerprint check was relaxed to obtain successful admission.

## Observed validation

The [receipt](evidence/codex-0166-continuity-20261104/full-cli-resume-epoch4.json)
records real official subscription inference through the production public CLI:
two natural prompts, normal `/exit`, a new `session resume` process and a third
related prompt. All three completed in the same Core session, frontend and native
executor thread, with three fresh closed receipts, three attempts, zero DAG,
zero workers, normal native/backend/wrapper exits and unchanged repository data.
Wall time was 61.452 seconds. The original authentication locations were retained.

The private source binary was rebuilt with identical production source after
validation; both binaries have SHA-256
`4e9622d10065142d322a6668724db6968a823eeacd25cbf9ae5a76ce0cfaedb5`.
The [source manifest and logs](evidence/codex-0166-continuity-20261104/validation.json)
separate this uncommitted-source proof from a future integrated/tagged build.
Focused App/resolver race tests, the complete components package under race,
affected-package vet and the official native frontend protocol package passed.
Core MCP/read-bootstrap/escalation/late-denial proofs used a synthetic local
provider and are not counted as subscription inference. Opt-in skips are not
counted as successful live tests. The first setup failure is also retained.

## Limits and rollback

This increment does not certify continuity after an earlier DAG/checkpoint,
foreign conversations, arbitrary runtime versions/platforms, a dynamic primary
MCP catalog expansion, Claude/Antigravity parity or global OpenCode removal.
Those existing guarded paths remain separate. Clear the adaptive flag for future
launches to roll back behavior; finish or cancel active turns before downgrading
and preserve claims, attempts, mappings, receipts and checkpoints.

The official CLI reports per-invocation usage; these figures are not billing or
a comparable Gateway OFF/ON cohort. **Global economic gate: INCOMPLETE.**
No token/time/quota saving is inferred from this functional integration.
