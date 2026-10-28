# Administrative OAuth route projection

The local administrative browser backend uses the existing authorization Store
directly for client registration, code exchange and token revocation. It only
needs the shared OAuth server's browser authorization form over HTTP. Mounting
the complete Web MCP OAuth router also exposed unused registration, token,
revocation and discovery endpoints on the admin listener.

The admin listener now projects only GET/POST `/oauth/authorize`. Unknown OAuth
GET/POST routes return 404; discovery routes also return 404. The full Web MCP
OAuth service is unchanged. This reduces unnecessary surface; it is not a claim
of an authentication bypass or a formal security certification.

## Validation

The new regression test first failed on the original source: external
`POST /oauth/register` returned 201. The corrected test exercises authenticated
and unauthenticated requests, rejects unused routes without setting cookies,
checks that persisted authorization state is byte-identical, and confirms the
existing browser session can still read the protected overview.

An intermediate routing attempt failed during ServeMux construction because
an all-method subtree conflicted with the existing GET static route. It was
replaced with explicit GET/POST projections. Both failures remain recorded;
neither is counted as a passing run.

On the corrected source, Go 1.27.3 `go test -race ./internal/admin
./internal/webauth` and `go vet` passed. The existing Web MCP authorization tests
remain intact (the webauth race package reused its successful Go cache).
`scripts/test-admin-console-ci.sh` then compiled this source and passed the
hermetic UI checks and two independent real local App/OAuth/Store browser flows
(logout and revocation), each requiring backend PASS and exit zero, with no
skipped fixture. Node 26.11.1 and system Chromium 156.0.8078.47 were used locally;
the exact-head CI uses its separately installed locked Playwright Chromium.
There was no provider inference, personal credential access or production state.

Source/log digests and the earlier, unrelated CI failure are in
[`evidence/admin-console/oauth-surface-validation.json`](evidence/admin-console/oauth-surface-validation.json).
The earlier PR44 head `8e56741a` failed its managed OpenCode readiness integration
in CI40715183470; the admin browser step had passed. That failure is not waived,
and the final candidate still requires all source/release gates.

Rollback restores the previous admin route exposure. No stored grants, personal
client configuration or Web MCP public contracts are migrated by this change.
