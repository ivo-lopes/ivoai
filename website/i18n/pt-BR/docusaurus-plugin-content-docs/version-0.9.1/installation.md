# Instalação

O instalador verifica a plataforma e os checksums da release e preserva instalações
que não pertencem ao IVOAI.

```bash
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sh
ivoai version
```

A instalação como root usa `/usr/local/bin/ivoai`; a instalação de usuário usa
`~/.local/bin/ivoai`. Execute `ivoai setup` depois da instalação. Para um checkout
privado do código-fonte, clone com acesso autenticado ao GitHub e execute
`./install.sh`.

As plataformas de servidor compatíveis são Debian 12+ e Ubuntu 22.04+ em amd64 e arm64.
