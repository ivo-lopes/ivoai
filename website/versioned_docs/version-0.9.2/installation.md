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

Supported server platforms are Debian 12+ and Ubuntu 22.04+ on amd64 and arm64.
