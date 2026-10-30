# IVOAI v0.15.0

Publicada em 2026-10-29 a partir de `8f688e26f0c4570b6a2b30c7e052672f102703f3` (PR57), árvore `ce778501a664b7639e26a4274bba339d11e52710`.

Esta entrega incremental consolida o contexto gerenciado de MCP: inventário e execução compartilham cwd, ambiente, confiança e configurações capturados. Preserva a independência do OpenCode no caminho nativo padrão, as verificações de autorização e as correções dos fixtures de cleanup/deadline. Inclui documentação EN/PT e site embarcado atualizados.

**Migração:** omita `--ignore-user-config` e os overrides nativos `-C`/`--cd` nas sessões gerenciadas; selecione o workspace com `cd` no shell. Compatibilidade OpenCode explícita, configurações, credenciais e históricos do usuário permanecem preservados.

## Validação

- CI final da candidata: 16 jobs passaram na tentativa1, head `1ac2f0fc`.
- [Workflow Release40817035302](https://github.com/ivo-lopes/ivoai/actions/runs/40817035302): cinco jobs passaram, incluindo ARM64 nativo e smoke amd64/QEMU sem AVX.
- Verificação independente após download: 12 assets, 12 digests/tamanhos da API e 11 checksums de payload conferem; metadados dos componentes, dependências, arquivos internos e pacote de skills passaram nos leitores limitados.
- Core amd64 baixado: `version` retornou `0.15.0` e `help` terminou com código0 em HOME descartável. ARM não foi executado localmente.
- Qualificação restrita oficial/Core com provedor falso: 4 eventos PASS, zero falhas/skips, sem login ou inferência real. Não certifica paridade geral de TUI, autenticação ou modelos.

Relatório independente de assets: SHA256 `c6cf95e97ce7b18c5e27d52f812a607e8fdf160324e971e52c0ec0b6e18a338c`. A verificação associa metadados e bytes publicados; não é uma atestação de build reproduzível. Assets não foram substituídos.

## Estado do roadmap

**Economia global não aprovada.** As metas agregadas de10% em entrada e5% em tempo não foram comprovadas; comparações desfavoráveis permanecem registradas. Nenhuma economia é inferida dos testes de protocolo ou da compressão de logs.

Gateway, criação de evidência protegida e piloto adaptativo continuam com flags experimentais OFF por padrão. G1 Memory segue sem ligação ao App; paridade administrativa e de clientes continua parcial. `CONTEXT_SYNC=BLOCKED` / IVOAI-175; escrita de memória não comprova ingestão documental. A observação LOW da documentação permanece `needs_review`, sem mudança de limiar de auditoria.

PR56, PR63, PR64, a qualificação autenticada de metadados e a evolução Direct-only posteriores à tag estão fora desta release. A pergunta real seguida de quit público normal na mesma sessão continua não testada.

[Escopo, migração e rollback na fonte publicada](https://github.com/ivo-lopes/ivoai/blob/8f688e26f0c4570b6a2b30c7e052672f102703f3/release-notes/v0.15.0.md) · [Qualificação restrita e seus limites](https://github.com/ivo-lopes/ivoai/blob/8f688e26f0c4570b6a2b30c7e052672f102703f3/release-notes/v0.15.0-final-runtime-qualification.md). Esses documentos preservam o checkpoint pré-publicação; o estado publicado e a verificação posterior estão registrados acima.
