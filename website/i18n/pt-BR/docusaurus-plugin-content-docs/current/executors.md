# Executores

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
