# FAQ

## Does `ivoai auto` use every connected server?

Enabled profiles are candidates for purpose-based bounded retrieval. Explicit
`--knowledge-source` selectors restrict the session; they never expand its grants.

## Are writes replicated to every source?

No. Federation applies to reads; an ambiguous new write fails closed.

## Does IVOAI need provider API keys?

Supported official-client flows use the user's authorized native authentication.
Managed OpenCode is removed. Headless experiments remain opt-in and do not imply
Claude TUI control or certification without the user's account. No paid API
fallback is introduced automatically.

## Is Headroom removed?

No. Its component and explicit choice remain for compatibility and rollback.
Unsafe managed wrapping remains unavailable by policy, with Direct fallback.
