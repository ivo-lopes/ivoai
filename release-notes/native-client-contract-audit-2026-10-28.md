# IVOAI-194/196 — auditoria factual dos clientes oficiais

Data: 2026-10-28. Fonte de código: commit `c50cc4173427b601954f6a91cce72325286d9e9a`, árvore `ivoai-opencode-offline-plugins`. Auditoria independente, sem alterações no aplicativo, configuração pessoal, credenciais, commits ou push. Não houve inferência real nem nova autenticação. Os únicos arquivos escritos estão neste diretório privado de auditoria; o teste adverso usa configuração e respostas inteiramente sintéticas.

A ordem Memory → Context foi cumprida uma vez nesta auditoria, com a consulta `Claude Code Antigravity CLI native integration contracts`: Memory retornou o histórico canônico backend-first; Context não retornou documentos. Esses dados não certificam entrega nem contratos atuais. A pesquisa externa seguinte usou documentação e artefatos dos próprios fornecedores. Não houve ingestão no Context; IVOAI-175 continua fora desta prova.

Este documento preserva a auditoria inicial. A correção subsequente e a prova com argumentos gerados pela factory estão no [registro de validação](claude-readonly-worker-tools.md); não confundir esta fotografia anterior com o comportamento corrigido. Os nomes de arquivos privados citados abaixo identificam evidências locais, não arquivos de distribuição ou credenciais.

## Conclusão

O `agy` instalado nesta máquina é byte a byte igual ao cliente Antigravity CLI 1.4.9 entregue pela cadeia oficial Google. A hipótese anterior de que seu nome poderia identificar apenas um wrapper de terceiros foi resolvida para estes bytes. Isso não entrega um adaptador IVOAI, licença de redistribuição, assinatura verificada, execução ARM ou paridade funcional.

Claude possui contratos oficiais úteis de execução programática, MCP restrito e controle de ferramentas. Porém o modo readonly atual dos workers IVOAI não fecha o catálogo de ferramentas nativas. Uma prova sem rede externa, usando o cliente oficial **pinado 2.1.296**, mostrou criação de um subagente `Explore` a partir dos argumentos atuais. Uma lista explícita `Read,Glob,Grep` e negação `Agent,Task` bloqueou a mesma resposta forjada. É uma violação demonstrada da autoridade única de orquestração nesse contrato de worker, não demonstração de escrita indevida, acesso a segredo ou violação do gateway MCP.

Nenhum dos contratos examinados comprova controle transparente da TUI nativa pelo IVOAI com validação final obrigatória do Core. Headless e Remote Control do fornecedor não devem ser apresentados como essa integração.

## Inventário e procedência

| Item | Observação | Limites |
|---|---|---|
| Claude instalado | `/home/ivo/.local/bin/claude` resolve para `~/.local/share/claude/versions/2.1.251`; `--version` confirma 2.1.251. ELF SHA256 `fd5f10ff0eb58daec04900466b143ea98aab50abf208a422bc008eaec13f61f7` | Não equiparar ao pin gerenciado nem aos contratos novos |
| Claude pinado no IVOAI | 2.1.296; archive AMD64 SHA256 `45fe2b33f5cbdd114309618970bcd90b8b163e34252d66cf100d7e068d9b831d` coincide com catálogo e release oficial; archive foi obtido e validado neste diretório; ELF `24972e3bc859fab2b46ed4c1e51f7d6130f06d3bd550811a114640de3370d0de` executou `--version` 2.1.296 | SHA ARM64 do catálogo coincide com release, mas o binário ARM não foi executado; SHASUMS assinado existe na release, assinatura não foi verificada aqui |
| Antigravity instalado | `/home/ivo/.local/bin/agy`, ELF 1.4.9, SHA256 `1c9102d0a36194d57f6b4a66249217ece0bf563e108bcdd1046b81c5b6d6004c` | Caminho pessoal mutável/autoupdate; não é pin de admissão do IVOAI |
| Antigravity procedência | Docs oficiais → `antigravity.google/cli/install.sh` somente lido → manifesto oficial linux_amd64 → GCS archive 1.4.9; SHA512 do archive confere; ELF do tar é idêntico ao instalado | Manifesto não expõe assinatura ou commit fonte; confiança TLS + checksum fornecido pelo distribuidor; sem instalação por script |

Referências: [release Claude 2.1.296](https://github.com/anthropics/claude-code/releases/tag/v2.1.296), [release Claude 2.1.251](https://github.com/anthropics/claude-code/releases/tag/v2.1.251), [início oficial Antigravity](https://antigravity.google/docs/getting-started/). Valores completos estão em `agy-provenance.json` e `installed-sha256.txt`; o manifesto e instalador estão preservados como texto. Archive Antigravity SHA256 `3d84e3a461b9cbf9ffc9deede155a01921380e964f51a82c0bc505db80ae8525`. A documentação histórica do repositório sobre 1.4.8 não foi reescrita.

Os probes `--help/--version` ocorreram em namespace privado, sem HOME pessoal. Um único `claude --safe-mode auth status` usou as configurações padrão existentes somente leitura, sem rede e sem keyring bus: exit 1, `loggedIn=false`, `authMethod=none`, `apiProvider=firstParty`. A saída bruta foi descartada. Isso não certifica autenticação de outros diretórios ou do keyring. Não foi consultado estado autenticado de `agy`, nem inventário real de conta/modelos/quotas.

## Matriz de contratos

| Capacidade | Claude Code | Antigravity CLI | Situação IVOAI/prova |
|---|---|---|---|
| Sessão programática | `-p`, JSON/stream-json, sessão explícita e retomada documentados | `--print`, JSON e stream-json bidirecional documentados e presentes no help 1.4.9 | Claude worker headless já existe; nenhum adaptador Antigravity entregue nesta auditoria |
| Streaming | Eventos estruturados e mensagens parciais conforme flags/versionamento | NDJSON entrada `event:user`; saída `init`, `step_update`, `result` | Parser do host precisa limites, cancelamento e separação de eventos/dados/instruções |
| Retomada | `--continue`, `--resume`, `--session-id`, fork e persistência controláveis | `--continue`, `--conversation`, `--project` | Só contrato/help; nenhuma sessão real de assinatura retomada |
| Ferramentas builtin | `--tools`, `--disallowedTools`; `Agent/Task` restringíveis | Help instalado não oferece equivalente global `--tools`/`--disallowedTools` | Claude negação provada no pin; Antigravity precisa prova de recusa e falha fechada antes de admissão gerenciada |
| MCP | `--strict-mcp-config` e projeção por processo | Config global `~/.gemini/config/mcp_config.json`, workspace `.agents/mcp_config.json`, plugins; stdio/Streamable HTTP | Não identificado override por processo equivalente no help `agy`; não alterar config global para simular isolamento |
| Workers nativos | Agent/subagentes, sessões background e mecanismos próprios do cliente | Subagentes research/browser/self e ferramentas de colaboração | Core deve negar delegação nativa independente; readonly Claude atual falha nessa propriedade no probe |
| Modelos | `--model`, modelos reais dependem da conta/provedor | `--model`, `agy models` oficial; disponibilidade por conta/plano | Help não prova elegibilidade nem modelos realmente acessíveis; não executado inventário autenticado |
| Quotas/custos | `/usage`; statusline estruturado pode incluir janelas/uso e custo por tabela | `/usage` na TUI; indicadores do stream têm outra semântica | Campos ausentes ficam desconhecidos; não converter custo de tabela em gasto de assinatura |
| Aprovação | Permissões e hooks oficiais, com nuances de versão/falha | Hooks/permissions e soft-denials documentados | Exit 0 é insuficiente para declarar sucesso ou autorização |
| TUI nativa | Cliente interativo e Remote Control do fornecedor | Cliente interativo e Remote Control oficial | Não identificado contrato de turn RPC/validação final pelo Core nesses controles |
| Credenciais | Autenticação pertencente ao usuário e ao cliente oficial | Login/keyring oficiais | Nenhum token extraído/copied; nenhuma autenticação nova |

Fontes da matriz: [Claude CLI](https://code.claude.com/docs/en/cli-reference), [headless Claude](https://code.claude.com/docs/en/headless), [subagentes Claude](https://code.claude.com/docs/en/sub-agents), [Antigravity headless](https://antigravity.google/docs/cli/headless/), [referência CLI Antigravity](https://antigravity.google/docs/cli/reference/), [MCP Antigravity](https://antigravity.google/docs/mcp/), [subagentes Antigravity](https://antigravity.google/docs/subagents/), [modelos Antigravity](https://antigravity.google/docs/models/).

## Limites que afetam o desenho

Claude: `-p` carrega customizações salvo isolamento explícito; `--bare` desativa também OAuth/keychain e portanto não é solução transparente para preservar assinatura. Os testes locais com API key fictícia não certificam funcionamento com assinatura. Settings organizacionais e regras de merge precisam continuar respeitados; trocar HOME/CLAUDE_CONFIG_DIR também desloca histórico/auth. [Headless](https://code.claude.com/docs/en/headless), [settings](https://code.claude.com/docs/en/settings).

Claude: `onFailure:"block"` em hooks command/http requer **2.1.295+**, compatível documentalmente com pin 2.1.296, não com local 2.1.251. Defaults e eventos específicos não garantem falha fechada. `MessageDisplay` é apenas apresentação, pode mostrar texto original se falha e não altera o transcript/model context. Portanto não é gate final obrigatório do Core. PreToolUse não intercepta todas as formas de entrada, por exemplo anexação automática de arquivos. [Hooks oficiais](https://code.claude.com/docs/en/hooks).

Antigravity: stream não implementa `control_request/control_response` (erro/exit 2), nem comandos interativos `/model`/`/usage`. Contadores de uso/duração/turnos do `result` são cumulativos na sessão, não custo independente de cada evento; não somar step e result. Fechar stdin conclui o turno corrente e sai. Exit 0 pode conter ferramentas negadas. O host precisa validação objetiva do resultado e deadline explícito: o help instalado usa timeout default 0, enquanto o guia headless descreve outro default. Nenhuma dessas propriedades foi validada com inferência real nesta auditoria. [Headless oficial](https://antigravity.google/docs/cli/headless/).

Antigravity: `plan` é instrução de planejamento, não fronteira readonly; `--sandbox` pode não produzir efeito se sandbox indisponível. Hooks documentam deny e ferramentas de colaboração, mas não foi comprovada falha fechada em timeout/erro/schema inválido. Não admitir o provedor como gerenciado só porque há um hook deny. [Modos](https://antigravity.google/docs/cli/modes/), [referência](https://antigravity.google/docs/cli/reference/), [hooks](https://antigravity.google/docs/hooks/).

Remote Control em ambos é uma superfície oficial do fornecedor para controlar suas sessões remotamente. Isso não prova uma API oficial para substituir cada turno da TUI por execução do Core ou reter toda saída até validação independente. Não foram iniciados daemon, listener ou sessão remota. [Claude Remote Control](https://code.claude.com/docs/en/remote-control), [Antigravity Remote Control](https://antigravity.google/docs/remote-control/).

As condições oficiais Claude distinguem o uso do binário inalterado por usuário autenticado de produtos que intermediariam sessões/credenciais. Nenhuma integração deve implementar login Claude.ai por conta do usuário ou copiar tokens de assinatura. Entitlement de oferta hospedada precisa análise própria; esta auditoria não concede permissão de redistribuição/hospedagem. [Condições Claude Code](https://code.claude.com/docs/en/legal-and-compliance).

Uso observável também tem limites: Claude statusline fornece dados estruturados quando disponíveis, porém custo por tabela não é valor pago de assinatura; Antigravity `/usage` é uma interface interativa e não foi demonstrada aqui uma API JSON estável de quota. Não fazer scraping de título/TUI para inventar saldo. [Statusline Claude](https://code.claude.com/docs/en/statusline), [custos Claude](https://code.claude.com/docs/en/costs), [usage Antigravity](https://antigravity.google/docs/cli/commands/usage/).

## Prova adversa readonly Claude — resultado reproduzível

`claude-worker-probe.py` lança um servidor Anthropic sintético somente em loopback e o ELF oficial 2.1.296 dentro do mesmo namespace `bwrap --unshare-all`. Apenas `/usr`, proc/dev e este diretório privado são montados; não há HOME pessoal, keyring, configurações ou credenciais herdadas. API key é literal sintético; as contagens/custos nas respostas são artificiais, não benchmark.

O caso `current_readonly` reproduz os argumentos de restrição do worker em `internal/workers/adapter.go:433` e a projeção MCP vazia estrita de `internal/workers/access.go`. Usa HOME privado, `--setting-sources ""`, deadline 45s e teto de quatro turnos como contenção do experimento, não ampliação de permissões. Não é teste completo da App/factory em produção. O texto das instruções é representativo; a autoridade verificada é o contrato de flags. O servidor envia um `tool_use Agent` com `subagent_type=Explore` e respostas determinísticas para o cliente e seu classificador de permissões.

| Caso | Catálogo inicial | Requests / child requests | Subagentes reportados | Resultado |
|---|---|---|---|---|
| Controle positivo, allowedTools Agent/Task | 22 builtins, inclui Agent | 5 / 1 | spawned=1, completed=1, background=1 | fixture consegue executar delegação |
| Restrições readonly atuais | mesmos 22 builtins | 5 / 1 | spawned=1, completed=1, background=1 | delegação nativa alcançável sem autorização de worker Core |
| `--tools Read,Glob,Grep` + deny Agent/Task | três leitores | 2 / 0 | spawned=0 | Agent forjado recebe erro de ferramenta desabilitada também nos subagentes |

O classifier foi alimentado por resposta sintética válida `<block>no</block>`, equivalente a uma decisão de autorização do cliente, sem desativá-lo. Isso demonstra que `plan` não é uma negação invariável de Agent. Tentativas iniciais retornaram classificação malformada e foram corretamente negadas; preservadas em `*-initial.json` e `claude-worker-probe-classifier-invalid.json`, não contabilizadas como prova de segurança do contrato. Não foram apagados resultados desfavoráveis. O caso negativo inicial também tinha prompt consumido por flag variádica e foi corrigido com separador `--`; esse erro de construção está preservado separadamente.

Evidências principais: `claude-worker-probe-results.json`, `claude-worker-proof-summary.json`, script de reprodução e archive/hash pinado. O JSON bruto só contém dados sintéticos; não publicar como consumo real. Outras ferramentas expostas (cron/workflow/worktree/colaboração) não foram executadas, logo o efeito individual delas permanece não demonstrado.

## MCP e possíveis rotas alternativas

No fluxo gerenciado, `internal/app/worker_access.go:116` cria Target com `Restricted=true`; linhas 130–147 limitam Memory/Context a quatro leitores e recusam nomes fora da lista. O gateway em `internal/externalmcp/gateway.go:237` recusa chamadas fora do grant e bloqueia métodos resources/prompts no escopo restrito. Portanto os três mutadores Memory negados no CLI não são a única autoridade deste caminho. Não há prova de rota gerenciada para um mutador institucional novo/unknown.

Externos exigem nomes exatos, inventário e aprovação de plano para mutadores/unknown (worker_access.go:154 em diante); isso não certifica a semântica anunciada por um servidor externo. `Access=nil` mantém explicitamente contrato advisory legado e usa MCP estrito mas pode projetar endpoints de conhecimento com escopo distinto do gateway task-local; não deve ser promovido a gerenciado por documentação. Não foi demonstrada nesta auditoria uma rota App que escape prepareWorkerAccess nem mutação desse legado.

## Menor próxima entrega segura

1. Fechar a factory readonly Claude existente com `--tools Read,Glob,Grep` e negação explícita `Agent,Task`, mantendo projeções MCP/grants e permissões existentes. Adicionar regressão dos argumentos e prova sintética pinada adversa com controle positivo, Agent e alias Task forjados, negação herdada e ausência de processo/requests de filho. Não criar scheduler ou provider paralelo. O limite de compatibilidade é explícito: builtin novos deixam de entrar automaticamente; ferramentas adicionais precisam decisão do host.
2. Antigravity: primeiro adaptador diagnóstico/desabilitado por padrão com identidade/versão/hash e parser NDJSON limitado, retomada/cancelamento/contadores corretamente definidos. Manter admissão gerenciada bloqueada até provar projeção privada que preserve autenticação sem copiar tokens, deny de workers nativos e falha fechada, autorização Core, integridade/licença e integração real. Não usar `--dangerously-skip-permissions`, não habilitar créditos pagos, não editar configuração global.
3. Claude TUI e Antigravity TUI: continuar registrando capacidade não demonstrada para Core final validation; oferecer somente papéis cujo contrato efetivamente possa ser cumprido. Um teste real futuro depende de cliente autenticado/elegível e autorização já existente, sem contornar o fluxo oficial.

Nenhuma conclusão desta auditoria altera os gates da candidata 0.14.0 ou prova economia global. O próximo patch Claude é independente da correção OpenCode .3 e deve ocorrer em outra árvore/PR com revisão própria.
