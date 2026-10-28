# Fase B: mapa de fontes e menor piloto vertical proposto

Investigação somente de código, sem implementação, inferência, acesso a credenciais ou pesquisa externa.
Fonte congelada: `origin/main` = `3be82befea6ed3875373afcb1bfaa450710310ac`.
A branch local `main` aponta para a versão antiga `75bb66f9`; não foi usada como código atual. O checkout original `ec2f610f` permanece intocado.
PR45/47 e demais branches posteriores não são presumidos incorporados. A política strict JSON/read-only/receipt do PR47 deve ser reconciliada na implementação futura, sem duplicá-la.

## O que existe de fato

| Contrato/caminho na fonte congelada | Estado e limite |
| --- | --- |
| `internal/evidence/gateway.go:59` Request; `:114` Gateway; `:137` Retrieve; `:244` Recover | Biblioteca operacional, não scheduler. Source admite code/memory/context/tool_read; adapters Memory/Context não existem. Owner, Scope, QueryDigest, Authorize e CurrentGeneration são fornecidos pelo host. Cache sempre habilitado no Retrieve; não existe opção NoCache. |
| `internal/evidence/gateway.go:146` chave; `:188` admissão de payload; `:209` packet; `:289` check | Cache inclui SessionID/scope/source/generation/query, não TaskID/WorkerID diretamente. Cada destinatário recebe outro artefato. Revalida auth/generation antes/depois de I/O; payload UTF8 de no máximo1MiB e orçamento1..32768B. Escopos são digests. HMAC aleatório pertence à instância/processo. |
| `internal/app/code_intelligence.go:157`; `internal/app/evidence_gateway.go:24` | Runtime com flag `IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY=1`, off por padrão. Só prefetch de código já autorizado. Autorização aplica tarefa, grants, Skills, worker/worktree/dependências; geração vem de Inventory+Snapshot locais. Não estender essa geração de arquivos para corpus remoto. |
| `internal/app/session.go:388,406,426,432` | OrchestratorServe abre o LocalStore existente e injeta PreparePrimaryEvidence/PrepareWorker no scheduler existente. Prefetch primary e worker desse DAG pode usar a mesma instância App/Gateway no processo do servidor Core. Não equivale a compartilhar cache com o App do frontend em outro processo. |
| `internal/orchestrator/primary_evidence.go:15,50,68,73` | Prefetch pós-admissão/plano, orçamento total4096B, prazo20s, campos de entrega transitórios. Wire atual chama-se primary_code_evidence. Não colocar Memory sob um rótulo de código; acrescentar campo genérico/knowledge sem quebrar o existente. |
| `internal/app/primary_code_evidence.go:23,44,59,75`; `internal/app/worker_access.go:31,115,205,243` | Pontos reutilizáveis de policy/SkillGate e preparação de contexto. AllowedMCPTools não pode ser contornado por prefetch. Workers recebem seleção explícita de fontes e capability revogável independente. |
| `internal/knowledgerouter/router.go:50,170,214,398,493` | Transporte institucional real: seleção/failover, credenciais do host, validação JSON/SSE e resposta limitada16MiB. `Event` só contém métricas, não corpo/args/generation. `callGroup` devolve o profile realmente utilizado; esse profile, inclusive failover, precisa ser a proveniência. |
| `internal/app/knowledge_router.go:67,81,95,122,136` | Cada router é criado por capacidade/sessão/preparação. As credenciais ficam no host; clientes recebem token loopback. Não existem geração do corpus, epoch de revogação remota ou ETag confiável nestes contratos. |
| `internal/core/contracts.go:186` MemoryBackend | Só Component/Configure/Disable. Não contém Query/ReadPage; AIMemoryBackend não é um adaptador de leitura de páginas. Não inventar um método existente nem forçar mudança desse contrato administrativo. |
| `internal/core/contracts.go:192,225`; `internal/context/backend.go:55`; `internal/context/mcp.go:95` | ContextBackend tem Search/GetDocument/Recent. Documento tem ModifiedAt/IngestedAt, mas isso não é geração monotônica nem prova do índice/corpus remoto. Handler MCP de Context é read-only; context_get_document retorna documento untrusted. |
| `internal/workingcontext/store.go`; `model.go:49,87,115`; `local.go:36,94,413` | ArtifactStore existente, ArtifactRef e ResultRef; SHA, TTL, sensibilidade e ownership. LocalStore16MiB/objeto,64MiB/sessão e24h TTL padrão. Dono session-only pode ler todos os artefatos dessa sessão; TaskID/WorkerID preenchidos restringem a seus donos. Copiar o mesmo Ref do primary ao worker viola esse limite. |
| `internal/orchestrator/server.go:432,457` | Recuperação atual chama LocalStore.Read/ReadRange com owner SOMENTE SessionID. Não chama Gateway.Recover nem revalida Scope/Generation. Não é uma API de recuperação Gateway já entregue. |
| `internal/app/worker_access.go:254`; `internal/workers/adapter.go:165,400,424` | Workers têm Access e MCPGrants exatos, SharedContextBrief de até32KiB, instrução explícita de dado não confiável. Não existe leitor de ResultRef projetado para worker. Dar todos os tools do orchestrator ao worker criaria autoridade indevida. |
| `internal/orchestrator/server.go:521`; `internal/workingcontext/projector.go:73` | Runtime de saída dos workers: grava raw.Evidence exato ANTES da projeção/compactação, retorna ResultRef. Já existe; não duplicar o raw ou criar outro banco de logs. Saída até15MiB no adapter não cabe no limite1MiB do Gateway. |
| `internal/evidence/output_view.go:14,46`; `internal/nativecodex/registry.go:28`; `runner.go:39` | SPI RTK/output-view sobre snapshots exatos e runner diagnóstico já entregues. Sem App wiring geral; runner exige provider loopback sintético. Não remover guard, recriar Registry ou anunciar integração nativa/RTK ao conectar fontes. |

## Lacunas que não podem ser resolvidas com uma constante de geração

1. `Retrieve` sempre pode reutilizar sua chave. Dar a todo `memory_read_page(query=X)` a mesma geração, timestamp local ou hash de configuração retornaria dado remoto antigo como fresco. ModifiedAt/IngestedAt/health/counts também não resolvem corpus, exclusões e permissões.
2. Gerar hash da resposta requer primeiro uma leitura real. Isso identifica o snapshot recebido, não prova a versão atual do servidor posteriormente. Sem protocolo explícito, cada pedido de leitura viva deve chegar ao upstream; deduplicar somente consumo do MESMO snapshot registrado.
3. Uma credencial ainda presente no Store do host não prova que o serviço remoto ainda a autoriza. O piloto pode autorizar retenção/reuso de evidência já obtida dentro do grant da sessão, como WorkingContext; não pode afirmar revogação remota contínua. Caso se exija autorização remota a cada recuperação, será necessária revalidação institucional ou bypass do reuso; esse requisito permanece BLOCKED até haver contrato verificável.
4. A instância Gateway do processo do frontend e a do subprocesso OrchestratorServe têm HMAC/caches diferentes. Não persistir o seal como se fosse recuperável após restart nem criar uma chave compartilhada ad hoc. O primeiro piloto deve manter emissão, reuso e recuperação no mesmo Core owner.
5. O transporte institucional preserva conteúdo e esquemas MCP. Não embrulhar silenciosamente toda resposta existente num novo structuredContent que contradiga outputSchema do provedor, nem reutilizar um JSON-RPC id original como resposta a outra chamada.

## Piloto recomendado para revisão do root

Nome sugerido: `IVOAI_EXPERIMENTAL_KNOWLEDGE_EVIDENCE=1`, off; caminho legado intacto quando off. Somente DAG Core já aprovado, primary e um worker read-only no mesmo repositório/worktree/sessão, um destino institucional explícito, sem federação, failover entre evidências sem atribuição, escrita, histórico/restart ou novo scheduler.

### Intenção e fonte

Usar um seletor tipado pequeno opcional no plano aprovado (ou entrada equivalente host-owned no mesmo contrato), não classificar palavras do prompt para conferir autoridade. Operações iniciais: `memory_read_page` com query/path bounded e `context_get_document` com id. Endpoints, headers e credenciais nunca vêm do seletor. Grants atuais de fonte/tool e SkillGate continuam decisivos. Campo opcional/nil preserva JSON legado; não usar todo Task.Task como query nem enviar prompt bruto como telemetria.

Começar com Memory e uma página exata; Context usa o mesmo recorte após a tentativa Memory exigida pela política de pesquisa. Um teste isolado de Context pode ser rotulado como teste do adaptador, sem fingir que exerceu a política institucional inteira. Search/federação/suficiência semântica e relações inferidas ficam fora do primeiro incremento.

Fatorar uma leitura host-owned no router existente, usando sua validação e callGroup, sem outro cliente de autenticação. Deve devolver corpo finito validado, profile realmente escolhido e metadados de chamada. `Observe` atual não serve para recuperar o corpo e não deve receber conteúdo/segredos em logs. RPCs mutáveis, hooks REST, unknown methods e anotações upstream não conferem elegibilidade para esse caminho.

### Snapshot e ResultRef

Toda consulta VIVA faz upstream real. Após sucesso e autorização repetida, criar SnapshotID aleatório host-owned e SHA do corpo recebido. Inicialmente limitar snapshot a256KiB e poucas refs por tarefa; acima disso bypass/degradação explícita, preservando o caminho original, sem fingir armazenamento integral nem truncar silenciosamente.

Request.Source=memory/context; CurrentGeneration descreve APENAS o snapshot local imutável, usando SnapshotID+sourceFingerprint+rawSHA. QueryDigest inclui operação/args normalizados+SnapshotID+orçamento; não é uma chave de consulta viva. SourceFingerprint inclui o profile realmente usado, finalidade/organização, config/credencial-lease e limites relevantes, sem expor headers, tokens ou URLs sensíveis.

O Gateway armazena bytes exatos da resposta MCP recebida como dados, não os reproduz como resposta de protocolo com outro RPCid. Envelope separado contém ResultRef, Source/SnapshotID, fetched_at, source_freshness=unknown, extracted/inferred, gaps, untrusted e exact_required. SourceComplete refere-se ao que foi efetivamente recuperado: desconhecido/partial permanece gap; um search excerpt não vira documento completo. Sem resumo livre ou compressão de evidência autoritativa.

Registrar apenas bindings/ref/proveniência bounded no runtimeTask/runtimePlan existente, não raw em Session/checkpoint/observações e não um novo Registry/banco/memória. Raw continua só no ArtifactStore/Gateway existentes. A política SourceSnapshot é distinta de qualquer promessa de corpus atualizado.

### Reuso primary → worker

Após o primary produzir snapshot, a tarefa worker recebe por plano um snapshotID/ref de origem registrado pelo host, não uma alegação textual de Ref do modelo. Adoção exige fonte e tool incluídos no grant do destino, dependências, task/worker vivos, identidade física do worktree, scope da organização, sessão e config/credencial-lease atualizados.

O host revalida o binding de origem e lê o artefato exato sob sua autoridade de sessão; o worker não recebe acesso direto ao artefato do primary. Emitir via Gateway um novo ResultRef de mesmo SHA sob Ownership{session,task,worker}. Se scope/generation/query/budget coincidirem há cache-hit process-local; grants diferentes podem impedir o hit, e isso não deve ser achatado para fabricar economia. Cache miss/eviction pode reler o SNAPSHOT local já registrado, nunca executar de novo uma operação mutável nem transformar uma nova consulta viva em hit.

Não depender de o primary originário continuar como tarefa ativa para permitir compartilhar um resultado concluído com seu dependente: o grant do DESTINATÁRIO e o binding de evidência concluída precisam autorizar a adoção. Isso é transferência de evidência sob política do Core, não empréstimo da capability nem credencial do primary. Cross-worktree e cross-processo ficam fora da primeira prova.

### Recuperação exata é parte obrigatória da fatia

Não basta anexar ResultRef ao SharedContextBrief se o cliente não o consegue recuperar. Para IDs emitidos pelo novo piloto, os handlers existentes de artifact_read/range devem consultar o binding canônico no runtimeTask/Core e chamar Gateway.Recover com Request derivado do host, não aceitar Scope/Owner/Generation do modelo. Um ID conhecido como Gateway não pode cair no fallback permissivo do LocalStore após revogação ou erro. Referências legadas de worker-output mantêm seu contrato atual, sem alegação de recuperação Gateway universal.

Para o worker, projetar apenas handlers de leitura exata/range sobre as refs reemitidas através de `externalmcp.Target.Local` e `workers.Access/MCPGrant` existentes. Reservar namespace Core próprio se necessário, usar o mesmo token revogável do gateway do worker e os mesmos callbacks de policy; não expor plan/spawn/approval/scheduler nem outro listener/serviço de autorização. Rotas continuariam `/mcp/<i>` do transporte já existente. Isso é uma projeção de capacidade, não um novo orquestrador. Preferir extrair/reutilizar o leitor existente a copiar seu parser.

O owner do Core recupera byte-exatamente; range deve ser aplicado depois de conferir integridade do artefato e auth/generation antes/depois. Perda do processo/HMAC ou do binding retorna indisponível; não reconstruir uma capability a partir de texto/persistência oportunista. Contexto pequeno usa referência; recuperação explícita é a saída para contexto insuficiente.

### Restart e binding ausente: marcador persistente obrigatório

O mapa em memória não basta: depois de restart, um ID antigo continua em disco e a ausência do binding não pode autorizá-lo pelo caminho legacy session-only. A decisão de exigir Recover precisa sobreviver ao processo e independer de a flag estar ligada no novo lançamento, tanto para read quanto read_range.

Hoje não há marcador: `Gateway.packet` grava `ArtifactStdout` (`gateway.go:214`) e `ArtifactRef` não tem campo de política/proveniência. Portanto não há como classificar retroativamente todos os antigos artifacts de Gateway separando-os do stdout direto só pelo ID/Kind atual.

**Extensão aditiva recomendada para revisão:** usar um namespace opaco de IDs `evidence_<32hex>` exclusivamente para artefatos emitidos pelo Gateway com recuperação protegida, mantendo `artifact_<32hex>` para legados. O formato usa a infraestrutura existente, sem outro diretório de banco, HMAC compartilhado ou tabela de autorização. Adicionar um discriminador host-owned opcional no PutRequest (zero conserva legado) para o LocalStore emitir o prefixo protegido. Não aceitar ID/prefixo do modelo. Gateway exige esse prefixo ao receber o Put antes de selar/entregar; um ArtifactWriter que ignore o contrato não pode devolver uma ref utilizável por fallback.

Atualizar em conjunto `workingcontext/model.go:26,90` (validação), `local.go:125,285,323,363,426` (emissão/inventory/GC/leitura) e os schemas/parser `orchestrator/server.go:179,180,535`, além das fixtures/consumidores do ResultRef. Ambos os formatos continuam válidos no Core novo; a mudança de valor opaco é aditiva e exige nota de migração para consumidores que tinham hardcode do prefixo.

Ordem obrigatória nos readers expostos:

1. Validar o ID. Prefixo `evidence_` significa somente **exigir o caminho protegido**, nunca conceder acesso.
2. Procurar binding host canônico e revalidar requester/sessão/task/worker/scope. Ausente, expirado, processo novo, cache de bindings descartado, HMAC novo ou armazenamento indisponível => erro fixo `EVIDENCE_BINDING_UNAVAILABLE`/denied, sem chamar reader legado. Isto vale mesmo com flagoff após restart.
3. Binding presente => Gateway.Recover com Request/Ref host-owned. Qualquer erro de grant, geração ou seal termina a leitura; não fazer fallback. Range ocorre apenas sobre bytes exatos que passaram esse caminho e valida overflow/limites.
4. Só IDs antigos explicitamente classificados como legados usam sua política prévia. Trocar `evidence_` por `artifact_` na string não aponta para outro nome de arquivo nem deve permitir relabeling.

**Por que não apenas um novo Kind/campo no Ref?** O reader antigo usa DisallowUnknownFields e validKind. `usageLocked` visita toda metadata reconhecida; novo Kind/campo pode fazer rollback degradar TODO o ArtifactStore, e GC antigo não corrige a metadata desconhecida. O namespace de ID evita esse envenenamento: o Core antigo rejeita o ID no parser e ignora diretórios que não casam seu regex (`local.go:285,323`), logo não recupera a evidência nem bloqueia artefatos legacy.

Limite do rollback por namespace: Core antigo não conta nem coleta esses objetos; permanecem limitados pelo piloto e preservados até novo Core/GC entender seu TTL. Documentar esse custo transitório de quota/disco, sem apagar evidência para tornar rollback verde. O Core novo deve contar/coletar os dois prefixos. Não afirmar suporte a recovery/downgrade de turno admitido.

**Legados pré-marker:** o piloto só pode prometer a garantia nova para refs marcadas que ele emitir. Os antigos `artifact_` do Gateway de código não são identificáveis após restart. Se o root exigir essa garantia retroativa, a única política segura sem registro de origem é negar a classe ambígua de stdout legado na rota afetada (com mensagem de refresh autorizado), ou uma migração separada comprovada. Não fingir que um mapa reconstruído por conteúdo resolve isso. Não apagar/renomear arquivos pessoais ou inventar capability persistente para recuperar seals perdidos.

Gates específicos: gravar ref protegida num processo; encerrar; abrir reader/Core novo com mesmo ArtifactStore e nenhum binding, com flagon E flagoff; read e range devem negar sem chamar o caminho legado nem devolver bytes. Repetir após descartar apenas o mapa, após expirar/revogar binding e ao forjar prefixo/Owner/Scope. Um Ref legacy genuíno segue seu contrato. Teste de rollback em binário antigo verifica rejeição do ID protegido e operação normal de Put/GC/read dos legados; documentos novos não somem nem envenenam o inventário antigo.

### Contrato mínimo do incremento

| Elemento | Contrato proposto, não implementado |
| --- | --- |
| Pedido vivo | SourceAlias selecionado; operação exata permitida; args curtos normalizados; nenhuma URL/header/credencial do cliente; seleção opcional aprovada no DAG. |
| Snapshot | ID aleatório host, origem REAL devolvida pelo router, argsDigest/rawSHA/fetched_at, freshness remota unknown, completeness/gaps, Ref exato e scope. Raw não entra em metadata de sessão. |
| Binding de runtime | Campo bounded no runtimeTask/runtimePlan existente ligando snapshot/ref a Request canônico e callbacks atuais. Não é restaurável de texto do modelo. Sem outro Registry/banco. |
| Artefato protegido | PutRequest host-owned com discriminador que gera `evidence_`; ArtifactRef/ResultRef existentes e Kind atual, sem novo esquema de database. Marca persistente de que é proibido reader legado. |
| Admissão/reuso | Gate de fonte/tool/Skill/plan atual; destinatário e worktree do host; ler snapshot registrado, reemitir Ref sob owner do destinatário; generation identifica só esse snapshot. |
| Leitor | Handler comum de artifact_read/range; ID protegido sempre binding+Recover ou erro; projeção worker pelo Access/MCPGrant/Target.Local já existentes, sem tools de scheduler. |
| Entrega | Campo aditivo knowledge_evidence separado de primary_code_evidence; SharedContextBrief bounded contendo só pacote/referências necessários e etiqueta untrusted. |
| Flags/migração | Nova flagoff controla criação/adaptação. O reconhecimento de IDs protegidos para negar fallback é SEMPRE ativo, inclusive após restart/flagoff. Valores zero e IDs legacy preservados; CI e nota de migration obrigatórios. |

## Tool outputs: integração seguinte, sem repetir RTK

O ponto existente é `projectWorkerResult → Projector.Project`, que já preserva o raw antes de compactar. Não refazer execução, gravar um segundo raw por conveniência nem chamar Retrieve com closure que executa o comando. Uma futura adaptação deverá adotar um ArtifactRef host-verificado de saída JÁ concluída, com origem/readonly da operação explicitada. Output de mutação pode ser lido como snapshot histórico, mas jamais substituir uma nova invocação da mutação por um resultado anterior.

O limite1MiB Gateway versus15/16MiB dos resultados exige bypass para os maiores; ampliar limite ou fazer chunking é outro recorte, não fingir que a ref cobre bytes não ingeridos. A SPI ViewOutput/RTK e o Projector/Caveman já existem: testar uma única camada com raw exato recuperável, sem proxy Headroom+Caveman empilhado. Esta proposta não os liga ao App nem remove seus guards.

## Prova fim a fim e gates

1. Fonte/HTTP reais no processo de teste: Context.MCPHandler com Service/Catalog temporários e doc fixture, sem ingestão institucional; Memory stub MCP rotulado synthetic upstream. Router real, LocalStore real e Gateway real. Zero credenciais pessoais e zero inferência.
2. Core real OrchestratorServe/Server: aprovar plano com primary+worker read-only, selecionar source explicitamente, executar fetch primary, reemitir owner do worker, preparar Access e cliente MCP SDK real no endpoint loopback. Demonstrar upstream1call, IDs diferentes, mesmo SHA, worker recupera corpo e range exatos, sem receber scheduler tools. Se o teste não usar processo de worker real, rotular apenas transporte/capability; não dizer cliente/worker certificado.
3. A chamada seguinte VIVA com mesmo query, após alterar/remover doc da fixture, precisa efetuar outra consulta e outro snapshot. O antigo permanece explicitamente histórico ou é revogado conforme a política de retenção, nunca relabelado fresh.
4. Revogar grant, source/credential-lease, task, worktree ou sessão durante fetch/Put/Recover/cópia/último byte: falha fechada sem raw em contexto. Trocar profile no failover muda proveniência. WorkerB/otherSession/otherOrg/otherWorktree não recuperam nem adotam ref. Inputs duplicados/case-alias reaproveitam o parser strict do PR47.
5. Mutators/unknown/hooks/configurados como readOnlyHint não entram no fetch cache. Testar contadores de upstream e side effects zero para requests rejeitados. Cancel leader/waiter, TTL/cache bounds, secret admission, payload sobre limite, JSON/SSE malformado, partial/isError e fim de processo; sem SKIP contado como PASS.
6. Forçar reference_only acima do orçamento e demonstrar recuperação exata pelo cliente primary E worker, não só chamada Go de biblioteca. Verificar que a rota legacy de ArtifactStore não contorna uma ref Gateway revogada. Faults de Store não fabricam evidence/quality success.
7. Flagoff conserva JSON/rotas/approvals/grants e testes existentes. Rodar focused/race/Core wirecompat, fullGo/vet e CI exato. Fixtures aprovadas não são benchmark de sessão nem economia; futuras AB/BA reais devem congelar binários/modelos/condições e preservar resultados adversos.

## O que permanece institucionalmente não comprovado

Memory server real/versionado e seus schemas/completeness/generation/revocation; Context institucional com documento recuperável, ingestão e revogação reais; retenção/reuso permitido por tenant e source; equivalência de clientes oficiais autenticados, TUI, performance/economia e reuso entre processos/restarts. Timestamp/hash de fixture não supre esses contratos. `CONTEXT_SYNC=BLOCKED`/IVOAI-175 continua; nada foi ingerido ou sincronizado por esta investigação.

## Decisões que o root precisa fechar antes de autorizar implementação

- Aceitar a semântica de snapshot retido sob grants atuais do Core, sem promessa de autorização/freshness remota contínua; caso contrário, manter reuso institucional bloqueado até contrato de revalidação.
- Escolher campo aditivo de seleção explícita do plano e campo de resultado separado do legado primary_code_evidence, com migração zero-value/flagoff.
- Incluir recuperação worker/primary estrita na primeira fatia. Captura/ref sem leitor verificável é entrega parcial, não vertical completo.
- Manter primeiro owner/processo e worktree únicos. A captura transparente das leituras do router do frontend em outro processo requer desenho de transferência de autoridade; não está resolvida por compartilhar arquivos em cache.
