# Update and rollback

```bash
ivoai update --dry-run
ivoai update
ivoai update --rollback
```

Updates validate release checksums, component provenance, schema compatibility and
transactional migration metadata before activation. Rollback restores the previous
binary and compatible IVOAI-owned mutable state. External executors and their auth
stores remain outside IVOAI ownership.
