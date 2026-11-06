# MCP remoto para ChatGPT e Claude

Validado com a documentação oficial dos produtos em **2026-09-03**. A interface dos
produtos, a disponibilidade por plano e a política administrativa podem mudar;
verifique novamente as páginas oficiais vinculadas antes de um rollout organizacional.

O IVOAI expõe um endpoint MCP remoto e unificado de Streamable HTTP em:

```text
https://ai.example.com/mcp
```

O endpoint é compatível com a inicialização MCP, descoberta de ferramentas, respostas
JSON ou SSE e autorização OAuth 2.1 com PKCE. Não coloque tokens na URL. Crie no
servidor um código de ativação limitado e de uso único:

```bash
sudo ivoai server web-access create --ttl 10m
```

## ChatGPT Web

Atualmente, a OpenAI documenta apps MCP completos e o modo de desenvolvedor para
workspaces Business e Enterprise/Edu; o Pro pode conectar MCPs de leitura/busca no
modo de desenvolvedor. Um administrador habilita o modo de desenvolvedor; depois, um
usuário autorizado acessa **Settings → Apps → Create**, informa o endpoint MCP HTTPS
e a autenticação, seleciona **Scan Tools**, conclui o OAuth e cria o app em draft.
Quando a página de autorização do IVOAI abrir, revise os escopos solicitados e informe
nela o código de ativação único. Selecione o app no menu de ferramentas do chat para
a mensagem que precisar dele. Administradores/proprietários publicam apps revisados
para um workspace.

O ChatGPT se conecta a servidores MCP remotos. Para um servidor privado/on-prem que
não deva ser exposto publicamente, use o Secure MCP Tunnel compatível da OpenAI em
vez de um bypass público improvisado. Consulte o
[guia oficial da OpenAI sobre modo de desenvolvedor e apps MCP](https://help.openai.com/en/articles/12584461-developer-mode-and-full-mcp-connectors-in-chatgpt).

Para revogar o acesso, desconecte/remova o app no ChatGPT e revogue o grant
correspondente do IVOAI:

```bash
sudo ivoai server web-access list
sudo ivoai server web-access revoke <id>
```

## Claude Web

Atualmente, a Anthropic documenta connectors MCP remotos personalizados para Free,
Pro, Max, Team e Enterprise; o Free é limitado a um. Em Team/Enterprise, um Owner ou
Primary Owner adiciona a URL remota em **Organization settings → Connectors → Add →
Custom → Web**. Depois, os membros acessam **Customize → Connectors** e selecionam
**Connect**. Em um plano individual, use **Customize → Connectors → + → Add custom
connector**. Conclua o OAuth revisando os escopos solicitados e informando o código
de ativação único do IVOAI; depois habilite ou desabilite o connector por conversa
no menu `+`.

O tráfego do connector remoto parte da nuvem da Anthropic. Portanto, o endpoint deve
estar acessível a partir dos intervalos de rede documentados, ou o firewall deve
permitir os intervalos de IP atuais da Anthropic. Consulte o
[guia oficial da Anthropic para connectors personalizados](https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp).

Remova o connector em **Customize → Connectors** e revogue o grant do IVOAI. Não
configure um bearer estático compartilhado nem uma chave de API de provider.

## Nginx Proxy Manager

O reverse proxy externo termina o TLS e encaminha o endpoint público `/mcp` ao
gateway do IVOAI conforme descrito em [Servidor](server.md). O site de documentação é
um listener HTTP separado:

```text
Public docs host: docs.example.com
Forward scheme:  http
Forward host:    IVOAI_SERVER_LAN_IP
Forward port:    7780
WebSockets:      off
TLS:             terminated by Nginx Proxy Manager
```

Quando possível, restrinja a porta de documentação no firewall à rede/IP do proxy.

## Conformidade e solução de problemas

Requisições POST do MCP usam `Content-Type: application/json` e aceitam tanto
`application/json` quanto `text/event-stream`. O IVOAI interpreta media ranges,
espaços em branco, q-values e parâmetros válidos de content-type em vez de comparar
strings literais. Um HTTP 406 significa que o peer rejeitou a negociação de conteúdo;
confirme exatamente o endpoint `/mcp`, a versão atual do cliente, o encaminhamento de
headers pelo reverse proxy e os dois valores de `Accept`. Nunca inclua valores de
`Authorization` nos diagnósticos.

Execute `sudo ivoai server doctor` e uma varredura segura e somente leitura das
ferramentas. Os gates esperados são inicialização bem-sucedida, sucesso em tools/list
e uma chamada segura a `context_health` ou `memory_status`. Um 401 significa que a
autenticação está ausente. A negação de escopo em `/mcp` é retornada como um erro
padrão de ferramenta MCP; a API do cliente inscrito usa HTTP 403 para uma credencial
autenticada com escopo insuficiente. Um 406 significa que a negociação do transporte
é incompatível.
