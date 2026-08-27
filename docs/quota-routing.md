# Subscription quota routing

The quota manager normalizes structured telemetry from official subscription-backed
clients. It never reads, stores, copies, or logs provider OAuth tokens, cookies, auth
headers, or raw auth responses.

## Sources

### Codex

ivoai starts the trusted managed `codex app-server --stdio`, performs the documented
JSON-RPC initialization, and calls `account/rateLimits/read`. The official response
can expose:

- rolling windows with their official duration (for example 300, 60, or 1440 minutes);
- a seven-day/weekly window when duration is 10080 minutes;
- an `individualLimit` provider-wide window whose cadence is not inferred;
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

The session-local statusline wrapper composes an existing user/project command when
it can be read safely, so quota capture does not silently remove the user's normal
statusline. Persistent Claude settings are never rewritten.

The current supported Claude payload does not require a monthly subscription window.
The primary monitor therefore does not render a Claude monthly row. ivoai does not
scrape claude.ai, browser sessions, ANSI UI output, Cloudflare, or internal tokens.

## Normalization

All percentages are clamped into `0..100`. For used-percentage sources:

```text
remaining = clamp(100 - used)
```

Every window records kind, official duration when exposed, optional model,
used/remaining, optional reset time, source, observation time, authority,
availability, and telemetry state. Context, rolling, session, weekly, individual,
monthly, model-specific, and credit windows are distinct. `pending`
means Claude has not yet returned rate limits, `not_exposed` means a subsequent
structured payload omitted that field, `stale` preserves an old value with an
explicit warning, and `exhausted` is an authoritative zero. Only the last state
blocks routing. Primary and secondary are treated as transport slots: their order
does not define semantics. A 300-minute Codex window is rendered as 5h; 10080 as
weekly; 60 as 1h; 1440 as 1d. Unknown durations remain representable.

## Eligibility and routing

A provider is eligible when its subscription authentication is valid and no
authoritative hard limit is reached. A model-scoped zero blocks only the exact model
named by authoritative telemetry; it does not disable an unrelated model or an
unspecified provider route. The manager resolves the preferred provider first, then
the alternate. It returns an explicit decision and reason; callers may not bypass
it.

Automatic task routing adds a capability registry above this gate. Codex model names,
default flags, and supported reasoning efforts come only from the structured official
app-server `model/list` response. Claude's validated CLI exposes supported effort
choices but no equivalent structured model catalog, so its model remains the client
default. Capability cache entries are keyed by client version and invalidated on a
version change.

For each task, IvoAI first determines the required tier from the objective score,
then finds the lowest sufficient catalog model and supported effort. An exact
model-specific zero rejects only that model; another sufficient model from the same
provider is tried before the alternate provider. If more than one profile meets the
quality floor, authoritative remaining quota can preserve the more constrained
provider. Quota pressure never permits a profile below the required tier. Unknown
telemetry remains unknown rather than becoming zero.

The progression LIGHT → BALANCED → STRONG → MAX is not a vendor mapping. Model IDs
are never hardcoded from tier names. Escalation advances one tier only after an
evidence-based validation failure or risk reassessment.

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

An explicit `ivoai connect chatgpt|claude` is an authentication-context boundary:
ivoai invalidates only that provider's quota before the official login/status flow
and force-probes after success. `disconnect` invalidates the same provider without
logging out the official client. This deliberately avoids token fingerprints or an
invented account ID. If the post-login probe fails, old account quota is not
restored. Legacy v0.5.0 snapshots without duration remain readable; no legacy
session value is guessed to be a 5-hour Codex window.

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

Session observability is an additive bounded event list with explicit fields for
component, operation, state, correlation IDs, duration and canonical routing/fallback
reasons. The schema has no fields for prompts, responses, artifacts, headers,
environments or credentials. Reasons are allowlisted and secret-shaped input is
reduced to a redacted sentinel.
