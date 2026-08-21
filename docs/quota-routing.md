# Subscription quota routing

The quota manager normalizes structured telemetry from official subscription-backed
clients. It never reads, stores, copies, or logs provider OAuth tokens, cookies, auth
headers, or raw auth responses.

## Sources

### Codex

ivoai starts the trusted managed `codex app-server --stdio`, performs the documented
JSON-RPC initialization, and calls `account/rateLimits/read`. The official response
can expose:

- a short primary/session window;
- a seven-day secondary/weekly window;
- an `individualLimit` monthly/spend-control window;
- additional named/model-scoped windows;
- hard rate-limit or spend-control signals and reset timestamps.

The probe removes all known PAYG provider keys and base-URL overrides from its
environment. It bounds execution time, stderr, line size, and response size. It does
not scrape the Codex TUI.

### Claude Code

Authentication capability is checked with structured `claude auth status`; only
`loggedIn`, `apiProvider`, and `authMethod` are parsed. API-key authentication is not
eligible for automatic subscription routing. During an automatic Claude session, a
private invocation-only statusline command receives Claude's official structured
payload and extracts:

- current model;
- context-window used/remaining percentage;
- five-hour/session quota;
- seven-day/weekly quota;
- monthly quota only if a future/current supported client explicitly supplies it.

The current supported Claude payload may not expose monthly quota. In that case the
monitor deliberately shows `Claude Monthly  N/A / not exposed`. ivoai does not
scrape claude.ai, browser sessions, ANSI UI output, Cloudflare, or internal tokens.

## Normalization

All percentages are clamped into `0..100`. For used-percentage sources:

```text
remaining = clamp(100 - used)
```

Every window records kind, optional model, used/remaining, optional reset time,
source, observation time, authority, and availability. Context, session, weekly,
monthly, model-specific, and credit windows are distinct. Unknown means unavailable,
not exhausted.

## Eligibility and routing

A provider is eligible when its subscription authentication is valid and no
authoritative hard limit is reached. A model-scoped zero blocks only the exact model
named by authoritative telemetry; it does not disable an unrelated model or an
unspecified provider route. The manager resolves the preferred provider first, then
the alternate. It returns an explicit decision and reason; callers may not bypass
it.

The dispatch gate runs:

- before the conversation primary starts;
- periodically while the primary is active;
- before every worker;
- after every worker completes;
- after a hard runtime limit signal;
- immediately before failover.

Authentication failures mark a provider unavailable. Confirmed hard limits trigger
fallback. Network/probe failures retain bounded stale metadata, clearly label it
stale, return the probe error, and do not invent exhaustion.

## Cache and security

The default refresh interval is 45 seconds and valid configuration is bounded to
30–300 seconds. `$XDG_STATE_HOME/ivoai/quota/snapshot.json` contains normalized
percentages, reset/source/observation metadata, and eligibility only. It is an atomic
`0600` file in a `0700` directory. A no-follow `0600` lock serializes concurrent
Codex supervisor and Claude statusline writes. Files are size-bounded and reject
symlinks, terminal escapes, line breaks, secret-shaped fields, invalid percentages,
and unknown providers.

`ivoai status` reads this cache only. `ivoai doctor` actively checks sources.
`ivoai monitor --watch` reads session snapshots and can run independently from the
official TUI.
