# Migração backend-first: baseline auditado e gates de entrega

Data da auditoria: 2026-10-27. Baseline de código: `v0.12.1` publicada, commit
`3ff6db8bb0174b1ae6f781f6754e2653a6dae5db`, incorporando o
[PR #30](https://github.com/ivo-lopes/ivoai/pull/30).
Este documento distingue esse baseline do destino aprovado para a migração.
Ele não certifica implementações adicionadas posteriormente em uma branch de desenvolvimento.

## Fontes e estado de execução

O Git e observações reproduzíveis do runtime estabelecem o comportamento entregue.
O Plane registra a intenção de execução, os requisitos de aceite e o histórico das
tarefas; a descrição ou o estado de uma tarefa, isoladamente, não comprova que sua implementação existe.

O projeto canônico de execução foi recuperado no workspace `pessoal-ivo` do Plane,
projeto `b99f4a34-0e48-455a-acc7-7d908ba00478`, identificador `IVOAI`.
O item principal é IVOAI-191. Suas decisões backend-first e tarefas relacionadas
foram recuperadas; algumas leituras posteriores do histórico de atividades encontraram limitação de requisições.
O projeto histórico `mindsite` não deve receber atualizações como substituto.

As páginas canônicas de memória solicitadas não puderam ser recuperadas nesta auditoria:

- `mindsite/ivoai/arquitetura-evidence-gateway-economia-backend-first-2026-10-27`
- `mindsite/ivoai/decisao-backend-first-nativos-antigravity-remocao-opencode-2026-10-27`

As leituras pelo escopo padrão `ivoai` não encontraram páginas correspondentes;
o conector Mindsite rejeitou a recuperação. Por isso, seus conteúdos não são
apresentados aqui como evidências históricas verificadas de forma independente.
Os requisitos atuais da migração aprovada continuam disponíveis na instrução
explícita do mantenedor e no Plane. A busca no Context não retornou documentos
indexados relevantes. Seu MCP voltado a agentes é somente leitura; portanto,
esta auditoria não ingeriu nem sincronizou documentos:
`CONTEXT_SYNC=BLOCKED` (IVOAI-175). Escrever uma página de memória não alteraria
esse estado nem comprovaria ingestão no Context.

A inspeção remota confirmou a v0.12.1 como a última release publicada. O checkout
local inicial estava em uma branch de funcionalidade antiga da v0.10.4; a
implementação usa uma worktree separada, baseada no HEAD de main recuperado acima,
preservando aquele checkout. Os PRs abertos no momento da auditoria eram
atualizações de dependências. O PR #30 já estava incorporado. Jobs recentes de
atualização de Docker pelo Dependabot incluíram falhas; esses jobs não devem ser
apresentados como certificação aprovada do produto. Verifique novamente o CI do
produto e os workflows de release no HEAD exato antes da promoção.

## Limites dos subsistemas existentes

O executável continua sendo um único programa Go, com modos cliente e servidor.
A tabela cobre os pacotes de primeiro nível de `internal` no commit auditado;
o agrupamento de pacotes não implica uma segunda autoridade de runtime.

| Pacotes | Interfaces e responsabilidades existentes |
|---|---|
| `cli`, `app`, `terminalui` | Entradas CLI/TUI, menus administrativos e raiz de composição da aplicação. `App.OrchestratedWithKnowledge`, `SessionStartWithKnowledge` e os métodos de inicialização e gerenciamento conectam os serviços existentes. `cmd/ivoai/main.go` é a entrada do executável. |
| `core` | Contratos neutros em relação a provedores: `Component`, `Executor`, `MemoryBackend`, `ContextBackend`, `CompressionProvider`, `Orchestrator`, `SkillSource`, `SkillRegistry`, `ToolProvider` e `CodeIntelligenceProvider`; capacidades de runtime, saúde, compatibilidade e proveniência. |
| `agents`, `codexresolver` | Inicialização e ciclo de vida dos CLIs oficiais, normalização de argumentos, observações de processos, seleção de executáveis compatíveis e identificação por fingerprint. Executores específicos do OpenCode ainda coexistem. |
| `codexfrontend`, `opencodebridge` | Facade do App Server nativo do Codex e transportes gerenciados do OpenCode. Tipos compartilhados de turno, modelo, estado e continuidade estão atualmente em `opencodebridge`, criando uma dependência arquitetural inclusive para o Codex. |
| `session` | Registros privados de ciclo de vida, decisões, checkpoints, eventos operacionais, mapeamentos de continuidade, identidade por PID/marcador de início e persistência limitada. Conversas pertencentes aos provedores permanecem separadas. |
| `orchestrator`, `orchestration` | MCP e scheduler existentes por sessão, admissão de DAGs, dependências, workers, recuperação, coleta/integração de worktrees, política de concorrência e adaptadores de ciclo de vida. A coordenação nativa já existe; Ruflo permanece como adaptador legado explícito de ciclo de vida. |
| `routing`, `toolrouting` | Perfis de executor/modelo/esforço, níveis de tarefa e seleção por capacidade e custo. Autorização, compatibilidade, atualização e qualidade precedem a seleção econômica; medições ausentes são distintas de zero observado. |
| `workers` | Adaptadores dos executores oficiais, acesso MCP por tarefa e resultados limitados. Leitores consultivos coexistem com workers de implementação controlados em worktrees pertencentes ao sistema. |
| `workingcontext` | `ArtifactStore`, `ArtifactRef`, `ResultRef`, `WorkerResult`, achados e `StateDelta` consultivo transitórios; payloads exatos, hashes, TTL, propriedade, quotas e representações limitadas. |
| `codeintel`, `codesandbox` | Roteamento por intenção entre leitura direta e Graft, inventário do repositório, snapshots, cache de índices/resultados, observações de custo e isolamento do helper estrutural gerenciado. `core.CodeIntelligenceProvider.Query` é o limite substituível. |
| `knowledgepolicy`, `serverpool`, `knowledgerouter` | Contrato de pesquisa Memory primeiro/Context depois, seleção por finalidade/redundância e roteamento de conhecimento autenticado por sessão via loopback. Federação de leitura difere da seleção do destino de escrita. |
| `memory` | Ciclo de vida, configuração e hooks do ai-memory. Operações de conteúdo durável permanecem atrás de seu limite MCP autenticado. |
| `context` | `LegacyQdrantContextBackend`, catálogo documental, interfaces de embeddings/armazenamento vetorial, divisão em chunks, conectores seguros de filesystem/Git, ingestão e ferramentas MCP somente leitura para agentes. |
| `compression`, `caveman`, `headroom`, `cavemaneval` | Classificação de fidelidade, seleção exclusiva de provedor, Caveman padrão, compatibilidade opcional com Headroom, fallback Direct e fixtures limitadas de avaliação. |
| `externalmcp`, `connections` | Inventário MCP, projeções locais, gateways autenticados, registro de conexões e grants de ferramentas/fontes. Descoberta não concede autoridade de execução. |
| `skills`, `skillcatalog`, `skillgate`, `policy` | Registro/índice/grafo de Skills, descoberta de fontes, seleção por tarefa, avaliação de políticas e admissão de ativação. Presença no registro não é permissão. |
| `supplychain`, `skillupdate` | Identidade de origem revisada, validação de integridade, staging sem execução, promoção e rollback de artefatos gerenciados. |
| `quota`, `observability` | Probes/cache de quotas oficiais e eventos operacionais tipados e limitados. A exibição lê o estado existente; a execução mantém verificações de admissão em tempo real. |
| `config`, `secrets`, `project`, `platform` | Configuração e caminhos versionados, armazenamento privado de credenciais, detecção de projeto, arquivos/processos seguros e primitivas de redação de dados sensíveis. |
| `components`, `componentupdate` | Catálogo de componentes gerenciados com versões fixadas, proveniência, setup/atualização e propriedade dos executáveis. |
| `migration`, `update` | Grafo ordenado de schemas reversíveis, snapshots limitados, staging de candidato verificado, promoção atômica, recuperação transacional e rollback. |
| `server`, `servercmd` | Assets de implantação, credenciais privadas do backend, ciclo de vida de serviços, administração de conectores, runner de ingestão, backup/restauração e composição de gateways. |
| `gateway`, `enrollment`, `webauth`, `webmcp` | Leituras HTTP versionadas de descoberta/controle, enrollment com escopo, Web OAuth e composição de MCP remoto de leitura/escrita. Ainda não constituem uma API administrativa completa. |
| `doctor`, `docsserver` | Diagnósticos de runtime/matriz de componentes e serviço de documentação embarcada. |
| `promptgate` | Avaliação de intake limitada e sem execução. No baseline, ainda exige critérios de aceite observáveis e rejeita algumas solicitações informais que já seriam executáveis. |

As definições dos contratos estão em [core/contracts.go](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/internal/core/contracts.go)
e [core/codeintelligence.go](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/internal/core/codeintelligence.go).
Os contratos de runtime são intencionalmente separados da configuração persistida
e dos schemas de propriedade; adicionar um adaptador não exige necessariamente uma migração de schema.

## Fundação de evidências reutilizável e lacunas restantes

`workingcontext.Projector` persiste a evidência bruta dos workers antes de derivar
sua representação limitada. `LocalStore` oferece IDs opacos, verificação SHA-256,
arquivos privados, quotas de tamanho/quantidade, TTL e propriedade por sessão/tarefa/worker.
Leituras integrais e por intervalo estão disponíveis nas ferramentas MCP de
orquestração local. O ai-memory durável, o Context documental e os artefatos
transitórios têm finalidades diferentes e devem permanecer separados.

Evidências de código já são preparadas por
`app.prefetchCodeEvidence`, compartilhado entre primary e workers. A preparação
para o primary verifica grants e Skill Gate, respeita um orçamento limitado de
contexto e revalida a admissão após a recuperação. `orchestrator.primaryEvidence`
recupera dados fora do lock do scheduler. Esses são pontos de composição para o
Evidence Gateway, não motivos para adicionar um scheduler.

A identidade atual do Graft vincula a worktree física, o escopo de conhecimento
selecionado pelo host e as permissões. Seu cache valida a identidade da origem,
do adaptador e do runtime. O provedor usa snapshots privados, fila limitada entre
processos, locks por escopo e um processo estrutural serializado. O helper possui
ambiente restrito, limites de filesystem/rede/syscalls e teto monitorado de
memória de processo de 512 MiB. O caminho direto evita consultas estruturais
desnecessárias para perguntas suportadas sobre literais e declarações exatas.

O baseline não fornece um Evidence Gateway genérico. Faltam um envelope comum
de evidência/proveniência, reutilização por sessão entre primary e workers,
deduplicação, relato de suficiência/lacunas, separação explícita entre fatos e
inferências e seleção com orçamento entre Memory, Context, código e resultados
de ferramentas. O cache existente de resultados de código não comprova essas capacidades mais amplas.

A migração deve tratar explicitamente estes limites:

- `Ownership` registra sessão/tarefa/worker, não a tupla completa de organização/
  repositório/worktree/permissão. Vincule esses escopos adicionais na composição
  confiável do host e verifique novamente a autorização na reutilização e na recuperação exata.
- Um artefato de código preserva exatamente o resultado selecionado. Quando esse
  resultado é um trecho truncado, sua referência não recupera os bytes omitidos
  da origem. Informe a completude com honestidade e mantenha uma rota válida para a fonte integral necessária.
- `LocalStore` serializa uma instância com um mutex. Não presuma que isso ofereça
  garantias de quota ou deduplicação entre processos para instâncias independentes
  do armazenamento. Adicione ou reutilize admissão explícita ao ampliar o acesso concorrente.
- A sensibilidade do artefato é fornecida pelo chamador. O armazenamento rejeita
  classes de sensibilidade proibidas; ele não comprova automaticamente que os
  bytes brutos não contêm credenciais. A admissão do conteúdo deve impedir a persistência de segredos antes da escrita.
- Um hit de cache não pode repetir uma mutação, contornar grants atuais nem converter
  conteúdo recuperado em instruções de execução. Não é necessário um novo banco durável.

## Acoplamento e compatibilidade dos clientes

No baseline, `ivoai codex` usa orquestração por padrão, com `--direct` disponível
explicitamente. A inicialização do Claude permanece direta; seu caminho legado
explicitamente orquestrado não equivale ao caminho AUTO nativo.
`OrchestratedWithKnowledge` aceita apenas frontends Codex e OpenCode.

O frontend atual do Codex usa a interface oficial do App Server, mas seu
`Options.Bridge` é um `opencodebridge.Bridge`. O tratamento de turnos da aplicação,
as interfaces de workers, os eventos operacionais e a continuidade também
referenciam tipos de turno/resultado/catálogo no namespace do OpenCode. Remover o
pacote quebraria o comportamento compartilhado do Codex. Primeiro extraia os
contratos comuns de turno e executor sem alterar seu comportamento; depois
isole a apresentação e os transportes específicos do OpenCode atrás de seu adaptador.

Não foram encontrados adaptadores de runtime para Antigravity, RTK ou Codebase
Memory, nem implementação de Context com OpenViking, nos pacotes de runtime
próprios do baseline. Seu aceite exige investigar contratos e licenças upstream e
realizar testes reais. Execução headless, por si só, não deve ser descrita como
controle transparente da TUI nativa. Os clientes oficiais mantêm a propriedade da
autenticação; não copie tokens de assinatura nem habilite fallback não solicitado para APIs pagas.

A descontinuação do OpenCode deve preservar configurações, credenciais,
executáveis e histórico pertencentes ao usuário. Durante a coexistência, mantenha
erros explícitos de compatibilidade ou roteamento de comandos descontinuados.
As funções compartilhadas devem chegar ao núcleo antes da remoção da integração gerenciada.

## Dependências e plataformas suportadas

O `go.mod` do baseline exige Go 1.27.2. Os módulos Go diretos são
`github.com/coder/websocket` 1.8.15,
`github.com/modelcontextprotocol/go-sdk` 1.8.0,
`github.com/pelletier/go-toml/v2` 2.4.3,
`golang.org/x/sys` 0.49.0 e `golang.org/x/term` 0.47.0.
`go.sum` fornece a integridade dos módulos; as versões indiretas permanecem definidas por `go.mod`.
A disponibilidade de uma toolchain Go 1.27.3 para desenvolvimento é um fato do
ambiente de build, não uma alteração da versão fixada para bootstrap publicada
nem prova de equivalência entre os binários do benchmark.

As versões externas fixadas e as licenças revisadas estão registradas em
[manifest/components.yaml](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/manifest/components.yaml) e no catálogo compilado:

| Componentes | Versões fixadas no baseline |
|---|---|
| Clientes oficiais | Codex e host Code Mode 0.162.1; Claude Code 2.1.296 |
| OpenCode gerenciado | 1.19.0 com o contrato de patch/build revisado do repositório |
| Memória/código | ai-memory 2.6.3; Graft/Trail-Code 0.21.1-ivoai.1 |
| Compressão/ciclo de vida | Caveman e Caveman MCP 2.1.0; Headroom 0.40.0; Ruflo 3.56.5 |
| Runtimes privados | Node 26.11.1; uv 0.13.0; CPython 3.14.8 |
| Servidor | Qdrant 1.19.2; Docker 29.7.2; Compose 5.6.0; TEI 1.9.4 |
| Embeddings | `intfloat/multilingual-e5-small`, revisão `614241f622f53c4eeff9890bdc4f31cfecc418b3` |

Essas são versões fixadas no repositório, não afirmações de que cada dependência
está instalada ou saudável no host auditado. A entrada do IVOAI no manifesto ainda
informa 0.12.0; a tag de release e o commit estabelecem o baseline da aplicação,
não esse campo descritivo.

Preserve Linux amd64, incluindo CPUs sem AVX/AVX2, e Linux ARM64. Hashes de
arquivos de componentes, digests OCI, integridade/locks npm e constraints Python
com hashes devem continuar obrigatórios. Novas integrações precisam de suas
próprias evidências revisadas de licença, versão/commit imutável, integridade,
recursos, sandbox e rollback. Em particular, o escopo RTK-OSS deve ser distinguido
dos componentes BUSL, e um provedor opcional OpenViking exige avaliação da distribuição sob AGPL antes da adoção.

## Baseline econômico e proveniência

O [PR #30](https://github.com/ivo-lopes/ivoai/pull/30) entregou recuperação limitada,
projeção de schemas, contabilização de falhas e diagnósticos. O
[relatório de validação](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/release-notes/economic-runtime-validation.md) herdado
preserva vários experimentos com escopos e identidades de binários diferentes.
Algumas seções descrevem decisões intermediárias de draft/release; o commit
realmente incorporado e as [notas da v0.12.1](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/release-notes/v0.12.1.md)
estabelecem o estado final da publicação.

O gate econômico continuava reprovado quando a v0.12.1 foi publicada. O piloto de
schemas aumentou a entrada agregada em 6.06% e reduziu o tempo em 2.73%, sem atingir
nenhuma das metas. Subconjuntos positivos de prefetch limitado não comprovam
economia da sessão inteira. A comparação obrigatória de 16 cenários continuava
incompleta. Preserve resultados negativos e falhas de correção, em vez de
apresentar apenas pares corretos em ambos os braços.

Comparações futuras devem distinguir:

- Artefatos publicados de release, binários de produção compilados localmente,
  harnesses de teste injetados, ablações apenas de schemas e microbenchmarks locais.
  Eles respondem a perguntas diferentes e não são baselines intercambiáveis.
- Modelo/esforço solicitado, configuração informada pelos métodos oficiais do
  cliente e identidade da inferência no backend observável de forma independente.
  Um seletor de modelo ou configuração de thread não comprova esta última.
- Uso de entrada/saída/cache reportado pelo provedor e estimativas como byte/4.
  Entrada em cache é um subconjunto da entrada, não um total adicional de tokens a somar.
- Índices/artefatos locais frios e estado do cache do provedor. Limpar caches locais
  não comprova condições equivalentes de cache no provedor.
- RSS/CPU de processos filhos, bytes alocados e consumo de recursos da sessão
  inteira. Atribuição indisponível é desconhecida, não zero.

Registre revisões imutáveis, valores SHA-256 dos executáveis, identidades de
harness/fixtures, permissões, versões dos clientes e todos os retries. Use ordem
AB/BA e múltiplas repetições com oráculos executáveis de correção. Meça durações
por fase, contagens de ferramentas/repetições, workers/delegações, uso de recursos
e observações disponíveis de quota ou custo, sem armazenar prompts, respostas
privadas ou segredos nos relatórios. Predeclare pelo menos 10% de redução da
entrada agregada e 5% de redução do tempo decorrido, com qualidade equivalente e
sem regressão de segurança; investigue regressões por cenário e variância.
Uma única inferência bem-sucedida é um preflight, não um gate.

## Sequência de migração, rollback e aceite

Cada entrega permanece revisável de forma independente. Uma release funcional
pode declarar um gate econômico não comprovado; ainda assim, precisa passar nos
gates de qualidade, segurança, plataforma e compatibilidade. A sequência abaixo
não marca itens do Plane como concluídos.

| Entrega e itens do Plane | Dependências e alteração limitada | Aceite e principal risco | Rollback |
|---|---|---|---|
| A: baseline econômico, IVOAI-198/138 | Amplie o benchmark reproduzível e as observações tipadas existentes antes de otimizar. Complete os 16 cenários documentados e cargas representativas de projetos/linguagens. | Condições equivalentes, correção executável independente, falhas preservadas, uso real do provedor versus estimativas e contagens de recursos/fases/workers. Impeça que diferenças de harness ou provedores pulados se tornem alegações de economia. | Reverta as alterações de harness/instrumentação; preserve os relatórios históricos imutáveis. |
| B: Evidence Gateway, IVOAI-199/148/192 | Componha a recuperação existente, WorkingContext e ArtifactStore atrás de um limite experimental com ativação explícita. Adicione escopo/proveniência comuns, seleção limitada, recuperação exata e deduplicação segura; mantenha o scheduling no engine existente. | Rejeição entre escopos e de grants revogados, invalidação por alterações na origem/runtime/política, tratamento de TTL/integridade, reutilização concorrente, evidência incompleta explícita e ausência de repetição de mutações pelo cache. | Desative o caminho experimental e use a recuperação existente; descarte apenas entradas de cache identificadas e reconstruíveis. Sem migração definitiva da memória durável. |
| F1: intake/orquestração adaptativos, IVOAI-193 | Use o baseline medido e a disponibilidade de evidências. Aceite linguagem natural executável sem títulos obrigatórios de aceite; direcione trabalho simples ao primary sem DAGs/workers desnecessários. | Preserve decisões sobre operações destrutivas/privilegiadas/mudança de executor; comprove os caminhos de execução apenas pelo primary e despacho complexo, cancelamento limitado e ausência de bypass de autorização. | Restaure a política anterior de intake/despacho por alteração reversível de configuração/padrão ou reversão de código. |
| C/D: compressão seletiva de ferramentas, IVOAI-200/201/46 | Audite RTK-OSS com versão fixada e compare alternativas individualmente após B. Preserve os originais no ArtifactStore e a seleção atual entre Caveman/Headroom/Direct. | Erros/diffs/testes exatos continuam recuperáveis; não use entrada com autoridade submetida a compressão com perdas; meça os totais da sessão. Evite proxies empilhados e interpretar logs menores como economia equivalente da sessão. | Desative a feature flag; use Direct ou a seleção atual mutuamente exclusiva de provedores. Mantenha a compatibilidade existente com Headroom até haver evidência que sustente sua remoção. |
| E: adaptador opcional de código, IVOAI-190 | Audite Codebase Memory MCP e implemente o contrato de provedor existente depois que a seleção de recuperação puder ser medida. Preserve as opções direta/Graft. | Precisão real, tempo de índice frio/incremental, custo de RSS/CPU/tokens, isolamento e verificações de sandbox. Sem cadeia fixa Graft-depois-Codebase ou indexação desnecessária. | Desative o adaptador; preserve Graft/leitura direta e remova apenas seu índice gerenciado reconstruível. |
| F2: clientes nativos, IVOAI-194/195/196 | Extraia contratos compartilhados de turno e integre somente capacidades de clientes oficialmente suportadas à mesma admissão/scheduler. | Sessões, streaming, ferramentas, modelos, quotas e retomada reais quando suportados. Certifique com honestidade os limites de Claude/Antigravity; preserve autenticação e configuração pessoal byte a byte. | Selecione o adaptador/versão anteriormente suportado com verificações de compatibilidade da continuidade; use estados explícitos de não suportado/degradado quando não houver equivalente. |
| Retirada do OpenCode, IVOAI-191 e trabalho nos clientes | Conclua primeiro a extração de contratos compartilhados, o aceite dos clientes nativos e a migração administrativa. | Nenhuma dependência operacional nos caminhos suportados; comandos antigos explicam a migração; nenhuma exclusão de dados pertencentes ao usuário. | A release de coexistência mantém o adaptador gerenciado anterior até a aprovação dos gates nativos e da janela de observação. |
| G: Context e memória, IVOAI-23/24/25/26/175 | Diagnostique o caminho real de conectores/ingestão do servidor independentemente do MCP somente leitura para agentes. Compare provedores opcionais sem substituir ai-memory. | Ingira fonte permitida, verifique a identidade da fonte/documento e a recuperação, registre falha parcial/retry. Provedor opcional exige aceite de licença e isolamento; sem migração automática para OpenViking. | Preserve o backend existente; reconstrua o índice a partir do corpus autoritativo. Qualquer migração de armazenamento durável exige seu próprio participante transacional explícito com escritas suspensas. |
| H: API/painel administrativos, IVOAI-189 | Exponha os serviços administrativos atuais por uma API de backend com escopo depois que os contratos do núcleo estabilizarem; construa a UI sobre essa API. | Inventário de capacidades CLI/TUI e testes de paridade, autenticação, autorização, isolamento, auditoria, visualizações sem segredos e proteção de mutações. Sem scheduler conversacional ou cópia da memória na UI. | Desative as rotas do painel/API e preserve CLI/TUI; mantenha o estado do backend e as credenciais existentes. |

Atualização e rollback devem usar o
[contrato de compatibilidade de produção](production-compatibility.md) existente.
Registre novos participantes persistentes ou mantenha dados experimentais como
caches explicitamente reconstruíveis. Os snapshots atuais de atualização não
incluem indiscriminadamente os armazenamentos ativos de Qdrant, memória,
enrollment ou Web OAuth. Não alegue rollback de banco de dados com base em um
teste de atualização de configuração/binário. Preserve campos TOML desconhecidos,
a propriedade dos componentes e os armazenamentos de autenticação dos clientes oficiais.

Para cada incremento: execute os testes afetados de unidade/integração/limites de
segurança, race e verificações de compatibilidade adequadas à alteração; registre
o CI do HEAD exato e as limitações dos provedores reais; atualize as evidências
no Plane e as decisões duráveis; depois faça commit/PR. Nunca contabilize uma
integração opt-in pulada como teste real aprovado. Use as verificações existentes
de amd64/ARM64, sem AVX, instalação e atualização/rollback antes de uma release
que declare suporte a essas plataformas. Este documento não autoriza migração em produção.

## Reconciliação da documentação e da segurança

A auditoria de código não é uma certificação de vulnerabilidades de todo o
repositório. Ela identifica obrigações de projeto e divergências na documentação
que devem ser reconciliadas ao alterar os caminhos afetados:

- [architecture.md](architecture.md) se identifica como baseline da v0.5.0 enquanto
  mistura capacidades posteriores. Textos antigos de limitações/compatibilidade
  dizem que a orquestração nativa é trabalho futuro, embora `NativeOrchestrator` esteja implementado.
- [SECURITY.md](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/SECURITY.md) descreve uniformemente workers automáticos como
  leitores consultivos. O adaptador real de workers também suporta escritores
  controlados usando `workspace-write` do Codex ou ferramentas restritas do Claude
  em worktrees pertencentes ao sistema. Descreva explicitamente seus conjuntos de
  escrita aprovados pelo host e os limites de isolamento, validação e integração;
  não remova essas verificações para corresponder à redação antiga.
- O gateway remoto atualmente expõe MCP de conhecimento, enrollment/OAuth e uma
  pequena API de leitura de estado/doctor/conectores. A paridade da UI administrativa
  exige novas operações de backend com escopo, não a reutilização de um token
  amplo de agente ou de um endpoint de shell.
- Checksums do canal de release detectam corrupção, mas não são assinaturas
  independentes do publicador. Preserve o limite de autenticidade existente e não
  o descreva como proveniência assinada sem implementar e validar esse trabalho.

O próximo passo de implementação é o baseline econômico reproduzível e a adição
do Evidence Gateway experimental. A conclusão da migração mais ampla continua
condicionada às evidências reais de clientes, isolamento, compatibilidade e operação descritas acima.
