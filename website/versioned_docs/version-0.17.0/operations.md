# Operations

Use `ivoai status` for concise readiness and `ivoai doctor` for actionable diagnostics.
Server operators additionally use `ivoai server status`, `ivoai server doctor`, backup,
restore, logs, enrollment, and Web-access lifecycle commands.

The docs service is managed by systemd, serves only a prebuilt Docusaurus site, and
logs through journald:

```bash
systemctl status ivoai-docs.service
journalctl -u ivoai-docs.service
curl -f http://127.0.0.1:7780/
```

## Managed ai-memory hook diagnosis and repair

`ivoai doctor` validates receipt-owned hook registrations, binary and wrapper
integrity, executable permissions, symlinks, shebangs and interpreter availability.
Use `ivoai doctor --repair-hooks` or the explicit **Repair Memory Hooks**
action in Dashboard diagnostics after reviewing the findings.

Installation renders the reviewed ai-memory hook definitions in an isolated
HOME/XDG/provider environment, then merges only its exact registrations. Private
receipts record the managed binary digest, wrappers and verified archive assets.
Repair restores missing files and executable permissions only for resources proven
by those receipts, saves private backups and
rolls back earlier writes if a later transaction step fails. Other tools' settings
and hooks are preserved. Repeating installation/repair is idempotent.

Unreceipted legacy registrations are diagnosed but never silently adopted by a
name or substring match. Review those explicitly before changing them. Modified
wrapper or source-file content is preserved and requires operator review. Changed managed binaries
need component repair/update before hooks can be restored. A missing wrapper
also requires explicit repair; a provider cannot invoke preflight inside a file
that does not exist.

Managed sessions create a private, process-local failure latch. An optional hook
backend that exits 127 is reported once and skipped for subsequent events in the
same session, without disabling other tools' hooks or changing global settings.
The latch is cleaned up with the session. Hook repair does not reinstall the
agent, authenticate either provider or migrate the ai-memory database.
