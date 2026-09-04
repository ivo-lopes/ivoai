# Conceitos

- **Executor:** o cliente oficial Codex, Claude Code ou OpenCode responsável por seu próprio login.
- **AUTO:** planejamento ciente de quota e orquestração consultiva de workers. O cliente oficial selecionado permanece como writer autoritativo.
- **Memory:** conhecimento operacional durável fornecido pelo ai-memory.
- **Context:** documentos privados indexados e expostos por meio do Context MCP.
- **Fonte de conhecimento:** um profile `ivoai-server` habilitado, com ID estável e credencial isolados.
- **Federação:** fan-out limitado de leitura pelas fontes selecionadas. Nunca implica replicação de escrita.
- **WorkingContext:** contexto transitório limitado; artifacts exatos continuam recuperáveis por `ResultRef`.
- **CompressionProvider:** Caveman, Headroom ou Direct. Providers nunca são encadeados.
