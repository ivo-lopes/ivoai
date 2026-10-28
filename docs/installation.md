# Installation

The installer verifies platform and release checksums and preserves installations
that are not owned by IVOAI.

```bash
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sh
ivoai version
```

Root installation targets `/usr/local/bin/ivoai`; user installation targets
`~/.local/bin/ivoai`. Run `ivoai setup` after installation. For a private source
checkout, clone with authenticated GitHub access and run `./install.sh`.

Default client setup and candidate setup during updates exclude OpenCode.
Existing OpenCode binaries, state, ownership, personal configuration and history
are retained. The deprecated explicit legacy path can be installed or repaired
with `ivoai setup --mode client --legacy-opencode`; it is not a prerequisite for
`ivoai codex`. Release assets and legacy updater rollback checks still include
OpenCode, so this is progressive migration rather than global removal.

Supported server platforms are Debian 12+ and Ubuntu 22.04+ on amd64 and arm64.
