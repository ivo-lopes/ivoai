# Releases

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
