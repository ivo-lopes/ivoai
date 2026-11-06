# Contrato de compatibilidade de produção

Este contrato se aplica a todas as releases estáveis do IVOAI a partir da `v0.5.0`.
Seu objetivo é manter as instalações existentes de cliente e servidor atualizáveis
no próprio local. Um host limpo, estado XDG excluído ou login repetido em provider
não são estratégias de migração aceitáveis.

## Garantias

1. A `v0.5.0` possui um caminho in-place compatível até a próxima release estável.
2. Toda release estável `N` possui um caminho testado até a estável `N+1`.
3. Quando viável, a release atual aceita o estado da `v0.5.0` e aplica em ordem todas
   as migrações intermediárias.
4. Novas funcionalidades não exigem excluir as raízes de configuração, dados, estado
   ou cache do IVOAI.
5. A autenticação do Codex e do Claude continua pertencendo aos clientes oficiais e
   nunca é migrada, copiada nem limpa por uma atualização do IVOAI.
6. Dados gerenciados pelo IVOAI e metadados dos componentes são migrados; arquivos
   não gerenciados e instalações de terceiros são preservados.
7. Uma atualização interrompida deixa um journal privado e é recuperada antes do
   início de uma atualização posterior.
8. Etapas concluídas nunca são repetidas silenciosamente. As etapas são ordenadas,
   validadas e idempotentes ou protegidas pelo estado da transação.
9. Cada etapa de schema declara origem, destino, precondition, aplicação, validação e
   comportamento de rollback.
10. Um candidato sem um caminho reversível explícito é rejeitado antes que qualquer
    arquivo gerenciado seja alterado.
11. Uma falha em migração, setup ou Doctor pós-atualização restaura o binário anterior
    e o snapshot exato e compatível dos arquivos gerenciados.
12. O rollback é idempotente e seguido pelo Doctor adequado ao modo cliente ou servidor.

## Transação

`ivoai update` executa estas fases:

```text
PREPARE
  release/checksum/archive validation
  candidate version + compatibility probe
  source/target schema validation
  path, permission, size and free-space preflight
  private exact-file snapshot
PROMOTE
  atomic promotion of the recovery-capable target binary
MIGRATE
  target candidate runs its ordered migration registry
SETUP
  target-owned managed component and runtime reconciliation
VERIFY
  version/load/setup/Doctor validation
COMMIT
  committed journal and bounded snapshot retention
```

Qualquer falha antes do commit entra em `rolling_back`, verifica todos os digests do
snapshot, restaura atomicamente arquivos e modos, remove arquivos opcionais que não
existiam antes da atualização, restaura o executável e registra `rolled_back`.

O binário de destino é promovido antes da migração para que a recuperação de uma
interrupção seja sempre executada por código que compreende o journal de destino. O
binário antigo já está presente no snapshot exato e no slot legado de rollback.
Qualquer falha antes do commit o restaura.

`ivoai update --dry-run` executa download da release, verificação de checksum,
sondagem de compatibilidade entre candidato/schemas e preflight somente leitura de
caminho, permissão, tamanho e espaço livre. Ele prepara e executa o candidato com
checksum verificado para sondagens limitadas de `version`/metadados, mas não executa
etapas de migração, não cria um journal de transação nem confirma alterações do estado
gerenciado. Trate-o com a mesma decisão de confiança no canal de releases de uma
atualização. `ivoai update --rollback` restaura o checkpoint atual de rollback. Ele
se recusa a sobrescrever arquivos gerenciados alterados depois do commit, a menos que
o operador forneça explicitamente `--force`. O binário legado `ivoai.previous`
continua compatível com uma atualização originada na v0.5.0.

## Limite dos dados sob ownership

A allowlist do snapshot contém o executável do IVOAI, configuração principal, estado,
manifesto de ownership, arquivos correspondentes dos componentes gerenciados dentro
da raiz de dados do IVOAI e assets explícitos de configuração/serviço do servidor.
Ela nunca inclui um diretório home inteiro, `~/.codex`, configuração/autenticação do
Claude, cookies, tokens OAuth de providers, ambiente bruto ou caminhos de componentes
gerenciados externamente.

A raiz da atualização e os diretórios de snapshot possuem modo `0700`; journals e
snapshots de arquivos regulares possuem modo `0600`. Os caminhos devem ser absolutos,
estar contidos na raiz gerenciada declarada e passar pelas verificações de arquivo
regular sem symlink. Cada snapshot possui metadados de tamanho, modo, proprietário,
grupo e SHA-256. O snapshot agregado é limitado por default a 1 GiB, o espaço livre é
verificado com uma margem reservada e o checkpoint atual é o único destino de
rollback. Armazenamentos duráveis ativos do servidor (Qdrant, memory, enrollment e
OAuth Web) não são copiados indiscriminadamente durante uma atualização. Uma futura
migração de schema para um desses armazenamentos deve adicionar um participante
transacional explícito e em quiescência, ou ser rejeitada por não ser segura para rollback.

## Schema e campos desconhecidos

Os schemas de configuração, estado e ownership possuem versões independentes e
permanecem no schema 1. O armazenamento privado de segredos do cliente possui versão
independente no schema 2: a credencial singleton da v0.5.0 migra reversivelmente para
a entrada opaca `srv_legacy_default`, preservando o espelho legado `server` para
rollback. Ele participa do mesmo snapshot privado da atualização e não exige novo
enrollment. Alterações futuras continuarão usando etapas reversíveis e ordenadas no
único registro de migrações pertencente ao destino.

O updater publicado na v0.5.0 é anterior ao protocolo de journal: ele valida e promove
o candidato e então invoca um `ivoai setup` simples. Esta base mantém esse caminho de
entrada compatível, detecta automaticamente um marcador existente de servidor e pode
usar o binário legado de rollback da v0.5.0. As bridges de cliente e servidor,
incluindo a etapa do armazenamento de segredos, são exercitadas hermeticamente.
Qualquer release futura que aumente um schema e ainda aceite um salto direto da
v0.5.0 deve preservar e testar uma bridge transacional de entrada legada; metadados
compreendidos somente pelo binário antigo não podem proteger retroativamente essa
primeira promoção.

As gravações TOML usam uma projeção tipada mesclada ao documento bruto existente.
Tabelas e campos desconhecidos sobrevivem aos ciclos de leitura e escrita. Mapas
dinâmicos, como servidores MCP e ownership de componentes, são autoritativos para
entradas conhecidas; portanto, uma remoção explícita não ressuscita uma entrada
antiga. O carregamento de um schema futuro incompatível falha de modo seguro.

## Mudanças progressivas de arquitetura

Futuras mudanças grandes seguem coexistência, validação disabled/shadow/canary,
promoção a default, uma release de observação e somente então remoção do legado. Esta
base mantém aditivas as alterações do caminho independente Direct do OpenCode, do
frontend gerenciado do OpenCode no AUTO e do Caveman como default; ela não adiciona
OpenViking nem NativeOrchestrator e não remove Headroom/Ruflo. O default do Caveman
usa o schema de configuração 1: valores legados de provider ausentes migram
semanticamente, valores explícitos de Direct/Headroom/Caveman permanecem inalterados,
e o snapshot da transação fornece rollback para atualizações modernas. A bridge
publicada na v0.5.0 permanece funcional porque seu binário ignora a tabela aditiva de
compressão e restaura seu próprio comportamento do Headroom.

## Cliente e servidor em um mesmo host

A unidade de atualização compatível atualmente é um modo de instalação e um journal
de transação. Um único executável gerenciado que possua simultaneamente o estado XDG
do cliente e o estado do servidor não é hoje uma topologia de atualização oficialmente
compatível: journals ambíguos de cliente+servidor falham de modo seguro, em vez de
adivinhar qual conjunto de rollback é autoritativo. Para tornar essa topologia
compatível no futuro, será necessário um modo ou seleção de transação explícito e uma
matriz que cubra ambos os domínios de rollback. Atualmente, não há evidência dos dois
inventários de produção que justifique ampliar esse escopo.

## Pendência da cadeia de suprimentos

A autenticidade da atualização depende atualmente do TLS do GitHub e de checksums
publicados pelo mesmo canal de release. Isso detecta corrupção, mas não constitui uma
assinatura independente do publicador. Attestations de artefatos, proveniência de
release assinada e uma política de verificação continuam sendo trabalhos futuros
explícitos; commits e tags publicados não são reescritos por esta base.
