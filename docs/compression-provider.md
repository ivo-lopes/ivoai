# CompressionProvider: Caveman, Headroom e bypass direto

## Decisão

O IVOAI trata compressão como uma escolha mutuamente exclusiva por sessão. A
seleção possui três resultados possíveis: Caveman, Headroom legado ou execução
direta. Caveman e Headroom nunca formam uma cadeia. Até o cutover futuro, o
comportamento operacional continua usando Headroom ou bypass exatamente como na
v0.5.0; esta fundação não muda o default.

O Caveman será o sucessor planejado do Headroom apenas como
`CompressionProvider`. Ele não é MemoryBackend, ContextBackend, ArtifactStore,
Skill Registry, executor, orquestrador, policy engine ou secret manager.

## Fidelidade e WorkingContext

As representações são classificadas como `compressible`, `exact_required`,
`bypass` ou `unsupported`. Somente a primeira classe é elegível para um provider.
WorkingContext e ArtifactStore continuam preservando a evidência byte-exact;
compressão pode reduzir apenas a representação colocada no contexto. Respostas
autoritativas de Memory/Context, metadata do Skill Registry, evidência de
segurança, erros, stack traces e falhas de testes permanecem recuperáveis sem
perda e são tratadas como exact-required ou bypass pela política futura.

## Lifecycle e fallback

Antes de usar um provider, o IVOAI exige instalação, health, compatibilidade e a
capability `compression.wrap`. Falha de preflight, health ou inicialização antes
do início efetivo do agente permite fallback explícito ao cliente oficial direto.
Depois que um wrapper iniciou a sessão, o IVOAI não abre uma segunda sessão
silenciosamente. Exit status, sinais e controle do terminal continuam pertencendo
ao processo interativo oficial.

Versões gerenciadas usam staging, provenance, promoção atômica e rollback do
supply-chain manager existente. Headroom permanece disponível durante a janela de
compatibilidade; sua remoção requer uma release de observação posterior.

## Ownership, autenticação e licença

Credenciais pertencem aos CLIs oficiais. Um proxy pode transportar autenticação em
memória para o provider, mas o IVOAI não persiste bearer tokens, cookies, headers
de autenticação nem payloads em config, state, journal, observabilidade ou logs.

No Caveman v2.3.1, a CLI e skills são MIT, enquanto o runtime/proxy usado para
compressão é BSL-1.1. O Additional Use Grant observado permite avaliação interna,
desenvolvimento local, CI, integração e tráfego first-party self-hosted, inclusive
produção; serviço hospedado/gerenciado para terceiros exige licença comercial.
O IVOAI registra essa classificação por artefato e não descreve o proxy como MIT.
Isto é registro técnico do upstream, não parecer jurídico.

## Non-goals desta fase

Esta fase não habilita memória, MCP, browse, learn, pixel conversion, skills,
hooks, statusline ou setup global do Caveman; não altera `~/.codex`, `~/.claude`,
config do OpenCode ou shell rc; e não realiza o cutover. A política fina de
fidelidade e a integração runtime dos três executores pertencem a IVOAI-41 e
IVOAI-40, respectivamente.

## Provenance validada

- Produto Caveman: `v2.3.1`, commit imutável
  `b5ec6351396b643a17cbbec4a6eee8b3fb9dd782`.
- Runtime bundle: `bin-v1.1.3`, commit imutável
  `0d2f052babfd613ec9b4186c86ec6f133cdfd4d7`.
- Fonte oficial: <https://github.com/JuliusBrussee/caveman>.
- Licenciamento: `LICENSING.md` da revisão do produto acima.
