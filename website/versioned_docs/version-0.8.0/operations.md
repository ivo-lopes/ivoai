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
