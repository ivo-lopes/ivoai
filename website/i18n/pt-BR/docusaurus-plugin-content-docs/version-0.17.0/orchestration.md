# Controle e orquestração de sessões

O IVOAI oferece um core nativo compartilhado pelos frontends Codex nativo e seu alias auto.
Veja [Frontends orquestrados](orchestrated-frontends.md). Os clientes oficiais
permanecem disponíveis em modo direto explícito, sem nova credencial de provider.

## Modo direto

Os comandos estabelecidos continuam sendo o caminho mais curto:

```sh
ivoai codex --direct
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

Codex nativo e seu alias auto orquestrados usam admissão nativa, aprovação do plano e despacho
automático pelo mesmo scheduler. O modo histórico Claude orquestrado abaixo
permanece inalterado; `ivoai claude` continua direto.

Nesse modo histórico Claude, antes de abrir o primary, o ivoai verifica a versão e o profile
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
ivoai codex
ivoai auto
ivoai claude
```

Codex gerenciado e seu alias auto usam a mesma admissão, scheduler, aprovação de
plano e validação do Core. Setup/execução OpenCode foram removidos. Claude acima
abre sua TUI oficial direta; hosts headless experimentais têm flags próprias,
OFF por padrão, sem controle transparente dessa TUI.

Prompts naturais não precisam de cabeçalhos formais de aceite. Primary adaptativo
sem DAG depende de opção experimental explícita, OFF por padrão. Ele preserva
somente leitura e escala trabalho complexo pelo mesmo protocolo de planejamento.
Cada turno é admitido de novo, mantendo fontes, quotas, escopos e aprovações.
Operações incertas não são repetidas após recusa ou cancelamento. Detalhes em
[auto-orchestration.md](auto-orchestration.md).

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
