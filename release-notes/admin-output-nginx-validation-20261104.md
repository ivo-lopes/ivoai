# Recoverable administrative output and real Nginx validation

Base: `7febead4`. This increment closes the earlier explicit limitation that only a
bounded administrative output preview was retained. It does not relabel the
remaining unavailable semantic matrix operations as certified.

## Delivered behavior

A result exceeding the visual preview budget now includes an existing
`workingcontext.ResultRef` and an authenticated download URL. The ArtifactStore
contains the **complete sanitized projection**, never a raw-output copy. The
browser offers a deliberate download button, shows the expiry, and retains its
instruction never to repeat a mutation to recover output. Download requests never
invoke the administrative dispatcher.

The artifact owner is derived from the configured host/resource, OAuth client and
exact authenticated credential. Different grants of the same client cannot read
each other's output. Authentication, grant revocation, owner/root integrity and
artifact integrity/expiry are rechecked on recovery. The reference remains usable
across a server restart with the original still-valid credential; knowledge of its
URL/ID does not confer authority. Responses use no-store, nosniff, a plain-text
attachment and the existing same-origin/proxy boundary. Audit contains only the
fixed download action, verdict and hashed actor.

Sanitization precedes persistence. It removes existing secret patterns, private
key blocks (including incomplete blocks), newly issued activation codes, private
input values, the current OAuth cookie and known server/MCP credentials collected
before/after dispatch. If the private redaction context cannot be read safely,
output is withheld. Explicit one-time creation codes remain a separate transient
response field and are absent from archived output, receipts and audit.

Limits are explicit: preview at most 256 KiB (adaptively smaller when JSON escaping
requires it); in-memory capture/artifact at most 16 MiB; 64 MiB per credential;
256 MiB per instance; 15-minute expiry. Incomplete capture or storage/quota failure
returns a warning without a complete reference and preserves the actual service
outcome/request claim. Expired artifacts cannot be read and are collected on
subsequent retention. Raw output never enters a temporary staging file. No
background job scheduler or new database was added.

The preview is bounded after JSON escaping: HTML-heavy text can no longer overflow
the response envelope and hide the reference after a committed mutation. A
measured long-line regex hot path was also reduced without changing current
redaction rules: plain ASCII text without any current pattern marker avoids the
regex machinery; non-ASCII retains the original Unicode case-folding path.
This is a local output-processing improvement, not a session token/cost claim.

## Actual checks

- Focused artifact tests: exact sanitized recovery including a tail beyond the
  preview; same-client/different-grant isolation; unauthenticated, expired and
  revoked denial; restart recovery; no credentials/codes/key material in retained
  files; no mutation replay; incomplete-capture warning; invalid private context;
  JSON expansion; storage failure preserving outcome; Unicode redaction parity.
- Browser contract fixture: explicit download retrieves the sanitized attachment
  under `/` and `/ivoai/admin`, without another mutation. Existing a11y/keyboard/
  viewport/review/one-time-code/revocation/no-replay gates remain enabled. This
  browser fixture is not an OAuth/backend deployment certification.
- **Real Nginx 1.26.3** executed from `/home/ivo/.local/bin/nginx`, SHA-256
  `254fac73e79add393fef33f514bc91b8a98ad1966c2b43c77a98c9359580a6d8`.
  A private configuration passed `nginx -t` and launched on a loopback ephemeral
  port with an ephemeral trusted TLS certificate, private key, PID, temp paths and
  disabled access log. No personal/global Nginx configuration was loaded/changed.
  All compiled-in temp paths were explicitly redirected into the fixture.
- Through that **actual Nginx process**, the test passed TLS certificate validation,
  mounted HTML/assets, prefix redirects, OAuth PKCE authorization/callback,
  Secure/HttpOnly/SameSite/path cookies, management scope, missing-CSRF and hostile-
  Origin denial, a real `App.ConfigSet` persisted change, complete sanitized output
  download, request replay denial, outside-prefix denial and logout/revoked
  download denial. This is separate from prior `httputil` proxy tests.

Reproduce the external-runtime gate explicitly:

```sh
GOMAXPROCS=2 IVOAI_TEST_NGINX=/absolute/audited/nginx \
  go test ./internal/admin -run '^TestRealNginxAdministrative' -v -count=1
```

Without that environment variable, the Nginx test reports a skip; it must not be
counted as a real proxy success. An explicitly configured missing/broken executable
fails. This gate certifies the recorded local binary and fixture, not an arbitrary
Nginx version, public domain, production certificate, Caddy deployment, provider
inference or privileged production service administration.

No historic benchmark/proxy evidence was replaced. Initial fixture failures
(compiled-in temp paths, Nginx's absolute prefix redirect, and an overlong Chromium
Unix socket path) were retained separately and corrected in isolated fixture
configuration. An obsolete slow race run was explicitly stopped after the
bounded-sanitization optimization; later applicable checks are recorded separately.
The full semantic matrix and its unavailable provider/systemd/update/restore/scope
preconditions remain governed by the previous report and release coordinator.
