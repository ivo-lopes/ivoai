---
sidebar_position: 17
---

# Console administrativo

O painel administra o mesmo App, configuração e serviços do IVOAI. Conversas e
login por assinatura permanecem nos clientes oficiais; o painel não cria chat,
scheduler ou banco de configuração paralelo.

```sh
ivoai admin init
ivoai admin serve --listen 127.0.0.1:7781
# In another terminal with the same OS user and HOME/XDG roots:
ivoai admin access create --ttl 10m
```

O acesso padrão é somente leitura. Para autorizar alterações, emita uma ativação
separada:

```sh
ivoai admin access create --ttl 10m --write
ivoai admin access list
ivoai admin access revoke ACCESS_ID
```

Substitua `ACCESS_ID` pelo identificador da autorização. O código é mostrado uma
vez e deve ser digitado no formulário OAuth, nunca na URL.

O catálogo cobre os 118 IDs públicos da TUI e acrescenta preview/aplicação de
perfis e ingestão/busca/recuperação documental do Context. As operações são
classificadas em leitura, alteração, orientação para cliente nativo e informação.
Navegação, política fixa e orientação para terminal não contam como operações web
executadas. Configuração, políticas, capacidades, MCPs, conexões, sessões,
serviços, conectores, backup/restauração, códigos de acesso e atualizações usam os
serviços e validações existentes. A configuração exibida é uma projeção sem
segredos, não o TOML privado bruto.

Testar MCP e executar Doctor completo são operações de administração: persistem
saúde/ferramentas, descoberta de capacidades ou cache de quotas. O acesso de leitura
continua consultando snapshots e inventário sem refresh, mas não pode atualizar
esses registros sem preview, confirmação e claim durável da requisição.

Alterações exigem `admin:manage`, origem correta, JSON, CSRF e confirmação explícita
de um preview. A confirmação vale uma vez e está vinculada aos parâmetros, acesso
e revisão da configuração. O ID da requisição é registrado em armazenamento
privado antes da execução: repetição, falha de transporte ou reinício não podem
reexecutar a mesma operação. Os processos do mesmo console compartilham lock de
sistema. Não há retry automático. Em resultado incerto, inspecione estado, auditoria
e recibo antes de autorizar uma nova operação. Revogação cancela o contexto em
execução, mas não desfaz alterações já efetivadas pelo serviço.

O OAuth usa PKCE e cookie HttpOnly/SameSite; não entrega token de atualização ao
browser nem copia credenciais dos clientes oficiais. A auditoria registra somente
ação, resultado e ator com hash. Entradas, saídas brutas, URLs e credenciais não
entram na auditoria. Códigos emitidos deliberadamente aparecem uma vez em um campo
separado. Recibos guardam somente ação/status/horários; o limite de 10000 recusa
novas operações sem apagar o histórico de prevenção de replay.

O processo mantém os privilégios do usuário que o iniciou. HTTP não eleva para
root. Operações privilegiadas de servidor exigem um processo administrativo já
iniciado com os privilégios correspondentes. `admin init` cria apenas roots
privadas ausentes e preserva permissões existentes; não instala clientes nem
altera credenciais ou serviços.

## Publicação com proxy reverso HTTPS

```sh
ivoai admin serve --listen 127.0.0.1:7781 \
  --public-url https://admin.example.com/ivoai \
  --trusted-proxies 127.0.0.1/32,::1/128
```

O listener continua em loopback. O proxy encerra TLS e preserva Host e o caminho
completo `/ivoai/`. A identidade pública vem da configuração, nunca de headers
Forwarded/X-Forwarded arbitrários. O peer real precisa pertencer aos CIDRs
configurados; Host/Origin estrangeiros, traversal e separadores codificados são
recusados. Redirects/cookies incluem o prefixo; em HTTPS os cookies são Secure.
Respostas não permitem cache nem enquadramento por páginas externas.

Use os exemplos versionados `deploy/admin/nginx.conf.example` e
`deploy/admin/Caddyfile.example`. No Nginx, proxy_pass não tem URI final; no Caddy,
handle preserva o prefixo, enquanto handle_path o removeria. Os exemplos evitam
logs de requisições do proxy para proteger queries OAuth e mantêm a auditoria
separada do Core. O exemplo global do Caddy é para instância dedicada; não substitua
configuração de outros sites sem adaptar. Valide com `nginx -t` ou `caddy validate`
antes de recarregar. Os exemplos não comprovam publicação de domínio/certificado
em produção.

Referências oficiais: [proxy_pass do Nginx](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_pass),
[reverse_proxy do Caddy](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy)
e [opções de log do Caddy](https://caddyserver.com/docs/caddyfile/options#log).

## Administração do Context

```sh
sudo ivoai server context ingest --connector approved-documents
sudo ivoai server context search --query 'distinctive phrase from an approved document'
sudo ivoai server context get --id DOCUMENT_ID
```

Essas ações também usam o catálogo administrativo. A ingestão aproveita o serviço,
conectores, embeddings, Qdrant e catálogo atuais. A execução manual e o daemon
periódico compartilham lock; concorrência é recusada, sem retry implícito. O recibo
inclui IDs, fonte, hashes e verificação de recuperação exata pelo catálogo. Corpus
vazio/não comprovado permanece BLOCKED; documentos identificados recuperados podem
ser PARTIAL. Para concluir IVOAI-175 ainda é preciso comprovar busca específica e
recuperação exata no deployment real.

O pipeline mantém sua semântica de falha parcial: índice e catálogo não são uma
transação distribuída. Corrija a causa antes de autorizar nova ingestão. Conteúdo
recuperado é dado não confiável, nunca instrução de execução. Os MCPs do agente
permanecem somente leitura; escrever Memory não sincroniza Context automaticamente.

Os testes do backend exercitam App/Store reais, separação de scopes, CSRF,
confirmação/replay/revogação, redaction, TLS/proxy/prefixo/OAuth/logout e o pipeline
Context em armazenamento determinístico. A prova no browser e a matriz final
pertencem à composição com a UI. Cobertura do catálogo não comprova execução de
todas as operações, inferência oficial, corpus Qdrant ou sincronização em produção.

Saídas maiores que o limite visual de 256 KiB oferecem download explícito da
**projeção integral sanitizada**, retida por 15 minutos no ArtifactStore existente.
A credencial, cliente/grant OAuth e recurso originais precisam continuar
autorizados; conhecer a URL não concede acesso. Logout/revogação bloqueiam o
download. Saída bruta, credenciais e códigos de uso único não são arquivados.
A captura tem limite de 16 MiB, com quota de 64 MiB por credencial e 256 MiB por
instância. Captura incompleta é inteiramente omitida; falhas de armazenamento recebem aviso explícito,
sem referência de conteúdo completo. Artefatos expirados ficam inacessíveis e são
coletados na próxima retenção. Não repita uma alteração para recuperar texto;
consulte o estado e os registros disponíveis.

O gate opt-in `IVOAI_TEST_NGINX=/caminho/absoluto/nginx go test ./internal/admin
-run TestRealNginxAdministrative -v` executa Nginx real com configuração,
certificado, PID e temporários privados em loopback. Valida TLS, prefixo, OAuth,
CSRF, alteração de configuração pelo App, download e recusas. Não usa nem
substitui configuração pessoal do Nginx e não certifica domínio de produção.
