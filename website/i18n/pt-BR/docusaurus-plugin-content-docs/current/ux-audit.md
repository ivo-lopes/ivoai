# Auditoria da UX de terminal gerenciada

## Connections multi-server (v0.9.7)

O par legado Connect/Disconnect de default único foi substituído por **IVOAI Servers**.
Add somente cria; cada profile oferece Test, Enable/Disable, Edit metadata,
Re-enroll e Remove confirmado. CLI e TUI usam ServerConnector/ServerPool e a
secret store existente por ID estável. Não existe configuração paralela da TUI.
Enrollment não é apresentado como health live. Testes explícitos ficam em cache
por um minuto; após restart, o estado volta a not_probed.

Tokens de cor existentes, fonte do terminal, alinhamento à esquerda, teclado,
largura adaptativa e NO_COLOR foram preservados, sem nova paleta ou decoração.
A confirmação destrutiva identifica o alias. Labels distinguem enabled, conexão,
health e metadados da credencial sem depender apenas de cor. O smoke com fixtures
cobre launcher real, teclado, entrada oculta, persistência, falhas e remoção seletiva.

Validada para a experiência AUTO OpenCode-first em 2026-09-04. A regra orientadora é
um único modelo de estado autoritativo: dados de sessão, executor, quota e conhecimento
têm origem no IVOAI e são renderizados pelo frontend gerenciado. Saídas JSON e non-TTY
nunca recebem branding nem decoração ANSI.

## Hierarquia da informação

O caminho normal é `ivoai auto`: preflight, seleção de conhecimento, backend privado
e então a TUI OpenCode com tema IVOAI. O rodapé da tela inicial mostra somente um
resumo acionável do estado; a barra lateral mostra as fontes de conhecimento;
`/ivoai` abre o painel completo e limitado. Símbolos são sempre acompanhados por
palavras, de modo que o estado nunca dependa apenas de cor.

| Campo da UI | Fonte autoritativa | Evidência ausente |
| --- | --- | --- |
| versão | metadados de build do binário | `N/A` |
| frontend | metadados da sessão | `N/A` |
| executor primary | scheduler de quota/sessão | `Unknown` |
| worker | task ledger/runtime | `Not available` |
| autenticação | probe de auth do executor oficial | `authentication required` |
| quota | quota manager | `N/A` |
| Memory / Context | bootstrap de conhecimento da sessão | `degraded` |
| servidores | ServerPool + observações do router da sessão | `not-probed`; enrollment não prova saúde atual |
| permissões do OpenCode | configuração de inicialização da sessão | `interactive` por padrão |
| escopo de conhecimento | seleção do Knowledge Router | `automatic` ou `restricted` |
| compressão | policy do CompressionProvider | `Unknown` |
| skills | Skill Registry | `Not available` |

## Inventário de menus e rotas

| Superfície | Decisão | Justificativa no modo gerenciado |
| --- | --- | --- |
| Launcher IVOAI: Automatic | KEEP / PRIMARY | Abre o frontend OpenCode sob controle do IVOAI. |
| Launcher IVOAI: Codex | KEEP | Escape hatch explícito para a TUI oficial do Codex. |
| Launcher IVOAI: Claude | KEEP | Escape hatch explícito para a TUI oficial do Claude Code. |
| Launcher IVOAI: OpenCode | RENAME | Abre o frontend OpenCode gerenciado do IVOAI. |
| `ivoai opencode` | KEEP | Mesmo control plane gerenciado do AUTO. |
| provider OpenCode standalone | MOVE | `session start --executor opencode --mode direct` explícito. |
| status / doctor | KEEP | Diagnósticos operacionais e legíveis por máquina. |
| monitor | KEEP | Visão read-only existente do runtime; não é duplicada no painel da TUI. |
| OpenCode `/ivoai` | ADD | Estado completo de sessão, executor, quota e fontes. |
| rodapé/barra lateral da home do OpenCode | ADD | Escopo e saúde persistentes e compactos. |
| OpenCode `/connect` | HIDE BY CONFIG | Somente o provider IVOAI fica habilitado; a autenticação dos executores permanece oficial. |
| seletor de modelo/provider do OpenCode | KEEP / GROUP BY CONFIG | AUTO mais modelos explícitos descobertos por capabilities e variantes nativas de reasoning; sem login duplicado. |
| política de aprovação do OpenCode | ADD | `opencode.permission_mode` persistente no IVOAI; `/ivoai` mostra o modo da sessão. Alterações valem na próxima sessão. |
| share do OpenCode | DISABLE IN MANAGED MODE | Impede publicação acidental da conversa. |
| auto-update do OpenCode | DISABLE IN MANAGED MODE | O pin e o rollback da supply chain permanecem autoritativos. |
| update do OpenCode | HIDE IN MANAGED MODE | O IVOAI atualiza o componente pinado. |
| seletor de sessão | KEEP | Sessões OpenCode são mapeadas para IDs limitados de sessão dos executores IVOAI. |
| seletor de tema | KEEP | O tema IVOAI é instalado e selecionado; a acessibilidade permanece disponível. |
| menus de `opencode` direto | KEEP UPSTREAM | O IVOAI não altera o uso fora do modo gerenciado. |

## Política de terminal responsivo

O IVOAI usa o renderer e os slots de plugin compatíveis do OpenCode, em vez de fazer
parsing de ANSI ou automatizar teclas. Aliases e purposes longos são sanitizados e
limitados; o painel mostra no máximo oito linhas de fontes mais a contagem restante.
O OpenCode é responsável por largura Unicode, resize, foco, cursor e limpeza do
terminal. `TERM=dumb`, `NO_COLOR`, pipes e JSON continuam pelas superfícies non-TUI
existentes do IVOAI.

## Inventário estável de ações públicas

Cada ação abaixo é coberta pelo teste de regressão da árvore de menus. As decisões são
`KEEP`, exceto quando a tabela acima diz explicitamente `RENAME`, `MOVE`, `HIDE`,
`DISABLE` ou `ADD`; entradas de seções aninhadas são agrupadas no launcher em vez de
duplicadas no nível superior.

```text
auto
status doctor doctor.inventory version
setup update.dry-run update rollback uninstall
connect.list connect.chatgpt disconnect.chatgpt connect.claude disconnect.claude connect.server
servers.list servers.add servers.manage servers.test servers.toggle servers.edit servers.re-enroll servers.remove
mcp.list mcp.add mcp.remove mcp.auth mcp.auth.remove mcp.header mcp.test
mcp.tools mcp.enable mcp.policy mcp.direct-policy
launch.codex launch.claude launch.opencode
memory.status memory.configure
session.direct.codex session.direct.claude session.direct.opencode session.orchestrated.codex session.orchestrated.claude
session.list session.monitor session.stop
project.status project.init
config.show config.headroom config.memory config.ruflo config.auto config.auto-planner config.auto-failover config.auto-checkpoint
config.auto-strategy config.auto-parallel config.auto-bootstrap config.auto-escalation config.session-mode config.primary config.reviewer config.workers
config.auto-gate config.auto-plan
config.auto-policy policy.sources policy.concurrency policy.worker-cap policy.writes policy.provider policy.low-quota policy.primary policy.mcp policy.confirmation
capabilities.update capabilities.ponytail capability.inspect capability.enable capability.disable capability.pin capability.unpin capability.update capability.rollback
server.setup server.status server.doctor server.start server.stop server.restart server.logs
server.enrollment.create server.enrollment.list server.enrollment.revoke
server.web-access.create server.web-access.list server.web-access.revoke
server.connector.list server.connector.add server.connector.remove server.context.status server.memory.status
server.gateway.configure server.backup server.restore
remote.status remote.doctor remote.connector.list
```

## Sistema visual

O tema da TUI usa superfícies neutras ink/slate, um azul primário contido e âmbar como
destaque. As cores semânticas de sucesso, alerta e erro possuem marcadores textuais.
As paletas clara e escura são criadas independentemente, com texto, bordas e cores de
diff em alto contraste. O lettering IVOAI usa o slot oficial `home_logo`; o binário,
a licença, os notices e a proveniência do OpenCode upstream não são alterados nem
ocultados.
