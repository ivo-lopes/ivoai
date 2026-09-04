# FAQ

## Does `ivoai auto` use every connected server?

Yes. Without `--knowledge-source`, every enabled connected profile is selected for
bounded reads. The flag restricts the session to exactly the named aliases/purposes.

## Are writes replicated to every source?

No. Automatic federation applies to reads. Ambiguous new writes fail closed.

## Does IVOAI need provider API keys?

No for the supported subscription-owned Codex, Claude Code and OpenCode flows.

## Is Headroom removed?

No. It remains available as a compatibility and rollback provider.
