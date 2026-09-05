# Decisões de arquitetura

A arquitetura atual é intencionalmente host-first e orientada a módulos profundos:

1. Os clientes oficiais dos executores são responsáveis pela autenticação e pelas escritas autoritativas.
2. As credenciais dos servidores são isoladas por um ID de servidor estável e opaco.
3. Roteadores locais à sessão expõem apenas as fontes de conhecimento selecionadas.
4. A federação de leitura é limitada; o roteamento de escrita falha de forma fechada quando é ambíguo.
5. A evidência exata é armazenada antes de qualquer projeção limitada ou compressão.
6. Caveman, Headroom e Direct são providers mutuamente exclusivos.
7. O MCP Web remoto usa Streamable HTTP e OAuth 2.1 com PKCE.
8. OpenCode é o frontend gerenciado do AUTO, enquanto o IVOAI permanece como proprietário da sessão e
   roteia o trabalho pelos contratos de executor das CLIs oficiais Codex/Claude.

## ADR: AUTO OpenCode-first sem transferência de credenciais

Status: aceita.

As opções de integração avaliadas foram uma bridge de provider local compatível com OpenAI,
um backend de compatibilidade somente para attach e um plugin upstream de TUI/server. O design
selecionado combina superfícies compatíveis do OpenCode: `serve`/`attach`, um plugin de TUI da
versão exata, um pequeno plugin de servidor para correlação de sessões e um provider loopback
compatível com OpenAI implementado pelo IVOAI. O protocolo do provider é somente o transporte da
UI; o contrato real do executor continua sendo a CLI oficial do Codex ou do Claude.

Isso preserva streaming, cancelamento, failover de quota, acesso a tools, Memory/Context,
WorkingContext e a política single-writer do IVOAI sem copiar credenciais de provider nem colocar
um segundo scheduler como autoridade. O OpenCode gerenciado usa configuração isolada, desativa
share e auto-update, ignora configurações de projeto não confiáveis e faz bind somente em
loopback. Uma sessão com provider nativo do OpenCode continua disponível explicitamente fora do
control plane do AUTO.

A manutenção de um fork downstream do OpenCode foi rejeitada. O lettering, o painel de status,
os comandos e o tema do IVOAI usam APIs oficiais de plugin e slots, preservando a licença e a
atribuição upstream.

## ADR: seleção nativa de modelo sem duplicação de login no provider

Status: aceita.

O provider OpenCode gerenciado publica um catálogo de modelos derivado do registry de executores
do IVOAI. O model picker nativo do OpenCode e seu suporte a variants permanecem como a única UI de
seleção. IDs opacos do catálogo são resolvidos dentro da bridge loopback para um executor, seu ID
de modelo nativo e um valor validado de reasoning/effort. `auto` mantém o scheduler como autoridade;
uma entrada explícita falha de forma fechada e nunca faz fallback silencioso para outro modelo.

Codex e Claude continuam sendo executados por meio de suas CLIs oficiais e permanecem responsáveis
pela autenticação. Nenhum access token, refresh token, cookie ou credencial de provider é copiado
para o OpenCode. A seleção solicitada/efetiva, que não é sensível, é persistida com o mapeamento de
sessão IVOAI↔OpenCode para que uma conversa retomada preserve a escolha do operador.

Um provider compatível com OpenAI continua apropriado aqui porque funciona como transporte privado
de UI entre dois processos em loopback, não como substituto do contrato de agente de qualquer
executor. A bridge traduz apenas o modelo selecionado, o reasoning effort, o stream do prompt, o
cancelamento e o estado de conclusão para os contratos de executor existentes.

## ADR: leituras MCP federadas preservam o envelope de resultado da tool

Status: aceita.

A federação de leitura multi-server é uma responsabilidade de roteamento do IVOAI, mas a resposta
ainda atravessa uma fronteira MCP `tools/call`. Consequentemente, o router deve retornar um
`CallToolResult` válido; um objeto JSON-RPC contendo diretamente um objeto customizado de federação
sob `result` não está em conformidade com o protocolo. O router agora retorna um item determinístico
de conteúdo textual que contém o objeto de federação bounded, incluindo a proveniência de cada
source. Deliberadamente, ele não anexa uma estrutura `structuredContent` estrangeira a uma tool
upstream cujo schema de saída declarado descreve apenas uma source.

Essa representação é validada com o SDK MCP oficial para Go e funciona tanto para pass-through de
source única quanto para leituras multi-source. Ela mantém inalterado o roteamento de escrita:
federação de leituras nunca implica broadcast de escritas.

O histórico da implementação e os tradeoffs detalhados permanecem no histórico do repositório e
no documento de arquitetura.
