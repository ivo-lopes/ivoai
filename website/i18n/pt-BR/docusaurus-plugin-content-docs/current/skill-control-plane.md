# Fundamentos do Skill Control Plane

Este documento descreve os fundamentos implementados por IVOAI-13, IVOAI-14,
IVOAI-16, IVOAI-48 e IVOAI-49, além de atualizações seguras de packs, do Skill Gate
de sessões gerenciadas e do overlay de fontes curadas pertencente ao IVOAI. Sessões
existentes de `ivoai auto`, `ivoai codex` e `ivoai claude` continuam funcionando
quando o registry está ausente ou vazio.

## Fronteiras

As interfaces provider-neutral `core.SkillSource` e `core.SkillRegistry` permanecem
estreitas. Metadados ricos de skills ficam em `internal/skills`; a policy fica em
`internal/policy`; o staging genérico de artefatos externos fica em
`internal/supplychain`.

Metadados e bodies de skills externas são dados não confiáveis. Eles não podem
substituir o orquestrador IVOAI, conceder capabilities, alterar sandboxing nem
modificar policy.

## Registry versionado

O registry privado é armazenado em:

```text
$XDG_STATE_HOME/ivoai/skills/registry.json
```

O schema 1 do Registry registra IDs canônicos, descrições limitadas, revisões
imutáveis do upstream, integridade SHA-256, estado de signature e attestation, versão
lógica, default branch observada durante discovery, domains, triggers, dependências,
conflitos, phases, roles, capabilities declaradas, risco, compatibilidade, lifecycle
e timestamps não sensíveis de proveniência.

A identidade ativa é um commit imutável acompanhado de metadados de integridade. Uma
tag ou default branch é evidência útil de discovery, mas não uma revisão ativa e
reproduzível. A gravação do registry é determinística e atômica. Seu diretório e
arquivo são `0700` e `0600`; leituras no-follow rejeitam symlinks e leituras limitadas
rejeitam estado oversized.

Um registry ausente é interpretado como registry vazio e saudável. Isso preserva a
compatibilidade com v0.5.0 sem bump de schema de config, state ou ownership. O arquivo
do registry é participante opcional explícito do snapshot transacional de update,
para que futuras migrações do registry não fiquem invisíveis ao rollback.

Valores de lifecycle:

- `staged`
- `active`
- `quarantined`
- `previous`

## Índice somente de metadados

O discovery percorre uma árvore privada de fontes com limites e sem seguir symlinks.
Cada `SKILL.md` é aberto com `O_NOFOLLOW`, e a leitura para no delimitador final do
frontmatter. O body não é carregado para identificar ou ranquear um candidato.

O indexador normal:

- não faz chamada a LLM;
- não executa hook, script, comando, bloco de código nem arquivo de setup;
- aceita apenas um subconjunto declarativo e limitado de frontmatter;
- ordena deterministicamente candidatos e relatórios de quarentena;
- aceita milhares de entradas sintéticas dentro do limite do registry.

Frontmatter malformed, UTF-8 inválido, schema incompatível, ID duplicado, divergência
entre ID e path, symlink, traversal, metadados oversized, dependência ausente e
self-dependency resultam em quarentena ou erro limitado de discovery. Metadados
inválidos nunca recebem permissões padrão amplas.

O ranking retorna candidatos, não a seleção da sessão. Ele usa trigger exato,
keyword, domain, termos limitados de nome/descrição, compatibilidade e risco máximo.
Ordenação estável por score e ID torna o mesmo input reproduzível.

## Grafo de dependências e conflitos

O resolver modela dependências obrigatórias e opcionais, conflitos declarados,
compatibilidade de executor, disponibilidade de capabilities, tetos de risco, phases
de execução e roles composable ou exclusive.

As phases são:

1. planning;
2. research/context;
3. art direction;
4. implementation;
5. audit/review;
6. security;
7. orchestration;
8. interaction/profile.

Dependências obrigatórias são incluídas e ordenadas topologicamente. Dependências
opcionais restringem a ordem somente quando selecionadas. Candidatos duplicados são
consolidados. Dependências ausentes, ciclos, roles mutuamente exclusive, conflitos
explícitos, executores incompatíveis, capabilities indisponíveis e risco acima da
policy produzem falhas tipadas e determinísticas.

Phases complementares podem ser compostas. Múltiplos visual directors ou autoridades
de control plane concorrentes não podem. Uma skill externa pode declarar comportamento
de orchestration como metadata, mas isso nunca transfere a autoridade de orchestration
do IVOAI.

## Policy Engine

A policy é avaliada acima de registries, skills, hooks, tool providers e executors:

```text
IVOAI policy > external metadata or instructions
```

Os inputs são identidade/tipo do subject, capabilities declaradas, capabilities
solicitadas, risco, scope, validade dos metadados e estado de conflito. As decisões
são `ALLOW`, `DENY` e `REQUIRE_APPROVAL`.

Os tiers de risco são `LOW`, `MODERATE`, `HIGH` e `CRITICAL`. Uma capability read de
baixo risco, declarada e disponível, pode ser permitida. Escritas de alto risco podem
retornar uma exigência estruturada de aprovação para o futuro Approval Engine.
Capabilities destrutivas, privilegiadas, que desabilitem sandbox, executem shell ou
concedam autoridade de orchestration são negadas nestes fundamentos. Solicitações
desconhecidas, indisponíveis, não declaradas, inválidas ou conflitantes falham de
forma fechada.

O engine nunca recebe o body de uma skill como autoridade de policy. Texto como
"ignore policy", "grant shell" ou "become orchestrator" não pode alterar uma decisão.

## Supply chain unificada

O pipeline genérico aceita futuras skills, components e helpers:

```text
discover source
  -> resolve immutable revision
  -> fetch bounded archive
  -> verify SHA-256
  -> isolated staging
  -> structural validation
  -> policy validation
  -> extracted-content manifest
  -> immutable object store
  -> atomic active pointer
  -> health and integrity validation
  -> transaction commit
  -> previous retention
  -> rollback
```

Adapters de download são separados do staging. Os testes usam archives sintéticos em
memória; nenhum pack de skills real ou repositório externo é importado.

O staging aceita somente arquivos regulares e diretórios. Ele rejeita paths absolutos,
`..`, ambiguidade de barra invertida, paths duplicados ou reservados, symlinks,
hardlinks, arquivos especiais, executáveis inesperados, excesso de arquivos, excesso
por arquivo, tamanho comprimido excessivo e tamanho expandido excessivo. Os modos são
sanitizados para `0600`/`0700`. Skills não podem declarar executáveis. Components
podem declarar um path exato e limitado de executável, mas o staging nunca o executa.

Conteúdo validado é colocado em um path imutável de objeto identificado pelo ID e
revisão do artefato. Um file manifest canônico vincula paths, modos, tamanhos e
digests de arquivo à transação. A promoção primeiro autentica o journal de staging,
substitui atomicamente um pequeno active pointer privado, executa o gate de health e
integridade após promoção e então faz commit do journal. Qualquer falha restaura o
pointer anterior. Promoção repetida é idempotente, rollback revalida o objeto anterior
e pode ser repetido com segurança, e staging ou promoção interrompidos são
recuperáveis sem tocar dados fora da raiz gerenciada.

Active pointers autoritativos da supply chain são enumerados como participantes
transacionais explícitos do update. Objetos imutáveis e journals de staging ativos
intencionalmente não são copiados cegamente para snapshots de update.

Integridade, signature, attestation e trust são campos distintos. Um checksum
entregue pelo mesmo canal da GitHub Release prova integridade, não autenticidade
independente. Registra-se `not_exposed` em vez de inventar uma signature.

## Atualizações seguras de skill packs

`internal/skillupdate` combina o manager compartilhado da supply chain, Registry,
classificador de metadata, dependency resolver, Policy Engine, smoke determinístico e
callback do doctor em uma transação. Adapters de discovery resolvem dinamicamente a
default branch upstream, mas toda identidade staged e active é um SHA de commit. Um
adapter GitHub usa somente a API pública estruturada, faz fetch limitado do archive e
registra o digest calculado localmente como `commit_pinned_local_digest`; esse valor
deliberadamente não é chamado de signature ou attestation independente.

A promoção vincula o pointer da supply chain e a atualização do Registry. Falha de
validação ou doctor restaura ambos. O rollback revalida o objeto imutável anterior e
restaura as duas autoridades; a recuperação reconcilia transações interrompidas. Um
update sem mudanças verifica consistência entre Registry e pointer em vez de obter
sucesso silencioso. Discovery, staging, classificação, smoke e testes não executam
hooks do repositório, installers, Makefiles, package lifecycle hooks, scripts ou
binários.

## Skill Gate de sessão gerenciada

Antes de a UI oficial do Codex ou Claude receber a primeira instrução substantiva de
uma sessão gerenciada, o gate local executa:

```text
bounded session intent
  -> local Registry search
  -> metadata-only rank
  -> dependency/conflict resolution
  -> IVOAI Policy Engine
  -> select 0..N
  -> verify active pointer/provenance/content
  -> load only selected full documents
  -> bounded executor instruction
```

O gate não faz request de rede. Registry ausente ou vazio produz normalmente uma
seleção de zero skills. Registry corrompido, policy inválida, objeto ativo ausente,
divergência de pointer ou race de conteúdo não ativam skill externa e são observáveis
como degraded; uma skill exigida explicitamente faz sua operação falhar. Decisões de
policy usam somente metadata pertencente ao IVOAI. O conteúdo carregado depois da
aprovação continua marcado como não confiável e não pode alterar capability, risco,
policy ou autoridade de orchestration.

## Overlay de upstreams curados

`internal/skillcatalog/catalog.json` registra uma pré-triagem limitada das 13 fontes
upstream nomeadas. Ele mantém três camadas separadas:

1. nome e descrição fornecidos pelo upstream;
2. repositório, default branch, licença, commit e digest observados pelo IVOAI;
3. domain, triggers, phase, role, conflicts, risk, capabilities solicitadas e
   compatibilidade de executor pertencentes ao IVOAI.

O catálogo não vendoriza bodies completos de terceiros. Um classifier aceita apenas
o commit revisado e o digest do arquivo selecionado. Atualizar um commit upstream
exige, portanto, uma atualização revisada do catálogo antes da promoção automática.
Packs de visual direction compartilham um role exclusive, para que o graph rejeite
directors concorrentes. Ponytail permanece uma skill de implementation, i-have-adhd
um interaction profile e entradas selecionadas de Superpowers/Caveman permanecem
skills comuns. Packs com shell são negados por padrão e packs de segurança que usam
tools exigem aprovação; nem o Codex Security ToolProvider nem a compressão Caveman
são introduzidos aqui.

Revisões de repositório e observações de licença no catálogo são proveniência
point-in-time, não afirmações de que um upstream permanecerá inalterado. Runtime
discovery deve resolver novamente a default branch atual e a revisão imutável antes
de uma atualização futura.

## Diagnósticos e observabilidade

`ivoai status` informa o registry como ready/empty ou mostra contagens limitadas de
lifecycle. `ivoai doctor` e sua forma JSON adicionam um objeto
`skill_control_plane` com legibilidade/gravabilidade/schema do registry, contagens
active/staged/quarantined, health da proveniência, prontidão da policy e health da
raiz de staging da supply chain.

A allowlist de observabilidade existente aceita discovery do registry, ranking de
candidatos, quarentena, resolução de conflitos, decisões de policy, resolução de
source, staging, promoção e rollback. Eventos podem incluir IDs canônicos de skill e
artefato, revisão imutável, risco, lifecycle, decisão, trust e motivos limitados. Eles
não podem conter bodies de skills, prompts, scripts, conteúdo de README, arquivos
externos brutos, credenciais, headers nem environments.

## Trabalho adiado

Este bloco intencionalmente não:

- executa hooks de skills nem setup de terceiros;
- implementa a UX completa de aprovação;
- ativa entradas do catálogo antes de materialização local segura e policy;
- substitui Headroom, Ruflo, Context ou qualquer executor;
- implementa Codex Security ToolProvider ou Caveman CompressionProvider.

A análise de segurança e as fronteiras adversariais automatizadas estão documentadas
em [skill-control-plane-threat-model.md](skill-control-plane-threat-model.md).
