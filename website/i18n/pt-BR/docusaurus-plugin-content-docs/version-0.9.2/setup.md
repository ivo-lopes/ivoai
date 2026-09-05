# Configuração

## Configuração do cliente

`ivoai setup` provisiona componentes gerenciados de forma transacional e preserva
instalações externas de executores e a autenticação delas.

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
