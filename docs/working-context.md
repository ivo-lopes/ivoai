# WorkingContext transitório

WorkingContext é a camada transitória de evidências de uma execução gerenciada pelo
IVOAI. Ela preserva integralmente a saída de um worker sem injetar automaticamente
essa saída no contexto do primary.

```text
worker Codex/Claude
  -> saída bruta exata
  -> ArtifactStore privado
  -> ResultRef opaca
  -> WorkerResult bounded
       +-- summary
       +-- findings
       +-- StateDelta proposto
       `-- ResultRefs
  -> primary
```

As autoridades não se misturam:

- ai-memory continua sendo a memória operacional durável compartilhada;
- ContextBackend continua sendo conhecimento/RAG persistente e read-only para agentes;
- WorkingContext contém evidência e estado transitório da sessão corrente;
- ArtifactStore guarda bytes exatos temporariamente;
- o primary continua sendo o único writer e consolidador autoritativo;
- WorkerResult e StateDelta são dados não confiáveis e apenas consultivos.

## Contratos e limites

`ArtifactRef` é uma referência opaca aleatória. Ela contém tipo, tamanho, SHA-256,
media type normalizado, criação, expiração, ownership de sessão/task/worker,
sensitivity e estado complete/truncated. Não contém path público nem conteúdo.
`ResultRef` associa a referência ao papel da evidência. `WorkerResult` contém status,
summary bounded, findings bounded, referências, erros importantes e `StateDelta`
tipado. Evidência longa nunca fica inline.

O output exato é persistido antes da projeção estruturada. Falhas de worker, testes,
build, segurança e cancelamento permanecem explícitas no resultado bounded; o detalhe
completo fica recuperável pela referência. Conteúdo `secret`, `credential` ou
`raw_auth` não é aceito no store comum. `internal` e `restricted` são privados e nunca
entram em observabilidade como body.

Cada projeção recebe uma classe provider-neutral: `compressible`,
`exact_required`, `bypass` ou `unsupported`. A classificação é determinística e
fail-safe. Respostas de Memory/Context, metadata do Skill Registry, evidência de
segurança, erros, stack traces, falhas de testes/build e payloads que influenciam
policy ou autoridade são `exact_required`; tipos desconhecidos também preferem
fidelidade. Falha ou cancelamento sempre sobrepõe uma sugestão compressible.

Um `association_id` bounded opcional liga ResultRef à chamada/resultado que
originou a evidência. Chamadas distintas preservam refs distintas e nunca são
mescladas implicitamente.

## ArtifactStore local

O store reside em `$XDG_CACHE_HOME/ivoai/working-context` (ou o cache XDG equivalente).
Diretórios usam `0700`; payload e metadata usam `0600`. IDs não derivam de input do
worker. Escritas usam staging privado, fsync e rename atômico. Leituras recusam
symlinks, validam containment, ownership, TTL, tamanho e SHA-256.

Os defaults atuais limitam cada artefato a 16 MiB, cada sessão a 64 MiB/256 objetos e
o store global a 256 MiB/2048 objetos. O TTL padrão é 24 horas e o máximo é sete dias.
GC é explícito, determinístico, não usa daemon e remove somente objetos expirados
dentro do root gerenciado; entradas corrompidas ou desconhecidas são preservadas e
reportadas, não apagadas por suposição.

## Context budget e recuperação

O storage budget preserva evidência; o context budget limita apenas a projeção
automática enviada ao primary. Os budgets atuais são 4 KiB (LIGHT), 8 KiB
(BALANCED), 12 KiB (STRONG) e 16 KiB (MAX). O primary pode usar as ferramentas locais
read-only `orchestration_artifact_read` e `orchestration_artifact_read_range` para
recuperar conteúdo exato ou um range de até 1 MiB. A leitura revalida sessão,
integridade, TTL e limites; uma referência nunca vira arbitrary file read.

Sessão, checkpoint, SharedContextBrief, handoff e instruções automáticas persistem ou
transportam somente metadata bounded e ResultRefs. Na troca Codex/Claude, o novo
primary reidrata as mesmas referências sem duplicar o output bruto.

Se o store estiver indisponível, o IVOAI retorna WorkingContext `degraded` e não usa o
output bruto como fallback para o prompt. Isso pode perder a evidência exata daquela
execução, mas nunca provoca uma injeção silenciosa e ilimitada.

WorkingContext não implementa scheduling nem substitui a evidência por compressão.
Quando Caveman é selecionado, o componente managed `caveman-mcp` pode projetar
representações menores somente depois do ArtifactStore e sem alterar
ResultRef/WorkerResult como fonte da evidência exata. O helper é o asset local
stdio BSL-1.1 `bin-v1.1.3`, pinado na mesma revisão do runtime; não usa `npx`, não
é registrado como MCP do primary e opera com recovery store efêmero privado.
Falha, timeout, resposta malformed/oversized ou indisponibilidade do helper retorna
ao projector determinístico bounded; output bruto grande nunca vira fallback de
prompt. JSON, logs, code, diffs, search results e texto são tipos compressible;
exact-required e bypass nunca chamam o helper.

Storage budget, context budget e compressão são controles separados. Caveman não
altera o limite de 16 MiB por artefato, isolamento de ownership, TTL/GC nem o range
máximo de leitura de 1 MiB.

Um futuro orquestrador pode consumir os mesmos contratos sem mudar a autoridade do
primary.
