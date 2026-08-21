# Relatório executivo do ivoai

## Visão geral

ivoai é uma plataforma pessoal para instalar, conectar e operar ferramentas de
inteligência artificial em computadores e servidores Linux. O produto reúne Codex
CLI, Claude Code, Headroom, ai-memory e Ruflo sob uma única experiência, sem exigir
chaves de API pagas para o caminho principal.

O usuário instala o cliente, executa o setup e pode conectar suas próprias contas ou
um servidor ivoai posteriormente. A ausência dessas conexões não impede a instalação
nem torna o ambiente defeituoso.

O mesmo acervo privado também pode ser consultado no ChatGPT Web e no Claude Web por
meio de um conector MCP protegido. Assim, a continuidade não fica limitada ao
terminal: histórico operacional e documentos indexados podem acompanhar o usuário
em diferentes interfaces, sob permissões revogáveis.

## Problemas que o produto resolve

- Reduz a instalação de várias ferramentas a um instalador e um comando de setup.
- Evita edição manual de TOML, JSON, hooks, MCPs, arquivos de token e aliases.
- Mantém Codex e Claude utilizáveis quando memória, contexto, Headroom ou Ruflo estão
  temporariamente indisponíveis.
- Centraliza diagnóstico, atualização, conexões, execução e administração.
- Oferece memória persistente e pesquisa contextual sem depender de OpenAI ou
  Anthropic para embeddings.
- Separa informações pessoais do produto: nenhuma empresa, conta ou infraestrutura
  específica é incorporada ao código.

## Experiência no computador

O comando `ivoai` abre um menu interativo com lettering próprio, resumo de saúde e
navegação por setas. A interface organiza as operações em painel, setup, conexões,
agentes, memória, projeto, configuração e administração server-side.

O layout se adapta à largura e à altura disponíveis. Em uma janela grande, apresenta
lettering, descrições e indicadores completos; em SSH ou telas pequenas, reduz o
cabeçalho e usa uma lista rolável sem ultrapassar os limites do terminal. Redimensionar
a janela não exige reiniciar o aplicativo.

Em terminais simples ou automações, o menu muda automaticamente para entrada
numerada. Todos os recursos continuam disponíveis por subcommands, permitindo uso em
scripts.

Operações demoradas exibem spinner, tempo decorrido ou progresso de download. Saídas
para automação permanecem separadas: dados ficam em stdout e indicadores em stderr.
Cores possuem significado consistente: verde confirma sucesso, amarelo indica
degradação recuperável, vermelho mostra erro, e cyan acompanha trabalho em andamento.
O instalador usa a mesma linguagem visual e termina informando os próximos comandos.

## Experiência no servidor

O mesmo binário instala a camada server-side em Ubuntu e Debian suportados. Ela
oferece um único gateway HTTPS para enrollment de clientes, memória operacional,
contexto/RAG somente leitura, health checks e administração remota limitada.

Qdrant, embeddings e ai-memory permanecem internos. Não é necessário expor banco de
dados, painel administrativo ou uma API de shell remoto.

## Uso no ChatGPT Web e Claude Web

O administrador cria um código temporário com `ivoai server web-access create` e
cadastra a URL pública terminada em `/mcp` como conector. O navegador conduz uma
autorização OAuth e mostra as permissões solicitadas. O código funciona uma vez e não
é reutilizado como token permanente.

O conector pode pesquisar contexto, consultar memória e, quando autorizado, gravar
ou excluir páginas de memória. Contexto institucional continua somente leitura.
Exclusões exigem permissão específica e confirmação do item. O acesso pode ser
listado e revogado pelo administrador a qualquer momento.

A skill distribuída com cada release orienta ChatGPT e Claude a consultar o ivoai
antes de responder sobre decisões, histórico ou estado do projeto. Essa orientação
é preferencial: a plataforma Web continua responsável pela decisão final de usar uma
ferramenta em cada interação.

## Componentes principais

| Componente | Papel |
|---|---|
| Codex CLI | Agente oficial conectado à assinatura ChatGPT |
| Claude Code | Agente oficial conectado à assinatura Claude |
| Headroom | Camada opcional de otimização antes do agente |
| ai-memory | Memória operacional entre sessões e agentes |
| Ruflo | Orquestração segura, workflows e coordenação |
| ivoai gateway | Descoberta, enrollment, autenticação e APIs públicas |
| Context service | Ingestão, fragmentação e pesquisa contextual |
| Qdrant | Índice vetorial reconstruível |
| Embeddings locais | Vetorização CPU-first sem API externa paga |

## Segurança em linguagem simples

- Logins ChatGPT e Claude são realizados pelos clientes oficiais.
- ivoai não captura cookies, senhas ou tokens OAuth desses provedores.
- A credencial do servidor fica em arquivo privado `0600`.
- Códigos de enrollment expiram, funcionam uma vez e são armazenados somente como
  hash.
- Códigos de ativação Web e tokens OAuth também são armazenados somente como hash.
- Permissões Web separam leitura de contexto, leitura, escrita e exclusão de memória.
- Logs e erros passam por redação central de segredos.
- O gateway não possui endpoint para executar comandos arbitrários no host.
- Documentos ingeridos são tratados como dados não confiáveis.
- Falhas opcionais são isoladas e não impedem a abertura dos agentes básicos.

## Operação cotidiana

```sh
ivoai
ivoai status
ivoai doctor
ivoai codex
ivoai claude
```

Conexões deliberadamente realizadas pelo usuário:

```sh
ivoai connect chatgpt
ivoai connect claude
ivoai connect server
```

No servidor:

```sh
sudo ivoai setup --mode server
sudo ivoai server doctor
sudo ivoai server enrollment create
```

Para Web:

```sh
sudo ivoai server web-access create --ttl 10m
sudo ivoai server web-access list
sudo ivoai server web-access revoke <id>
```

## Continuidade e recuperação

O setup é idempotente. Atualizações preservam configuração e credenciais e mantêm um
binário anterior quando rollback é possível. Backups do servidor protegem metadados,
corpus e memória autoritativa; índices vetoriais podem ser reconstruídos.

## Estado e limitações

O baseline atual é Linux-first, com suporte inicial a Ubuntu 22.04+, Ubuntu 24.04+ e
Debian 12+. Windows não faz parte do escopo inicial. macOS depende da validação de
todos os componentes.

Ruflo opera no perfil seguro: execução direta de providers PAYG e memória durável
duplicada ficam desabilitadas. Conectores externos como Google Drive, S3 e Notion são
extensões futuras; o core funciona sem eles.

ChatGPT Web e Claude Web exigem que o gateway esteja publicado em HTTPS válido. A
disponibilidade de conectores customizados e skills também depende do plano e das
políticas do workspace de cada provedor.

## Sessões observáveis e orquestradas

O usuário pode continuar abrindo Codex e Claude da forma mais simples, sem qualquer
interferência do Ruflo. Quando precisa acompanhar uma atividade, escolhe uma sessão
direta; quando precisa delegar revisões ou testes, escolhe explicitamente uma sessão
orquestrada. O app confirma que a coordenação Ruflo está segura antes de abrir o
agente e permite acompanhar executor, modelo declarado, workers e serviços por
`ivoai monitor --watch`.

Essa evolução não cria um novo chat e não exige chaves pagas. Codex e Claude oficiais
continuam fazendo todo o trabalho inteligente com os logins do usuário. Ruflo mantém
somente a organização temporária; ai-memory continua responsável pela memória
durável e Context continua responsável pelo conhecimento recuperável.

## Leituras relacionadas

- [Relatório técnico](relatorio-tecnico.md)
- [Arquitetura](architecture.md)
- [Cliente](client.md)
- [Servidor](server.md)
- [Segurança](security.md)
