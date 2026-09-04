# Fontes de conhecimento em múltiplos servidores

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

Sem a flag, todos os profiles conectados e habilitados participam automaticamente da
federação de leitura limitada. Um novo profile inscrito e habilitado será incluído
nas futuras sessões sem filtro, sem alterar um alias especial `default`:

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
a mesma seleção durante o failover entre Codex/Claude, e workers consultivos herdam os
endpoints locais, não os tokens upstream.

No frontend gerenciado do OpenCode, o status compacto e o painel `/ivoai` mostram as
quantidades configuradas, conectadas e selecionadas para a sessão a partir do mesmo
snapshot do ServerPool usado pelo roteador. Sessões automáticas marcam todas as fontes
habilitadas como selecionadas; sessões restritas diferenciam fontes selecionadas e
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
