# Administrative action UI candidate

This increment expands the previous read-only console to consume the same
administrative action catalog as the backend. It introduces searchable/filterable
operations, typed forms, read/manage grant states, explicit preview/confirmation,
short-lived review, sanitized results and native-client guidance. No browser
scheduler or conversational frontend is introduced.

Relative assets and script-derived API paths support a configured proxy prefix.
The page validates the backend's declared base path. Newly issued one-time codes
are masked, revealed/copied only by an explicit user action, omitted from tab
history and cleared on close, sign-out or authority loss. Pending write outcomes
are not replayed by the interface.

## Validation and limits

The source-bound receipt records the original read-view browser regression and
new administrative-action browser fixtures at both `/` and `/ivoai/admin/`.
Both use isolated browser homes, deny external page requests and exercise WCAG
A/AA/2.1 checks, overflow and44px interactive targets at375/768/1024/1440 pixels.
New checks cover read-only denial, immutable reviewed values, explicit single
mutation, masking/clearing private values, native handoff labels, failed
transport, authority loss and clearing all tab state. The fixture models server
NoReplay; it does not certify production mutation or a deployed proxy.

Earlier fixture failures are retained in private local diagnostics: the transport
fixture initially asserted that a consumed confirmation could never be seen
again. Chromium can retransmit a POST after transport loss, so the fixture now
returns409 for an already-consumed confirmation and requires exactly one committed
operation. A subsequent check exposed a close-event timing window; the UI now
clears a one-time code synchronously on Escape or the close button. Neither
failure is counted as a passing run.

The live browser test additionally supports real isolated App.ConfigSet and a
fresh configuration read. Its proxy mode uses an ephemeral local TLS reverse
proxy, verifies prefixed OAuth redirects/assets/API/sign-out and Secure/path-bound
cookies. Only that explicit disposable fixture may ignore its self-signed test
certificate. This does not relax production TLS.

The CI runner requires two hermetic browser groups and four real App/OAuth/Store
modes, with backend exit and no skipped fixture checked separately. Seven negative
process-contract tests prove compilation/browser/backend failures, skipped
backend fixtures, missing management proof and missing actual proxy evidence
cannot produce the aggregate PASS marker. These fake process tests are never
browser or provider evidence.

The UI copy inventory covers123 catalog identifiers, but live certification is
recorded per operation. Administrative commands unavailable without existing OS
privileges stay unavailable. No provider inference, personal configuration,
production corpus, production reverse proxy or global economic certification is
claimed by this UI increment. Integration depends on the corresponding backend
catalog, write grants, confirmation/NoReplay and proxy implementation.
