# Executores

## Sessões HTTP controladas

O contrato interno `OpenCodeExecutor.OpenSession` reutiliza o cliente HTTP/SSE
gerenciado, autenticado e restrito ao loopback. Ele oferece criação, consulta e
listagem de sessões, prompts síncronos e assíncronos, eventos, cancelamento, status,
diff, permissões, arquivos, agents e status MCP. O fechamento libera backend e
lease. Requisições estruturadas de `StartSession` usam esse lifecycle; a sessão
direta por CLI continua abrindo a TUI nativa. A autenticação nativa pertence ao
OpenCode e não é copiada para o frontend. Esse adapter, sozinho, não torna o
OpenCode um worker elegível no AUTO: os critérios do scheduler continuam válidos.

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
sessão gerenciada de `ivoai auto` ou `ivoai opencode`, não no backend já aberto.
O painel `/ivoai` mostra o modo efetivo da sessão. `full` pré-aprova as operações
configuráveis do OpenCode; leituras diretas de `.env` e `.env.*` continuam negadas
(`.env.example` é permitido). Trata-se de política de aprovação, não de sandbox para
segredos. Ela não altera sandbox do executor, permissões Unix, Skill Gate, política
MCP, destino de escritas ou isolamento de credenciais. Aprovações próprias do executor
continuam independentes. Somente o overlay privado gerenciado muda; configurações e
autenticação pessoais ou de projeto do OpenCode não são editadas. Nenhum login
adicional de provider é necessário.

## Codex

`ivoai codex` inicia o cliente oficial e preserva seu login da assinatura do ChatGPT.

## Claude Code

`ivoai claude` inicia o cliente oficial e preserva seu login nativo da assinatura.

## OpenCode

`ivoai auto` e `ivoai opencode` iniciam a TUI do OpenCode na versão fixada como o
frontend gerenciado do IVOAI. O backend do OpenCode escuta apenas em uma porta
aleatória autenticada no loopback. Sua configuração gerenciada e isolada desabilita
a configuração do projeto, o compartilhamento e a atualização automática do
OpenCode; o uso direto de `opencode` fora do IVOAI permanece inalterado.

O provider gerenciado é uma bridge local do IVOAI. Ela seleciona `CodexExecutor` ou
`ClaudeExecutor` e executa a CLI oficial correspondente com seu login nativo já
existente. Nenhum token do Codex ou do Claude é lido, copiado, convertido ou
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
