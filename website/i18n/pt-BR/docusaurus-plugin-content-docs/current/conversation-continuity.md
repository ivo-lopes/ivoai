# Continuidade de conversas

## Localizar e reabrir

O ID da sessão IVOAI identifica a conversa lógica. Ele é diferente dos IDs de
frontend, provider, worker e turno. Reiniciar a interface não autoriza criar outro
primary para uma sessão já ativa.

```sh
ivoai session list --json
ivoai session show --json <id>
ivoai session resume <id>
ivoai session resume <id> --frontend opencode
ivoai session resume <id> --knowledge-source voicecorp
ivoai session recover <id>
ivoai session recover <id> --knowledge-source voicecorp
ivoai session handoff <id> --to claude --confirm
```

`show --json` preserva o formato existente de array com um elemento. A lista
textual mostra modo, frontend, primary, estado, atualização e possibilidade de
retomada. Em `ivoai`, abra **Session Control → Conversations — Inspect / Resume**
para inspecionar, retomar, recuperar turno, escolher frontend, confirmar handoff
ou parar a sessão. Não é necessário copiar IDs nativos manualmente.

## Escopo organizacional e histórico preservado

Conversas orquestradas têm uma identidade versionada de escopo organizacional.
Ela inclui o diretório do projeto, as identidades e os endpoints dos servidores
habilitados e as restrições explícitas de `--knowledge-source`. Registros legados
habilitados de Memory/Context também participam. Um compositor sem propósito
selecionado não significa que a conversa tenha escopo organizacional vazio.
A indisponibilidade temporária de um servidor não altera essa identidade;
mudanças nas fontes configuradas ou em seus endpoints podem alterá-la.

Resume, troca de frontend, descoberta no picker nativo e busca de mappings do
provider exigem o mesmo escopo. Informe as restrições originais de
`--knowledge-source` ao retomar ou recuperar uma conversa restrita. O menu de
conversas também aceita esses seletores. Um escopo diferente retorna
`SESSION_KNOWLEDGE_SCOPE_MISMATCH` antes de reabrir a conversa. Registros antigos
sem escopo verificado retornam `SESSION_KNOWLEDGE_SCOPE_UNVERIFIED`; seus metadados
e seu histórico nativo permanecem no local original.

Para preservar uma conversa gerenciada existente sob uma seleção de fontes
revisada, visualize primeiro a proposta de adoção:

```sh
ivoai session adopt-scope <id> --knowledge-source voicecorp
ivoai session adopt-scope <id> --knowledge-source voicecorp --confirm
```

Sem seletor, são usadas as fontes configuradas habilitadas. `--json` apresenta
a mesma prévia ou resultado. **A adoção preserva o histórico nativo existente do
provider, incluindo informações do escopo organizacional anterior.** Ela não
apaga, remove trechos nem separa esse histórico. Inicie outra conversa quando as
informações anteriores não puderem entrar no novo contexto.

A aplicação exige uma conversa orquestrada inativa, um mapping existente da
sessão do provider e um fingerprint de identidade compatível, informado pelo
cliente oficial. Prova de conta ausente, divergente ou indisponível bloqueia a
alteração. A prévia não inicia o provider nem muda o vínculo. O menu oferece a
mesma prévia e exige digitar `ADOPT SCOPE` para aplicar.

Antes de alterar o escopo, IVOAI grava um backup privado e imutável dos metadados
em `sessions/scope-backups/`. O caminho retornado permite visualizar e restaurar
o vínculo anterior:

```sh
ivoai session restore-scope <id> --backup /absolute/path/from-adoption.json
ivoai session restore-scope <id> --backup /absolute/path/from-adoption.json --confirm
```

A restauração verifica localização, tipo de arquivo, permissões, digest,
identidade da sessão e prova da conta atual. Primeiro preserva os metadados
atuais e depois restaura somente o campo de escopo: estado de processos, grants,
mappings do provider e tarefas concluídas não voltam ao estado anterior.
Restaurar um escopo histórico não verificado também restaura sua restrição de
resume. O menu exige digitar `RESTORE SCOPE`. Nenhuma das operações copia
transcripts ou credenciais, reconstrói histórico nativo ausente ou importa uma
conversa não gerenciada do provider.

## `/resume` nativo do Codex

Dentro de `ivoai codex`, o picker oficial permite listar, pesquisar, visualizar,
cancelar e selecionar conversas gerenciadas do projeto e escopo organizacional
atuais. A seleção altera o
vínculo lógico IVOAI; não inicia outro orchestrator. Threads externas ao IVOAI não
são importadas silenciosamente.

O picker abre outra conexão autenticada ao App Server. A façade aceita conexões
de descoberta limitadas, remapeia IDs por cliente e devolve respostas à origem.
Eventos de execução pertencem ao frontend principal. Fechar o picker não encerra
a sessão; uma conexão de descoberta não executa ferramentas nem aprova decisões.

No Codex 0.154.0, overrides de sandbox/approval passados à TUI remota também
impediam selecionar uma conversa. Esses controles permanecem no App Server
IVOAI e nos requests sanitizados, não nos flags de resume da TUI. A remoção da
duplicação no cliente não enfraquece o Prompt Gate nem o sandbox do servidor.

O Codex mantém seu histórico nativo em um home privado e persistente por projeto.
IVOAI não copia configuração pessoal nem autenticação para esse diretório. O
executor oficial continua dono do login e de seu histórico. IVOAI guarda mappings
opacos e fingerprint da identidade da conta, não credenciais. Previews do picker
são tráfego transitório, não entradas do journal. Sem prova compatível de conta,
não se reutiliza um mapping não verificado.

O frontend nativo recebe um `CODEX_HOME` isolado; `SQLITE_HOME` não é repassado
do ambiente do operador. Os diretórios de histórico nativo existentes são
preservados, enquanto descoberta e resume gerenciados verificam o escopo.
Isso não estabelece a causa dos resets históricos na primeira execução:
esse diagnóstico permanece desconhecido.

Cada novo turno usa modelo/effort e routing de conhecimento atuais. Informações
já discutidas continuam na conversa nativa; resume não apaga esse histórico.
Use outra conversa IVOAI quando o contexto organizacional precisar ficar separado.
Sources históricas não recebem novas consultas ou grants automaticamente.

## Resume, recovery e handoff

- **Resume:** reabre a conversa nativa e aguarda novo input; não repete o último
  turno. Codex e Claude diretos usam argumentos oficiais de resume. OpenCode
  gerenciado conecta à sessão nativa. Novos prompts orquestrados passam pelo gate.
- **Troca de frontend:** muda a apresentação, não o provider nem a identidade
  lógica. Os mappings de ambos os frontends são preservados. Pode ser necessária
  uma nova thread de apresentação; isso não migra transcripts. Frontend Codex
  exige primary Codex: se o primary for Claude, faça handoff explícito primeiro.
- **Recover:** reconcilia checkpoint aprovado, identidade/HEAD do repositório,
  tarefas, worktrees e processos. Tarefas concluídas não repetem. As pendentes
  usam modelos, quota, Skills e grants MCP atuais. Exige nova aprovação do plano,
  inclusive quando a execução normal está configurada como immediate.
- **Handoff:** transfere explicitamente um brief limitado para uma nova conversa
  do provider escolhido. `--confirm` autoriza a transferência, separadamente da
  aprovação do plano. A linhagem registra origem, destino e momento da decisão.
  Direct continua direct; orchestrated continua orchestrated. Não copia transcript,
  grant de ferramenta nem credencial.

`AMBIGUOUS_PREVIOUS_EXECUTION` impede repetir uma escrita sem commit coletado e
verificado ou uma operação externa já tentada com resultado incerto. Não repita
deploy, push, release, update ou delete cegamente. Preserve a worktree e inspecione
o resultado antes de reconciliar o trabalho restante. Repositório alterado ou
worker ainda ativo também bloqueiam a recuperação. Commits coletados passam pela
integração isolada existente; conflitos não são resolvidos automaticamente.

## Persistência e privacidade

O registry continua sendo o session store existente. Mappings e linhagem são
aditivos. Checkpoints privados ficam em `sessions/checkpoints/<id>.json`, limitados
a 32 KiB; o brief de handoff tem limite de 8 KiB. São permitidos objetivo conciso,
restrições, aceite, decisões, trabalho concluído/pendente, blockers e próximo passo.
A parte de recovery contém o DAG aprovado pelo host, não prompts expandidos de
workers ou bodies de Skills.

O journal não deve conter transcript completo, chain-of-thought, auth, dump de
ambiente ou resposta bruta de ferramenta. Campos com formato de segredo e controles
de terminal são recusados. Arquivos privados são escritos atomicamente; leitura e
locks recusam symlinks. Grants efêmeros não são restaurados. Locks mantidos pelo
kernel impedem retomadas concorrentes, e PID/start impedem sinalizar PID reutilizado.

O schema v0.10.2 continua legível. Checkpoints antigos no runtime são promovidos sem
resetar configuração. Profiles, auth ownership, MCP, Skills/Ponytail e policies
permanecem. Binários antigos ignoram o diretório aditivo de checkpoints; não oferecem
as operações novas, mas o reapply v0.10.3 recupera acesso ao estado preservado.
A validação versionada do escopo organizacional é aditiva. Leitores antigos ainda
conseguem ler os metadados da sessão; um escopo legado pode exigir adoção explícita
e verificada antes de ser retomado por uma versão que aplica o novo limite.

## Limites e diagnóstico

A descoberta direta Codex/OpenCode é limitada e só registra uma nova sessão nativa
quando inequívoca. Se várias conversas foram criadas simultaneamente ou nenhum ID
foi observado, o mapping fica indisponível; não se escolhe a mais recente por palpite.
Claude live depende da autenticação oficial existente, sem remover seu suporte.

Históricos apagados pelo home efêmero da façade v0.10.2 não podem ser reconstruídos
por metadata. Nesses casos é necessário um checkpoint seguro para handoff; não há
resume nativo fabricado. Planos nunca aprovados ou sem checkpoint válido também
não são recuperados automaticamente.

O erro antigo `Failed to start TUI session picker: failed to connect to remote app
server` era causado pela recusa HTTP 409 da segunda conexão do picker. Atualize
IVOAI e reinicie o frontend gerenciado; não desabilite `/resume` nem use direct como
correção. Se a sessão já estiver ativa, feche ou pare seu frontend antes de retomar.
Não apague arquivos de lock ou de sessão.

Esta release não implementa OpenCode Full Console, OpenViking ou a próxima geração
de orquestração paralela.
