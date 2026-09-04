# Controle e orquestração de sessões

O ivoai possui modos de sessão direto, orquestrado explícito e automático. Nenhum
deles adiciona uma credencial de provider pay-as-you-go, nem substitui a interface
oficial do Codex ou do Claude Code.

## Modo direto

Os comandos estabelecidos continuam sendo o caminho mais curto:

```sh
ivoai codex
ivoai claude
```

Eles transferem o diretório atual, stdin, stdout, stderr, grupo de processos do
terminal em foreground e sinais para o cliente oficial selecionado. O runtime cria
snapshots dos modos do terminal e os restaura ao retornar. O Ruflo não é inicializado.
O Headroom só é usado após sua sondagem de compatibilidade ser bem-sucedida, com o
fallback seguro e já existente para início direto.

Para usar o mesmo runtime com observabilidade de ciclo de vida, inicie uma sessão direta:

```sh
ivoai session start --executor codex --mode direct
ivoai session start --executor claude --mode direct
```

O control plane registra metadados não sensíveis, como ID da sessão, PID, executor,
proveniência do modelo, uso do Headroom, estado do serviço e código de saída. Ele não
registra prompts nem respostas.

## Modo orquestrado

Use esse modo explicitamente em trabalhos que se beneficiam de delegação limitada:

```sh
ivoai session start --executor codex --mode orchestrated
# or
ivoai session start --executor claude --mode orchestrated
```

Antes de abrir o cliente primary, o ivoai verifica a versão instalada e o profile
seguro do Ruflo, confirma que a execução pelo provider e a memória durável do Ruflo
estão desabilitadas, executa um `swarm init` real, obtém e verifica seu Swarm ID e
registra uma tarefa opaca do ciclo de vida do primary. Um gate com falha interrompe a
inicialização com status diferente de zero; não existe fallback silencioso para o
modo direto, e a interface nunca identifica essa inicialização como orquestrada.

O primary oficial recebe um único MCP stdio local à sessão chamado
`ivoai-orchestrator`. Ele é injetado por uma substituição de configuração do Codex com
escopo de processo ou por um arquivo MCP privado e temporário do Claude. Ele não é
adicionado ao gateway remoto e é removido quando o diretório de runtime da sessão é
excluído.

A bridge oferece:

- `orchestration_status` — status seguro da sessão e do swarm;
- `orchestration_agents` — metadados do primary e dos workers;
- `orchestration_delegate` — delegação limitada a um worker oficial do Codex ou Claude;
- `orchestration_result` — um resultado estruturado e limitado do worker;
- `orchestration_artifact_read` — recuperação explícita de evidência exata por ref opaca;
- `orchestration_artifact_read_range` — recuperação explícita de um intervalo limitado;
- `orchestration_cancel` — cancelamento de um worker pertencente à sessão.

Em sessões automáticas, ela também oferece `orchestration_quota` somente leitura e,
quando habilitado, `orchestration_checkpoint`, além do scheduler automático validado:

- `orchestration_bootstrap`, `orchestration_capabilities` e `orchestration_plan`;
- `orchestration_spawn` e `orchestration_spawn_batch` assíncronos;
- liberação de dependências com `orchestration_primary_complete`;
- `orchestration_wait` limitado e baseado em notificações;
- `orchestration_escalate` com um nível por vez.

O gerenciador de cotas e o roteador de capabilities, não o modelo, possuem autoridade
final sobre provider, modelo verificado em runtime, esforço e se a delegação é econômica.

## Modo automático

```sh
ivoai auto
ivoai auto --planner codex
ivoai auto --planner claude
```

O modo automático usa a TUI do OpenCode na versão fixada como frontend interativo,
enquanto o IVOAI permanece como planner, proprietário lógico da sessão, writer
autoritativo e consolidador de resultados. O IVOAI invoca a CLI oficial selecionada
do Codex ou Claude Code por trás de uma bridge privada em loopback, usando o login de
assinatura existente em cada cliente sem copiar credenciais para o OpenCode. Ele
verifica ambos os clientes de assinatura antes de iniciar o Ruflo, usa automaticamente
a alternativa quando o provider solicitado possui um limite rígido confirmado,
atualiza a cota antes e depois do trabalho de um worker e monitora o primary ativo. Um
limite rígido no meio da sessão interrompe apenas o grupo de processos correspondente,
preserva a working tree e inicia a alternativa com um checkpoint limitado mais um
resumo de Git status/diff-stat. São aceitos no máximo dois failovers automáticos
consecutivos; um checkpoint bem-sucedido zera esse contador.

O frontend OpenCode permanece conectado durante o failover limitado entre executores.
O Codex recebe instruções da sessão por uma substituição oficial de configuração com
escopo de processo. O Claude as recebe por `--append-system-prompt-file` e um arquivo
`--settings` privado e exclusivo da sessão, que captura telemetria estruturada da
statusline. Nenhuma configuração permanente de terceiros é sobrescrita. Os detalhes
estão em [auto-orchestration.md](auto-orchestration.md).

Na primeira solicitação significativa, Memory e Context são tentados uma vez cada, e
um SharedContextBrief limitado é compartilhado com os workers. O IvoAI valida o DAG
de tarefas, calcula níveis ponderados de capability, mantém no primary os trabalhos
antieconômicos e inicia simultaneamente workers consultivos independentes. Os workers
são estruturalmente somente leitura; o primary continua sendo o único writer. O texto
integral da tarefa e o corpo dos resultados nunca entram no JSON da sessão; apenas
ResultRefs limitadas do WorkingContext entram. Detalhes de pontuação e roteamento estão
em [auto-scheduler.md](auto-scheduler.md).

Essas instruções da sessão impõem a mesma prioridade de pesquisa do modo direto:
primeiro `ivoai-memory`, depois `ivoai-context`, e fontes Web/externas somente depois
que os dois estágios internos tiverem sido tentados. Os adapters dos workers também
recebem a política pelas flags oficiais de instrução com escopo de processo do Codex
e do Claude.

As tarefas delegadas e os corpos dos resultados nunca entram no Ruflo nem no JSON da
sessão. O Ruflo recebe somente IDs opacos de sessão/worker por comandos de ciclo de
vida independentes de provider. O adapter do worker usa
`codex exec --json --output-last-message` ou `claude --print --output-format json`,
selecionado a partir de caminhos confiáveis dos componentes. Variáveis de ambiente
com chaves de provider são removidas dos workers; a autenticação de assinatura
permanece dentro de cada cliente oficial. As instruções de pesquisa não adicionam
credenciais de provider nem roteiam inferência pelo Ruflo.

A saída do worker não é confiável. O IvoAI primeiro persiste seus bytes exatos no
ArtifactStore transitório privado e depois projeta um WorkerResult limitado e
independente de provider. Um StateDelta é apenas uma observação proposta; ele não pode
conceder capability, alterar política, desabilitar um sandbox nem aplicar mutações à
working tree. Consulte [WorkingContext](working-context.md).

## Funções dos componentes

- **ivoai:** ciclo de vida da sessão, preflight seguro, identidade do processo,
  adapter de worker, observabilidade e limpeza.
- **Codex / Claude Code:** toda a inferência, raciocínio, ferramentas e interação com
  o usuário.
- **Ruflo:** somente topologia efêmera do swarm e coordenação opaca de ciclo de vida.
- **Headroom:** wrapper opcional para processos primary e worker; a telemetria registra
  se ele realmente foi usado. Ele é ignorado enquanto qualquer MCP de conhecimento
  compartilhado estiver ativo, para que resultados exatos de ferramentas de memory e
  Context não sejam encurtados com perda.
- **ai-memory:** memória operacional durável e continuidade entre sessões. O Ruflo usa
  `CLAUDE_FLOW_MEMORY_BACKEND=memory`, nunca um armazenamento durável concorrente.
- **IvoAI Context:** serviço independente de RAG/contexto. O control plane da sessão
  informa apenas sua integridade e preserva a integração MCP existente.
- **WorkingContext:** evidência exata e transitória de workers e ResultRefs limitadas
  para a execução atual; não é memória durável nem RAG.

## Proveniência do modelo

Nomes de modelos nunca são inferidos a partir de versão do binário, assinatura ou
fornecedor. As fontes informadas, em ordem de prioridade, são:

1. `runtime_verified` — somente quando uma evidência estruturada de runtime o confirma explicitamente;
2. `argument` — `--model`/`-m` fornecido ao cliente oficial;
3. `configured` — o arquivo de configuração do cliente oficial;
4. `unknown` — não há evidência confiável.

Os adapters atuais não promovem uma saída comum do cliente a `runtime_verified`.
Portanto, `unknown` é esperado quando nem um argumento nem uma configuração compatível
contém um modelo.

## Comandos de monitoramento e ciclo de vida

```sh
ivoai session list
ivoai session list --json
ivoai session show <session-id>
ivoai session stop <session-id>
ivoai monitor
ivoai monitor --watch
ivoai monitor --session <session-id> --json
```

`monitor --watch` foi projetado para um segundo terminal. Ele acompanha mudanças de
estado até a sessão selecionada terminar e reutiliza a apresentação responsiva do
ivoai no terminal. Durante o acompanhamento, o JSON é delimitado por linhas, não
contém ANSI e inclui somente metadados.

Os arquivos de sessão residem em `$XDG_STATE_HOME/ivoai/sessions` (normalmente
`~/.local/state/ivoai/sessions`), em diretórios com modo `0700` e arquivos JSON
atômicos com modo `0600`. Os IDs de sessão e worker são aleatórios. Marcadores de
início de processo no Linux impedem que um PID reutilizado seja encerrado. O default
de dois workers simultâneos e o limite rígido de três impedem delegação ilimitada.

## Configuração

Os defaults compatíveis com versões anteriores são:

```toml
[orchestration]
enabled = true
provider_execution = false
default_mode = "direct"
primary_executor = "codex"
review_executor = "claude"
max_workers = 2

[orchestration.auto]
enabled = true
default_planner = "codex"
automatic_failover = true
checkpoint_enabled = true
quota_refresh_seconds = 45
max_workers = 2

[orchestration.auto.quota]
enabled = true
show_weekly = true
show_monthly = true
show_session = true
show_context = true
show_model_scoped = true

[orchestration.auto.optimization]
strategy = "efficient"
parallelism = true
shared_context_bootstrap = true
progressive_escalation = true

[orchestration.auto.optimization.weights]
complexity = 30
risk = 25
reasoning_depth = 20
verification_need = 15
context_breadth = 10
```

Substituições avançadas de modelo e esforço por provider/nível são opcionais. Valores
vazios significam resolução automática em runtime/default do cliente; uma substituição
de modelo só é aceita quando aparece no catálogo oficial de capabilities do runtime.

`provider_execution=true`, executores desconhecidos, modos desconhecidos e limites de
workers fora de 1–3 são rejeitados. O menu interativo Configuration gerencia essas
preferências, ou a automação pode usar
`ivoai config set orchestration.<field> <value>`.

## Isolamento de falhas

Context, ai-memory e o servidor remoto podem estar degradados sem impedir o início do
cliente primary ou dos workers. Uma falha de preflight/inicialização do Headroom usa
o fallback documentado para agente direto. Depois que um processo wrapper é iniciado,
o ivoai não tenta novamente de modo automático, pois não pode provar que o agente não
recebeu a tarefa. Em contraste, uma falha de integridade, profile, swarm ou registro
do primary no Ruflo é fatal somente para uma sessão explicitamente orquestrada. Os
comandos originais `ivoai codex` e `ivoai claude` permanecem independentes do Ruflo.
