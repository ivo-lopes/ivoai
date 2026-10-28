# Local administrative console

The console provides an authenticated, read-only view of the existing IVOAI
backend. It runs only when explicitly started and does not start executors or
replace native clients. Administrative parity is partial: the inventory contains
118 TUI actions, with six partial read projections and no completely covered action.

## Start and authorize

Use the existing local IVOAI owner and configuration/state roots. Start the
foreground listener:

```sh
ivoai admin serve --listen 127.0.0.1:7781
```

In another terminal, issue a disposable activation code:

```sh
ivoai admin access create --ttl 10m
```

Open the displayed loopback URL and enter that code in the authorization form.
The command prints the code once; keep it out of logs and reports. Native-client
credentials are not requested or copied. The OAuth exchange uses PKCE, state and
form CSRF checks. Access uses an HttpOnly/Strict cookie, with no browser token
storage or refresh token in JavaScript.

The default listener is local HTTP. Only numeric loopback addresses are accepted;
forwarded headers confer no authority. This delivery does not provide remote or
organization RBAC. Keep the listener local. Existing private roots must belong to
the running owner; the command does not relax their permissions.

## Read and revoke

Sessions expose lifecycle state, executor/model identity and the latest recorded
invocation usage when available. Cached input is already included in input.
Missing measurements are unknown, not zero; invocation usage is not a session
total or a savings claim. Component versions and connection counts are recorded
configuration snapshots, not live health probes.

Listing is bounded and can return a subset. The interface labels partial and
truncated snapshots; it does not assert a complete global session count.
Prompts, raw traces, tool output, working paths and connection secrets are excluded.

```sh
ivoai admin access list
ivoai admin access revoke ACCESS_ID
```

Revocation takes effect on subsequent authenticated requests. The browser's
sign-out action revokes its access token and clears displayed snapshots.
Stop the foreground listener with Ctrl+C to disable the surface. Grant metadata
remains in the existing OAuth storage mechanism under the selected state root.

This slice does not configure connectors, edit secrets, approve work, start/stop
executors, manage evidence/indexes, or perform setup, update, rollback or restore.
Those operations retain their existing CLI/TUI contracts. No JavaScript scheduler
or authorization authority is introduced.

The source-bound parity inventory, adverse runs and real local browser proofs
are recorded in [the validation report](https://github.com/ivo-lopes/ivoai/blob/main/release-notes/admin-console-validation.md).
The browser proofs use seeded sessions and no provider inference.
