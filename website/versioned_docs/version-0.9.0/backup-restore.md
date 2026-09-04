# Backup and restore

```bash
sudo ivoai server backup --output /var/lib/ivoai/backups/ivoai-backup.tar.gz
sudo ivoai server restore --input /var/lib/ivoai/backups/ivoai-backup.tar.gz
```

Backups include authoritative IVOAI data needed for restoration, exclude secrets and
rebuildable indexes, reject links/traversal, and quiesce managed services around the
operation. Keep a separate protected backup of enrollment and OAuth administration
according to your organization's secret-management policy.
