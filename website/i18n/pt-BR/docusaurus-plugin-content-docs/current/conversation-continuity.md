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
ivoai session recover <id>
ivoai session handoff <id> --to claude --confirm
```

`show --json` preserva o formato existente de array com um elemento. A lista
textual mostra modo, frontend, primary, estado, atualização e possibilidade de
retomada. Em `ivoai`, abra **Session Control → Conversations — Inspect / Resume**
para inspecionar, retomar, recuperar turno, escolher frontend, confirmar handoff
ou parar a sessão. Não é necessário copiar IDs nativos manualmente.

## `/resume` nativo do Codex

Dentro de `ivoai codex`, o picker oficial permite listar, pesquisar, visualizar,
cancelar e selecionar conversas gerenciadas do projeto atual. A seleção altera o
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

Novas conversas portáveis usam o histórico pertencente ao Codex com um home de
configuração IVOAI isolado. O `sqlite_home` oficial tem precedência sobre
`CODEX_SQLITE_HOME`. Apenas diretórios de conversas e locks nativos são
compartilhados: não se copia banco, transcript, configuração ou autenticação.
Históricos gerenciados legados continuam legíveis no local original, sem migração
silenciosa para outro store. O executor oficial continua dono do login e de seu
histórico. IVOAI guarda mappings
opacos e fingerprint da identidade da conta, não credenciais. Previews do picker
são tráfego transitório, não entradas do journal. Sem prova compatível de conta,
não se reutiliza um mapping não verificado.

Threads paginadas legadas não podem mudar entre bancos Codex independentes só
informando o caminho do rollout: o resume do Codex 0.154.0 continua exigindo o
contexto/índice paginado original. Uma tentativa focada foi recusada pelo App
Server oficial. O resume original permanece disponível; uma troca de modo que
exigiria mover esse estado retorna `NATIVE_SESSION_NOT_PORTABLE`, sem copiar banco
ou inventar outra conversa. Novas conversas no estado compartilhado e adoções do
store nativo elegível não têm essa restrição.

### Adoção explícita e modo do próximo turno

Em **Session Control → Adopt native Codex conversation**, selecione uma das até
100 threads elegíveis do projeto atual e confirme a associação. Equivalente CLI:

```sh
ivoai session native --json
ivoai session adopt <codex-thread-id> --confirm
ivoai session resume <ivoai-session-id>
ivoai session resume <ivoai-session-id> --mode direct --confirm
ivoai session resume <ivoai-session-id> --mode orchestrated --confirm
```

A adoção registra somente identidade, origem do projeto e confirmação. Uma
associação já existente mantém seu ID e modo; uma nova adoção é orquestrada.
O picker `/resume` lista a conversa após adoção. Execute a descoberta no diretório
original; um ID exato pode ser adotado mesmo fora da listagem recente limitada.
Trocar modo altera a admissão dos próximos turnos, não o histórico passado.

| Transição | Classe |
| --- | --- |
| Codex nativo/direto/orquestrado no mesmo store elegível | `SAME_NATIVE`: mesma thread; adoção/troca de modo explícitas quando necessárias |
| Frontend Codex ↔ OpenCode, mantendo primary | `SAME_IVOAI_SESSION`: mesma identidade lógica e mapping verificado |
| Conversa Codex externa → associação IVOAI | `EXPLICIT_ADOPTION`: exatamente uma conversa, sem importação global |
| Provider próprio do OpenCode direto ↔ primary Codex/Claude orquestrado | `EXPLICIT_HANDOFF`: nova conversa nativa com lineage; não igualdade de IDs |

Com checkpoint bounded disponível, use
`ivoai session handoff <id> --to codex --mode orchestrated --frontend opencode --confirm`
ou `ivoai session handoff <id> --to opencode --mode direct --confirm`.
Sem checkpoint, o erro é explícito: não se substitui o brief por transcript copiado.

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
  O modo é preservado salvo escolha explícita de `--mode` no destino. Não copia transcript,
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
