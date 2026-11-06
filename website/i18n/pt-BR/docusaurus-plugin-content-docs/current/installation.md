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

O setup e o updater atuais não instalam, reparam, sondam nem executam OpenCode. `--legacy-opencode` apenas informa a migração. Binários, propriedade, configurações, credenciais, perfis e históricos permanecem intactos. O bundle novo exclui OpenCode; rollback usa o Core/perfil anterior e os assets originais imutáveis.

As plataformas de servidor compatíveis são Debian 12+ e Ubuntu 22.04+ em amd64 e arm64.
