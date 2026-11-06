# Perguntas frequentes

## `ivoai auto` usa todos os servidores conectados?

Perfis habilitados são candidatos à recuperação limitada por propósito. Seletores
`--knowledge-source` restringem a sessão e nunca expandem seus grants.

## Escritas são replicadas para todas as fontes?

Não. A federação aplica-se a leituras; uma nova escrita ambígua falha fechada.

## O IVOAI precisa de chaves de API do provedor?

Fluxos suportados usam a autenticação nativa autorizada do próprio usuário.
OpenCode gerenciado foi removido. Experimentos headless continuam opt-in, sem
implicar controle da TUI Claude ou certificação sem a conta do usuário. Nenhum
fallback para API paga é introduzido automaticamente.

## Headroom foi removido?

Não. O componente e a escolha explícita permanecem para compatibilidade e rollback.
Wrapping gerenciado inseguro continua indisponível por policy, usando Direct.
