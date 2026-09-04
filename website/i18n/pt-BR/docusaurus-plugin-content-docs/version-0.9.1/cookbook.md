# Receitas

## Instalar ou atualizar um cliente

```bash
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sh
ivoai setup
ivoai status
ivoai doctor

ivoai update --dry-run
ivoai update
```

A autenticação permanece nos clientes oficiais do Codex, Claude Code e OpenCode.
O IVOAI nunca precisa dos tokens de seus providers.

## Inicializar um servidor Debian 12, inclusive em LXC

```bash
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sudo sh
sudo ivoai setup --mode server
sudo ivoai server doctor
sudo ivoai server memory status
sudo ivoai server context status
sudo ivoai server docs status
```

No Debian 12, o setup provisiona um Engine ausente pelo repositório APT oficial e
assinado da Docker. Se o diagnóstico informar que o Docker está inacessível dentro
do LXC, habilite nesting, `keyctl` e acesso compatível a cgroups/dispositivos no host
e execute novamente o mesmo comando de setup. Não crie `qdrant.env` nem `memory.env`
manualmente.

## Fazer enrollment do primeiro e do segundo servidor privado

Em cada servidor, crie um código único diferente com `sudo ivoai server enrollment
create --ttl 10m`. No cliente, forneça cada código pela entrada padrão:

## Adicionar dois servidores e usar todas as fontes habilitadas

```bash
ivoai connect server add company-a --url https://ai-a.example.com --purpose company-a --code-stdin
ivoai connect server add company-b --url https://ai-b.example.com --purpose company-b --code-stdin
ivoai auto
```

Restrinja uma sessão com `--knowledge-source company-a`. Repita a flag para selecionar
intencionalmente um subconjunto. Use `ivoai connect server test company-a` quando uma
fonte estiver indisponível. O modo automático mantém as fontes saudáveis e relata
uma leitura parcial degradada. Uma fonte indisponível selecionada explicitamente
falha em vez de ser substituída por outra. Novas escritas em Memory nunca são
transmitidas a todos os destinos; restrinja a sessão a um destino quando o alvo da
escrita for ambíguo.

## Executar os executores e o AUTO

```bash
ivoai codex
ivoai claude
ivoai opencode
ivoai auto
ivoai auto --planner codex
```

`ivoai auto` e `ivoai opencode` abrem o frontend gerenciado do OpenCode. O IVOAI
roteia os turnos para a CLI oficial do Codex ou do Claude Code; assim, seus logins de
assinatura existentes são reutilizados sem copiar um token de provider. Para uma
sessão independente e inalterada com um provider do OpenCode, use
`ivoai session start --executor opencode --mode direct`.

## Verificar Memory e Context

```bash
ivoai memory status
ivoai memory configure
sudo ivoai server memory status
sudo ivoai server context status
sudo ivoai server connector list
```

Para adicionar um corpus local revisado:

```bash
sudo ivoai server connector add --name handbook --type filesystem --path /srv/handbook
sudo ivoai server context status
```

## Selecionar a compressão com segurança

```bash
ivoai config set compression.provider caveman
ivoai config set compression.provider direct
ivoai doctor
```

Caveman é o default solicitado. O IVOAI seleciona Direct quando Memory/Context
autoritativos ou a compatibilidade do executor exigem o caminho exato; ele nunca
encadeia Caveman e Headroom.

## Adicionar o MCP remoto ao ChatGPT Web ou Claude Web

```bash
sudo ivoai server web-access create --ttl 10m
```

Configure `https://ai.example.com/mcp` no produto Web, conclua o OAuth e informe o
código de ativação único somente na página de autorização do IVOAI. Consulte
[MCP remoto para ChatGPT e Claude](mcp-web.md) para conhecer os passos atuais de
planos/administração, revogação, túnel seguro e diagnóstico de conformidade.

## Diagnosticar um setup parcial do servidor

Execute `sudo ivoai server doctor`, corrija a `ROOT_CAUSE` informada e depois execute
novamente `sudo ivoai setup --mode server`. Não crie arquivos `.env` de backend
manualmente.

## Diagnosticar HTTP 406 do MCP

Confirme se o connector usa exatamente a URL `/mcp`, atualize o cliente Web/CLI e
faça o reverse proxy preservar `Accept`, `Content-Type`, `Authorization` e
`X-Forwarded-Proto`. Um POST em conformidade aceita tanto `application/json` quanto
`text/event-stream`. Nunca cole um valor bearer em um comando de diagnóstico.
Consulte o [runbook de solução de problemas](troubleshooting.md#chatgpt-or-claude-web-cannot-connect-to-mcp).

## Hospedar a documentação atrás de um Nginx Proxy Manager externo

O serviço gerenciado de documentação escuta em `0.0.0.0:7780`. No NPM, crie um
Proxy Host com scheme `http`, forward host definido como o IP privado/LAN do servidor
IVOAI e forward port `7780`. Termine o TLS público no NPM. WebSockets não são
necessários. Quando possível, restrinja a porta 7780 no firewall à rede/endereço do NPM.

```text
Internet -> HTTPS/Nginx Proxy Manager -> HTTP/LAN -> ivoai-server:7780
```

Consulte [MCP Web](mcp-web.md) para os connectors do ChatGPT e Claude e
[Solução de problemas](troubleshooting.md) para diagnósticos do HTTP 406 do MCP.

## Fazer backup, restaurar e executar rollback

```bash
sudo ivoai server backup --output /var/lib/ivoai/backups/ivoai-backup.tar.gz
sudo ivoai server restore --input /var/lib/ivoai/backups/ivoai-backup.tar.gz
ivoai update --rollback
ivoai status
ivoai doctor
```

Os backups excluem segredos e índices que podem ser reconstruídos; proteja a
administração de segredos separadamente. O rollback da atualização restaura de modo
transacional o binário anterior e o estado mutável compatível pertencente ao IVOAI.
