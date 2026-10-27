# Evidence Gateway experimental

Estado: implementação inicial, desabilitada por padrão, para IVOAI-199/148/192.
Esta é uma camada transitória de recuperação dentro do backend Go existente.
Ela não substitui scheduler, Memory, Context, provedores de código ou ArtifactStore.
A migração e a certificação completas continuam pendentes.

## Habilitação e rollback

Defina `IVOAI_EXPERIMENTAL_EVIDENCE_GATEWAY=1` apenas no processo host IVOAI para
um experimento isolado. Outros valores mantêm a implementação anterior de prefetch.
Remova a variável para reverter. Não há alteração de configuração pessoal dos
clientes, pin de componente, schema persistido ou armazenamento de autenticação.
A flag não é recomendação de ativação em produção: a comparação econômica real
inicial falhou nos critérios de qualidade e economia.

O primeiro adaptador atende prefetch de código já admitido para primary e workers.
A classificação de intenção e a seleção entre leitura direta e Graft continuam
responsáveis pela escolha. Esta entrega não implementa adaptadores Memory/Context/
saídas de ferramentas, filtros de compressão ou suficiência geral entre fontes.
Não há consultas obrigatórias a todas as fontes ou novo endpoint MCP público.

## Recuperação e isolamento

`internal/evidence` vincula cada pedido à sessão e a digests derivados pelo host
para organização, repositório, worktree física e permissões. O adaptador de código
vincula o escopo de conhecimento e a raiz canônica do sistema de arquivos; por
precaução, raízes de trabalho diferentes representam repositórios diferentes.
Consultas normalizadas equivalentes compartilham recuperação somente no mesmo
escopo, geração da fonte e orçamento. Callbacks do host repetem autorização e
verificação da geração antes de servir cache e depois da recuperação. Snapshots
detectam edições, adições, exclusões e renomeações; mudanças em grants ou Skills
invalidam a admissão.

O cache em memória é local ao processo, limitado a 64 entradas / 4 MiB / cinco
minutos, com até oito recuperações distintas ativas. Não é um cache entre processos.
Pedidos simultâneos equivalentes aguardam a mesma recuperação e revalidam a
admissão individualmente. Cancelamento é limitado; uma recuperação que falhe ou
gere panic libera sua vaga. Pedidos mutáveis são recusados antes do callback.

Cada destinatário recebe um artefato exato separado, com a ownership existente de
tarefa/worker. Compartilhar recuperação não concede a um worker o artefato do
primary ou a autoridade de outro worker. O registro ativo do worker deve coincidir
com a tarefa e a worktree própria; subdiretórios válidos são aceitos sem escapes.
A admissão de leitura do primary preserva a política existente de plano imediato
e recusa decisão explicitamente pendente/rejeitada. Ela não cria aprovações.

O conteúdo bruto é validado antes de persistir: classes de sensibilidade proibidas,
padrões reconhecidos de credenciais, metadados de mídia inseguros, UTF-8 inválido,
NUL e payloads excessivos são recusados. A detecção de padrões é defesa adicional,
não prova de que qualquer texto de código esteja livre de segredos. A política
de origem do host e as exclusões do inventário existente continuam necessárias.

## Fidelidade e proveniência

A biblioteca registra `extracted` versus `inferred`, a geração da fonte, a
referência exata com SHA-256 e a completude da fonte recuperada. Referências são
seladas para impedir que um artefato seja reclassificado em outro escopo. Conteúdo
é sempre dado não confiável e nunca altera instruções ou grants de execução.

Quando o payload excede o orçamento de contexto, o pacote genérico contém apenas
sua referência; não corta diff, stack trace ou resultado autoritativo. Os bytes
exatos permanecem no ArtifactStore. Um excerpt armazenado é recuperável exatamente
como excerpt: sua referência não recupera bytes de origem nunca buscados.
`source_incomplete` e `inference_requires_verification` são lacunas explícitas.
`requires_task_validation` é o estado inicial de suficiência; fonte completa não
comprova conclusão da tarefa.

O adaptador de código preserva o envelope existente de texto/artefato/fidelidade/
untrusted. Metadados internos de cache e proveniência ficam fora do contexto do
modelo; a reutilização aparece no evento existente, apenas de metadados,
`capability.resolve`, com `capability=evidence.code`. ArtifactRef indica truncamento.

A API selada `Recover` da biblioteca repete verificações atuais de escopo, geração
e admissão. Ela ainda não é um novo protocolo para clientes. As ferramentas de
artefatos mantêm sua política existente de sessão/ownership e não estão certificadas
como essa API mais restrita. Esta entrega não comprova recuperação geral entre
organizações ou compartilhamento Memory/Context. Integrar uma política comum de
recuperação em todos os clientes e transportes administrativos continua pendente.

## Limites de validação e desempenho

Testes cobrem separação de escopos, revogação de grants, geração do conteúdo,
cancelamento, deduplicação concorrente, TTL, limites de fila/cache, admissão de
segredos, recuperação exata, adulteração de referências, reutilização entre primary
e worker, modo imediato e worktree Git real com subdiretório de monorepo. Eles não
comprovam paridade real dos clientes/workers.

A comparação inicial de prefetch frio registrou baseline correto em 9/9 tentativas
e candidato em 6/9. Os seis pares corretos aumentaram entrada em 5,84% e duração
em 7,90%. Todas as falhas foram preservadas nas evidências. Um diagnóstico
identificou divergência no campo de símbolo do oráculo sem armazenar respostas.
Execuções e ajustes seguintes devem continuar em datasets separados; nenhuma
amostra negativa pode ser substituída por uma repetição favorável.

As verificações de geração ainda fazem snapshots repetidos do inventário autorizado.
Isso pode ser caro em repositórios grandes. O benchmark sintético pequeno não
quantifica esse overhead no limite de 128 MiB, reutilização entre turnos ou múltiplos
workers reais. Mantenha a flag desligada até comprovar qualidade objetiva, segurança
e economia em cobertura maior. Consulte o
[registro de validação](https://github.com/ivo-lopes/ivoai/blob/feat/backend-first-evidence/release-notes/backend-first-validation.md) e a
[auditoria de migração](backend-first-migration.md).
