# Memory hook wiring health

The Memory menu exposes **Hooks — Status**, **Hooks — Validate**, and
**Hooks — Repair owned wiring**. The equivalent interface is:

```sh
ivoai memory hooks status
ivoai memory hooks validate --json
ivoai memory hooks repair
```

Validation does not invoke lifecycle events or upload a test payload. It checks
the registered target, executable permission, interpreter, and managed wrapper.
This is wiring health, not proof that the Memory service is reachable.

Repair requires existing IVOAI component ownership. A command which merely
resembles an ai-memory installation is not sufficient ownership evidence.
Unknown former paths are reported as `ownership_verification_required`, and are
left untouched. Other hooks and settings are preserved.

If an operator has independently verified a former executable's ownership,
`repair --verified-previous-binary /absolute/old/ivoai/bin/ai-memory` permits
migration of that exact target. The TUI repair action offers the same optional
path with an `OWNED` confirmation. Do not attest a path just because it resembles
an IVOAI installation. Normal preflight never adopts unknown paths.

Persistent hook installation uses a restricted environment. Provider credentials
and an ambient `AI_MEMORY_AUTH_TOKEN` must not be baked into hook commands.
Authentication belongs to the launched session's transient environment.

## Estado dos hooks de memória

O menu Memory oferece status, validação e reparo do wiring gerenciado. A validação
não executa eventos nem envia payloads. `healthy` confirma o registro local; não
certifica a disponibilidade do servidor Memory. Registros sem ownership
comprovado permanecem intactos e indicam `ownership_verification_required`.
