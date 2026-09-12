# Executores

## Codex nativo orquestrado

`ivoai codex` abre a TUI oficial diretamente, por uma façade App Server IVOAI.
O Prompt Gate é aplicado antes de encaminhar o turno. Plano e routing usam
aprovação nativa; o core continua criando workers Codex/Claude conforme policy.
`ivoai codex --direct` preserva o cliente direto sem orquestração. Nenhuma
credencial é copiada e a configuração pessoal permanece intacta.
Veja [Frontends orquestrados](orchestrated-frontends.md) para limites de compatibilidade.

## Sessões HTTP controladas

O contrato interno `OpenCodeExecutor.OpenSession` reutiliza o cliente HTTP/SSE
gerenciado, autenticado e restrito ao loopback. Ele oferece criação, consulta e
listagem de sessões, prompts síncronos e assíncronos, eventos, cancelamento, status,
diff, permissões, arquivos, agents e status MCP. O fechamento libera backend e
lease. Requisições estruturadas de `StartSession` usam esse lifecycle; a sessão
direta por CLI continua abrindo a TUI nativa. A autenticação nativa pertence ao
OpenCode e não é copiada para o frontend. O AUTO e os workers consultivos reutilizam
esse mesmo lifecycle controlado após os critérios de elegibilidade do scheduler.

## OpenCode nativo elegível no AUTO

Sem nova preferência, o IVOAI preserva a prioridade existente de Codex/Claude e
considera o OpenCode nativo como terceiro candidato. Para selecioná-lo explicitamente:

`ivoai opencode --planner opencode`

A seleção explícita falha de forma fechada se não houver modelo nativo autenticado
e capaz de usar ferramentas. O model picker também apresenta entradas sem ambiguidade
para os IDs nativos `provider/model` e suas variantes descobertas. Nenhum modelo,
reasoning, janela de assinatura ou quota ilimitada é inventado. A telemetria de quota
nativa é **unknown**, separada da autenticação e da elegibilidade de execução.

O executor nativo usa outro backend autenticado no loopback, sem retornar
recursivamente ao provider IVOAI. Somente o processo oficial OpenCode acessa seu
próprio armazenamento de autenticação. O IVOAI usa os metadados oficiais de
`auth list` e uma projeção limitada de providers/modelos; campos upstream com
credenciais são descartados antes de chegar ao catálogo, cache, diagnóstico ou UI.
Configurações, plugins e MCPs pessoais ou do projeto não são importados. Providers
que dependam de configuração customizada não projetada não são anunciados como
disponíveis.

As instruções do executor primário incluem as instruções aprovadas pelo Skill Gate
e a política de conhecimento do IVOAI. Somente Memory/Context da sessão e o
orchestrator IVOAI são projetados. Workers consultivos recebem o SharedContextBrief
e podem usar apenas ferramentas de conhecimento read-only e operações de leitura/busca;
`full` no frontend não autoriza escritas de workers. Subagentes nativos, carregamento
de skills não revisadas e diálogos nativos separados de perguntas são desabilitados:
pedidos de esclarecimento pertencem à mesma conversa no frontend. Aprovações nativas
interativas são encaminhadas ao frontend e autorizam somente uma operação pendente,
uma vez; cancelar o diálogo rejeita a operação.

O cancelamento aborta a sessão nativa e fecha seu backend. Sinais comprovados de
rate limit permitem failover limitado no AUTO antes da resposta final, nunca contra
seleção explícita de modelo/executor nem para executor já tentado no mesmo turno.
Rate limits nativos provocam um cooldown local curto, não um horário inventado de
reset do provider. A continuidade de autenticação permanece conservadora: IDs
nativos de resume não são reutilizados sem identidade/geração comprovada.

O rollback mantém os leitores de runtime da v0.9.2 utilizáveis: a telemetria de
quota nativa é uma extensão compatível, e sessões com metadata de AUTO/worker
nativo ficam em um namespace privado de sessões ignorado por binários anteriores.
O reapply volta a ler esse histórico sem alterar executor ou identidade da conversa.
Escritores antigos de quota podem descartar telemetria desconhecida; o discovery
a reconstrói.

## Continuidade da autenticação e resume

O IVOAI consulta novamente o cliente oficial antes de cada turno gerenciado. Como
os probes atuais não fornecem identidade estável da conta, o bridge inicia uma
conversa nativa nova em vez de reutilizar um ID de resume sem continuidade
comprovada. O histórico do frontend permanece no OpenCode. `/ivoai` informa essa
política. Nenhum arquivo de credencial ou hash de segredo identifica a conta.
Quando um adapter confiável fornecer identidade/geração não sensível, o resume
nativo também exigirá correspondência exata. Logout ou probe indisponível impede
o despacho; mudança de identidade não reutiliza o ID nativo anterior.

## Frontend gerenciado indisponível

Antes de qualquer despacho, falhas de startup/readiness do backend ou de attach
podem levar à TUI nativa do executor selecionado. O IVOAI informa
`AUTO_STATE=DEGRADED`, o executor e a indisponibilidade do painel/model picker do
OpenCode. Memory/Context, Skill Gate e políticas do executor permanecem ativos.
Essa recuperação está disponível para Codex/Claude. Um executor OpenCode nativo
selecionado falha de forma fechada se o frontend gerenciado não iniciar; o IVOAI
não substitui a execução por uma TUI pessoal não gerenciada ou por outro executor.
Não há troca silenciosa nem segunda execução. O fallback é recusado após o
registro de uma requisição ou quando outro frontend detém o lease exclusivo.
Resolva a sessão ativa antes de tentar novamente; não apague o lock para forçar
um segundo writer.

## Política de aprovação gerenciada

Configurações novas ou sem escolha explícita usam `interactive`: leituras e buscas
comuns em arquivos são permitidas; shell, edições, acesso fora do diretório e outras
operações exigem aprovação do OpenCode. Configure uma vez no IVOAI:

```bash
ivoai config set opencode.permission_mode full
ivoai status
ivoai config set opencode.permission_mode interactive
```

O último comando restaura as aprovações interativas. A alteração vale na próxima
sessão gerenciada de `ivoai opencode`, não no backend já aberto.
O painel `/ivoai` mostra o modo efetivo da sessão. `full` pré-aprova as operações
configuráveis do OpenCode; leituras diretas de `.env` e `.env.*` continuam negadas
(`.env.example` é permitido). Trata-se de política de aprovação, não de sandbox para
segredos. Ela não altera sandbox do executor, permissões Unix, Skill Gate, política
MCP, destino de escritas ou isolamento de credenciais. Aprovações próprias do executor
continuam independentes. Somente o overlay privado gerenciado muda; configurações e
autenticação pessoais ou de projeto do OpenCode não são editadas. A execução via
bridge de Codex/Claude não exige login adicional; a execução nativa opcional do
OpenCode exige a autenticação oficial própria do provider escolhido.

## Codex

`ivoai codex` inicia o frontend orquestrado controlado pelo IVOAI, usando o executor
Codex estruturado oficial. Codex é o primary preferencial, não o executor obrigatório
de todos os workers. `ivoai codex --direct` preserva a TUI oficial sem Prompt Gate
ou DAG. A autenticação permanece sob responsabilidade do cliente oficial.

## Claude Code

`ivoai claude` inicia o cliente oficial e preserva seu login nativo da assinatura.

## OpenCode

`ivoai opencode` iniciam a TUI do OpenCode na versão fixada como o
frontend gerenciado do IVOAI. O backend do OpenCode escuta apenas em uma porta
aleatória autenticada no loopback. Sua configuração gerenciada e isolada desabilita
a configuração do projeto, o compartilhamento e a atualização automática do
OpenCode; o uso direto de `opencode` fora do IVOAI permanece inalterado.

O provider gerenciado é uma bridge local do IVOAI. Ela seleciona `CodexExecutor`,
`ClaudeExecutor` ou um `OpenCodeExecutor` nativo elegível. Codex e Claude usam as
CLIs oficiais correspondentes com seus logins existentes. Nenhum token deles é lido, copiado, convertido ou
colocado no OpenCode. A bridge preserva streaming, cancelamento, failover de cota
limitado e um mapeamento opaco entre os IDs de conversa do OpenCode e do executor.

O seletor de modelos nativo do OpenCode contém uma entrada do scheduler e o catálogo
de runtime descoberto nos clientes oficiais. As entradas explícitas apresentam
somente variantes de raciocínio verificadas. O modelo e o esforço selecionados são
repassados a `codex exec` ou `claude --print`; seleções incompatíveis ou obsoletas
são rejeitadas antes de iniciar um executor. Retornar à entrada automática restaura
a seleção do IVOAI baseada em cotas.

Para usar intencionalmente os próprios providers do OpenCode fora dessa bridge,
execute `opencode` diretamente ou use:

```bash
ivoai session start --executor opencode --mode direct -- <upstream-options>
```

Esse caminho independente preserva a autenticação sob responsabilidade do OpenCode
e é distinto do frontend AUTO baseado no OpenCode.
