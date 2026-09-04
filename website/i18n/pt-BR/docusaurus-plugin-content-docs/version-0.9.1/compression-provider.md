# CompressionProvider: Caveman, Headroom e bypass direto

## Decisão

O IVOAI trata compressão como uma escolha mutuamente exclusiva por sessão. A
seleção possui três resultados possíveis: Caveman, Headroom legado ou execução
direta. Caveman e Headroom nunca formam uma cadeia. Caveman é o default para
instalações novas e configurações legadas sem override; Direct continua sendo o
fallback de segurança e Headroom permanece temporariamente disponível para
compatibilidade e rollback durante a janela de observação.

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

No Caveman v2.3.1, a CLI e skills são MIT, enquanto o runtime/proxy usado para
compressão é BSL-1.1. O Additional Use Grant observado permite avaliação interna,
desenvolvimento local, CI, integração e tráfego first-party self-hosted, inclusive
produção; serviço hospedado/gerenciado para terceiros exige licença comercial.
O IVOAI registra essa classificação por artefato e não descreve o proxy como MIT.
Isto é registro técnico do upstream, não parecer jurídico.

## Non-goals desta fase

Esta fase não habilita memória, MCP, browse, learn, pixel conversion, skills,
hooks, statusline ou setup global do Caveman; não altera `~/.codex`, `~/.claude`,
config do OpenCode ou shell rc. Compressão seletiva de
resultados Memory/Context permanece fora de escopo até que possa preservar bytes
e associações de chamadas de forma comprovável.

## Provenance validada

- Produto Caveman: `v2.3.1`, commit imutável
  `b5ec6351396b643a17cbbec4a6eee8b3fb9dd782`.
- Runtime bundle: `bin-v1.1.3`, commit imutável
  `0d2f052babfd613ec9b4186c86ec6f133cdfd4d7`.
- Proxy Linux amd64: SHA-256
  `d883b9ab4b559e0c1935335c0e24400deb5c61d5e247f1ca239c4149f57885b0`.
- Fonte oficial: [Caveman](https://github.com/JuliusBrussee/caveman).
- Licenciamento: `LICENSING.md` da revisão do produto acima.

O asset pinado responde ao probe estruturado, mas informa `version: "dev"`.
Portanto o IVOAI não o apresenta como versão semântica verificada em runtime: a
revisão imutável e o digest do supply chain permanecem a autoridade.

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
