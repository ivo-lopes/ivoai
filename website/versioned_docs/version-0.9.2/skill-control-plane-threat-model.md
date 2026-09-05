# Skill Control Plane threat model

## 1. Executive summary and methodology

This document models the surface introduced by IVOAI's Registry, indexing,
dependency/conflict graph, Policy Engine, supply chain, pack updates, and Skill Gate.
Every upstream, archive, metadata record, body, Context/RAG item, and worker/tool
output is treated as untrusted data. The protected asset is not only skill content:
it also includes policy authority, executor permissions, active-object integrity,
session availability, and the confidentiality of user credentials.

The method was source-backed: actual paths from discovery through activation,
persistence operations, and existing tests were inspected. The gate uses the local
Registry, metadata-only ranking, dependency resolution, and policy before opening any
body
([gate.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillgate/gate.go#L50)). Promotion occurs only after
checksum verification, bounded extraction, and structural/policy validation
([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L207)); the pointer is
retained only after health, integrity, and external activation complete
([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L344)).

Out of scope for this version: executing skill hooks, granting shell access through
a skill, installing third-party dependencies, replacing the orchestrator,
semantically isolating every malicious instruction inside an LLM, and providing an
independent signature when the upstream does not publish one.

## 2. Assets, actors, and trust boundaries

### Assets

- IVOAI policy and capability allowlist;
- private Registry and its IDs, revisions, digests, and lifecycle;
- active/previous pointers and immutable supply-chain store manifests;
- official Codex/Claude TUI and sandbox/tool approval configuration;
- provider secrets, memory, Context, and user data;
- availability, reproducibility, and rollback of managed sessions;
- bounded observability without raw prompts, bodies, credentials, or environment.

### Actors and capabilities

- legitimate, compromised, or malicious upstream maintainer;
- attacker who has taken over a repository, branch, tag, or download channel;
- archive crafted for traversal, links, bombs, executables, or duplication;
- local user able to tamper with the Registry, pointer, or object outside the flow;
- skill/transitive dependency attempting to expand capability, risk, or authority;
- untrusted text originating from a skill, Context/RAG, artifact, worker, or tool;
- process failure/interruption during staging, promotion, reading, or rollback.

### Boundaries and flows

```text
Internet/upstream (untrusted)
  -> discovery da default branch
  -> resolução para commit imutável
  -> fetch bounded + digest
  -> staging privado (sem execução)
  -> metadata-only index / quarantine
  -> graph + IVOAI Policy
  -> deterministic smoke
  -> immutable object + atomic pointer
  -> Registry activation na mesma transação

Managed session
  -> intent bounded
  -> local Registry only (sem rede)
  -> ranking / graph / policy
  -> authenticated active object
  -> bounded body read + second active-pointer check
  -> official Codex/Claude instruction channel
```

The Registry uses bounded reads, strict JSON, normalization, atomic writes, and
symlink rejection along the path
([store.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skills/store.go#L25)). Indexing reads frontmatter only
and quarantines symlinks, invalid metadata, duplicate IDs, and missing dependencies
([index.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skills/index.go#L37)). Extraction accepts only regular
files/directories, limits bytes and item count, rejects links, and uses `O_NOFOLLOW`
([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L620)).

## 3. Threat analysis and controls

| Scenario | Impact | Control and evidence | Residual risk |
|---|---|---|---|
| Compromised/malicious upstream maintainer or takeover | malicious pack is promoted | requested source must match; active revision is an immutable commit; digest and trust are distinct fields; policy can raise the minimum trust level ([update.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillupdate/update.go), [policy.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/policy/policy.go#L94)) | commit + local digest prove reproducibility/integrity, not independent identity |
| Revision, provenance, or digest mismatch | artifact substitution or false rollback | floating resolution is refused; object manifest and stored provenance are revalidated before/after promotion and rollback ([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L344)) | simultaneous compromise of upstream and unsigned channel |
| Tar traversal, symlink, hardlink, executable, or bomb | write/execution outside the root or DoS | path containment, forbidden links, compressed/expanded/per-file/count limits, sanitized modes, and no execution of staging code ([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L230), [supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L620)) | hashing/parsing cost remains within configured limits |
| Malicious metadata/frontmatter | panic, privilege inflation, or graph poisoning | bounded parser, schema allowlist, quarantine, and strict Registry; graph detects missing/cycle/conflict/authority | malicious terms may affect ranking, but not policy |
| Body with prompt injection | model tries to ignore policy or request shell access | body opens only after graph/policy; policy never receives the body; bundle declares IVOAI precedence; tool/sandbox enforcement remains outside the text ([gate.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillgate/gate.go#L85)) | the LLM can still be semantically influenced within already granted capabilities; origin/trust and minimal selection remain essential |
| Skill requests shell access, privilege, sandbox disablement, or orchestration authority | arbitrary execution or control-plane takeover | undeclared/unknown/unavailable capability fails closed; destructive and authority capabilities are denied; high risk requires approval ([policy.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/policy/policy.go#L160)) | future Approval UX must present scope clearly |
| Manipulated transitive dependency or conflict metadata | unreviewed skill enters through composition | every attempt resolves the complete closure and evaluates policy for every member; conflicts prevent the combination ([gate.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillgate/gate.go#L85)) | incorrect IVOAI-owned classification remains a human risk |
| Registry/pointer/object tampering | activation of an unregistered body | Registry, source, revision, and archive digest must match; active object is authenticated; read repeats `Active` to detect replacement ([gate.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillgate/gate.go#L170)) | local attacker with complete control of the user can tamper with code and data together |
| TOCTOU during content load | mixed revision A/B | bounded regular-file read between two authenticated checks of the same pointer/revision/digest ([gate.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillgate/gate.go#L170)) | does not protect against complete process/OS compromise |
| Failure/interruption during promotion | pointer and Registry diverge | `PromoteWithActivation` restores Registry and pointer if apply/validate/journal fails; recovery reconciles the external index ([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L344)) | physical failure that prevents all writes requires operator intervention |
| Context/RAG, artifact, worker, or tool attempts to change policy | policy bypass through external content | only structured IVOAI types reach graph/policy; external text is not interpreted as configuration/capability | an executor may repeat misleading text to the user; it receives no additional authority |
| Exfiltration through logs/Registry/journal | persisted secret | events use an allowlist with bounded IDs/reasons; Registry rejects secret-shaped URLs; journals store only operational provenance ([event.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/observability/event.go), [store.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skills/store.go#L25)) | approved body is sent to the executor by design and must be licensed/reviewed |

Critical failures of integrity, provenance, path containment, Registry/pointer,
policy, or graph block promotion/activation. An absent/empty Registry selects zero
skills. A corrupt Registry or unavailable optional component degrades to a basic
session without skills; an explicitly required skill fails the operation.

## 4. Validation, assumptions, and residual risks

Hermetic tests cover A→B/no-change/rollback, checksums and limits, malicious archives,
staging without execution, interruption, corrupt previous state, Registry/pointer
divergence, TOCTOU, dependency/conflict/authority, policy deny/approval, prompt
injection, and observability without body/secret. Bounded fuzz targets exercise
frontmatter and archive paths. No regular test authenticates a provider, uses a real
LLM, or executes third-party code.

Assumptions:

- the kernel/filesystem honors `O_NOFOLLOW`, atomic rename, and private permissions;
- the process and local user account are not fully compromised;
- discovery adapters deliver untrusted data to the pipeline, never implicit
  authorization;
- the IVOAI-owned classification/overlay is reviewed and committed as code;
- an active version is always commit-pinned; a branch/tag is only for discovery.

Residual risks and promotion blockers:

- a source without verifiable licensing/provenance must remain deferred/quarantined;
- a trust policy stronger than a local digest requires an actually published
  attestation/signature; it cannot be fabricated;
- an approved body remains untrusted natural language and may influence the LLM
  within existing permissions;
- complete human approval remains future work; HIGH does not activate automatically;
- changes to the parser, extractor, pointer, Registry, or event allowlist require
  rerunning the adversarial suite and the v0.5.0 matrix;
- any failure that logs a body, prompt, raw environment, credential, or authorization
  header is a promotion blocker.

Independent automated review by a subagent was not used in this cycle because the
active collaboration mode did not authorize delegation. A second sequential pass
focused on boundaries, persistence, TOCTOU, and redaction; its result is reflected
in the table and cited tests.
