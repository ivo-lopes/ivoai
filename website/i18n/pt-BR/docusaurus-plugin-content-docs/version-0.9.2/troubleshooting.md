# Solução de problemas

## AUTO informa `ivoai_bridge_error`

**Sintoma:** a conversa OpenCode gerenciada continua aberta, mas um turno informa
falha do executor IVOAI. O erro curto agora inclui uma classificação estável, como
`executor_exit_nonzero`, `executor_auth_failure`, `executor_stream_incomplete`,
`executor_timeout` ou `executor_cancelled`; saída parcial nunca é aceita como resposta
bem-sucedida.

Execute `ivoai doctor`, examine o estado não sensível da sessão com
`ivoai session show --json <session-id>` e verifique diretamente o cliente oficial.
Não inclua credenciais nem resultados brutos de tools em um bug report. Uma chamada
federada de Memory/Context deve retornar um `CallToolResult` MCP; builds antigos da
v0.9.0 retornavam diretamente o envelope de federação, e o Codex informava
`Unexpected response type`. Atualize para a release corretiva e tente novamente o
turno original. Se a classificação for falha de autenticação, autentique pelo cliente
oficial nomeado, não por `/connect` do OpenCode.

## A opção de modelo ou raciocínio está ausente

O catálogo gerenciado é descoberto quando AUTO inicia. Abra o seletor nativo de
modelos do OpenCode e selecione uma entrada explícita IVOAI Codex/Claude; somente as
variantes de raciocínio verificadas aparecem. `IVOAI Automatic Orchestration`
intencionalmente não possui variante fixa de raciocínio, pois essa decisão pertence
ao scheduler. Se existir apenas a entrada automática, execute `ivoai doctor` e
confirme que os clientes oficiais estão instalados e aceitam capability discovery.
Nunca habilite um login de provider paralelo apenas para fazer um modelo aparecer.

## Memory ou Context informa `Unexpected response type`

Isso indica que uma resposta `tools/call` atravessou a fronteira MCP com formato de
resultado inválido. No modo multi-server, o IVOAI retorna um `CallToolResult` canônico
cujo conteúdo textual contém o envelope federado determinístico e a proveniência das
fontes. Execute o teste de conformidade do Knowledge Router e compare uma chamada
single-source com a mesma chamada two-source. Não desabilite a federação nem remova
uma fonte apenas para esconder o erro. Releases corretivas preservam as respostas
single-source exatas e validam a resposta federada com o SDK Go oficial do MCP.

## Codex informa HTTP 406 no MCP `ivoai-memory` durante o startup

Um erro como `JSON-RPC -32022: upstream HTTP 406` significa que o endpoint Memory MCP
rejeitou a negociação de conteúdo HTTP. Para Streamable HTTP, um POST JSON-RPC usa
`Content-Type: application/json` e deve anunciar os dois tipos de resposta aceitos
com `Accept: application/json, text/event-stream` (a ordem e parâmetros normais do
media range não importam). Depois da inicialização, o `MCP-Protocol-Version`
negociado é encaminhado nos requests seguintes.

Primeiro verifique metadados não secretos do endpoint e a versão do cliente:

```sh
codex --version
codex mcp get ivoai-memory --json
ivoai doctor
```

A entrada MCP deve usar a URL Streamable HTTP gerada para a sessão IVOAI ativa. Não
copie bearers para comandos nem altere o endpoint para suprimir o alerta. Para testar
a negociação sem contatar upstream, execute:

```sh
scripts/test-mcp-codex-memory-handshake.sh
```

Para executar o teste limitado live e read-only com login Codex normal e um servidor
IVOAI já conectado, use:

```sh
scripts/test-mcp-codex-memory-handshake.sh --live
```

O diagnóstico live informa somente contadores pass/fail. Não imprime credenciais,
payloads MCP nem conteúdo de Memory. Se o teste hermético passar, mas o live ainda
retornar 406, compare a versão do Codex, o endpoint MCP configurado e o tratamento de
`Accept`, `Content-Type` e `MCP-Protocol-Version` pelo reverse proxy. Um 406 legítimo
é erro determinístico de protocolo/configuração e não deve ser repetido indefinidamente.

## O update foi interrompido ou falhou

Não apague o diretório de update nem reinstale sobre ele. Execute `ivoai update`
novamente (ou `sudo ivoai update` no servidor); o updater detecta um journal privado
ativo e restaura o snapshot anterior ao update antes de tentar novo candidato. Para
voltar explicitamente à última transação compatível, execute:

```sh
ivoai update --rollback
```

Em instalações server, use `sudo`. Depois, execute o Doctor correspondente. Uma
mensagem sobre journal corrompido é condição fail-closed: preserve o diretório de
update para diagnóstico e colete `ivoai doctor --inventory --json`; não edite o
journal nem arquivos de autenticação dos providers. A compatibilidade do candidato
pode ser verificada sem commit de mudanças gerenciadas usando
`ivoai update --dry-run`. O comando prepara e executa o candidato verificado por
checksum em probes de compatibilidade limitados; use a mesma decisão de confiança no
canal da release que usaria em um update real.

## O modo automático não inicia o cliente selecionado

Execute `ivoai doctor` e examine **Automatic Orchestration**. Um provider deve usar
autenticação de assinatura first-party; sessões PAYG por API key não são fallback do
roteamento automático. `ivoai status` intencionalmente lê somente quota em cache,
enquanto Doctor executa probes ativos.

Se Codex mostrar erro no probe, confirme suporte a `codex app-server --stdio` e que
`codex login status` funciona. Se Claude estiver autenticado, mas a quota de cinco
horas/semanal disser `awaiting first response`, inicie uma conversa Claude automática
e conclua um turno para que a statusline oficial publique telemetria.
`N/A / not exposed` significa que uma resposta estruturada observada omitiu o campo;
`stale` indica percentual de uma observação anterior. Quota mensal Claude não é
presumida.

## O frontend OpenCode gerenciado não inicia

**Sintoma:** `ivoai auto` ou `ivoai opencode` encerra antes de aparecer a TUI com
branding IVOAI. **Causas comuns:** componente pinado ausente, versão diferente do
manifest ou backend loopback privado sem readiness. Execute `ivoai status` e
`ivoai doctor`; repare somente por `ivoai setup` ou updater transacional. Não aponte
AUTO para um binário OpenCode global não relacionado.

## OpenCode informa incompatibilidade de versão ou plugin

**Sintoma:** health do backend passa, mas o plugin gerenciado não carrega. Doctor
informa versões esperada e observada sem paths de credenciais. Execute
`ivoai update --dry-run` e depois `ivoai update` se o candidato revisado for
compatível. Plugin e pin do OpenCode formam uma unidade de rollback; não instale
plugin flutuante nem habilite auto-update do OpenCode no modo gerenciado.

## Codex ou Claude aparece não autenticado no painel IVOAI

**Sintoma:** `/ivoai` informa `authentication required`, embora uma sessão anterior
fosse válida. O IVOAI nunca copia credenciais para o OpenCode. Verifique o cliente
oficial (`codex login status` ou a superfície normal de login do Claude Code) e use
`ivoai connect chatgpt` ou `ivoai connect claude` para executar o fluxo oficial e
invalidar o cache stale de quota. Não informe provider key em `/connect` do OpenCode;
o AUTO gerenciado habilita apenas o provider IVOAI.

## O mapeamento de sessão não pode ser retomado

**Sintoma:** uma conversa OpenCode abre, mas o IVOAI inicia nova conversa no executor
oficial. O mapping é vinculado deliberadamente ao working directory e aos IDs estáveis
das fontes de conhecimento selecionadas. Alterar o diretório ou escopo cria nova
fronteira em vez de reutilizar estado potencialmente inseguro. Volte ao diretório e
escopo originais ou continue em nova sessão; nunca edite manualmente o JSON da sessão.

## O painel Knowledge está stale, vazio ou degraded

**Sintoma:** o rodapé mostra `0 configured`, uma source selecionada está down ou o
painel ainda não refletiu operação recente. Confirme profiles com
`ivoai connect server list` e use `ivoai connect server test <alias>`. O painel
atualiza status limitado a cada cinco segundos e incorpora operações observadas das
fontes selecionadas. O modo automático mantém fontes saudáveis e marca a sessão como
degraded; uma fonte explícita indisponível falha sem substituição.

## Tema, cores ou renderização em terminal estreito estão indisponíveis

O overlay gerenciado seleciona o tema IVOAI por plugin oficial do OpenCode.
`NO_COLOR`, `TERM=dumb`, non-TTY e fluxos JSON permanecem simples e sem branding.
Redimensione para um tamanho útil; labels longos são limitados, e o painel limita as
linhas visíveis. Se o terminal não renderizar marcas Unicode, use `IVOAI_ASCII=1` nas
superfícies nativas de status/menu do IVOAI.

## A configuração OpenCode gerenciada está corrompida

Arquivos gerenciados ficam sob raízes privadas de runtime/state e são regenerados por
sessão. Interrompa a sessão e execute `ivoai setup` ou update validado. Não copie a
config global nem credential store do usuário para a raiz gerenciada. Configuração
`.opencode` do projeto, sharing e auto-update são desabilitados somente no modo
gerenciado; o uso direto de `opencode` continua sob responsabilidade upstream.

Linhas de quota Codex são classificadas pela duração retornada pelo app-server
oficial. `Codex 5h: N/A / not exposed` não é erro nem bloqueia roteamento. Se a quota
de uma conta anterior continuar após login feito diretamente na CLI do provider,
execute uma vez `ivoai connect chatgpt` ou `ivoai connect claude`: essa é a fronteira
de autenticação compatível que invalida e executa novo probe somente para aquele
provider, sem copiar nem hashear credenciais.

## O proxy Headroom aparece, mas a TUI Codex/Claude Code não abre

O IvoAI 0.4.0 podia colocar `headroom wrap` em novo process group e deixar o terminal
foreground group com o IvoAI. Quando Headroom ou cliente oficial lia stdin, o kernel
suspendia o grupo de background com `SIGTTIN`.

Examine processos com `ps -o pid,ppid,pgid,tpgid,stat,args`. Filhos afetados têm
`PGID != TPGID` e `STAT` contendo `T`. Atualize para o patch corrigido. Versões
corrigidas mantêm a pilha interativa no foreground group atual, restauram modos do
terminal ao sair e preservam exit code do cliente oficial.

Headroom inicia seu proxy em sessão detached e pode reutilizar proxy saudável na
porta 8787. Não use `pkill headroom`: um proxy pode ser compartilhado ou preexistente.
O wrapper registra clientes com marcadores PID/start-identity e limpa um proxy que
criou depois que o último wrapper encerra normalmente.

## A sessão automática trocou de provider

Use `ivoai monitor --watch` ou `ivoai session show --json <session-id>`. Um failover
registrado contém primary atual, horário e motivo não secreto. Hard quota aciona
fallback; erro de rede não. A alternativa recebe o último checkpoint confirmado e
Git status/diff-stat limitado, mas o ivoai nunca executa reset, checkout ou clean no
working tree.

Se ambos estiverem exhausted, a sessão para em `BLOCKED` ou `WAITING_FOR_QUOTA`;
nenhum worker nem provider PAYG é iniciado. Aguarde o reset exibido, autentique um
cliente de assinatura elegível e inicie nova sessão. O supervisor recusa mais de dois
failovers automáticos consecutivos.

## Customização da statusline do Claude

O modo automático passa arquivo `--settings` privado somente ao processo Claude
iniciado. Não edita a statusline persistente do usuário. A statusline automática é
necessária para capturar model/context/rate-limit estruturados. Fora desse processo,
as configurações normais do Claude permanecem intactas.

Comece com:

```sh
ivoai status
ivoai doctor
```

Em um servidor, use:

```sh
sudo ivoai server doctor
sudo ivoai server status
```

`ivoai doctor --json` é adequado para automação e nunca inclui secrets. Não ignore
checks de checksum, TLS, ownership, symlink ou credenciais para continuar uma
instalação. Se o host foi instalado antes de uma correção abaixo, atualize para uma
release que a contenha ou reinstale a partir de checkout autenticado e então execute
novamente o setup idempotente.

## Instalação e primeiro setup do servidor

Esta seção registra falhas observadas ao conduzir hosts Linux limpos e já configurados
pelo fluxo real de instalação. Prefira estes checks antes de editar manualmente
arquivos Compose ou systemd gerados.

### O installer público não consegue baixar ou verificar o ivoai

O installer público baixa um archive da release e `checksums.txt` e recusa assets
cujo checksum não corresponda. Se `Download ivoai release` retornar 404, confirme que
existe GitHub Release para a versão e que ela contém archive da plataforma e checksum.
Não desabilite a verificação nem substitua por binário não revisado.

Em checkout autenticado, execute:

```sh
./install.sh
```

O installer de source usa Go compatível do sistema quando disponível. Se ausente ou
mais antigo que `go.mod`, baixa o toolchain Go oficial pinado para a arquitetura,
verifica SHA-256, usa caches temporários de build/modules e remove o toolchain depois.
Checksum divergente, arquitetura incompatível ou `no reviewed Go toolchain is pinned`
é hard stop, não algo a contornar.

Se o destino já contiver executável ou symlink `ivoai` não relacionado, o installer
recusa substituí-lo. Mova/remova somente após confirmar que não é instalação de
terceiro ou gerenciada separadamente.

### Docker está ausente ou antigo demais para o setup server

O servidor exige Docker Engine 28.0.0+ e Docker Compose 2.33.1+. O pacote `docker.io`
20.10 do Debian 12 é insuficiente: antecede gateway-priority, usado para rotear o
container de embeddings pela rede temporária de download.

```sh
docker version --format 'Engine server: {{.Server.Version}}'
docker compose version --short
```

No Debian 12, execute novamente `sudo ivoai setup --mode server`; o IVOAI pode
instalar Engine ausente pelo repositório oficial assinado após verificar a chave. Não
substitui configuração desconhecida nem Engine antigo não oficial. Caso contrário,
instale/atualize conforme instruções oficiais para
[Debian](https://docs.docker.com/engine/install/debian/) ou
[Ubuntu](https://docs.docker.com/engine/install/ubuntu/) e execute o setup novamente.
O ivoai não substitui deliberadamente Engine existente porque o host pode executar
containers não relacionados.

Com Engine compatível, o ivoai instala o plugin oficial Docker Compose, pinado e
verificado por checksum, em `/usr/local/lib/docker/cli-plugins/docker-compose` quando
Compose estiver ausente ou abaixo de 2.33.1. O download tem cerca de 49 MiB, janela
limitada de 30 minutos e informa bytes recebidos a cada 10 segundos.

Se houver plugin incompatível nesse path, o ivoai o preserva. Examine-o e mova-o
somente se quiser que o ivoai administre o local; depois execute o setup novamente.
Interromper download gerenciado é seguro; o arquivo temporário incompleto é removido.

### O setup parece travado enquanto dependências inicializam

Um primeiro setup limpo pode passar minutos em:

```text
Starting server dependencies; waiting for container health checks...
```

Isso não é necessariamente travamento. O setup informa tempo e o comando seguro:

```sh
sudo docker compose -f /etc/ivoai/compose.yaml ps
```

Para mais diagnósticos:

```sh
sudo ivoai server logs ivoai-dependencies.service
sudo docker compose -f /etc/ivoai/compose.yaml logs --tail=100 qdrant embeddings ai-memory
```

Não reinicie repetidamente enquanto o primeiro modelo de embedding é baixado; isso
reinicia a janela de health e pode fazer bootstrap lento parecer falha persistente.

### A primeira inicialização de embeddings leva mais de cinco minutos

O modelo local pinado é baixado no primeiro setup limpo. Hosts somente CPU e links
lentos podem levar mais de cinco minutos até Text Embeddings Inference ficar healthy.
Releases atuais fornecem grace period de dez minutos; probes bem-sucedidos a encerram
imediatamente. Starts posteriores reutilizam o cache persistente.

```sh
sudo docker compose -f /etc/ivoai/compose.yaml ps embeddings
sudo docker compose -f /etc/ivoai/compose.yaml logs --tail=150 embeddings
```

Erro de download ou DNS/conectividade difere de inicialização lenta e deve ser
diagnosticado como problema de rede, não aumentando novamente os timeouts.

### Embeddings não consegue baixar o modelo em host limpo

O container possui egress temporário somente para bootstrap. Ele é anexado às redes
interna, de publicação loopback e `ivoai-model-download`; esta recebe o gateway
preferido durante o download. Quando embeddings fica healthy, systemd desconecta a
rota de download.

```sh
sudo docker inspect ivoai-embeddings --format '{{json .NetworkSettings.Networks}}'
sudo docker network inspect ivoai-model-download
```

Em instalação atualizada, a rede deve estar anexada durante bootstrap. Não adicione
permanentemente egress amplo a `ivoai-internal` como workaround.

### Executar setup novamente falha com `too many levels of symbolic links`

Lógica antiga alterava ownership recursivamente em caches do modelo. Caches Hugging
Face usam symlinks legítimos e podiam produzir:

```text
open service-owned entry config.json: too many levels of symbolic links
```

O setup atual muda ownership somente nas raízes de mounts gerenciados e não percorre
caches normais. Atualize/reinstale e execute novamente. Não substitua symlinks do cache
por arquivos nem use `chown` recursivo indiscriminado.

### Qdrant falha com permissões sob `/qdrant`

Layouts antigos podiam impedir o processo Qdrant non-root de criar marcador de
inicialização ou dados temporários de snapshot:

```text
/qdrant/.qdrant-initialized
/qdrant/snapshots/tmp
```

O setup atual usa mounts do host separados e pertencentes ao serviço para storage,
snapshots e inicialização, mantendo o container non-root. Atualize/reinstale e execute
setup em vez de tornar todo o filesystem gravável.

### Containers saudáveis, mas Context informa `127.0.0.1:6333: connection refused`

Docker pode omitir/inutilizar publicação de porta quando containers estão somente em
rede interna, e desconectar a rede de publicação pode remover o bind loopback. As
releases atuais mantêm `ivoai-host-publish` anexada com IP masquerading desabilitado;
somente a rota temporária de download é desconectada.

```sh
sudo ss -ltnp | grep -E ':(6333|8080|49374|7744)\b'
sudo docker network inspect ivoai-host-publish
sudo docker compose -f /etc/ivoai/compose.yaml ps
```

Não desconecte `ivoai-host-publish`; ela é necessária aos mapeamentos loopback e não
fornece egress masqueraded ao backend.

### ai-memory é informado unhealthy embora o container esteja executando

ai-memory não expõe `/health` genérico esperado por diagnósticos antigos. Checks
atuais autenticam no MCP e fazem `tools/list` limitado. O gateway também regrava o
Host upstream e substitui a credencial do cliente pela credencial backend privada.

```sh
sudo ivoai server memory status
sudo ivoai server doctor
sudo docker compose -f /etc/ivoai/compose.yaml ps ai-memory
sudo ivoai server logs ivoai-gateway.service
```

Se instalação antiga informar indisponível com container saudável, atualize antes de
alterar autenticação ou Host allowlists manualmente. Context e agentes básicos
continuam utilizáveis enquanto memory está degraded.

### Reverse proxy retorna HTTP 502 depois do setup

`502` significa que o proxy não alcança o gateway. Primeiro faça o health local passar:

```sh
sudo ivoai server doctor
```

O gateway escuta em loopback por padrão, correto somente para proxy no mesmo host. Em
proxy de outro host/container, faça bind no endereço privado e confie só no CIDR de
origem:

```sh
sudo ivoai server gateway configure \
  --public-url https://ai.example.com \
  --listen 192.0.2.10:7744 \
  --trusted-proxy 192.0.2.20/32
```

Use endereços reais. Não use `0.0.0.0/0`. Requests em modo trusted-proxy também devem
ter `X-Forwarded-Proto: https`.

### Enrollment funciona localmente, mas é rejeitado pelo proxy

Respostas de rejeição são uniformes. Clientes atuais transportam o código one-time em
scheme dedicado de Authorization e metadados não sensíveis separadamente, tolerando
proxies que reescrevem bodies. O gateway aceita body legado somente durante transição
e rejeita requests ambíguos com os dois transportes.

```sh
sudo ivoai server logs ivoai-gateway.service
```

A auditoria segura distingue input malformed/divergente, scopes não autorizados,
problemas de state ou request roteado a outro gateway sem registrar código, verifier,
token emitido ou nome do cliente. Se expirou/foi consumido, crie novo código; não o
recupere nem reutilize.

### Restore termina, mas serviços reiniciam com erro de permissão

Backup validado é restaurado como arquivos regulares root-owned. Releases atuais
reaplicam ownership dedicado às árvores context, corpus e memory antes de reiniciar,
enquanto setup normal evita atravessar caches recursivamente.

Se host restaurado por build antigo falhar em permissões, atualize antes de repetir.
Não use `chown -R /var/lib/ivoai`: caches podem conter symlinks legítimos, e mudanças
amplas enfraquecem a separação entre gateway, context e dependências.

## Renderização do menu interativo

- Use `NO_COLOR=1 ivoai` quando cores ANSI não forem compatíveis.
- Use `IVOAI_ASCII=1 ivoai` quando a fonte não renderizar blocks; o header usa o texto
  simples `ivoai`, nunca lettering alternativo.
- Input por pipe e `TERM=dumb` selecionam intencionalmente o fallback numerado.
- Estado raw do terminal é restaurado em erros normais, EOF, Esc, `q` e cancelamento.
- Progresso animado vai para stderr; redirecione stdout separadamente para comandos/JSON.
- Resize é detectado com o menu aberto. Se SSH não propagar `SIGWINCH`, reabra depois
  do resize ou defina `COLUMNS` e `LINES` corretamente.
- Terminais muito baixos ocultam descrições e usam viewport rolável; o indicador mostra
  itens não exibidos.
- Se build antigo encerrar ao pressionar Up/Down, atualize. Releases atuais decodificam
  sequências já no buffer, sem confundir seta com Esc isolado.

## O agente está instalado, mas não conectado

Isso é esperado logo após setup. Execute `ivoai connect chatgpt` ou
`ivoai connect claude`. A autenticação no navegador pertence ao cliente oficial. Uma
`OPENAI_API_KEY` ou `ANTHROPIC_API_KEY` externa pode ter precedência dentro da CLI do
fornecedor; o ivoai não altera o ambiente shell do usuário.

## Headroom falha

Headroom é provider de compatibilidade deprecated, não o default. Doctor ainda
informa sua saúde na janela de observação. Selecione Direct com
`ivoai config set compression.provider direct` ou restaure o default Caveman com
`ivoai config set compression.provider caveman`. Memory e server state são independentes.

## Caveman faz fallback para Direct

É esperado quando Caveman está indisponível, Memory/Context autoritativo exige caminho
byte-exact ou OpenCode usa provider nativo subscription-only incompatível com o proxy
pinado. `ivoai status` mostra providers configurado/default e efetivo;
`ivoai doctor --json` inclui motivo limitado. IVOAI não exige chave PAYG nem repete
executor já iniciado.

## O servidor está inacessível

Codex e Claude Code continuam utilizáveis. Verifique base URL, hostname do certificado,
`/.well-known/ivoai`, `/health` e `/ready`. Incompatibilidade de protocolo exige
update de um dos lados; o ivoai não persiste conexão parcialmente compatível.

## Setup não consegue instalar um componente

O erro identifica o componente e preserva os outros. Confirme OS/arquitetura no
manifest, acesso HTTPS ao host da release upstream e execute `ivoai setup` novamente.
Setup repetido não duplica hooks nem substitui tools preexistentes. Para falhas de
installer, Docker, Qdrant, embeddings e primeiro server, use a checklist acima antes
de mudanças manuais.

## Serviços do servidor não iniciam

Execute `ivoai server doctor` e `ivoai server logs`. Verifique Docker/Compose e use
`systemctl status ivoai-gateway ivoai-context`. Diagnósticos redigem autenticação, mas
revise a saída antes de compartilhá-la externamente.

## ChatGPT ou Claude Web não conecta a `/mcp` {#chatgpt-or-claude-web-cannot-connect-to-mcp}

Verifique paths públicos sem secret:

```sh
curl -i https://ai.example.com/.well-known/ivoai
curl -i https://ai.example.com/.well-known/oauth-authorization-server
curl -i https://ai.example.com/.well-known/oauth-protected-resource
```

`502` indica proxy sem acesso à porta 7744. `401`/`403` gerado pelo proxy normalmente
indica NPM Access List, Basic Auth, WAF ou outra camada interceptando OAuth; remova o
desafio extra desse host. Preserve `Authorization` e `X-Forwarded-Proto: https`,
desabilite buffering no Streamable HTTP e permita read timeout longo.

Se OAuth informar redirect inválido, remova e adicione novamente o connector para
registrar dinamicamente a URI atual. Nunca amplie matching de redirect. Se o código
expirou/foi usado, crie outro com `ivoai server web-access create`; não coloque o
código na URL ou logs.

Use `ivoai server web-access list` para confirmar grant ativo e scopes. Revogue e
reconecte se a rotação de refresh token foi interrompida. Falha em tool de memory não
implica indisponibilidade de context; confira os status separadamente.

## O menu interativo está diagonal ou não cabe no terminal

Atualize se as linhas parecerem escada, avançarem à direita ou deixarem grandes áreas
vazias. Builds antigos escreviam newline simples no raw mode. Builds atuais incluem
carriage return, relêem largura/altura, paginam verticalmente e respeitam colunas.
`TERM=dumb` permanece fallback numerado.

## `ivoai connect claude` parece não fazer nada

Atualize antes de tentar. Builds antigos moviam o login Claude para background e o OS
o suspendia ao ler o terminal. Builds atuais mantêm Claude no foreground, exibem
preflight e solicitam fluxo oficial de assinatura:

```sh
ivoai connect claude
```

Conclua no navegador ou abra a URL impressa. Verifique com `ivoai doctor`. O ivoai
não lê nem armazena credenciais Claude. Se falhar, use `claude auth status` para
verificar o cliente oficial e confirme input interativo no terminal.

## Ruflo está instalado, mas sessão orquestrada é recusada

Execute `ivoai setup` e `ivoai doctor`. O modo exige wrapper privado e versão exata do
safe profile, health probe sem provider, Swarm ID real e registro do lifecycle primary.
Profile editado manualmente, allowlist antiga, `provider_execution=true`, memory
durável Ruflo ou falha em `ruflo swarm init/status` gera recusa explícita. Comandos
diretos continuam:

```sh
ivoai codex
ivoai claude
```

Não adicione provider keys para contornar. Ruflo coordena; não fornece inferência.

## Worker não iniciou

Doctor verifica `codex exec --help` e `claude --help` sem inferência. Repare com
`ivoai setup`, verifique os logins e examine com `ivoai monitor --session <id>`. Só
paths de componentes Codex/Claude gerenciados ou descobertos são aceitos; prompt de
delegação não fornece executável. Falha inicial do Headroom tenta o worker oficial
direto; falha após o wrapper iniciar é informada normalmente.

## Model é `unknown`

É resultado seguro, não falha de health. O ivoai só informa model verificado em
runtime, passado por `--model`/`-m` ou em configuração oficial compatível. Nunca infere
por versão da CLI, plano ou binário. Passe argumento oficial explícito se precisar de
proveniência determinística.

## Monitor mostra sessão stale

Execute `ivoai session show <id>` e `ivoai session stop <id>`. O ivoai só envia sinal
quando PID e marcador de start do processo Linux correspondem. Sem processo próprio,
o lifecycle stale é finalizado como failed em vez de arriscar PID reciclado. O JSON
da sessão pode ser mantido como histórico não sensível; não o edite.

## Recuperação de worker órfão

Shutdown normal cancela bridge, encerra process groups próprios, fecha tasks Ruflo e
remove runtime privado. Após queda de energia ou kill do ivoai, use
`ivoai session stop <id>`. Marcador divergente não é morto. Verifique o cliente
oficial apenas se processo correspondente realmente permanecer.

## ai-memory ou Context está degraded durante sessão

Esses serviços são independentes da inferência. Monitor informa `ready`, `degraded`
ou `disabled`, mas Codex, Claude e workers limitados continuam. Use
`ivoai memory configure`, `ivoai doctor` e status server de context/memory. Ruflo
nunca recebe cópia do corpus nem vira fallback de memória durável.

Se fato escrito por Claude não for lembrado pelo Codex, confira cronologia: a escrita
deve concluir antes da query. Execute Doctor e confirme que ambos os registros
`ivoai-memory` usam o mesmo servidor. Primaries gerenciados usam
`memory_read_page(query=...)` como primeira consulta limitada e no máximo um fallback
`memory_query`; em pesquisa tentam `context_search` antes da web. Escritas explícitas
usam uma página canônica e uma verificação, sem duplicar scopes. Hooks usam o
repositório Git principal como scope, evitando buckets diferentes por paths/linked
worktrees. Context aceita apenas documentos ingeridos, não conversas.

Se a tool encontra a página correta, mas o body termina antes da resposta, verifique
launch antigo com compressão lossy. Headroom 0.36.0 pode comprimir frames
`custom_tool_call_output`. Launches atuais ignoram compression quando Memory/Context
autoritativo está ativo e imprimem o motivo; inicie nova sessão após atualizar.
Processos existentes preservam sua policy inicial.

Para serviços IVOAI remotos registrados, leituras recebem overrides de aprovação
Codex locais e estreitos. Se `memory_query` ainda pedir aprovação, confirme que o
launch passou pelo `ivoai codex`, session ou automatic atual. Escritas não são
autoaprovadas.

## Múltiplos servidores e seleção de conhecimento

```sh
ivoai connect server list
ivoai connect server show mindsite
ivoai connect server test mindsite
ivoai doctor
```

Sem flag de source, IVOAI lê automaticamente todos os profiles habilitados e
conectados. Use `ivoai codex --knowledge-source mindsite` para restringir e repita a
flag para subconjunto exato. Escrita Memory ambígua é rejeitada; repita com um destino
em vez de desconectar outra source.

Resposta federada parcial significa timeout, HTTP failure, resposta oversized ou
JSON-RPC malformed em alguma source, preservando atribuição individual. Teste aliases
separadamente. Falha de um profile não apaga outro, e
`ivoai disconnect server mindsite` preserva Voicecorp.

Nomes MCP dentro de processos concorrentes podem ser os mesmos; cada um aponta para
router loopback e capability local próprios. Não infira upstream pelo nome. Confira
aliases selecionados e a lista; outra sessão não regrava configuração global.

Se Codex informar ausência de `codex-code-mode-host`, atualize. O setup atual instala
o asset companion oficial com mesma versão e SHA-256 revisado. Launch é recusado se
ausente/divergente; não copie executável não verificado de outra instalação.

Em WSL, VPN e split-horizon DNS, a primeira resolução pode levar cerca de cinco
segundos. Probes atuais aguardam essa janela. Falha real permanece limitada e não
bloqueia clientes oficiais.

## O scheduler automático está degraded

Execute `ivoai doctor` e examine **Automatic Scheduler**, **Parallel Worker Runtime**,
rotas de models, suporte a effort e Shared Knowledge Bootstrap. Router de model pode
estar degraded enquanto TUI oficial continua. Execute `ivoai setup` se cliente
gerenciado estiver ausente; discovery é renovado quando a versão muda.

Se task ficar `queued`, use `ivoai monitor --watch` e examine `depends`. Conclua o
pré-requisito do primary ou aguarde workers. `mode=primary` com “worker overhead is
not lower” é intencional.

Se bootstrap estiver degraded, confira Memory e Context separadamente. AUTO pode
continuar para trabalho autocontido, mas informa a lacuna. Não copie conteúdo para
session JSON ou Ruflo. Objetivo materialmente diferente gera novo bootstrap limitado;
follow-ups relacionados reutilizam brief com delta planning.

Se effort for `unsupported`, IvoAI permite que o cliente oficial escolha default.
Nunca adivinha effort ou model. Quota exhausted por model bloqueia apenas aquele
model; bloqueio do provider exige exaustão autoritativa ou falta de auth elegível.

Em LXC, `LXC_DETECTED=true` com daemon inacessível costuma indicar nesting, `keyctl`,
cgroup ou device permissions ausentes no host. Altere no Proxmox/LXC e repita setup.
Doctor e status informam `SERVER_SETUP=INCOMPLETE`; `.env` ausente é apenas sintoma.

### O serviço de documentação está indisponível

Execute `sudo ivoai server docs status`, `systemctl status ivoai-docs.service` e
`journalctl -u ivoai-docs.service`. O serviço deve escutar `0.0.0.0:7780`, salvo porta
alterada em `/etc/ivoai/docs.json`. Teste primeiro
`curl -f http://127.0.0.1:7780/` e depois o endereço LAN. Se loopback funcionar e LAN
não, examine firewall e rota do Nginx Proxy Manager; não substitua o serviço de
produção por `docusaurus start`.
