# Limitações conhecidas

- Primary adaptativo, reutilização CodeIntel no Gateway e hosts headless continuam
  opções experimentais explícitas, OFF por padrão. Startup/version ou teste offline
  não certifica inferência, paridade TUI ou execução ARM.
- OpenCode gerenciado foi removido. Comandos antigos orientam a migração; dados,
  perfis e assets históricos são preservados para rollback. Nomes compatíveis de
  pacotes/contratos não restauram execução.
- O picker Codex aceita seleções Codex revisadas. Contratos Claude de worker ou
  headless não comprovam controle Claude por essa TUI. Inferência Claude permanece
  sem validação sem conta autorizada do usuário. Antigravity headless limita-se às
  provas de runtime/plataforma registradas separadamente.
- Claims e histórico duráveis preservam continuidade elegível, mas não recriam
  histórico nativo apagado por clientes antigos. Falhas, cancelamentos, registros
  estrangeiros e mutações incertas exigem conciliação segura, sem replay implícito.
  Veja [continuidade](conversation-continuity.md).
- Cache do Gateway é isolado e local ao processo. Integração de leitura de código
  não significa mediação universal de Memory/Context/outputs ou compartilhamento
  Primary→workers. Cache reportado, redução de output e economia da sessão diferem.
  O gate econômico global permanece INCOMPLETE.
- Headroom e suas escolhas explícitas permanecem para compatibilidade; wrapping
  gerenciado inseguro continua recusado por policy. Caveman e Direct não são
  encadeados com ele.
- OpenViking continua opcional/experimental. Gravar Memory não comprova sincronismo
  documental do Context; mantenha BLOCKED/PARTIAL até validar ingestão e recuperação.
- O painel web é administrativo, usa o mesmo Core e não cria chat/scheduler. Web
  MCP remoto exige HTTPS público suportado ou túnel seguro da plataforma.

Veja [executores](executors.md) para comandos suportados e limites explícitos.
