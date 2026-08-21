# Relatório executivo do ivoai

## Visão geral

ivoai é uma plataforma pessoal para instalar, conectar e operar ferramentas de
inteligência artificial em computadores e servidores Linux. O produto reúne Codex
CLI, Claude Code, Headroom, ai-memory e Ruflo sob uma única experiência, sem exigir
chaves de API pagas para o caminho principal.

O usuário instala o cliente, executa o setup e pode conectar suas próprias contas ou
um servidor ivoai posteriormente. A ausência dessas conexões não impede a instalação
nem torna o ambiente defeituoso.

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

Em terminais simples ou automações, o menu muda automaticamente para entrada
numerada. Todos os recursos continuam disponíveis por subcommands, permitindo uso em
scripts.

Operações demoradas exibem spinner, tempo decorrido ou progresso de download. Saídas
para automação permanecem separadas: dados ficam em stdout e indicadores em stderr.

## Experiência no servidor

O mesmo binário instala a camada server-side em Ubuntu e Debian suportados. Ela
oferece um único gateway HTTPS para enrollment de clientes, memória operacional,
contexto/RAG somente leitura, health checks e administração remota limitada.

Qdrant, embeddings e ai-memory permanecem internos. Não é necessário expor banco de
dados, painel administrativo ou uma API de shell remoto.

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

## Leituras relacionadas

- [Relatório técnico](relatorio-tecnico.md)
- [Arquitetura](architecture.md)
- [Cliente](client.md)
- [Servidor](server.md)
- [Segurança](security.md)

