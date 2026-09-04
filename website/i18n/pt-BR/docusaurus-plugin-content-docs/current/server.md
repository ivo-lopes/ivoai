# Servidor

## Atualizações in-place

Execute `sudo ivoai update --dry-run` antes de atualizar um servidor. Isso prepara e
executa o candidato verificado por checksum em um preflight limitado, mas não faz
commit de mudança no estado gerenciado. A transação do servidor usa
`/var/lib/ivoai/updates`, cria snapshot somente dos assets explícitos do servidor
IVOAI e do executável gerenciado, executa `setup --mode server` e verifica com
`ivoai server doctor`. Ela não cria estado XDG de cliente apenas para atualizar um
servidor. Em caso de falha, `sudo ivoai update --rollback` restaura o checkpoint
compatível anterior e reconcilia systemd pelo setup restaurado do servidor. Stores
ativos do Qdrant, memory, enrollment e OAuth Web não são copiados cegamente; qualquer
alteração futura no formato deles deve declarar uma migração reversível e quiesced.
Siga o [procedimento de canário em duas produções](canary-rollout.md); nunca atualize
as duas instâncias de produção ao mesmo tempo.

A administração do servidor está disponível pelos subcommands `ivoai server ...` e
pela seção **Server Administration** do menu interativo. Ações que alteram o servidor
local aparecem indisponíveis, a menos que o ivoai esteja em host Linux compatível com
privilégios root. Restore, stop, revogação de enrollment e remoção de connector exigem
confirmação explícita no menu.

## Sistemas compatíveis

O suporte inicial do servidor contempla Ubuntu 22.04, Ubuntu 24.04 e Debian 12 em
Linux amd64 e arm64. As duas arquiteturas usam o digest OCI imutável e revisado do
runtime de embeddings. Execute `ivoai setup --mode server` como root. A operação é
idempotente.

O setup do servidor exige Docker Engine 28.0.0 ou mais recente e Docker Compose
2.33.1 ou mais recente. O Engine 28 introduziu o suporte a gateway-priority usado para
fornecer ao container de embeddings egress temporário para download do modelo sem
dar acesso permanente à Internet às redes backend. Antes de fazer commit dos assets,
o setup valida OS, arquitetura, estado do container/LXC, systemd, privilégios, CLI e
daemon Docker, Engine e Compose. No Debian 12, um Engine ausente é provisionado
idempotentemente pelo repositório APT oficial assinado do Docker, após verificar o
fingerprint revisado da signing key; candidatos de pacotes são resolvidos e
instalados em versões exatas. Arquivos ou chaves de repositório desconhecidos nunca
são sobrescritos. Ubuntu e instalações legadas não oficiais recebem diagnóstico
fail-closed e usam o procedimento oficial do operador para
[Debian](https://docs.docker.com/engine/install/debian/) ou
[Ubuntu](https://docs.docker.com/engine/install/ubuntu/).

Dentro do LXC, um daemon inacessível é diagnosticado separadamente. Nesting,
`keyctl`, cgroup ou permissões de device no host precisam ser habilitados no host
Proxmox/LXC; o setup no guest nunca finge que consegue alterar a policy do host.
Depois de corrigir o pré-requisito, execute novamente
`sudo ivoai setup --mode server`. Doctor e o status de Memory/Context informam
`SERVER_SETUP=INCOMPLETE` e a causa raiz do pré-requisito quando o setup não fez
commit, em vez de tratar arquivos `.env` gerados ausentes como falha principal.

Quando o Engine é compatível, mas Compose está ausente ou antigo, o ivoai instala o
plugin Docker Compose oficial para a arquitetura em
`/usr/local/lib/docker/cli-plugins/docker-compose`, após verificar seu checksum
SHA-256 pinado. O progresso é exibido a cada 10 segundos, e a janela limitada de
download é de 30 minutos para links lentos. Uma instalação Compose compatível
existente é preservada.

## Layout

| Finalidade | Path |
| --- | --- |
| Configuração | `/etc/ivoai` |
| Secrets | `/etc/ivoai/secrets` |
| Dados autoritativos persistentes | `/var/lib/ivoai` |
| Backups | `/var/lib/ivoai/backups` |
| Assets da aplicação | `/opt/ivoai` |
| Logs | journald |

## Serviço da documentação do produto

O setup do servidor habilita `ivoai-docs.service`. Ele serve o build imutável de
produção do Docusaurus embutido no binário IVOAI instalado; Node.js, o dev server do
Docusaurus e um checkout do repositório não são usados em runtime. O serviço executa
como a conta não privilegiada `ivoai-docs`, reinicia em caso de falha, inicia no boot,
não grava estado da aplicação e envia logs ao journald.

O default gerenciado fica em `/etc/ivoai/docs.json`:

```json
{"listen_address":"0.0.0.0:7780"}
```

`sudo ivoai server docs status` informa health, endereço de escuta e URL loopback. O
bind wildcard é intencional para que um reverse proxy externo possa alcançá-lo;
restrinja TCP 7780 no firewall do host/rede à origem do Nginx Proxy Manager sempre que
possível. Endereço de bind e exposição no firewall são controles diferentes.

Quando necessário, escolha idempotentemente outro listener sem conflito:

```sh
sudo ivoai server docs configure --listen 0.0.0.0:7781
```

Para um Nginx Proxy Manager externo, crie um Proxy Host dedicado:

```text
Internet --HTTPS--> Nginx Proxy Manager --HTTP/LAN--> ivoai-server:7780

Domain:          docs.example.com
Forward scheme:  http
Forward host:    <IVOAI_SERVER_LAN_IP>
Forward port:    7780
WebSockets:      not required
TLS:             terminated by Nginx Proxy Manager
```

Não instale Nginx Proxy Manager dentro do IVOAI. `ivoai setup --mode server` e os
updates posteriores atualizam idempotentemente o build embutido e a unit gerenciada.

Os serviços gateway e context executam como contas não privilegiadas distintas
(`ivoai-gateway` e `ivoai-context`) em um grupo read-only compartilhado. As units
systemd ocultam processos não relacionados, negam a cada serviço o estado privado do
outro, usam `Restart=on-failure`, `NoNewPrivileges=yes` e allowlists de escrita
estreitas. A imagem não privilegiada do Qdrant, o runtime de embeddings e o ai-memory
usam a identidade de container non-login `ivoai`. Portas de dependências escutam
somente em loopback, exigem credenciais internas geradas separadamente e não são
públicas. Depois que TEI baixa o modelo pinado e passa nos health checks, sua rede de
download é desconectada.

Executar o setup novamente altera ownership somente nas raízes de mounts gerenciados;
não percorre conteúdo criado pelas aplicações. Isso preserva symlinks legítimos do
cache do Hugging Face. A readiness do Qdrant usa o endpoint não autenticado `/readyz`,
enquanto sua API de dados continua exigindo a credencial interna gerada. O storage do
Qdrant permanece em `/var/lib/ivoai/qdrant`; seu workspace gravável de snapshots e o
marcador de inicialização ficam em `/var/lib/ivoai/qdrant-snapshots` e
`/var/lib/ivoai/qdrant-init`. Esses mounts separados permitem que a imagem pinada seja
executada como identidade non-root `ivoai` sem tornar `/qdrant` gravável.

## Operações

```sh
ivoai server setup
ivoai server status
ivoai server doctor
ivoai server start
ivoai server stop
ivoai server restart
ivoai server logs
ivoai server gateway configure --public-url https://ai.example.com
ivoai server web-access create --ttl 10m
ivoai server web-access list
ivoai server web-access revoke <id>
ivoai server backup [--output <path>]
ivoai server restore --input <backup>
ivoai server remote status
ivoai server remote doctor
ivoai server remote connector list
```

O gateway expõe liveness em `/health`, readiness em `/ready` e discovery não sensível
do protocolo em `/.well-known/ivoai`. A versão 1 do protocolo é verificada antes de
um cliente persistir o estado da conexão.

Cada instância server emite sua própria credencial de enrollment com escopo. Pooling
multi-server, seleção de purpose, federação explícita de leitura e roteamento
primary/standby são responsabilidades do cliente; um ivoai server não conhece peers,
não replica dados de outra organização nem implementa quorum oculto. Consulte
[Fontes de conhecimento multi-server](multi-server.md).

## Gateway HTTPS público

O setup escuta em `127.0.0.1:7744` sem TLS. No deployment habitual, mantenha esse
listener loopback atrás de um reverse proxy HTTPS administrado e registre sua origem
pública sem editar arquivos:

```sh
sudo ivoai server gateway configure --public-url https://ai.example.com
```

Se o reverse proxy HTTPS executar em outro host ou container, faça bind explícito do
gateway no endereço privado do servidor e permita somente o endereço de origem do
proxy:

```sh
sudo ivoai server gateway configure \
  --public-url https://ai.example.com \
  --listen 192.0.2.10:7744 \
  --trusted-proxy 192.0.2.20/32
```

Use o endereço privado real do ivoai server e o IP/CIDR de origem real do proxy.
Requests de outros peers e requests do proxy sem `X-Forwarded-Proto: https` são
rejeitados. Não use `0.0.0.0/0`.

Como alternativa, permita que o ivoai sirva TLS diretamente. Certificado e chave são
copiados para `/etc/ivoai/secrets/tls` como arquivos `0600` pertencentes ao serviço,
e um listener non-loopback só é aceito quando ambos são fornecidos:

```sh
sudo ivoai server gateway configure \
  --public-url https://ai.example.com \
  --listen 0.0.0.0:7744 \
  --tls-cert /absolute/path/fullchain.pem \
  --tls-key /absolute/path/privkey.pem
```

Emissão e renovação de certificados continuam sob responsabilidade do operador.
Execute novamente o comando configure para atualizar as cópias gerenciadas. Qdrant,
embeddings e ai-memory permanecem em mapeamentos loopback; somente o gateway ou
reverse proxy é público. Os containers de dependências entram em uma rede não interna
somente enquanto o Docker estabelece os binds de loopback; systemd desconecta essa
rede transitória depois do startup. Depois que systemd carrega os arquivos de
environment dos backends, o serviço context não consegue acessar a árvore gerenciada
de secrets.

### Nginx Proxy Manager

Crie um Proxy Host para o hostname público com scheme `http`, o endereço privado do
ivoai server e porta `7744`. Habilite certificado válido e Force SSL. Não associe NPM
Access List, Basic Authentication ou outro desafio de login ao host: conectores Web
precisam alcançar os metadados OAuth e o fluxo de authorization do ivoai.

Quando NPM executar em outro host ou container, configure o listener do gateway e o
CIDR de origem restrito como acima. O NPM deve preservar `Authorization`, `Host` e o
scheme HTTPS original. A configuração Advanced abaixo é adequada para a rota MCP
Streamable HTTP:

```nginx
proxy_set_header Authorization $http_authorization;
proxy_set_header Host $host;
proxy_set_header X-Forwarded-Proto https;
proxy_buffering off;
proxy_request_buffering off;
proxy_read_timeout 3600s;
```

Não use `0.0.0.0/0` como proxy confiável nem publique as portas 6333, 6334, 8080 ou
49374. Somente a origem do proxy HTTPS deve estar exposta à Internet.

## Acesso Web MCP

A URL pública do connector é a origem configurada acrescida de `/mcp`, por exemplo
`https://ai.example.com/mcp`. Antes de conectar um cliente Web, crie um código
one-time:

```sh
sudo ivoai server web-access create --ttl 10m
```

O comando exibe o código de ativação uma vez. Durante o fluxo OAuth no navegador do
connector, revise os scopes solicitados e digite esse código. A concessão padrão
contém `context:read`, `memory:read` e `memory:write`. O scope destrutivo
`memory:delete` só fica disponível quando selecionado explicitamente. Se mutação não
for necessária, escolha um conjunto mais restrito. `list` mostra identificadores,
scopes, expiração e estado de revogação sem tokens. `revoke` invalida a concessão Web
selecionada e sua família de refresh tokens.

O MCP unificado expõe search/read de context e CRUD limitado de memory. Excluir
memory exige scope `memory:delete` e confirmação explícita do path normalizado da
página. Context permanece read-only. O gateway não faz proxy de tools administrativas
do ai-memory nem chamadas MCP arbitrárias.

## Enrollment

```sh
ivoai server enrollment create --ttl 10m
ivoai server enrollment list
ivoai server enrollment revoke <id>
```

Somente o comando create exibe o código one-time. O servidor persiste digest
criptográfico, expiração e estado, nunca o código original. O consumo emite uma
credencial de cliente com escopo; replay é rejeitado. O backend de autenticação v0.1
é o store local em `/var/lib/ivoai/enrollment/state.json`, exclusivo do owner e com
lock cross-process; contém hashes e metadados, não códigos plaintext nem bearer
tokens emitidos.

## Connectors de Context

Connectors de filesystem e Git normalizam texto, rejeitam paths sensíveis e
inseguros, dividem documentos em chunks, produzem embeddings locais e fazem upsert em
uma collection Qdrant versionada. Connectors são administrados com comandos
explícitos como `ivoai server connector add --name docs --type filesystem --path /srv/docs`,
`ivoai server connector list` e `ivoai server connector remove docs`. O core
permanece saudável com zero connectors e zero documentos. Definições dos connectors
são carregadas quando o serviço context inicia. A remoção elimina catálogo e entradas
vetoriais antes de remover a definição do registry e então reinicia o serviço context
para que sua configuração ativa corresponda ao registry.

As tools de context expostas aos agentes são read-only: `context_search`,
`context_get_document`, `context_recent` e `context_health`. Ingestão e administração
de connectors são operações autenticadas separadas.

## Backup e restore

Backups incluem configuração sem secrets plaintext desnecessários, metadados dos
connectors e corpus, metadados de context, dados persistentes do ai-memory e
metadados de reconstrução do índice. Corpus original e dados de memory são
autoritativos; índices vetoriais podem ser reconstruídos. O restore valida entradas
limitadas, rejeita links e traversal, exclui secrets e grava arquivos regulares
atomicamente. A CLI interrompe automaticamente os serviços gerenciados de gateway,
context e dependências ao redor de cada operação de backup/restore e os reinicia em
seguida. Restore faz merge dos arquivos validados nas raízes gerenciadas e não apaga
arquivos stale ausentes do archive. Antes do restart, reaplica ownership dedicado
somente às árvores restauradas e validadas, sem percorrer caches normais das
aplicações.
