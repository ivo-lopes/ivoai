# Saúde do wiring dos hooks de memória

O menu Memory oferece **Hooks — Status**, **Hooks — Validate** e
**Hooks — Repair owned wiring**. Interface equivalente:

```sh
ivoai memory hooks status
ivoai memory hooks validate --json
ivoai memory hooks repair
```

A validação não executa eventos lifecycle nem envia payload de teste. Verifica
target registrado, permissão de execução, interpretador e wrapper gerenciado.
`healthy` certifica wiring local, não disponibilidade do serviço Memory.

Repair exige ownership de componente IVOAI existente. Um comando semelhante a
instalação ai-memory não comprova ownership. Paths anteriores desconhecidos
permanecem intactos com `ownership_verification_required`. Outros hooks e
settings são preservados.

Se o operador verificou independentemente o ownership de um executável antigo,
`repair --verified-previous-binary /absolute/old/ivoai/bin/ai-memory` permite
migrar exatamente esse target. A TUI oferece o path opcional com confirmação
`OWNED`. Não confirme só porque o path parece IVOAI. O preflight nunca adota
paths desconhecidos automaticamente.

A instalação persistente usa environment restrito. Credentials de provider e
`AI_MEMORY_AUTH_TOKEN` ambiente não podem entrar nos comandos dos hooks.
Autenticação pertence ao environment transitório da sessão iniciada.
