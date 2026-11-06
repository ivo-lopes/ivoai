# Administrative console delivery

The web console manages the existing backend through its typed action catalog,
without another scheduler or conversational frontend. Read/manage authority,
CSRF, exact confirmations, durable request claims and live revocation govern
mutations. Native-client login remains an explicit terminal handoff; OS privileges
are never acquired from a web request.

HTTPS reverse-proxy publication supports an explicit origin and base path,
trusted socket peers, scoped cookies/callbacks and reviewed Nginx/Caddy templates.
The actual Nginx test passed, including OAuth, CSRF, persisted App configuration,
output download, duplicate rejection and logout/revocation. It used only private
config/certificate/PID directories and loopback ports.

Large complete sanitized results offer an expiring, grant-bound download through
the existing ArtifactStore. Raw credentials and one-time codes are not archived.
Incomplete captures are entirely withheld, with the committed operation outcome
preserved and no automatic replay. Writer fast paths cannot bypass capture limits.

Both documentation locales and the embedded site are refreshed. The homepage now
describes the managed backend and administration; generated CLI help is maintained
in both locales. Strict translation gates were preserved and exposed missing
translation/command-block/link parity, all corrected before validation.

The source-bound [validation](evidence/admin-complete-delivery-20261106/validation.json)
records152 browser accessibility checks/zero skips,2 hermetic and4 real App/OAuth/
Store browser modes, full admin race with actual Nginx36.583s, vet and CLI reference
checks. Existing operation-by-operation semantic matrices retain their unavailable
OS/provider cases; catalog count is not a claim of successful external execution.
No production deployment, provider-inference parity or global economy is claimed.
