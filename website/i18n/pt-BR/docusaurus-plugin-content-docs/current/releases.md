# Releases

## Candidata v0.14.0

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
revisado ([CI](https://github.com/ivo-lopes/ivoai/actions/runs/40729664838)); o
combinado Claude/admin/`.3` exige CI próprio e a prova Claude de cinco casos.
Release também deve executar version/help do Core exato com HOME/XDG privados em
ARM nativo e Nehalem amd64 antes de empacotar.
A publicação ainda depende dos gates do fonte final e de Release. Isso não
representa implantação em produção ou migração concluída.

A nova comparação de sete casos preservou 42 tentativas reais corretas: entrada
+2,85% e tempo −1,44%; as metas de 10%/5% foram reprovadas. As 72 tentativas
anteriores continuam preservadas. Outros [12 turnos de edição](https://github.com/ivo-lopes/ivoai/blob/99fe07a93e242d2163116bf6dd8d265c3c8f9d8c/release-notes/benchmark-matrix16-edit-live-validation.md)
passaram nos oráculos declarados, mas reprovaram as mesmas metas: entrada −0,01257%,
tempo das fases −2,28091% e processo −2,46501%. Ambos usaram leitura direta, sem
prefetch/Graft/Gateway. Os 126 turnos históricos possuem condições diferentes e
não são agregados como economia causal. O novo harness não integra esta release.
Os PRs [45](https://github.com/ivo-lopes/ivoai/pull/45),
[46](https://github.com/ivo-lopes/ivoai/pull/46) e
[47](https://github.com/ivo-lopes/ivoai/pull/47) continuam excluídos.
A matriz global é INCOMPLETE. Essas medições
antecedem as mudanças posteriores e não comprovam seu efeito econômico. Remoção
OpenCode, execução adaptativa geral, equivalência Claude, Antigravity e ingestão
Context continuam pendentes. Consulte as [notas bilíngues da candidata](https://github.com/ivo-lopes/ivoai/blob/main/release-notes/v0.14.0.md)
para evidências ligadas ao fonte, compatibilidade e limites de rollback.

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
