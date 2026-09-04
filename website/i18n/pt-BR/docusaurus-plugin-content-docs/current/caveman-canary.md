# Canário do Caveman e avaliação de fidelidade

O cutover do Caveman é condicionado por evidências, não apenas pela taxa de compressão. O
canário compara Direct, Headroom e Caveman sem alterar o default configurado. Uma única
divergência byte-exact, crossover de credencial, vazamento entre purposes ou corrupção silenciosa
bloqueia a promoção.

## Camadas

O canário commitado possui três camadas:

1. Testes herméticos usam runners falsos, servidores MCP e fixtures determinísticas. Eles
   exercitam o ciclo de vida do executor, WorkingContext, roteamento de Memory/Context,
   cancelamento, fallback e observabilidade redigida sem acesso ao provider.
2. Testes opt-in podem executar os assets revisados do proxy `bin-v1.1.3` e MCP depois que
   seus valores SHA-256 pinados forem verificados. Os assets permanecem temporários e nenhum
   setup global, `npx`, hook, skill ou memória do Caveman é habilitado.
3. O smoke do executor autenticado é explicitamente opt-in. Ele delega a autenticação a cada
   CLI oficial, nunca lê stores de autenticação e não é executado no CI normal.

O CI normal ignora todo teste que possa consumir quota de assinatura. Revisores habilitam os
smokes locais apenas com flags explícitas de ambiente e binários já baixados e verificados por
checksum. Autenticação ausente ou executor indisponível é reportado como blocked, nunca como pass.

## Corpus e contratos

`internal/cavemaneval` fornece um corpus determinístico limitado para JSON, JSONL, YAML, logs,
stack traces, Go, Python, shell, TypeScript, diffs, saída do Git, resultados de busca, resultados
MCP, tabelas, falhas de teste/compilador, texto longo, saída grande e repetitiva, texto de alta
entropia e conteúdo misto. As fixtures não contêm dados de produção da Voicecorp ou Mindsite.

Cada saída é gravada no ArtifactStore antes da projeção. A recuperação é comparada por SHA-256 e
igualdade de bytes. Entradas `exact_required` também exigem zero divergência, enquanto entradas
non-exact retêm os fatos declarados em sua representação limitada. A contagem de tokens do
harness usa uma heurística documentada de quatro bytes e é rotulada como estimada; ela não é uso
reportado pelo provider nem faturado.

Saída binária/não UTF-8 é sempre exact-required e nunca alcança a interface de compressão somente
de strings. Resultados com falha ou cancelados retêm status crítico, motivo de saída e ResultRef
exato.

## Gates de segurança

- a proteção primária de Memory/Context é provider-neutral e usa somente a projeção MCP
  selecionada para a sessão;
- workers ignoram Headroom quando Memory ou Context local à sessão está ativo;
- tools de Memory desconhecidas são tratadas como escritas potenciais, impedindo federation ou
  retry por redundancy até que sua semântica somente leitura seja revisada;
- falha no preflight do Caveman pode fazer fallback para Direct antes do launch;
- uma falha do Caveman depois do launch do executor encerra aquela sessão e nunca inicia um
  executor duplicado;
- o proxy pinado escuta em loopback, cria estado privado por sessão e é limpo quando o lease fecha;
- o helper MCP é stdio local, usa um runtime privado por chamada e nunca é exposto automaticamente
  ao primary;
- a observabilidade contém apenas enums, tamanhos, taxas e motivos limitados.

## Evidência local de 2026-08-31

O digest revisado do proxy amd64
`d883b9ab4b559e0c1935335c0e24400deb5c61d5e247f1ca239c4149f57885b0`
passou pelo probe estruturado `version --json` (que reporta verdadeiramente `dev`), readiness em
loopback e cleanup para os formatos de adapter compatíveis do Codex e Claude. O digest MCP
revisado
`c5c9a850f388570e2b822ac86ac35ad0e9f2c8ec0162b966f5536013042c058d`
processou todos os 22 cenários do corpus com zero divergência de artifact, exact-required,
semântica ou observabilidade. Sua taxa de contexto limitado medida foi aproximadamente
`0.028615` (983.089 bytes de entrada para 28.131 bytes de contexto); os números de tokens foram
estimativas heurísticas.

Os smokes locais autenticados do Caveman passaram para Codex e Claude Code. O executável OpenCode
gerenciado posteriormente passou em seu smoke Direct live por um fallback explícito de caminho
Caveman incompatível. O profile Caveman pinado do OpenCode só pode redirecionar providers
OpenAI/Anthropic; o IVOAI não solicita suas API keys nem reutiliza credenciais do Codex ou Claude.
Esse resultado datado é apenas evidência de apoio; relatórios completos voláteis ficam fora do
repositório.

## Non-goals

O canário não promove Caveman, migra defaults, remove Headroom, ativa memory/tools do Caveman,
adiciona suporte AUTO/worker ao OpenCode, acessa produção nem publica uma release.
