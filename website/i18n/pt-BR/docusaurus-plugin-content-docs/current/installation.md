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

O setup padrão do cliente e o setup do candidato em updates excluem OpenCode.
Binários, estado, ownership, configurações pessoais e históricos existentes são
preservados. O caminho legado explícito e descontinuado pode ser instalado ou
reparado com `ivoai setup --mode client --legacy-opencode`; ele não é requisito
para `ivoai codex`. Assets de release e verificações legadas de rollback do updater
ainda incluem OpenCode, portanto a migração é progressiva, sem remoção global.

As plataformas de servidor compatíveis são Debian 12+ e Ubuntu 22.04+ em amd64 e arm64.
