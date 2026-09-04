# Executores

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
