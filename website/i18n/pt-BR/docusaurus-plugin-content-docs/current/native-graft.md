# Inteligência de código com Graft nativo

O IVOAI controla a inspeção local de código, sua autorização e seu orçamento de
recursos. O Graft é uma biblioteca estrutural substituível atrás de
`CodeIntelligenceProvider`; ele não controla a sessão, escolhe executores nem se
torna outro servidor de conhecimento. A ordem das fontes institucionais permanece
**Memory → Context**. Resultados de compiladores e testes continuam sendo a
evidência de comportamento executável.

Esta página descreve o contrato implementado de provedor, sandbox, gateway e build.
A integração com o lançamento da aplicação e a matriz final de aceite dos clientes
nativos ainda estão sendo concluídas neste candidato. Resultados de benchmark estão
pendentes; esta página não afirma ganhos medidos de latência, memória ou tokens.
A validação real com Claude continua pendente no IVOAI-188 e não é comprovada por
fixtures locais nem por testes com Codex.

## Implementação gerenciada e ciclo de vida

O upstream revisado é `trailhq/Graft`, versão **0.21.1**, commit
`fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad`, sob licença MIT. A identidade gerenciada
pelo IVOAI é **0.21.1-ivoai.1**. O SHA-256 do tar de origem é
`ab27f9123eeb59b3987a342a24b7e357ad3412f41c556fde3a76ca9e250d2db1`;
o SHA-256 do lock npm é
`c05064ea18ed1b538e27cc788f1d6ea2cae00b2c00c79ef552fc1bb4c54fd5f5`.

O build de release instala as dependências fixadas com scripts de ciclo de vida
desabilitados, compila explicitamente os dez bindings nativos com os headers do
Node gerenciado **26.11.1** e gera o viewer e as queries das gramáticas. O patch de
compatibilidade nativa altera duas flags C++17 do `tree-sitter@0.21.1` para C++20,
exigido por esses headers. A compilação usa bubblewrap sem rede; o isolamento de
runtime usa o sandbox Go descrito abaixo. O CI seleciona GCC 12 em runners nativos
Linux amd64 e arm64. As flags amd64 excluem AVX/AVX2; o QEMU Nehalem exercita o Node
e a biblioteca reais, incluindo as gramáticas nativas e WASM.

O empacotamento rejeita bindings que exijam versões superiores a **GLIBC 2.35**,
**GLIBCXX 3.4.30** ou **CXXABI 1.3.13**. Selecionar o compilador não comprova, por
si só, compatibilidade. O arquivo contém a biblioteca compilada, dependências de
runtime, assets do viewer e queries, licenças, inventário de integridade das
dependências, SHA-256 por arquivo, evidência de ABI e proveniência do build. Os
metadados de release vinculam origem, entradas revisadas do build e digest do
arquivo final à identidade imutável do componente. Os gates de arquitetura nativa,
ABI e execução sem AVX precisam passar no CI; o experimento local com GCC 14 foi
corretamente rejeitado pelo gate de ABI.

O fluxo final previsto da aplicação não exige ativação separada do Graft,
instalação do CLI upstream nem inicialização manual do projeto. Uma release que
contenha os metadados revisados participa dos fluxos normais de `ivoai setup`,
`ivoai update` e promoção/rollback de componentes gerenciados. Consulte
[Atualização e rollback](update-rollback.md). Builds de desenvolvimento sem esses
metadados informam Graft indisponível, em vez de instalar um substituto não revisado.
Um executável `graft` encontrado no ambiente não é adotado. O `graft-runtime`
empacotado pelo IVOAI aceita somente `--version`; as operações usam o adaptador
privado controlado pelo Go. Configurações pessoais de provedores e credenciais
não são modificadas nem importadas.

## Escolha da menor inspeção suficiente

A capability do host é `code_inspect`. Ela oferece busca limitada de símbolos,
ocorrências literais, inspeção da API de arquivos, chamadores, mapas do repositório,
análise de impacto, verificação de atualização, builds explícitos e exportações
privadas de visualização. Os aliases nativos `graft_find_code`, `graft_find_all`,
`graft_trace_calls`, `graft_file_api`, `graft_repo_map` e
`graft_check_freshness` preservam o escopo do mesmo gateway. Eles não expõem um
servidor MCP upstream nem permitem escolher outra raiz, organização ou escopo de
permissões.

A inspeção automática pode responder diretamente a uma busca literal. Na inspeção
da API de um arquivo pequeno identificado, pode retornar o arquivo completo quando
ele couber em metade do orçamento de resultado solicitado. Relações e questões
estruturais maiores usam Graft. Esse roteamento evita um subprocesso estrutural
quando a evidência local exata já basta. Os custos usados nessa escolha são
estimativas, não economias observadas. Um classificador conservador de tarefas
pode antecipar evidência limitada de código; ele não concede capabilities nem
substitui a pesquisa institucional em Memory/Context.

Enriquecimento `deep`/LLM e subprocessos LSP ficam desabilitados até existir uma
integração segura e revisada pelos clientes oficiais. O adaptador gera somente o
grafo, sem integração cloud, hooks do repositório, carregamento de configuração de
provedores ou inicialização do CLI upstream. Evidência estrutural ausente continua
ausente; uma ocorrência literal não comprova uma relação de chamada nem uma análise
de impacto completa.

## Escopo, snapshots e cobertura

O host fornece a raiz canônica da worktree, o escopo de organização/conhecimento e
a identidade de permissões. Os três compõem a chave do cache privado; a ausência de
escopo confiável causa rejeição. Texto do repositório, argumentos de ferramentas e
nomes de diretórios parecidos com organizações não concedem autoridade. Worktrees
ou escopos de permissões distintos não compartilham um índice apenas por conterem
a mesma revisão Git.

O inventário usa arquivos rastreados e arquivos não rastreados que não sejam
ignorados pelo Git, com hooks e fsmonitor desabilitados e sem configuração Git
global ou de sistema. Repositórios aninhados permanecem separados. Somente
extensões de fonte admitidas e `go.mod` entram no inventário; caminhos ocultos,
dependências, diretórios de build/saída, bundles JavaScript gerados com hash ou
minificados, arquivos não regulares e arquivos grandes demais ficam excluídos.
Linguagens não suportadas, fontes geradas excluídas e arquivos grandes estão fora
da cobertura. Contar arquivos no inventário não comprova que todos receberam
extração estrutural.

O host lê bytes exatos e limitados através de `os.Root`, verifica caminhos e tipos
de arquivo e copia os bytes para um snapshot privado. O Graft recebe esse snapshot,
sem permissão de escrita no repositório original. Digests das fontes, identidade
do adaptador e versão gerenciada controlam atualização e reutilização de
resultados. Recibos também validam os arquivos auxiliares do grafo antes de
reutilizar um índice; evidência inválida de cache provoca reconstrução. O cache
mantém no máximo 64 resultados por geração de fontes. Texto do repositório e
resultados em cache continuam sendo dados não confiáveis, não instruções ou
autorização.

## Limites de runtime e comportamento em falhas

| Limite | Valor aplicado |
| --- | --- |
| Inventário de fontes | 20.000 arquivos |
| Fonte individual | 1.000.000 bytes |
| Snapshot total de fontes | 128 MiB |
| Texto do resultado | Até 32 KiB; a requisição pode definir um orçamento menor |
| Heap old-space do V8 | 256 MiB |
| RSS do processo | Limiar de 512 MiB, monitorado a cada 20 ms |
| Requisição estrutural | 90 segundos, incluindo a espera após admissão |
| Processos estruturais | Um processo ativo por raiz de cache compartilhada |
| Fila de admissão | Oito requisições admitidas no total, incluindo a ativa |

O limiar de RSS aciona cancelamento monitorado; não promete um teto de memória
exato aplicado pelo kernel. Timeout, cancelamento, excesso de memória e saturação
da fila geram falhas limitadas. Métricas ausentes não são substituídas por zero ou
valores inventados.

O helper descartável aplica Landlock e seccomp antes de iniciar o Node. Ele permite
leitura da distribuição gerenciada, snapshot de fontes e bibliotecas compartilhadas
necessárias, e escrita somente no diretório privado do índice. Rede e novos
processos filhos são negados; as threads necessárias ao Node continuam permitidas.
O filho recebe um ambiente mínimo, sem acesso ao HOME/configurações pessoais,
credenciais de provedores ou sockets conectados herdados. Plataformas não
suportadas ou ausência das proteções do kernel causam falha segura; o adaptador
não tenta novamente sem isolamento. O runtime exige Linux amd64/arm64 e Landlock
ABI 3 ou superior.

O gateway autenticado existente controla a admissão da sessão e as permissões de
ferramentas. A indisponibilidade do provedor pode selecionar uma inspeção direta
permitida quando ela responde à pergunta, ou um resultado explícito de
indisponibilidade. Ela nunca amplia o escopo de arquivos ou de autorização.

## Fidelidade e evidência operacional

As observações de código usam a classe de fidelidade **`exact_required`**. Elas
registram somente fatos operacionais, como provedor/motivo do roteamento, duração,
bytes do resultado, truncamento, uso de cache, atualização do índice, contagens de
fontes/arquivos indexados e CPU/pico de RSS medidos quando disponíveis. Acertos de
cache não herdam medições de CPU/RSS de um processo anterior. Consultas, nomes de
arquivos, texto das fontes, hashes das fontes, erros arbitrários e conteúdo de
artefatos exportados ficam fora dos eventos de observação.

Uso e economia de tokens permanecem indisponíveis sem evidência real. O adaptador
remove as anotações teóricas de economia de tokens do upstream; contagens de bytes
não são medições de tokens. Os diagnósticos do console preservam os estados
desconhecido/indisponível. Uma visualização privada é um artefato inspecionável,
não prova de cobertura completa do código ou de correção executável.

As fixtures implementadas cobrem identidade de escopo, snapshots limitados,
atualização, tratamento de corrupção, roteamento, chamadas do adaptador,
observações e negações do sandbox. Builds de release também exigem smoke das
gramáticas nativas/viewer, verificações de ABI e o gate sem AVX. A integração
completa, o aceite dos benchmarks e os resultados com clientes reais devem ser
registrados no relatório final de validação do candidato; esta descrição de
arquitetura não transforma trabalho pendente em concluído.
