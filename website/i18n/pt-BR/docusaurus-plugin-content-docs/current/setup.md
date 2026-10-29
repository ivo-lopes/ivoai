# Configuração

## Configuração do cliente

`ivoai setup` provisiona componentes gerenciados de forma transacional e preserva
instalações externas de executores e a autenticação delas.

A candidata local v0.15.0 omite OpenCode do setup padrão de cliente e do setup
candidato do updater. Para provisionar explicitamente o componente de
compatibilidade preservado, use `ivoai setup --mode client --legacy-opencode`;
setup de servidor recusa essa flag. O setup comum preserva propriedade, binários
e histórico OpenCode existentes; não desinstala, repara nem adota o componente.
Consulte [releases](releases.md) para estado da candidata e limites da migração.

## Escopo de configuração nativo gerenciado

No Codex gerenciado, entre no workspace desejado com `cd` no shell:

```sh
cd /path/to/project
ivoai codex
```

Não encaminhe `-C`/`--cd` nativos nem `--ignore-user-config`: são recusados com
`CODEX_MANAGED_CONFIG_SCOPE_UNSUPPORTED`. Sessões existentes preservam workspace;
use uma sessão nova ao selecionar outro diretório inicial. O host compartilha
diretório/ambiente/config capturados entre inventário MCP e execução, preservando
grants Core, sandbox e aprovações. Não é necessário editar arquivos pessoais de
configuração ou credenciais. O snapshot não congela configurações/política cloud
nem certifica paridade geral de clientes. Consulte [releases](releases.md).

## Configuração de servidor em Debian 12 e LXC

`sudo ivoai setup --mode server` executa um preflight antes de gravar o estado do
servidor: sistema operacional, arquitetura, estado do container/LXC, privilégios,
systemd, CLI e daemon do Docker, versão do Engine e Compose v2.

No Debian 12 compatível, o IVOAI pode instalar um Docker Engine ausente a partir do
repositório APT oficial do Docker e atualizar um Engine antigo que já pertença a esse
repositório. Instalações desconhecidas ou não oficiais continuam sob responsabilidade
do operador. O IVOAI nunca usa um instalador shell remoto. Se o Docker não puder
iniciar dentro do LXC, habilite nesting e os recursos de container necessários no
host Proxmox e execute o setup novamente. A configuração do host não pode ser
alterada com segurança de dentro do guest.

Um setup interrompido é reparado de forma idempotente ao executar novamente o mesmo
comando. Antes da conclusão, os diagnósticos do servidor informam
`SERVER_SETUP=INCOMPLETE` e o pré-requisito real, em vez de tratar arquivos `.env`
ausentes dos backends como causa raiz.
