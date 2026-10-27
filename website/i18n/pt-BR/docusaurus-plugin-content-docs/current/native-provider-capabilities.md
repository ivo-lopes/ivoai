# Clientes nativos e provedores opcionais: limites da pesquisa

Data da pesquisa: 2026-10-27. Roadmap: IVOAI-150, 190, 194, 196, 200, 201 e
23–26, sob a IVOAI-191. Este documento registra contratos e riscos de integração;
não implementa um adaptador, certifica paridade em execução, aprova atualização
de dependências nem altera valores padrão. Esta pesquisa não executou inferência,
instaladores de terceiros, extração de credenciais ou edição de configurações pessoais.

A linha de base do repositório é a v0.12.1 (`3ff6db8b`). É necessário distinguir
a implementação existente destas possibilidades oferecidas pelos projetos de origem.
O gate de aceite econômico continua sem comprovação; percentuais de compressão
de terceiros não representam economia das sessões do IVOAI.

## Limite atual do Core na v0.13.0

Entradas, resultados, falhas e traces operacionais de executores foram movidos
para `internal/core/turn.go`; aliases compatíveis preservam a superfície anterior
do pacote. A conclusão do turno e as leases de recursos são locais a cada turno
preparado. O frontend Codex ainda usa Bridge e catálogo de modelos do OpenCode;
é uma extração parcial, não a remoção do adaptador nem alteração no aceite do DAG.
Consulte a [validação dos contratos](https://github.com/ivo-lopes/ivoai/blob/v0.13.0/release-notes/backend-neutral-turn-contracts.md).

O probe administrativo do Claude retornou `loggedIn=false`, `authMethod=none` e
exit 1; nenhuma inferência com Claude foi tentada. Antigravity permanece apenas
como pesquisa, sem certificação de paridade da TUI nativa. A ingestão no Context
continua `CONTEXT_SYNC=BLOCKED` na IVOAI-175. A autenticação existente dos clientes
nativos e a propriedade da configuração pessoal permanecem inalteradas.

O CI de runtime passou em 15/15 nos heads dos dois PRs incorporados; o
[registro da release](releases.md) vincula esses CIs e os gates separados de
metadados/documentação e Release. CI de runtime aprovado não comprova paridade
dos clientes nativos.

## Evidências e matriz de clientes

As verificações locais usaram somente `--version` e `--help`. Uma opção listada
comprova uma superfície da CLI, não autenticação bem-sucedida, quota utilizável,
isolamento efetivo ou execução correta. As instalações locais diferem das versões
fixadas e gerenciadas pelo repositório; nenhuma foi promovida ao catálogo de
componentes por esta pesquisa.

| Cliente | Versão e superfície observadas localmente | Linha de base do IVOAI / trabalho pendente |
|---|---|---|
| Codex | 0.164.0; `app-server --help` expõe transportes stdio, Unix socket e WebSocket, substituições de configuração por processo e geração de schemas | `internal/codexfrontend` já fornece integração nativa gerenciada. Os tipos compartilhados de turno de executor foram movidos para o Core; dependências de Bridge/catálogo de modelos permanecem. A presença do protocolo nativo não comprova que o OpenCode já possa ser removido. |
| Claude Code | 2.1.251; a ajuda expõe modo print, entrada/saída JSON e streaming, retomada/ID de sessão, ferramentas explícitas, modo de permissão, configurações por processo e configuração estrita de MCP | Existem caminhos de executor/worker pela CLI oficial. A IVOAI-194 ainda exige mediação gerenciada da conversa nativa e evidências reais de paridade. Flags headless, por si só, não comprovam controle transparente da TUI nativa. |
| Antigravity | `agy` 1.4.8; a ajuda expõe formatos print/stream, retomada de conversa, modelo/effort, modo de execução, sandbox, caminho de log e controle de comandos com barra | Subsídio de pesquisa para IVOAI-150/196. Estas verificações não comprovam integração do Antigravity nem certificação de inferência. |

O SHA-256 observado do executável Antigravity foi
`70b1d27f1812f5ebccc8f13daf2209112447413437dd1ab6d87c58b8a04fe35b`.
Ele identifica os bytes locais; não constitui assinatura de release verificada
independentemente nem versão aprovada para redistribuição.

O App Server documentado do Codex expõe o ciclo de vida de threads/turnos e o
tratamento de aprovações; o adaptador IVOAI deve continuar usando o núcleo como
autoridade. Consulte a [documentação oficial do App Server](https://learn.chatgpt.com/docs/app-server)
e o [contrato de frontend](orchestrated-frontends.md) existente.

A ajuda observada do Claude inclui `--mcp-config`, `--strict-mcp-config`,
`--setting-sources`, `--settings`, `--allowedTools`, `--disallowedTools`,
`--tools`, `--permission-mode`, `--resume` e `--no-session-persistence`.
São candidatos à projeção com escopo por processo, sujeitos a testes específicos
de versão. A ajuda de `--bare` exclui explicitamente autenticação por OAuth/keychain;
portanto, essa opção não substitui de forma transparente um login de assinatura
existente. Isso é evidência da CLI local, não uma nova afirmação sobre os termos
das contas suportadas.

## Contrato e limites do Antigravity

O [contrato headless oficial](https://antigravity.google/docs/cli/headless/)
suporta `--input-format stream-json --output-format stream-json`. A entrada usa
eventos `user`; a saída usa `init`, `step_update` e o evento terminal `result`.
A retomada usa `--conversation`. Em um processo persistente, uso, duração e
contagem de turnos são cumulativos: subtraia os contadores anteriores sem contar
duas vezes o uso dos passos e do resultado. Mensagens
`control_request`/`control_response` não são suportadas. `/usage` deve ser executado
fora do stream. Uma ferramenta pode ser recusada sem interromper a execução,
enquanto o processo termina com código zero; exija o estado terminal, o resultado
pretendido e os diagnósticos relevantes. Escritas no workspace em modo headless
podem ser permitidas por padrão. Esses contratos não comprovam um RPC externo
de aprovação nem interceptação da TUI nativa.

A [documentação dos modos de execução](https://antigravity.google/docs/cli/modes)
descreve `plan` como um prefixo de instrução. Ele não é uma barreira de segurança
de somente leitura. A [referência da CLI](https://antigravity.google/docs/cli/reference/)
também informa que a flag de sandbox pode não ter efeito quando o sandbox da
plataforma está indisponível. A integração de workers exige contenção verificada
pelo SO, ferramentas com escopo definido e proteção explícita contra escrita;
nenhuma dessas flags, isoladamente, atende ao contrato dos workers consultivos
do IVOAI. O adaptador deve definir um timeout limitado em vez de presumir um
valor padrão: a ajuda local da 1.4.8 e a referência mostram zero, enquanto o guia
headless descreve cinco minutos.

A [autenticação nativa](https://antigravity.google/docs/install/) usa o keyring do
SO e login interativo quando necessário. Preserve esse fluxo sem ler ou copiar
tokens. A [documentação de quota](https://antigravity.google/docs/cli/commands/usage/)
descreve um painel `/usage` atualizado, não uma API JSON de quota comprovadamente
estável. Quota indisponível deve continuar desconhecida. Isso não implica fallback
para API paga.

A [documentação MCP](https://antigravity.google/docs/mcp) suporta stdio local e
Streamable HTTP remoto; a CLI não suporta SSE legado. Ela indica o arquivo global
`~/.gemini/config/mcp_config.json` e o arquivo de workspace `.agents/mcp_config.json`.
Não foi demonstrada aqui uma projeção privada com escopo por processo equivalente
à do Codex/Claude. Não edite a configuração do usuário para ocultar essa lacuna.

O aceite da IVOAI-196 ainda exige autenticação, streaming, cancelamento, retomada,
recusa de permissão, comportamento de quota, isolamento de fontes e testes dos
clientes nativos na versão efetivamente suportada. Relate separadamente as
capacidades headless e da TUI.

## Identidades imutáveis dos projetos pesquisados

Estes commits foram inspecionados para obter evidências de código-fonte e licença.
São entradas propostas para experimentos, não atualizações de runtime aprovadas.
As conclusões de licenciamento abaixo descrevem os arquivos inspecionados e não
certificam todos os artefatos transitivos.

| Projeto | Commit inspecionado | Evidência de licença |
|---|---|---|
| RTK 0.49.0 | `e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5` | [Apache-2.0 na raiz](https://github.com/rtk-ai/rtk/blob/e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5/LICENSE); engine e proxy têm arquivos BUSL-1.1 separados |
| Codebase Memory MCP | `83109dde52b3d5c766451f84d346cf98ad05bf3f` | [MIT](https://github.com/DeusData/codebase-memory-mcp/blob/83109dde52b3d5c766451f84d346cf98ad05bf3f/LICENSE) |
| Headroom | `17d38e5699b7d7c69ac6addcd762f39e42dd7f00` | [Apache-2.0](https://github.com/headroomlabs-ai/headroom/blob/17d38e5699b7d7c69ac6addcd762f39e42dd7f00/LICENSE) |
| Caveman | `9b68f7ed12b05e7efaff18e65c5a3fc2849fd0a4` | [Apache-2.0](https://github.com/JuliusBrussee/caveman/blob/9b68f7ed12b05e7efaff18e65c5a3fc2849fd0a4/LICENSE) |
| OpenViking | `54d4c0fac91fae976325ebed618d7278ff9b5c77` | [AGPL-3.0](https://github.com/volcengine/OpenViking/blob/54d4c0fac91fae976325ebed618d7278ff9b5c77/LICENSE) |

## RTK-OSS: filtrar a saída armazenada sem repetir comandos

O [manifesto Cargo](https://github.com/rtk-ai/rtk/blob/e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5/Cargo.toml)
inspecionado habilita `engine` por padrão. Um experimento somente Apache deve usar
o artefato OSS ou um build com dependências fixadas e `--no-default-features`,
verificando depois que `rtk-engine` está ausente do grafo de dependências vinculadas.
A indicação Apache do pacote raiz, sozinha, não identifica os limites de licença
do binário completo. A [documentação das edições](https://github.com/rtk-ai/rtk/blob/e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5/docs/guide/resources/editions.md)
distingue os filtros OSS do engine/proxy BUSL. Não se propõe um proxy RTK.

A [implementação do pipe](https://github.com/rtk-ai/rtk/blob/e05c7bdfacfcfe1f845d11f47339e4eae63eb3f5/src/cmds/system/pipe_cmd.rs)
suporta `rtk pipe --filter <name>`. Ela consome stdin UTF-8 com tamanho limitado
e retorna a saída filtrada sem executar o comando original. Os filtros incluem
Git status/log/diff, rg, build/test do Go, pytest, testes Cargo, tsc, vitest e logs.
O filtro de testes Go espera JSON. UTF-8 inválido, entrada acima do limite,
filtros não suportados ou falha do processo devem usar a representação direta.

O limite do experimento é: executar um comando autorizado uma vez; preservar
stdout/stderr originais e o código de saída no ArtifactStore existente; passar
os bytes elegíveis a um filtro da lista permitida; retornar a representação com
referências exatas autorizadas. Nunca repita uma mutação para obter a saída bruta.
Preserve falhas, diffs e evidências de testes segundo a política de fidelidade
existente. Não invoque `rtk init` nem crie outro banco de recuperação. Use caminhos
isolados de runtime/configuração, limites de recursos e uma feature flag
inicialmente desabilitada. A IVOAI-200 continua pendente de comparação real de
qualidade e custo da sessão.

## Provedor Codebase: implicações do daemon compartilhado

O [README](https://github.com/DeusData/codebase-memory-mcp/blob/83109dde52b3d5c766451f84d346cf98ad05bf3f/README.md)
inspecionado descreve coordenação por daemon mesmo em operações comuns da CLI.
Portanto, um subprocesso não é automaticamente descartável ou isolado. O
[contrato de configuração](https://github.com/DeusData/codebase-memory-mcp/blob/83109dde52b3d5c766451f84d346cf98ad05bf3f/docs/CONFIGURATION.md)
usa um cache canônico por conta; outra raiz de cache pode ser recusada enquanto
uma sessão estiver ativa. `CBM_ALLOWED_ROOT` restringe a indexação, mas não
substitui o isolamento pelo SO. Watchers são habilitados por padrão; a indexação
automática é desabilitada. Os limites de quantidade de arquivos e tamanho total
do código-fonte indexado são desabilitados por padrão.

A IVOAI-190 exige namespace privado para o daemon e seu ponto de encontro, cache
privado, admissão com limites de recursos, ausência de UI automática, indexação
explícita e comprovantes de atualização. Verifique o comportamento sem interferir
no daemon existente do usuário. O isolamento por projeto/worktree/ACL deve ser
testado entre sessões simultâneas. Exponha somente operações de consulta aprovadas
por meio de `CodeIntelligenceProvider`; mantenha mutações do índice, exclusão de
projetos, escritas de ADR e ingestão de traces fora das permissões dos workers.
Prefira leitura direta ou Graft quando forem suficientes; não há sequência
obrigatória Graft→Codebase.

## Decisões de compressão e Context preservadas

O [README](https://github.com/headroomlabs-ai/headroom/blob/17d38e5699b7d7c69ac6addcd762f39e42dd7f00/README.md)
inspecionado do Headroom oferece ContentRouter e originais recuperáveis localmente
por CCR. É um candidato a experimento de compressão seletiva, não motivo para
adicionar outro sistema de memória ou substituir o ArtifactStore. Compare-o
separadamente com Caveman e Direct.

O [README](https://github.com/JuliusBrussee/caveman/blob/9b68f7ed12b05e7efaff18e65c5a3fc2849fd0a4/README.md)
inspecionado do Caveman informa que a telemetria de uso da CLI atual é habilitada
por padrão e pode ser desabilitada com `DO_NOT_TRACK=1`. Essa observação diz
respeito à revisão upstream inspecionada; não é uma constatação de que a versão
fixada no IVOAI envia telemetria. Qualquer atualização deve auditar o executável
e o ambiente efetivos. A IVOAI-201 mantém Caveman como padrão, Headroom como
opcional e Direct como fallback; esta pesquisa não aprova uma sequência de
proxies nem a remoção do Headroom.

Para IVOAI-23–26, preserve o ContextBackend existente e o ai-memory durável como
capacidades distintas. OpenViking permanece opcional/em shadow. Suas obrigações
AGPL exigem avaliação em relação à distribuição e à implantação pretendidas antes
da adoção; nenhum cutover automático se justifica. A validação de ingestão e
recuperação do Context continua sendo uma questão separada da IVOAI-175, e uma
escrita de memória não comprova sincronização do Context.
