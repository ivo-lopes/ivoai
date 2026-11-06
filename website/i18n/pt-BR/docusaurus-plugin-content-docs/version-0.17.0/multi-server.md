# Fontes de conhecimento em múltiplos servidores

## Estados de diagnóstico

Gerencie os mesmos profiles em **ivoai → Connections → IVOAI Servers**.
Adicione, teste, habilite/desabilite, edite, faça re-enrollment e remova seletivamente
pela TUI. Profiles desabilitados preservam credenciais. Veja [Conexões](connections.md).


`ivoai status`, `ivoai doctor` e `ivoai memory status` priorizam os mesmos profiles
do runtime. Probes MCP autenticados de leitura são separados da seleção de destino
de hooks/escrita. Dois purposes distintos podem ter leituras saudáveis enquanto os
hooks informam `ambiguous_no_write`; HTTP 409 por destino ambíguo não significa
Memory offline. Selecione exatamente um purpose para escrita. Hooks nunca fazem
fan-out; a migração não reenvia nem exclui eventos no spool.

List/show não executam probes: o objeto de saúde informa `probed=false` e `state`,
`memory_state`, `context_state` como `not_probed`. Os booleanos existentes permanecem
por compatibilidade; false sem probe não significa indisponibilidade. Use `connect
server test` para um probe atual. Os estados de leitura distinguem `healthy`,
`not_configured`, `not_probed`, `auth_error`, `transport_error`, `protocol_error` e
`upstream_error`; agregados podem ser `degraded`. Context vazio válido não é falha.
`status --json` usa o envelope do relatório Doctor, incluindo estados por profile e
`hook_destination` agregado, sem credenciais.

Um cliente ivoai pode manter múltiplos servidores ivoai independentes inscritos ao
mesmo tempo. Cada `ServerProfile` possui um ID estável e opaco, um alias legível,
purpose, grupo de redundância opcional, prioridade, endpoints de descoberta e
metadados limitados de funcionalidades. Sua credencial com escopo é armazenada
separadamente sob o ID opaco; tokens nunca aparecem em TOML, status, Doctor, estado
da sessão ou configuração do agente.

Purpose e redundância têm significados diferentes:

- purposes diferentes são domínios de conhecimento independentes e nunca fazem
  fan-out nem recebem implicitamente as escritas uns dos outros;
- membros de um grupo de redundância representam fontes equivalentes para um mesmo
  purpose. Números de prioridade mais baixos são tentados primeiro nas leituras, com
  failover limitado por integridade/circuito. As escritas ocorrem somente no primary
  e nunca são repetidas automaticamente depois de uma falha incerta.

## Exemplo com Voicecorp e Mindsite

Use aliases sintéticos, suas próprias origens HTTPS e códigos de enrollment únicos:

```sh
printf '%s\n' "$VOICECORP_ENROLLMENT_CODE" | \
  ivoai connect server add voicecorp \
    --url https://voicecorp.example.invalid --purpose voicecorp --code-stdin

printf '%s\n' "$MINDSITE_ENROLLMENT_CODE" | \
  ivoai connect server add mindsite \
    --url https://mindsite.example.invalid --purpose mindsite --code-stdin

ivoai connect server list
ivoai connect server show mindsite
ivoai connect server test mindsite
ivoai doctor
```

Selecione uma fonte sem desconectar a outra:

```sh
ivoai codex --knowledge-source mindsite
ivoai claude --knowledge-source voicecorp
ivoai auto --planner codex --knowledge-source mindsite
ivoai session start --executor claude --mode orchestrated \
  --knowledge-source voicecorp
```

Sessões diretas sem flag preservam a federação all-enabled. AUTO agora usa
`purpose-auto`: seleciona aliases/purposes relevantes no prompt admitido antes de
qualquer lookup institucional. Sem purpose relevante, não há consulta institucional.
`--knowledge-source` permanece autoritativo. Configure
`orchestration.auto.knowledge_routing=all-enabled` para o comportamento AUTO anterior,
ou `explicit-only` para exigir seleção. A TUI de orquestração expõe a mesma policy:

```sh
ivoai auto
```

Forneça a flag para restringir a sessão. Repita-a ou use um valor separado por
vírgulas para selecionar um subconjunto exato:

```sh
ivoai codex \
  --knowledge-source mindsite \
  --knowledge-source voicecorp
```

Leituras federadas por `tools/call` são executadas simultaneamente, com deadlines
individuais e um resultado agregado limitado. Cada entrada preserva `source_id`,
alias, purpose e metadados de redundância; caminhos de documentos idênticos em fontes
diferentes permanecem distintos. Um timeout parcial ou uma fonte malformada fica
visível em vez de ser informado como sucesso total. Uma escrita entre múltiplos
purposes, ou entre dois destinos independentes com o mesmo purpose, falha
explicitamente.

Uma fonte indisponível no modo automático é informada como resultado parcial/
degradado; fontes saudáveis ainda retornam. Uma fonte indisponível selecionada
explicitamente falha na seleção em vez de substituir silenciosamente outro purpose.
A federação automática tem semântica somente leitura: uma nova escrita em Memory com
múltiplos destinos possíveis falha com `WRITE_DESTINATION=AMBIGUOUS`, em vez de ser
transmitida a todos.

A desconexão é seletiva:

```sh
ivoai disconnect server mindsite
ivoai connect server list       # voicecorp remains
ivoai disconnect server --all   # explicit bulk operation
```

## Isolamento da sessão

Cada sessão selecionada recebe um roteador privado em loopback em `127.0.0.1` e uma
capability local aleatória e de curta duração. Codex e Claude veem apenas os endpoints
`ivoai-memory` e `ivoai-context` locais ao processo e permitidos para essa sessão. O
roteador mantém as credenciais upstream em memória e anexa cada token somente ao ID
opaco de servidor correspondente. Ele rejeita redirects entre origens, limita
requisições a 4 MiB e respostas a 16 MiB, revoga a capability local ao encerrar e
nunca reescreve uma configuração MCP global do agente para trocar de organização.

Os hooks de ciclo de vida do ai-memory usam o mesmo roteador local à sessão. Portanto,
sessões simultâneas da Voicecorp e da Mindsite permanecem independentes. O AUTO mantém
a mesma seleção admitida no turno durante o failover entre Codex/Claude. Workers
recebem apenas seu subset aprovado e capabilities process-local, não todas as
sources nem tokens upstream.

No frontend gerenciado do OpenCode, o status compacto e o painel `/ivoai` mostram as
quantidades configuradas, conectadas e selecionadas para a sessão a partir do mesmo
snapshot do ServerPool usado pelo roteador. AUTO marca sources admitidas no prompt
como selecionadas; sessões restritas diferenciam fontes selecionadas e
excluídas. Uma fonte sem integridade é marcada tanto por um símbolo quanto por texto,
e uma sessão automática pode continuar em um estado degradado visível. Uma fonte
indisponível selecionada explicitamente falha antes de o frontend iniciar; assim, o
painel nunca sugere que outro purpose a substituiu.

O roteador preserva respostas MCP autoritativas de uma única fonte. Resultados
federados adicionam o envelope de origem necessário para atribuição. WorkingContext,
ArtifactStore e as regras de fidelidade exact-required permanecem independentes e
inalterados.

## Exemplo de redundância

```sh
ivoai connect server add mindsite-1 --url https://one.example.invalid \
  --purpose mindsite --redundancy-group mindsite-production --priority 10 \
  --code-stdin
ivoai connect server add mindsite-2 --url https://two.example.invalid \
  --purpose mindsite --redundancy-group mindsite-production --priority 20 \
  --code-stdin
ivoai codex --knowledge-source mindsite
```

Esse é um failover de leitura primary/standby determinístico, não quorum nem
replicação. Nenhum failover cruza um limite de purpose, nenhuma falha de profile
remove outro profile e uma escrita com falha não é repetida silenciosamente.

## Compatibilidade legada

A configuração publicada para um único servidor continua legível. Ela é normalizada
para o profile estável `default`, e a credencial antiga torna-se a entrada de
`srv_legacy_default`; nenhum novo enrollment é necessário. Configuração, estado e
ownership permanecem no schema 1. O armazenamento privado de segredos possui versão
independente no schema 2, mantém o espelho legado `server` para rollback somente em
`default`, participa do snapshot da transação de atualização e possui uma migração
reversível 1→2.
