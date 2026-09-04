# Perguntas frequentes

## O `ivoai auto` usa todos os servidores conectados?

Sim. Sem `--knowledge-source`, todos os profiles conectados e habilitados são
selecionados para leituras limitadas. A flag restringe a sessão exatamente aos
aliases/purposes informados.

## As escritas são replicadas para todas as fontes?

Não. A federação automática se aplica às leituras. Novas escritas ambíguas falham de
modo seguro.

## O IVOAI precisa de chaves de API dos providers?

Não nos fluxos compatíveis do Codex, Claude Code e OpenCode controlados por assinatura.

## O Headroom foi removido?

Não. Ele permanece disponível como provider de compatibilidade e rollback.
