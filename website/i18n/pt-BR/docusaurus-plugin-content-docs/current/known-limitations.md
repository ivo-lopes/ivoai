# Limitações conhecidas

- O OpenCode permanece como frontend/executor legado explícito e descontinuado.
  Sessões nativas Codex e workers Codex/Claude o excluem mesmo quando instalado.
  Assets legados de release e participantes de rollback do updater permanecem;
  a remoção global continua pendente.
  Configurações pessoais customizadas de provider/plugin/MCP não são importadas em
  sessões controladas. Subagentes nativos e diálogos separados de perguntas são desabilitados.
- A bridge gerenciada exibe o streaming de texto do executor e o estado da sessão. A
  animação nativa de ferramentas, específica do provider na CLI oculta do executor,
  não é reproduzida como uma segunda TUI aninhada.
- OpenViking e NativeOrchestrator v2 são trabalhos futuros e não são os defaults.
- O Ruflo permanece como orquestrador de ciclo de vida limitado nos modos orquestrados atuais.
- O Headroom permanece disponível para compatibilidade e rollback.
- [Conversation Continuity](conversation-continuity.md) oferece retomada nativa,
  recovery limitado e handoff explícito. Históricos já apagados não são recriados.
  Handoff AUTO nativo para Claude e `ivoai codex --planner claude` falham fechado
  até existir contrato verificado de frontend nativo gerenciado. Claude direto
  continua disponível; não há alegação de paridade da TUI nativa Claude. O console
  OpenCode legado não é requisito dos clientes nativos.
- O MCP Web remoto exige uma origem HTTPS publicamente acessível ou o túnel seguro
  compatível da plataforma.
