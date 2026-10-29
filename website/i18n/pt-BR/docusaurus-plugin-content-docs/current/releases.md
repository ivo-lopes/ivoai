# Releases

## v0.15.0 — candidata local, não publicada

**Publicação bloqueada por falha no canário real; a candidata não foi publicada.**
O head `03492d1d` do PR57 passou 16 checks no [CI do head anterior](https://github.com/ivo-lopes/ivoai/actions/runs/40780164326).
Esse resultado não é CI desta atualização. O canário separado do Core `0bab9b34`
teve uma tentativa, zero eventos JSON e nenhuma resposta após 5.219 segundos;
uso UNAVAILABLE e causa de startup UNKNOWN. Workers/tasks/failovers Core foram
zero; não houve segunda tentativa Core/CLI. Retries nativos seguem UNKNOWN.
O canário fica fora dos 126 turnos históricos e não prova ausência de chamadas
ao provedor. Consulte o [registro do canário reprovado](https://github.com/ivo-lopes/ivoai/blob/release/v0.15.0-candidate/release-notes/v0.15.0-canary-failure.md).

A candidata local agora compõe main58 `eb30d6ca`/runtime revisado `55ec7ab9`
e PR59 `5683c922` com PR57 `dc9dc587`. Codex gerenciado recusa explicitamente
`--ignore-user-config`: migre omitindo a flag; configurações ficam intactas.
Isso não implementa escopo equivalente nem dispensa inventário MCP. PR59 altera
somente publicação de PID/cleanup de uma fixture. A falha original do CI57 fica
FAIL; [CI59](https://github.com/ivo-lopes/ivoai/actions/runs/40788883401) passou
16/16 na tentativa1 no head `5683c922`, incorporado ao main `4368d549` com a mesma
árvore. Esse CI não é desta composição. Prova Core Code Mode continua NOT_MET;
publicação permanece BLOCKED.

A candidata inclui independência dos provedores/setup nativos,
Gateway no prefetch de código já admitido sob flag OFF por padrão, recuperação
exata G0 Core/MCP protegida e SPI G1a de snapshots Memory sem ligação ao App,
controles/preflight offline e piloto adaptativo opcional. Caminhos nativos excluem
descoberta/fallback OpenCode; setup padrão de cliente deixa de provisioná-lo.
`ivoai setup --mode client --legacy-opencode`, comandos legados descontinuados e
históricos/checkpoints preservados continuam disponíveis. A flag adaptativa fica
OFF: primeiro envelope literal elegível, sem classificador, com DAG/aprovações Core.
Provas oficiais sintéticas de metadados não validam modelos reais nem paridade TUI.

PR55 é pré-requisito offline de fonte, não concorrência observada de clientes.
PR54/56 não incorporados ficam excluídos. Economia global continua INCOMPLETE;
evidências adversas permanecem. Paridade geral de modelos/TUI/Claude/Antigravity
e administração não está comprovada; Context segue BLOCKED/IVOAI-175. O advisory
LOW de deepmerge na documentação permanece needs_review. O canário real falhou;
a publicação está bloqueada. CI desta atualização e validação dos artefatos
continuam gates separados pendentes. Consulte as [notas bilíngues preliminares](https://github.com/ivo-lopes/ivoai/blob/release/v0.15.0-candidate/release-notes/v0.15.0.md)
para migrar/reverter sem apagar dados preservados nem reproduzir mutações.

## v0.14.0

A [v0.14.0](https://github.com/ivo-lopes/ivoai/releases/tag/v0.14.0) foi publicada
em 28/10/2026 às 19:10:11 UTC, do fonte `6d99e320`. Os 12 assets públicos foram
baixados e conferidos por tamanho/digest da API; os 11 checksums de payload passaram.
Módulo, VCS, dependências/componentes do Core e skill correspondem ao fonte revisado.
O [relatório pós-publicação](https://github.com/ivo-lopes/ivoai/blob/main/release-notes/v0.14.0-publication-verification.md)
é um commit posterior de documentação, sem inclusão na tag ou nos assets publicados.

Adiciona o [console administrativo local de consulta](admin-console.md), controles
de workers nativos do Codex gerenciado, métricas estritas de uma execução e
correções da política de compressão. Workers Claude readonly passam a expor
somente `Read,Glob,Grep` e negar `Agent`/`Task`; pesquisa adicional usa capacidades
Core/MCP aprovadas e o contrato de escrita permanece. Isso delimita delegação,
sem provar paridade da TUI ou da conta. O frontend legado privado `.3` dispensa
instalação do SDK npm em runtime e exige atualização revisada de builds antigos.
VERSION, RTK-OSS e Codebase continuam pilotos
opcionais limitados; o runner nativo de evidências ainda recusa produção.

Os PRs [41](https://github.com/ivo-lopes/ivoai/pull/41),
[42](https://github.com/ivo-lopes/ivoai/pull/42),
[43](https://github.com/ivo-lopes/ivoai/pull/43) e
[44](https://github.com/ivo-lopes/ivoai/pull/44) passaram em 15/15 verificações cada.
O PR [48](https://github.com/ivo-lopes/ivoai/pull/48) passou em 15/15 no seu fonte
revisado ([CI](https://github.com/ivo-lopes/ivoai/actions/runs/40729664838)). O main
publicado passou depois em [16/16 jobs](https://github.com/ivo-lopes/ivoai/actions/runs/40746711281),
e os cinco [jobs Release](https://github.com/ivo-lopes/ivoai/actions/runs/40746774761)
passaram, incluindo a prova Claude de cinco casos e version/help do Core exato
com HOME/XDG privados em ARM nativo e Nehalem amd64 antes do empacotamento.
Um smoke local separado do download passou em version/help AMD64; não houve
execução ARM local. Essa verificação pós-download não fez chamadas a provedores/
modelos, instalação local ou implantação em produção.

A nova comparação de sete casos preservou 42 tentativas reais corretas: entrada
+2,85% e tempo −1,44%; as metas de 10%/5% foram reprovadas. As 72 tentativas
anteriores continuam preservadas. Outros [12 turnos de edição](https://github.com/ivo-lopes/ivoai/blob/99fe07a93e242d2163116bf6dd8d265c3c8f9d8c/release-notes/benchmark-matrix16-edit-live-validation.md)
passaram nos oráculos declarados, mas reprovaram as mesmas metas: entrada −0,01257%,
tempo das fases −2,28091% e processo −2,46501%. Ambos usaram leitura direta, sem
prefetch/Graft/Gateway. Os 126 turnos cobrem somente seis coortes backend-first (18×4+42+12), sem
representar todas as tentativas do repositório. Pilotos prefetch/schema do PR30 e
pesquisas MCP anteriores permanecem separados; não há total geral nem ganho
causal agregado entre condições diferentes. O novo harness não integra esta release.
Os PRs [45](https://github.com/ivo-lopes/ivoai/pull/45),
[46](https://github.com/ivo-lopes/ivoai/pull/46),
[47](https://github.com/ivo-lopes/ivoai/pull/47),
[50](https://github.com/ivo-lopes/ivoai/pull/50) e
[51](https://github.com/ivo-lopes/ivoai/pull/51) continuam excluídos.
A matriz global é INCOMPLETE. Essas medições
antecedem as mudanças posteriores e não comprovam seu efeito econômico. Remoção
OpenCode, execução adaptativa geral, equivalência Claude, Antigravity e ingestão
Context continuam pendentes. Consulte as [notas bilíngues da tag](https://github.com/ivo-lopes/ivoai/blob/v0.14.0/release-notes/v0.14.0.md)
para escopo, compatibilidade e limites de rollback. A linguagem de candidata
anterior à publicação foi preservada; o relatório separado acima registra o resultado real.

A auditoria desta atualização documental passou no limiar alto inalterado,
com um advisory baixo residual em deepmerge 4.3.1 transitivo. A revisão limitada
identificou dependência Node/SVGR de build, sem demonstrar rota explorável pelo
HTTP Go ou navegador. Não há alegação de zero vulnerabilidades ou segurança
universal. O relatório pós-publicação preserva os detalhes e o exit 1 no limiar baixo.

## v0.13.0

Entrega backend-first incremental: evidências de benchmark que falham de forma
fechada, Evidence Gateway opcional e desativado por padrão, intake de prompts
informais, contratos de turno de executor neutros em relação ao provedor e
atualizações isoladas de perfis de dependências do Ruflo gerenciado. OpenCode
permanece operacional; a execução do primary ainda segue a política existente
de conclusão do DAG. A autenticação do Claude não foi confirmada; a integração
real com Antigravity e a ingestão no Context continuam não verificadas/bloqueadas.

**O aceite econômico foi reprovado:** a comparação final de três cenários reduziu
a entrada em 5.43% e o tempo em 0.12%, abaixo das metas de 10%/5%. Os quatro datasets
preservam 72 tentativas reais; o gate global de 16 cenários permanece INCOMPLETE.
Não há alegação de economia geral, migração concluída ou nova paridade de clientes.

Os [PRs #31](https://github.com/ivo-lopes/ivoai/pull/31) e
[#32](https://github.com/ivo-lopes/ivoai/pull/32) foram incorporados como `c475d2a3`
e `7f5cf051`. Seus heads de runtime `d6f445e3` e `5949f20d` passaram em 15/15
verificações cada ([CI do PR #31](https://github.com/ivo-lopes/ivoai/actions/runs/40594966702),
[CI do PR #32](https://github.com/ivo-lopes/ivoai/actions/runs/40595049106)).
A publicação exige aprovação do CI de metadados/documentação e do pipeline
Release, que recompila e valida os artefatos de distribuição. Isso não implanta
em produção nem comprova publicação de artefatos antes da execução desse pipeline.

Consulte a
[validação do backend](https://github.com/ivo-lopes/ivoai/blob/v0.13.0/release-notes/backend-first-validation.md), a
[validação dos contratos Core](https://github.com/ivo-lopes/ivoai/blob/v0.13.0/release-notes/backend-neutral-turn-contracts.md) e o
[relatório de dependências/rollback](https://github.com/ivo-lopes/ivoai/blob/v0.13.0/release-notes/dependency-remediation-validation.md) já versionados. Consulte as [notas bilíngues da release](https://github.com/ivo-lopes/ivoai/blob/v0.13.0/release-notes/v0.13.0.md).
Os pilotos locais posteriores de `VERSION` e RTK estão excluídos.

Os pipelines anteriores dos PRs #31/#32 passaram nas verificações de testes,
race, docs, auditorias e ShellCheck, mas reprovaram no gate de vulnerabilidade do
Go 1.27.2 por [GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809). O incremento
agora exige Go 1.27.3, incluindo os dois arquivos Linux de bootstrap
verificados. Os heads de runtime corrigidos passaram nos CIs vinculados acima;
isso não transforma retroativamente aprovações parciais anteriores em promoção
com CI verde.

## v0.12.1

Correções compatíveis na recuperação de código/API, telemetria sanitizada e
prontidão do catálogo OpenCode. Publicada por instrução explícita do mantenedor,
com **aceite econômico ainda reprovado** e matriz de 16 cenários incompleta.
Não há comprovação de economia agregada de tokens ou latência. Consulte as
[notas da versão](https://github.com/ivo-lopes/ivoai/blob/v0.12.1/release-notes/v0.12.1.md)
para validação, compatibilidade e limites de rollback.

## v0.12.0

Consolida as mudanças P1/P2 de sessões, recuperação de hooks, telemetria e perfis
de automação com a [inteligência de código Graft nativa](native-graft.md). O runtime
seleciona inspeção direta ou estrutural limitada, preserva o roteamento institucional
Memory/Context e confina a indexação a snapshots privados, sem rede nem execução
de scripts do projeto. Claude mantém suporte por testes contratuais; a validação
real da IVOAI-188 continua adiada. Consulte as
[notas completas](https://github.com/ivo-lopes/ivoai/blob/v0.12.0/release-notes/v0.12.0.md)
para compatibilidade, evidências medidas e limites de recuperação.

## v0.11.0

Atualiza ai-memory para 2.6.3, Ponytail para 5.1.0, os executores e os locks
revisados, corrige as vulnerabilidades conhecidas do Go/npm e incorpora o Claude
Red ao catálogo governado. A validação real do Claude continua pendente por falta
de acesso. Antes de migrar ai-memory 1.x, faça backup dos dados com as escritas
paradas; rollback de binário não reverte migração de banco. Consulte versões,
limites de compatibilidade, segurança e reversão nas
[notas completas](https://github.com/ivo-lopes/ivoai/blob/v0.11.0/release-notes/v0.11.0.md).

## v0.9.2

O patch estável `v0.9.2` preserva a arquitetura OpenCode-first enquanto restaura
resultados federados conformes de Memory/Context para o Codex. Também expõe escolhas
de modelo e nível de raciocínio em runtime pelo seletor nativo do OpenCode, persiste
metadados de seleção solicitada e efetiva, classifica falhas do bridge sem aceitar
saída parcial, drena a inicialização federada deterministicamente, unifica o canvas
da documentação e adiciona locales completos em inglês e português do Brasil. As
notas completas estão em
[`release-notes/v0.9.2.md`](https://github.com/ivo-lopes/ivoai/blob/v0.9.2/release-notes/v0.9.2.md).

## v0.9.0

A release estável `v0.9.0` torna a TUI OpenCode pinada o frontend gerenciado do AUTO,
enquanto o IVOAI continua sendo o control plane de sessão, executores, policy, quota
e conhecimento. Codex e Claude Code continuam responsáveis pela própria autenticação
de assinatura; nenhuma credencial de provedor é copiada para o OpenCode. Ela também
adiciona mapeamentos retomáveis entre sessões do frontend e dos executores,
visibilidade persistente de escopo e saúde multi-server, um tema e painel IVOAI
construídos com as APIs de plugin compatíveis do OpenCode e uma rodada de hardening de
acessibilidade no portal de documentação self-hosted. As notas completas estão em
[`release-notes/v0.9.0.md`](https://github.com/ivo-lopes/ivoai/blob/v0.9.0/release-notes/v0.9.0.md).

## v0.8.0

A release estável `v0.8.0` adiciona federação de leitura all-enabled multi-server,
bootstrap reforçado para Debian 12/LXC, o portal de documentação de produção embutido
e conformidade explícita do MCP Web remoto. As notas públicas completas estão em
[`release-notes/v0.8.0.md`](https://github.com/ivo-lopes/ivoai/blob/v0.8.0/release-notes/v0.8.0.md).

Releases estáveis são tags imutáveis geradas pelo GitHub Actions. Cada release contém
binários Linux amd64/arm64, o arquivo da skill Memory/Context e checksums SHA-256.

O portal de documentação expõe `latest` a partir do binário instalado e preserva um
snapshot versionado para cada release documentada. As notas de release descrevem
migrações, comportamento de fallback, limitações conhecidas e instruções de rollback.
