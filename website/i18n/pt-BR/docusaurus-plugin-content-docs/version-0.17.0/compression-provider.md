# CompressionProvider: Caveman, Headroom e bypass direto

## Decisão

O IVOAI trata compressão como uma escolha mutuamente exclusiva por sessão. A
seleção possui três resultados possíveis: Caveman, Headroom legado ou execução
direta. Caveman e Headroom nunca formam uma cadeia. Caveman é o default para
instalações novas e configurações legadas sem override; Direct continua sendo o
fallback de segurança. O componente Headroom e a configuração explícita são mantidos,
mas o wrapper gerenciado de Codex/Claude fica `unavailable_by_policy` e usa Direct.

Os wrappers do Headroom 0.40.0 fixado registram configuração MCP/memória de código
nos diretórios pertencentes aos clientes. A opção documentada `--no-mcp` torna os
marcadores de compressão inutilizáveis; um caminho sem alteração de configuração
com recuperação exata não foi verificado. O IVOAI recusa o wrapper antes de probes
ou inicialização. Instalação/saúde do binário permanece separada da capacidade do
wrapper. A correção trata um caminho opcional preexistente; não remove Headroom,
não muda seu pin e não altera o padrão Caveman.

O Caveman é uma implementação selecionável de `CompressionProvider` e o default
solicitado. O provider efetivo ainda pode ser Direct quando fidelity, health ou
compatibilidade exigirem. Ele não é
MemoryBackend, ContextBackend, ArtifactStore, Skill Registry, executor,
orquestrador, policy engine ou secret manager.

## Fidelidade e WorkingContext

As representações são classificadas como `compressible`, `exact_required`,
`bypass` ou `unsupported`. Somente a primeira classe é elegível para um provider.
WorkingContext e ArtifactStore continuam preservando a evidência byte-exact;
compressão pode reduzir apenas a representação colocada no contexto. Respostas
autoritativas de Memory/Context, metadata do Skill Registry, evidência de
segurança, erros, stack traces e falhas de testes permanecem recuperáveis sem
perda e são tratadas como exact-required ou bypass.

Para a sessão primária, qualquer fonte Memory ou Context autoritativa presente
na projeção MCP da própria sessão força execução direta enquanto o caminho de
compressão não oferecer proteção seletiva byte-exact para tool results. A regra
é provider-neutral: vale igualmente quando Caveman ou Headroom foi solicitado e
não depende de `headroom.enabled`. Ela é avaliada depois da seleção das fontes;
servidores não selecionados não alteram outra sessão, enquanto federação
explícita protege todas as fontes selecionadas. A observabilidade registra apenas
provider solicitado/efetivo, bypass, motivo e quantidade de fontes, nunca o
conteúdo de Memory/Context.

## Lifecycle e fallback

Antes de usar Caveman, o IVOAI revalida o objeto imutável ativo no supply chain,
executa `caveman-proxy version --json`, cria um runtime privado da sessão, inicia
diretamente o proxy gerenciado e aguarda `/health/ready`. O processo escuta apenas
em `127.0.0.1` numa porta efêmera. `CAVEMAN_HOME` e `CAVEMAN_CONFIG` apontam para
`<session-runtime>/caveman/proxy-*`; diretórios usam modo `0700` e a configuração
usa `0600`. Nenhuma captura é habilitada.

Falha de preflight, health ou inicialização antes do início efetivo do agente
permite fallback explícito ao cliente oficial direto. Depois que o agente iniciou,
uma queda do proxy encerra aquela sessão e é reportada; o IVOAI não abre uma
segunda sessão silenciosamente. Ctrl-C, SIGTERM, cancelamento e saída do executor
encerram o proxy e removem seu runtime transitório.

Versões gerenciadas usam staging, provenance, promoção atômica e rollback do
supply-chain manager existente. Headroom permanece disponível durante a janela de
compatibilidade; sua remoção requer uma release de observação posterior.

## Configuração e migração

`compression.provider` aceita `caveman`, `direct` ou `headroom`. Quando a chave
está ausente, o IVOAI resolve Caveman e registra `compression.source=default` em
uma instalação nova ou `migration` ao normalizar uma configuração legada.
Overrides persistidos são registrados como `explicit` e nunca são substituídos.
Como a v0.6.0 já materializava `provider=headroom` sem registrar se isso era
intenção do operador ou default histórico, o upgrade preserva esse valor de forma
conservadora. Campos TOML desconhecidos permanecem intactos.

## Ownership, autenticação e licença

Credenciais pertencem aos CLIs oficiais. Um proxy pode transportar autenticação em
memória para o provider, mas o IVOAI não persiste bearer tokens, cookies, headers
de autenticação nem payloads em config, state, journal, observabilidade ou logs.

Para Codex, a configuração process-local aponta um provider compatível com
`requires_openai_auth=true` para a rota `/chatgpt`; o próprio Codex continua dono
de `Authorization` e `ChatGPT-Account-ID`. Para Claude Code, somente a base URL é
redirecionada e nenhum token sintético é definido. O perfil OpenCode do runtime
Caveman pinado exige credenciais próprias dos providers OpenAI/Anthropic e não
consegue reutilizar os logins de assinatura pertencentes aos CLIs Codex/Claude.
Como o IVOAI não aceita chaves PAYG nem compartilha credenciais entre executores,
OpenCode falha no preflight Caveman antes do proxy e executa Direct exatamente uma
vez. Nenhum arquivo global ou de projeto é alterado.

O produto Caveman v3.2.0 e o runtime bin-v2.1.0 usam Apache-2.0. O IVOAI
preserva LICENSE, NOTICE e a atribuição MIT histórica no pacote nativo.

## Non-goals desta fase

Esta fase não habilita memória, MCP, browse, learn, pixel conversion, skills,
hooks, statusline ou setup global do Caveman; não altera `~/.codex`, `~/.claude`,
config do OpenCode ou shell rc. Compressão seletiva de
resultados Memory/Context permanece fora de escopo até que possa preservar bytes
e associações de chamadas de forma comprovável.

## Provenance validada

- Produto Caveman: `v3.2.0`, commit imutável
  `e20f07e8152a0c0360f58c09e79d30ac94329991`.
- Runtime bundle: `bin-v2.1.0`, commit imutável
  `e20f07e8152a0c0360f58c09e79d30ac94329991`.
- Proxy Linux amd64: SHA-256
  `44436d69705ad9968563c181cd11d396d6ae5eeccd8dbf228f6225d92dfadcb1`.
- Fonte oficial: [Caveman](https://github.com/JuliusBrussee/caveman).
- Licenciamento: `LICENSE` e `NOTICE` da revisão do produto acima.

O asset pinado informa `version: "bin-v2.1.0"` no probe estruturado.
A revisão imutável e o digest permanecem a autoridade de integridade.

## Telemetria segura

Eventos de compressão usam somente dimensões bounded: executor/provider, tipo de
payload, classe de fidelidade, bytes antes/depois, tokens estimados antes/depois,
ratio, latência, recovery count, resultado, bypass e fallback. Tokens retornados
pelo Caveman são sempre rotulados como `inferred`/`estimated`; não representam
billing nem telemetria autoritativa do provider. Prompt, response, output bruto,
código, diff, paths, environment, cookies e headers de auth não fazem parte do
schema de eventos.

Doctor/status mostram o provider configurado e a saúde dos componentes. Monitor
mostra o provider realmente usado na sessão e o último resultado de compressão
bounded, incluindo fidelidade e bytes; falhas de telemetria nunca impedem a
execução nem substituem a evidência original.

O corpus, os gates byte-exact e os smokes opt-in dos artefatos pinados estão em
[Canário do Caveman e avaliação de fidelidade](caveman-canary.md). Um resultado de
performance nunca substitui esses gates. O default solicitado é Caveman; a policy
continua escolhendo Direct quando necessário.
