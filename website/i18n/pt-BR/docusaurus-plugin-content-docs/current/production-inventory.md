# Inventário de produção somente leitura

Atualmente, o IVOAI possui duas instalações de produção, mas este ambiente de
desenvolvimento não contém hostnames documentados nem um caminho de acesso para ambas.
Nenhum host de produção foi contatado durante a criação desta base de compatibilidade.

## Coleta compatível

Execute isto de forma independente em cada host de produção, com a mesma conta que
normalmente opera o IVOAI:

```sh
ivoai doctor --inventory --json > ivoai-inventory.json
```

Para uma instalação de servidor gerenciada por root:

```sh
sudo ivoai doctor --inventory --json > ivoai-inventory.json
```

O comando é somente leitura e informa formato/versão, sistema operacional/arquitetura,
modo cliente ou servidor, executável e raízes XDG, schemas de configuração/estado/
ownership, protocolo do servidor, versão/caminho/ownership dos componentes, indicadores
booleanos de conexão, um resultado limitado de integridade do inventário, estado dos
serviços, tipo de backend, proveniência da instalação e disponibilidade de rollback.
Ele deliberadamente não executa todo o Doctor, pois sondagens de capability/cota dos
providers podem atualizar caches locais. Ele nunca lê `secrets.json`, bancos de dados
de autenticação dos providers, prompts, respostas, cookies, access tokens, refresh
tokens nem variáveis de ambiente brutas.

Antes de anexar um inventário a uma issue, substitua nomes de usuário e caminhos
específicos do host caso identifiquem uma pessoa ou organização. Não anexe arquivos
dos providers nem arquivos `.env` do servidor. O JSON deve ser coletado uma vez em
cada produção e identificado como `PROD-1` e `PROD-2`; hostnames e IPs não precisam
entrar no repositório.

## Evidências obrigatórias antes de um canário

Compare os dois inventários quanto a:

- versão e proveniência da release do IVOAI;
- papel cliente/servidor, sistema operacional, arquitetura e caminho do executável;
- versões dos schemas de configuração/estado/ownership;
- ownership e versões de componentes gerenciados em comparação com externos;
- estado legado do Headroom/Ruflo;
- metadados dos backends de memory/context e integridade dos serviços;
- resultado do inventário, evidências coletadas separadamente de status/Doctor e
  disponibilidade de rollback;
- método de instalação e qualquer divergência em relação às fixtures canônicas da v0.5.0.

O baseline canônico sanitizado está em `tests/fixtures/v0.5.0`. Ele foi derivado da
tag real, não de nenhuma das instalações de produção, e não deve ser tratado como
prova dos hosts ativos.
