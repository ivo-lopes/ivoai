# Segurança

## Fronteira de credenciais multi-server

Cada servidor inscrito possui um `server_id` estável e opaco; sua credencial é
armazenada no secret store privado sob esse ID, nunca sob um alias não confiável.
Aliases, purposes e redundancy groups usam um conjunto seguro e limitado de
caracteres. Base URLs rejeitam userinfo, queries e fragments, exigem HTTPS fora de
loopback, e endpoints de serviço descobertos devem permanecer same-origin.

Sessões gerenciadas recebem somente um router loopback efêmero e uma capability local
aleatória. `IVOAI_KNOWLEDGE_SESSION_TOKEN` e o `IVOAI_SERVER_TOKEN` de compatibilidade
contêm essa capability local — não um bearer upstream. O router escuta em
`127.0.0.1`, compara a capability em tempo constante, rejeita redirects cross-origin
e a revoga ao final da sessão. O token upstream A é consultado pelo ID opaco e
anexado somente à source A; nunca é renderizado em argumentos do agente, arquivos
MCP, estado de sessão, observabilidade ou logs.

O isolamento por purpose falha de forma fechada. Uma sessão sem filtro faz leituras
limitadas de todas as fontes habilitadas; qualquer flag `--knowledge-source` restringe
a sessão ao subconjunto solicitado. Uma escrita com múltiplos purposes ou múltiplos
destinos independentes é rejeitada antes de contatar um upstream. O failover de
leitura por redundância permanece dentro de um purpose/group; escritas não são
repetidas automaticamente porque o primary pode já ter aplicado o efeito colateral.
Falhas federadas parciais preservam atribuição da fonte.

O enrollment primeiro persiste um marcador indisponível, depois a nova credencial
com escopo e então marca atomicamente o profile como conectado. Falhas no commit do
runtime restauram profile e credencial anteriores. Uma interrupção pode degradar um
profile, mas não pode associar um token novo a uma URL antiga. IDs de servidor
duplicados/adulterados são rejeitados antes de operações connect, test ou disconnect
que transportem credenciais.

## Fronteiras de confiança

- Credenciais dos agentes oficiais pertencem ao Codex CLI e ao Claude Code, não ao
  ivoai.
- No AUTO, OpenCode é um frontend somente loopback. O IVOAI invoca as CLIs oficiais
  Codex e Claude e nunca lê, copia nem converte as credenciais delas para o OpenCode.
- Um ivoai server conectado é autenticado, mas ainda é tratado como externo.
- Conteúdo de connectors e texto RAG recuperado são dados não confiáveis, nunca
  instruções do installer ou do control plane.
- Componentes de terceiros são pinados no manifest central.

## Tratamento de secrets

Secrets nunca são armazenados na configuração principal nem em exemplos. Diagnósticos
e logs da CLI redigem bearer authorization, enrollment-code, access-token e formatos
comuns de API key identificados. Diretórios e arquivos secretos são criados com
`0700` e `0600`; gravações são atômicas e recusam alvos symlink inseguros.

Códigos de enrollment têm alta entropia, TTL, consumo one-time, revogação e
persistência somente do digest. Credenciais de cliente têm scopes mínimos. A
administração remota oferece operações seguras nomeadas e nunca aceita comandos do
host nem executáveis. Enrollment via `--code-stdin` ou prompt sem echo é preferível;
a flag de automação compatível `--enrollment-code` pode ficar visível na lista de
processos ou no histórico do shell.

A autorização de conectores Web usa OAuth 2.1 Authorization Code com PKCE S256 e
correspondência exata de redirect URI. O navegador também deve apresentar um código
de ativação one-time e de curta duração criado localmente por
`ivoai server web-access create`. Códigos de ativação e autorização, access tokens e
refresh tokens rotativos são persistidos somente como hashes. Access tokens duram
uma hora; refresh tokens duram 30 dias; a revogação invalida a família de tokens.

Dynamic client registration não dispensa a validação de redirect. O consentimento
mostra os scopes solicitados, e cada tool MCP verifica o scope necessário.
`memory_delete_page` também exige confirmação vinculada ao path normalizado do alvo.
Credenciais Web nunca são intercambiáveis com credenciais de enrollment do cliente
nativo nem com tokens dos serviços backend.

O parâmetro OAuth `resource` é vinculado à URL pública canônica `/mcp` durante
authorization, code exchange, rotação de refresh e validação do bearer token. Um
token emitido para outro audience é rejeitado antes do tratamento do request MCP.

Durante enrollment, o código one-time usa um scheme dedicado de `Authorization`, em
vez de um campo JSON analisado pelo proxy. A auditoria do gateway nunca registra esse
header. O transporte legado no body permanece aceito somente para compatibilidade de
transição.

## Controles de rede

Clientes usam validação TLS padrão e timeouts HTTP limitados. HTTP em claro é aceito
somente em loopback para desenvolvimento ou proxy TLS no mesmo host. Um listener
plaintext non-loopback exige origem pública HTTPS e CIDRs explícitos de proxies
confiáveis; o gateway valida o peer do socket e o scheme HTTPS encaminhado. TLS
direto continua disponível para listeners non-loopback. Discovery não é sensível.
O gateway é o único endpoint público pretendido. Qdrant, embeddings e ai-memory usam
credenciais bearer internas e independentes mesmo em mapeamentos de host somente
loopback; credenciais de cliente nunca são encaminhadas a eles. Depois que o modelo
pinado está saudável, systemd desconecta o container de embeddings da rede de
download e todos os containers de dependências da rede transitória usada para criar
os binds do host.

Para TLS direto, o ivoai copia certificado e chave selecionados para o diretório
`/etc/ivoai/secrets/tls`, pertencente ao gateway, como arquivos `0600`. Os serviços
gateway e context usam UIDs distintos, visão de processos oculta e deny lists de
filesystem mutuamente exclusivas. Depois que systemd carrega as duas credenciais de
backend necessárias, o sandbox do context bloqueia acesso ao filesystem de toda a
árvore de secrets e aos estados de enrollment e memory. O sandbox do gateway também
bloqueia acesso direto aos dados de memory, Qdrant, modelo e corpus que não necessita.

## Ingestão de conteúdo

Connectors restringem paths de filesystem, rejeitam traversal e usam opens no-follow
do sistema operacional para a raiz e cada componente do path. A enumeração Git
desabilita hooks do repositório e programas fsmonitor. Filtros excluem arquivos de
credenciais, estado de cloud e chaves; quotas limitam um documento a 8 MiB, um
connector a 10.000 documentos/256 MiB e uma ingestão a 250.000 chunks. Texto
recuperado é marcado como contexto não confiável. Métodos do Context MCP são read-only
por padrão. Remover um connector elimina seu catálogo e suas entradas vetoriais.

A skill incluída reforça essa fronteira: memory e texto RAG recuperados são evidência,
não instruções. Ela permite escrita em memory somente após solicitação explícita do
usuário e delete somente após confirmação separada e específica do path. O Web MCP
não expõe manutenção do ai-memory, self-improvement, execução de provider, shell
remoto nem encaminhamento irrestrito de tools upstream.

Algumas tools upstream de memory não anunciam annotations read-only do MCP; assim, o
Codex pediria aprovação mesmo para uma consulta. O IvoAI adiciona overrides de
aprovação locais ao processo somente para as tools de leitura nomeadas dos servidores
registrados `ivoai-memory` e `ivoai-context`. Ele não aprova automaticamente escritas
ou exclusões de memory, administração de connectors, tools MCP arbitrárias nem
servidores ausentes do registry IvoAI.

## Supply chain e operações

O installer verifica checksums da release antes da extração. Versões e estratégias
de checksum dos componentes são pinadas. Archives só são extraídos após validação de
seus paths. Nenhum update ocorre silenciosamente; `ivoai update` informa a release
selecionada e preserva config e secrets. `ivoai server logs` redige a saída
renderizada; journald ainda deve ser revisado antes da exportação ou compartilhamento
dos logs.

O Skill Control Plane usa um pipeline compartilhado e não executável de staging para
futuras skills, components e helpers. Ele exige revisões imutáveis e SHA-256,
representa separadamente o estado de signature/attestation, rejeita traversal, links,
duplicatas, arquivos especiais, limites de descompressão e executáveis inesperados,
e promove somente por um pointer privado e atômico após validação estrutural e de
policy. Discovery nunca executa hook ou comando encontrado em conteúdo externo.
Policy é deny-by-default, e prosa externa não pode conceder capabilities nem
autoridade de orchestration. Consulte
[skill-control-plane.md](skill-control-plane.md).

Reporte vulnerabilidades de forma privada ao proprietário do repositório. Não inclua
tokens, documentos privados nem logs de produção nos relatórios.

## Session control plane

O JSON da sessão contém somente metadados operacionais e usa diretórios XDG privados,
gravações atômicas `0600`, leituras no-follow e lock consultivo aberto com
`O_NOFOLLOW`. IDs aleatórios de sessão/worker, metadados limitados e validação de cada
campo de executor/model/state reduzem riscos de adulteração e escapes de terminal.
Prompts, respostas, tokens e ambientes brutos nunca são campos desse schema.

`ivoai-orchestrator` é um MCP stdio local anexado somente pela configuração por
processo do cliente oficial. O startup exige sessão ativa com Swarm ID verificado,
status Ruflo seguro e execução de provider desabilitada. O gateway remoto não possui
rota para ele. A delegação seleciona somente paths confiáveis do estado de componentes
cujo basename seja `codex` ou `claude`; não aceita shell, path de executável nem
working directory como input. Tarefas são limitadas a 32 KiB, resultados a 1 MiB,
workers concorrentes ao limite configurado e três de forma absoluta.

Ruflo recebe um ambiente limpo, `RUFLO_PROVIDER=ivoai-disabled`, memória local ao
processo e somente IDs opacos de lifecycle. Variáveis PAYG, prompts e resultados não
cruzam essa fronteira. Workers oficiais recebem as variáveis de provider-key removidas
e mantêm os stores compatíveis de autenticação por assinatura.

Grupos de processos primary e worker recebem sinais somente quando o marcador de
start do kernel Linux registrado ainda corresponde ao PID. O shutdown cancela
workers, encerra tarefas de lifecycle e remove o runtime transitório privado. Sessões
stale podem ser finalizadas sem matar um PID reciclado.

## Frontend OpenCode gerenciado

O IVOAI inicia o servidor OpenCode pinado em uma porta efêmera de `127.0.0.1`, com
secret Basic-auth aleatório e independente. Seu bridge privado de provider usa outro
listener loopback efêmero e uma capability bearer aleatória comparada em tempo
constante. Ambas as credenciais existem somente no ambiente do processo da sessão e
em arquivos gerenciados `0600` dentro do diretório privado de runtime; nenhuma é
persistida no JSON da sessão, config normal, logs, status ou argv.

O modo gerenciado usa raízes XDG isoladas, desabilita configuração OpenCode do
projeto, auto-update e compartilhamento, e habilita somente o provider IVOAI. Isso
impede que um plugin ou override de provider local ao repositório entre
silenciosamente no caminho de controle confiável. A configuração global e o store de
autenticação do OpenCode pertencentes ao usuário permanecem intocados, e o uso direto
de `opencode` preserva o comportamento upstream.

O plugin da TUI consome somente metadados de status limitados pelo bridge local
autenticado. Aliases, purposes, estado de executor e IDs têm caracteres de controle,
ANSI e overrides bidirecionais removidos antes da renderização. A atualização de
health usa cache com timer de cinco segundos, em vez de executar probe a cada frame.
O mapeamento de sessões frontend→executor contém somente IDs seguros e limitados;
prompts e resultados não são armazenados nele.

Em cancel, failover ou shutdown, o IVOAI termina o grupo de processos do executor
oficial, fecha ambos os listeners e remove o runtime privado pelo lifecycle normal da
sessão. O backend OpenCode nunca escuta em endereço LAN. Isso difere do servidor de
documentação, cujo default intencional no modo servidor continua
`0.0.0.0:<docs-port>` por trás da policy de firewall e reverse proxy administrada pelo
operador.
