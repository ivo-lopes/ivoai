# Referência da CLI

<!-- GENERATED-CLI-HELP:START -->
```text
ivoai - personal AI client and server platform

Usage:
  ivoai                         interactive menu
  ivoai help | version | status | uninstall
  ivoai setup [--mode client|server]
  ivoai doctor [--json] [--inventory]
  ivoai update [--dry-run] | update --rollback [--force]
  ivoai connect [list|chatgpt|claude]
  ivoai connect server [--url URL] [--purpose PURPOSE] [--redundancy-group GROUP] [--priority N] [--enrollment-code CODE|--code-stdin]
  ivoai connect server add <alias> [--url URL] [--purpose PURPOSE] [--redundancy-group GROUP] [--priority N] [--enrollment-code CODE|--code-stdin]
  ivoai connect server list [--json] | show <alias> [--json] | test <alias> [--json]
  ivoai connect mcp [list] | add <name> <https-url> | remove <name>
  ivoai disconnect <chatgpt|claude|server [alias|--all]>
  ivoai codex [--knowledge-source <alias|purpose>] [-- agent arguments...]
  ivoai claude [--knowledge-source <alias|purpose>] [-- agent arguments...]
  ivoai opencode [--knowledge-source <alias|purpose>]
  ivoai auto [--planner codex|claude] [--knowledge-source <alias|purpose>] [-- agent arguments...]
  ivoai session start --executor <codex|claude|opencode> --mode <direct|orchestrated> [--knowledge-source <alias|purpose>] [-- agent arguments...]
  ivoai session list [--json] | show [--json] <id> | stop <id>
  ivoai monitor [--watch] [--session <id>] [--json]
  ivoai memory [status|configure]
  ivoai config [show|set <key> <value>]
  ivoai project [init|status]
  ivoai server setup | status | doctor | start | stop | restart
  ivoai server logs [service]
  ivoai server enrollment create [--ttl 10m] | list | revoke <id>
  ivoai server web-access create [--ttl 10m] [--scopes SCOPE,...] | list | revoke <id>
  ivoai server connector list | add --name NAME --type filesystem|git --path PATH | remove NAME
  ivoai server context status | memory status | docs status | docs configure --listen IP:PORT | docs serve
  ivoai server gateway serve | gateway configure --public-url HTTPS_ORIGIN [--listen HOST:PORT] [--trusted-proxy CIDR] [--tls-cert PATH --tls-key PATH]
  ivoai server backup [--output PATH] | restore --input PATH
  ivoai server remote status | doctor | connector list
```
<!-- GENERATED-CLI-HELP:END -->

O bloco acima é gerado a partir da saída canônica de help do executável e verificado no CI por
`scripts/generate-cli-reference.sh --check`.

## Flag de fonte da sessão

Repita `--knowledge-source` ou use aliases/purposes separados por vírgula. Sem a flag, todos os
profiles de server conectados e habilitados são selecionados. Com qualquer flag, ela se torna um
filtro restritivo. Fontes explícitas desconhecidas, desabilitadas ou desconectadas falham.

## Defaults públicos e efeitos

| Superfície | Default / efeito |
| --- | --- |
| `setup --mode` | `client`; uma instalação server existente é detectada e preservada quando a flag é omitida |
| `auto --planner` | seleção interativa/default do planner; somente `codex` e `claude` são overrides válidos |
| `session start --executor` | `codex` |
| `session start --mode` | `direct` |
| `connect server add --purpose` | o alias validado |
| `connect server add --priority` | `100`; valores menores vencem dentro de um redundancy group |
| `doctor --inventory` | adiciona inventário de compatibilidade sanitizado; combine com `--json` para automação |
| `update --dry-run` | prepara e testa sem commit |
| `update --rollback` | restaura o último checkpoint transacional; `--force` só é válido com rollback |
| `monitor --watch` | atualiza até o cancelamento; `--session` restringe a sessão exibida |
| `server enrollment/web-access create --ttl` | `10m` |
| `server backup --output` | path de backup gerenciado com timestamp |
| `server docs status` | testa o listener de produção configurado, default `0.0.0.0:7780` |

`--enrollment-code` é aceito por compatibilidade, mas expõe um segredo nos argumentos do processo;
prefira `--code-stdin` ou o prompt interativo oculto. Flags JSON escrevem JSON legível por máquina
sem banners ou ANSI. Erros de validação e runtime são escritos em stderr; valores sensíveis são
redigidos.

## Documentação do server

`ivoai server docs status` reporta o listener configurado; `ivoai server docs serve` é reservado
para a unidade systemd gerenciada. O default é `0.0.0.0:7780`.

## Variáveis de ambiente públicas

- `NO_COLOR=1`: desabilita saída ANSI.
- `IVOAI_ASCII=1`: usa renderização ASCII no terminal.
- `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`: realocam estado pertencente ao usuário.
- `IVOAI_VERSION`: seleciona uma release no instalador bootstrap.
- `IVOAI_INSTALL_DIR`: seleciona o destino do instalador.

Variáveis internas de capability da sessão e variáveis apenas de teste não são interfaces públicas
intencionalmente. Comandos retornam zero em sucesso, diferente de zero em falha de
validação/runtime e 130 em cancelamento quando o ciclo de vida do executor oferece suporte.
