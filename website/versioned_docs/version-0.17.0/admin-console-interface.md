# Administrative operations in the web console

The administrative console uses the existing IVOAI backend. Conversations remain
in the official native clients. Open **Administração** to search operations by
name or identifier, choose an area and filter consultations, changes and native
client guidance. Sessions, components and connections retain their dedicated
views.

## Access and changes

A read grant can query state. A management grant additionally permits changes;
the browser does not upgrade a read grant. Operations requiring operating-system
privileges remain unavailable when the administrative process lacks those
privileges.

For a change, complete its form and choose **Revisar alteração**. Review the
summary and values, explicitly authorize the change and choose **Confirmar
alteração**. The backend binds the short-lived confirmation to the action,
values and access identity. A consumed confirmation or request identifier does
not execute another mutation, including after a restart. Changing the form after
review requires a new review.

A lost connection can leave the result unknown. The console does not submit
another operation automatically. Consult the current state before explicitly
starting a new change. Closing an operation in progress is disabled; closing the
browser is not proof that the operation was cancelled.

Results describe completion, failure, cancellation or native-client guidance.
An instruction to continue in a native client is not an execution in that client.
Backend failures or unavailable actions remain visible; they are not represented
as successful operations.

## Private values

Existing credentials are excluded from administrative results. A password field
is masked and its value is omitted from review text. A newly created one-time
access code appears only in its result, masked until explicitly revealed. Copy
it deliberately before closing that result; it is removed on close or sign-out
and never enters the activity list. Clipboard copying occurs only when requested.

When a complete sanitized result exceeds its preview, the console offers an
authorized download with an expiry. This projection excludes redacted credentials.
An incomplete capture above the safety limit is withheld entirely and never
presented as a complete artifact. An already executed operation is not replayed
to recover unavailable output.

The activity list contains operation names, outcome labels, request identifiers
and timestamps for this tab. It is cleared on sign-out or loss of authority and
is not a durable audit log. OAuth tokens remain in HttpOnly cookies; the UI uses
no localStorage or sessionStorage. CSRF and confirmation tokens exist only in
memory.

## Reverse proxy and accessibility

Assets, API requests and authorization links stay under the configured public
base path, such as `/ivoai/admin/`. The page checks that the backend's base path
matches its own asset origin. It does not infer authority from forwarded headers
or construct another host from browser input. The backend's reverse-proxy
configuration defines the public origin, trusted proxy and cookie policy.

The same interface supports keyboard navigation, explicit field labels, focus
restoration, visible errors and reduced motion. Layouts are checked at375,768,
1024 and1440 pixels, including the review drawer. An unknown measurement remains
unknown; invocation token usage is not a session saving.

## Coverage and evidence

The candidate catalog contains123 entries: the existing118 menu identifiers plus
five additional profile/context operations. Searchable titles and supported form
controls cover all123 entries. This counts UI contract coverage, not123 successful
live administrative executions. Native-client handoffs and navigation-only
entries are classified separately. The source-bound UI inventory and browser
results are in `release-notes/evidence/admin-console-management/`.

Hermetic browser fixtures validate UI behavior without production state or
provider inference. The separate integration runner requires successful real
App/OAuth/Store fixtures for sign-out, revocation, an isolated configuration
change and a TLS reverse proxy; a missing or skipped fixture fails the gate.
