# Local administrative console: first read-only slice

This change partially implements IVOAI-189. `ivoai admin serve` explicitly starts
a local-owner console over a numeric HTTP loopback address, using the existing
App, configuration, session and Web OAuth stores. It adds no conversation UI,
executor, scheduler, background service, network provider probe or administrative
execution endpoint. Ordinary native-client sessions do not start the listener.

The console reads a bounded session subset, allowlisted lifecycle events, recorded
component versions and aggregate connection configuration. Persisted Codex token
reports remain `client_reported` / `codex_exec_turn_completed` / `cli_invocation`;
cached input is a subset of input. Missing usage stays unknown. These values do
not establish session totals, quotas, costs or economic improvements.

## Access and isolation

Run the console as the existing local IVOAI owner. In a separate terminal, issue
`ivoai admin access create --ttl 10m`, open the displayed loopback console and enter
the one-time code in the OAuth form. Only this explicit issuance command prints
the secret code. `admin access list` prints metadata; `admin access revoke <id>`
revokes the grant and its subsequent authenticated reads. Stop the listener to
disable this surface. No native-client settings or credentials are migrated.

The existing Web OAuth mechanism accepts an immutable per-store scope policy.
Its old constructor retains the Web MCP defaults. The admin policy accepts only
`admin:read`, in a private `StateRoot/admin/<instance>/oauth.json` store using the
same storage mechanism. Its audience, cookie names and instance identity bind
the owner-selected ConfigRoot/StateRoot; sharing a StateRoot with another
ConfigRoot does not share activation codes. Web MCP tokens, connector credentials
and session/Bridge capabilities are not admin authority.

Browser login uses existing single-use OAuth codes, S256 PKCE, a bounded in-memory
flow map, state-cookie matching and the existing form CSRF nonce. The BFF exchanges
the code itself and places only the access token in an HttpOnly/SameSiteStrict
cookie. No access/refresh token is placed in browser JavaScript, local/session
storage, page URLs or logs. The browser receives no refresh token. A one-time
authorization code necessarily passes through the callback URL; the callback
immediately redirects and uses `no-referrer`/`no-store`.

Only the GET OAuth form uses `Referrer-Policy: same-origin`. A real Chromium
form submission from a `no-referrer` form produced `Origin: null` and was rejected;
HTTP cookie-jar tests had not exposed this. Keeping a same-origin referrer on that
one document permits an exact Origin check without accepting opaque/null origins.
Console, callback and API responses retain `no-referrer`. Cross-site, wrong-Host,
wrong-Origin and non-loopback-peer requests are refused. Forwarded headers do not
confer authority. There is no remote operator or organization-RBAC delegation in
this first slice; do not publish this local listener through a reverse proxy.

Every API snapshot revalidates the grant, audience, scope and root identity before
and after collection. Callback rechecks also surround the code exchange before
issuing an access cookie. These checks detect changes; they are not an atomic
transaction against another privileged local process. The local owner and
filesystem remain trusted. Shared Web OAuth locking and filesystem I/O are
cooperative rather than forcibly interrupted by a request deadline.

## Bounds and audit

The server admits eight requests concurrently, 240 requests/minute, at most
64 KiB request bodies and 512 KiB serialized JSON responses. Session reads reuse
the existing parser, namespace rules and validator through `GetBounded` and
`ListBounded`. Listing scans at most 33 entries in each of three existing
namespaces and returns at most 32 validated files of 256 KiB each. It never creates
a session directory. The subset is not guaranteed to contain the latest global
session; `partial` and `truncated` remain visible. Canonical session opens now use
`O_NONBLOCK` so a FIFO can be rejected as non-regular before an open blocks.

DTOs do not serialize raw configuration, session traces, prompts, working paths,
MCP aliases/URLs/headers, tool output or credentials. No GET starts a provider or
changes execution state. Authentication still uses the existing private OAuth
lock/store bookkeeping. Audit goes to the existing App error stream with fixed
action/verdict labels and a hashed actor identifier; request URLs, queries, codes,
cookies and bodies are not logged. Explicit activation issuance remains an
operator-facing secret channel, not telemetry.

## Administrative parity

`evidence/admin-console/administrative-parity.json` enumerates all 118 canonical
`PublicMenuActionIDs` from `internal/cli/menu.go`, binding the source SHA256. Zero
actions have complete administrative parity: six have a partial read projection
and 112 remain unimplemented. Dynamic menus, command-only operations and service
surfaces also require acceptance before global parity can be claimed.

This change does not implement setup/update/rollback/uninstall, executor start or
stop, approvals, secret editing, backup/restore, connector purge, native login,
evidence/cache/index management or mutations to session execution. The frontend
only refreshes backend snapshots. There is no second authorization or orchestration
authority in JavaScript.

## Validation and adverse results

The backend checks use Go1.27.3 and private ext4 scratch space. Focused admin,
Web OAuth, session and CLI packages passed; the same four packages passed race
detection and vet. Tests cover scope/default compatibility, wrong audience,
cross-instance/shared-StateRoot activation rejection, bearer rejection, expiry,
revocation, logout, PKCE/state/CSRF replay, opaque Origin refusal, Host/fetch
boundaries, source canaries, unknown token metrics, bounded reads, FIFO/symlinks,
concurrent snapshots and replaced roots. A callback test changes ConfigRoot
inside the code exchange and verifies no access cookie is issued.

The first full Go suite failed the existing
`TestNativeLowQuotaRequiresConfirmationAndPreservesExplicitRoute/opencode/nine_approve_conservation`
test after its five-second deadline during concurrent validation. The original
failure is retained. The unmodified case passed alone in 6.150 seconds across all
its subcases; the complete suite then passed with `go test -p 1 ./...`. This is a
separate successful retry, not a relabeling of the first run or proof of the
precise cause. No test timeout or assertion was weakened.

Earlier interrupted-session preflights encountered a full `/tmp`, and CLI Skills
integrity checks failed on the workspace NTFS filesystem while passing on tmpfs.
Those historical reports are not counted as successful gates. The temporary
artifacts from that session were lost when `/tmp` was cleared; new validation uses
private ext4 scratch and publishes sanitized source/log hashes. No missing
historical artifact has been reconstructed as an original result.

Independent real Chromium runs completed the App/Store OAuth login, session/event
projection, reported100 input/2 output/60 cached tokens, three views at375/768/1024/1440
widths, accessibility checks and filters/drawer interactions. Separate runs proved
logout and externally revoked grants clear the displayed snapshot. A first live
harness run reached the backend/UI assertions but failed during provenance metadata
collection because a dependency package.json was not exported; that run remains
FAIL, followed by complete successful retries. The hermetic frontend fixtures are
reported separately and are not counted as real backend integration. See
`evidence/admin-console/ui-validation.json` for sanitized versions, hashes and limits.

The canonical CLI-reference check and CGO-disabled LinuxAMD64v1/ARM64 builds passed.
The in-tree frozen-lockfile documentation build and full embedded-content check
both passed for EN/PT, with i18n parity at50 current and44 v0.9.2 documents. No
other worktree node_modules or NODE_PATH was borrowed. Sanitized backend log hashes,
source identities, WIP build identities, cache counts and failure history are in
`evidence/admin-console/backend-validation.json`. No production deployment, provider inference, paid API,
personal-client configuration change, ARM execution or no-AVX runtime certification
is claimed here.
