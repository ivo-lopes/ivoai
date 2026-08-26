# IVOAI v0.5.0 compatibility fixtures

These secret-free fixtures were derived from the real `v0.5.0` tag by
building that tag and running client/server setup in an isolated test root.
The inventory example is a normalized projection into the current support
inventory schema because v0.5.0 did not yet expose that command. Host paths,
timestamps, connection URLs, and installation provenance were normalized. No
production data or provider credentials are included.

- `client/config.toml`: disconnected client with legacy Headroom and Ruflo.
- `client/config-connected.toml`: connection metadata without credentials.
- `client/state.toml`: managed component metadata emitted by v0.5.0.
- `client/ownership.toml`: fully IVOAI-managed installation.
- `client/ownership-mixed.toml`: externally managed Codex plus managed tools.
- `server/server.toml`: server configuration emitted by v0.5.0.
- `server/inventory.json`: sanitized server/runtime fixture metadata.

Provider authentication stores and server secret files are intentionally out
of scope because they are not owned by IVOAI updates.
