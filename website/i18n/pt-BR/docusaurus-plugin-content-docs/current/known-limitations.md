# Limitações conhecidas

- O OpenCode é o frontend gerenciado do AUTO e um executor/worker nativo opcional
  quando sua autenticação oficial e capabilities de ferramentas estão disponíveis.
  Codex/Claude preservam a prioridade; a telemetria de quota nativa pode continuar unknown.
  Configurações pessoais customizadas de provider/plugin/MCP não são importadas em
  sessões controladas. Subagentes nativos e diálogos separados de perguntas são desabilitados.
- A bridge gerenciada exibe o streaming de texto do executor e o estado da sessão. A
  animação nativa de ferramentas, específica do provider na CLI oculta do executor,
  não é reproduzida como uma segunda TUI aninhada.
- OpenViking e NativeOrchestrator v2 são trabalhos futuros e não são os defaults.
- O Ruflo permanece como orquestrador de ciclo de vida limitado nos modos orquestrados atuais.
- O Headroom permanece disponível para compatibilidade e rollback.
- Conversation Continuity e a TUI completa do monitor continuam planejadas.
- O MCP Web remoto exige uma origem HTTPS publicamente acessível ou o túnel seguro
  compatível da plataforma.
