# Roteamento por quota de assinatura

O quota manager normaliza telemetria estruturada dos clientes oficiais respaldados
por assinatura. Ele nunca lê, armazena, copia nem registra tokens OAuth, cookies,
headers de auth, respostas brutas de auth ou credenciais dos provedores.

## Fontes

### Codex

O ivoai inicia o `codex app-server --stdio` gerenciado e confiável, realiza a
inicialização JSON-RPC documentada e chama `account/rateLimits/read`. A resposta
oficial pode expor:

- janelas móveis com sua duração oficial (por exemplo, 300, 60 ou 1440 minutos);
- uma janela de sete dias/semanal quando a duração é 10080 minutos;
- uma janela provider-wide `individualLimit` cuja cadência não é inferida;
- outras janelas nomeadas ou com escopo de modelo;
- sinais de hard rate-limit ou spend-control e timestamps de reset.

O probe remove do ambiente todas as chaves PAYG conhecidas e overrides de base URL de
provedores. Ele limita tempo de execução, stderr, tamanho de linha e tamanho da
resposta. Não faz scraping da TUI do Codex.

### Claude Code

A capacidade de autenticação é verificada com `claude auth status` estruturado;
somente `loggedIn`, `apiProvider` e `authMethod` são analisados. Autenticação por API
key não é elegível para roteamento automático por assinatura. Durante uma sessão
Claude automática, um comando privado de statusline, válido somente para aquela
invocação, recebe o payload estruturado oficial do Claude e extrai:

- modelo atual;
- percentual usado/restante da janela de contexto;
- quota de cinco horas/sessão;
- quota de sete dias/semanal;
- quota mensal somente se um cliente atual/futuro compatível a fornecer explicitamente.

O wrapper de statusline local à sessão compõe um comando existente do usuário/projeto
quando consegue lê-lo com segurança, de modo que a captura de quota não remova
silenciosamente a statusline normal do usuário. As configurações persistentes do
Claude nunca são regravadas.

O payload Claude atualmente compatível não exige uma janela mensal de assinatura.
Por isso, o monitor primary não renderiza uma linha mensal do Claude. O ivoai não faz
scraping de claude.ai, sessões do navegador, saída ANSI da UI, Cloudflare nem tokens
internos.

## Normalização

Todos os percentuais são limitados ao intervalo `0..100`. Para fontes que informam
percentual usado:

```text
remaining = clamp(100 - used)
```

Cada janela registra tipo, duração oficial quando exposta, modelo opcional,
usado/restante, horário de reset opcional, fonte, horário da observação, autoridade,
disponibilidade e estado da telemetria. Janelas de contexto, rolling, sessão,
semanal, individual, mensal, por modelo e de créditos são distintas. `pending`
significa que o Claude ainda não retornou limites; `not_exposed` significa que um
payload estruturado posterior omitiu o campo; `stale` preserva um valor antigo com
alerta explícito; e `exhausted` é um zero autoritativo. Somente o último estado
bloqueia o roteamento. Primary e secondary são tratados como slots de transporte: a
ordem deles não define semântica. Uma janela Codex de 300 minutos é exibida como 5h;
10080 como semanal; 60 como 1h; e 1440 como 1d. Durações desconhecidas continuam
representáveis.

## Elegibilidade e roteamento

Um provedor é elegível quando sua autenticação de assinatura é válida e nenhum hard
limit autoritativo foi atingido. Um zero com escopo de modelo bloqueia somente o
modelo exato nomeado pela telemetria autoritativa; não desabilita outro modelo nem
uma rota não especificada do provedor. O manager resolve primeiro o provedor
preferido e depois a alternativa. Ele retorna decisão e motivo explícitos; callers
não podem ignorá-los.

O roteamento automático de tarefas adiciona um capability registry acima desse gate.
Nomes de modelos Codex, flags padrão e níveis de raciocínio compatíveis vêm apenas da
resposta estruturada oficial `model/list` do app-server. A CLI validada do Claude
expõe níveis de esforço compatíveis, mas nenhum catálogo estruturado equivalente;
portanto, seu modelo permanece o padrão do cliente. Entradas do cache de capabilities
são vinculadas à versão do cliente e invalidadas quando ela muda.

Para cada tarefa, o IvoAI primeiro determina o tier necessário a partir do score do
objetivo e então encontra o menor modelo suficiente e esforço compatível no catálogo.
Um zero exato por modelo rejeita somente aquele modelo; outro modelo suficiente do
mesmo provedor é tentado antes do provedor alternativo. Se mais de um perfil atingir
o piso de qualidade, a quota restante autoritativa pode preservar o provedor mais
restrito. Pressão de quota nunca permite um perfil abaixo do tier exigido. Telemetria
desconhecida continua desconhecida em vez de virar zero.

A progressão LIGHT → BALANCED → STRONG → MAX não é um mapeamento de fornecedor. IDs
de modelo nunca são hardcoded a partir dos nomes dos tiers. A escalada avança apenas
um tier depois de falha de validação baseada em evidência ou reavaliação de risco.

O dispatch gate é executado:

- antes de iniciar o primary da conversa;
- periodicamente enquanto o primary está ativo;
- antes de cada worker;
- depois que cada worker termina;
- após um sinal de hard limit em runtime;
- imediatamente antes do failover.

Falhas de autenticação tornam um provedor indisponível. Hard limits confirmados
acionam fallback. Falhas de rede/probe preservam metadados stale limitados, rotulam-nos
claramente como stale, retornam o erro do probe e não inventam exaustão.

Um `ivoai connect chatgpt|claude` explícito é uma fronteira de contexto de
autenticação: o ivoai invalida apenas a quota desse provedor antes do fluxo oficial de
login/status e força um probe após o sucesso. `disconnect` invalida o mesmo provedor
sem fazer logout no cliente oficial. Isso evita deliberadamente fingerprints de token
ou um ID de conta inventado. Se o probe após login falhar, a quota da conta anterior
não é restaurada. Snapshots legados v0.5.0 sem duração continuam legíveis; nenhum
valor de sessão legado é adivinhado como janela Codex de cinco horas.

## Cache e segurança

O intervalo padrão de atualização é 45 segundos, e a configuração válida fica entre
30 e 300 segundos. `$XDG_STATE_HOME/ivoai/quota/snapshot.json` contém somente
percentuais normalizados, metadados de reset/fonte/observação e elegibilidade. É um
arquivo atômico `0600` em diretório `0700`. Um lock no-follow `0600` serializa escritas
concorrentes do supervisor Codex e da statusline Claude. Os arquivos têm tamanho
limitado e rejeitam symlinks, escapes de terminal, quebras de linha, campos com forma
de segredo, percentuais inválidos e provedores desconhecidos.

`ivoai status` apenas lê esse cache. `ivoai doctor` verifica ativamente as fontes.
`ivoai monitor --watch` lê snapshots da sessão e pode ser executado
independentemente da TUI oficial.

A observabilidade de sessão é uma lista aditiva e limitada de eventos com campos
explícitos para componente, operação, estado, IDs de correlação, duração e motivos
canônicos de roteamento/fallback. O schema não possui campos para prompts, respostas,
artefatos, headers, ambientes ou credenciais. Motivos pertencem a uma allowlist, e
input com formato de segredo é reduzido a um sentinel redigido.
