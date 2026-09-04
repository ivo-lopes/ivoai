# Início rápido

## Cliente

```bash
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sh
ivoai setup
ivoai doctor
ivoai auto
```

A autenticação dos executores oficiais permanece dentro de cada executor. O IVOAI
não solicita nem copia credenciais de assinatura. O AUTO abre o frontend OpenCode
gerenciado pelo IVOAI e usa como executores esses clientes oficiais já autenticados.

## Adicionar servidores privados

```bash
ivoai connect server add company-a --url https://ai-a.example.com --purpose company-a --code-stdin
ivoai connect server add company-b --url https://ai-b.example.com --purpose company-b --code-stdin
ivoai connect server list
ivoai auto                         # all enabled sources
ivoai auto --knowledge-source company-a
```

## Servidor

Em um host Debian 12 ou Ubuntu compatível:

```bash
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sudo sh
sudo ivoai setup --mode server
sudo ivoai server doctor
```

Consulte [Configuração do servidor](setup.md), incluindo os pré-requisitos de LXC e
a hospedagem da documentação.
