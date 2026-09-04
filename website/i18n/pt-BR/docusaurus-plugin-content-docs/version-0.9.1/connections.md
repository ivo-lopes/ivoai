# Conexões

Todas as alterações de conexão são realizadas por comandos do ivoai; edições manuais em config do
agent, arquivos MCP, hooks, profiles de shell ou arquivos de token não são necessárias.

## ChatGPT / Codex

`ivoai connect chatgpt` verifica o Codex, executa `codex login status` e invoca o fluxo oficial
`codex login` no navegador somente quando necessário. Uma verificação final de status oficial é
exigida antes de o ivoai registrar a conexão. O ivoai não lê nem copia arquivos de token do Codex.
`ivoai disconnect chatgpt` altera o estado do ivoai, mas deliberadamente não faz logout do usuário
no Codex.

## Claude / Claude Code

`ivoai connect claude` usa `claude auth status` e o fluxo oficial `claude auth login`. O login por
assinatura Claude Pro, Max, Team e Enterprise é compatível; uma API key da Anthropic não é o
default. As credenciais permanecem sob controle do Claude Code. Desconectar o ivoai não remove o
login do cliente oficial.

## ivoai server

A conexão interativa solicita uma URL HTTPS base e um enrollment code. Profiles nomeados mantêm
purposes e credenciais independentes. A automação pode fornecer a URL por flag e o código por
standard input, evitando o histórico do shell. Por exemplo:

```sh
printf '%s\n' "$IVOAI_ENROLLMENT_CODE" | ivoai connect server add mindsite \
  --url https://ai.example.com --purpose mindsite --code-stdin
ivoai connect server list
ivoai connect server show mindsite
ivoai connect server test mindsite
```

O legado `ivoai connect server --url ... --code-stdin` continua compatível e mapeia para o profile
`default`. `ivoai disconnect server <alias>` remove somente esse profile e sua credencial;
`ivoai disconnect server --all` é a forma bulk explícita.

`--enrollment-code` também é compatível com automação restrita, mas standard input é preferível
porque argumentos podem ficar visíveis em listas de processos e no histórico do shell.

O client:

1. valida a URL e o certificado TLS;
2. lê `/.well-known/ivoai` e verifica protocol 1, health e endpoints de features;
3. consome o enrollment code de uso único;
4. salva o segredo limitado ao client com modo `0600`, indexado por um ID de server opaco, e
   registra o profile sem expor globalmente esse upstream;
5. testa o context MCP e qualquer memory MCP anunciado com a nova credencial, reportando falhas
   como warnings sem descartar o enrollment consumido;
6. configura hooks genéricos do ai-memory em best effort; cada sessão gerenciada depois os associa
   ao seu knowledge router privado em loopback.

Clients atuais carregam o código de uso único no header `Authorization` usando o scheme de
enrollment do ivoai; o body JSON contém somente a identidade do client e os scopes solicitados. O
gateway aceita o campo JSON legado para rolling upgrades, mas rejeita requests que forneçam ambos
os transports de forma ambígua.

HTTP é rejeitado para servers que não sejam loopback. Reconectar um alias preserva todos os outros
profiles, e um enrollment com falha não pode remover um profile existente. Consulte
[Fontes de conhecimento multi-server](multi-server.md) para isolamento por purpose, federation
explícita, redundancy e sessões concorrentes.

## Registry MCP

MCPs HTTP adicionais do usuário permanecem no registry interno compartilhado. Memory/Context do
IVOAI server são diferentes: fontes selecionadas são renderizadas por um router loopback por
sessão, portanto conectar vários upstreams não expõe todos eles a cada agent. Estes comandos
gerenciam entradas adicionais do registry:

```sh
ivoai connect mcp list
ivoai connect mcp add example https://mcp.example.com
ivoai connect mcp remove example
```

Esses comandos gerenciam o registry do ivoai; a renderização específica de cada agent permanece
um edge adapter, não uma fonte de verdade separada.

## ChatGPT Web e Claude Web

Produtos Web conectam diretamente ao MCP remoto unificado do server; eles não usam a credencial
de enrollment do desktop. Os pré-requisitos são uma origem HTTPS publicamente acessível, um
`ivoai server doctor` aprovado e acesso do reverse proxy às rotas OAuth e `/mcp`.

Crie um código de ativação de curta duração no server:

```sh
sudo ivoai server web-access create --ttl 10m
```

No ChatGPT Web, habilite o developer mode para connectors customizados quando exigido pelo
workspace, adicione um connector e informe `https://ai.example.com/mcp`. No Claude Web, adicione
um custom connector com a mesma URL. Ambos descobrem a metadata OAuth 2.1 do ivoai e abrem o fluxo
de autorização no navegador. Revise os scopes solicitados e informe o código de ativação ali; não
o coloque na URL do connector ou em um header customizado.

O connector pode solicitar estes scopes:

| Scope | Capability |
| --- | --- |
| `context:read` | Buscar e ler contexto indexado não confiável |
| `memory:read` | Consultar, listar e ler páginas de memória |
| `memory:write` | Gravar páginas e enviar feedback de memória |
| `memory:delete` | Excluir um path de página normalizado e confirmado |

O código de ativação default permite os três scopes não destrutivos. Solicite `memory:delete`
explicitamente somente quando a exclusão for necessária, ou gere um grant mais restrito para uso
somente leitura. Access e refresh tokens permanecem sob responsabilidade do connector Web; o
ivoai armazena somente hashes de token e metadata de revogação.

O discovery MCP compatível com ChatGPT anuncia a skill `ivoai-memory-context` incluída. Para
Claude Web, baixe `ivoai-memory-context.zip` da release correspondente do ivoai e importe-a como
uma Skill customizada. As instruções pedem que o modelo consulte o ivoai antes de respostas
dependentes do projeto e antes de toda pesquisa web/externa, na ordem memory → Context → web. A
inicialização MCP e as descrições das tools anunciam a mesma ordem. Nenhum formato de MCP remoto
ou skill consegue garantir uma chamada de tool em cada turno do modelo, pois o produto Web mantém
o controle final sobre a seleção de tools.

Referências dos providers: [conectar um servidor MCP ao ChatGPT](https://developers.openai.com/plugins/deploy/connect-chatgpt),
[usar custom connectors no Claude](https://support.claude.com/en/articles/11176164-use-connectors-to-extend-claude-s-capabilities)
e [Skills customizadas do Claude](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview).
