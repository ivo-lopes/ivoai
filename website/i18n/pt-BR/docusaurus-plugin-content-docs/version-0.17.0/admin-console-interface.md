# Operações administrativas no painel web

O painel administrativo usa o backend existente do IVOAI. As conversas continuam
nos clientes oficiais. Abra **Administração** para pesquisar operações pelo nome
ou identificador, escolher uma área e filtrar consultas, alterações e orientações
para os clientes nativos. Sessões, componentes e conexões mantêm suas próprias
visualizações.

## Acesso e alterações

Uma autorização de leitura permite consultar o estado. Uma autorização de gestão
também permite alterações; o navegador não amplia a autorização de leitura.
Operações que exigem privilégios do sistema operacional ficam indisponíveis
quando o processo administrativo não possui esses privilégios.

Para alterar algo, preencha o formulário e escolha **Revisar alteração**. Confira
o resumo e os valores, autorize a mudança explicitamente e escolha **Confirmar
alteração**. O backend vincula a confirmação temporária à operação, aos valores
e à identidade autorizada. Uma confirmação ou identificador já consumido não
executa outra mutação, inclusive após reiniciar o serviço. Alterar o formulário
exige uma nova revisão.

Uma conexão interrompida pode deixar o resultado desconhecido. O painel não
repete a operação automaticamente. Consulte o estado atual antes de iniciar
outra alteração explicitamente. O fechamento de uma operação em andamento fica
desabilitado; fechar o navegador não comprova o cancelamento da operação.

Os resultados indicam conclusão, falha, cancelamento ou orientação para um
cliente nativo. A orientação para continuar no cliente não significa que ele
foi executado. Falhas do backend e operações indisponíveis permanecem visíveis.

## Valores privados e saídas

Credenciais existentes são excluídas dos resultados administrativos. Campos de
senha ficam mascarados e seus valores não aparecem no resumo de revisão. Um
novo código de acesso de uso único aparece apenas no respectivo resultado,
mascarado até ser revelado explicitamente. Copie-o antes de fechar o resultado;
ele é removido ao fechar ou sair e não entra na lista de atividades. A cópia
para a área de transferência ocorre apenas quando solicitada.

Quando um resultado completo sanitizado excede a prévia, o painel oferece seu
download autorizado, com prazo de expiração. Essa saída sanitizada não contém
as credenciais excluídas. Uma captura incompleta acima do limite de segurança
é inteiramente omitida e não gera um arquivo apresentado como completo. A
operação já executada não é repetida para recuperar a saída ausente.

A lista de atividades contém nomes de operações, resultados, identificadores
de pedidos e horários desta aba. Ela é apagada ao sair ou perder a autorização
e não substitui a auditoria durável. Os tokens OAuth permanecem em cookies
HttpOnly; a interface não usa localStorage nem sessionStorage. Tokens CSRF e
de confirmação permanecem apenas na memória.

## Proxy reverso e acessibilidade

Arquivos estáticos, chamadas da API e links de autorização permanecem sob o
caminho público configurado, como `/ivoai/admin/`. A página verifica se o
caminho informado pelo backend corresponde à origem dos seus arquivos. Ela
não obtém autoridade de cabeçalhos encaminhados nem escolhe outro servidor
a partir de valores fornecidos no navegador. A configuração do backend define
a origem pública, o proxy confiável e a política dos cookies.

A interface oferece navegação por teclado, rótulos explícitos, restauração de
foco, erros visíveis e redução de movimento. Os layouts são verificados em
375, 768, 1024 e 1440 pixels, incluindo a revisão de alterações. Medições
desconhecidas continuam desconhecidas; tokens de uma chamada não comprovam
economia da sessão inteira.

## Cobertura e evidências

O catálogo desta entrega contém 123 entradas: os 118 identificadores existentes
do menu e cinco operações adicionais de perfis e contexto. Títulos pesquisáveis
e controles de formulário cobrem essas entradas. Isso mede a cobertura do
contrato da interface, não 123 execuções administrativas reais bem-sucedidas.
Encaminhamentos para clientes nativos e itens de navegação são identificados
separadamente. O inventário e os resultados vinculados às fontes ficam em
`release-notes/evidence/admin-console-management/`.

Fixtures isolados de navegador verificam a interface sem dados de produção ou
inferência de provedores. O teste de integração separado exige os fluxos reais
App/OAuth/Store para saída, revogação, alteração isolada de configuração e proxy
TLS; um fixture ausente ou pulado reprova o gate. O ensaio com Nginx real e os
testes de download sanitizado possuem evidências próprias.
