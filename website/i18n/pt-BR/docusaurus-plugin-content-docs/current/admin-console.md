# Console administrativo local

O console oferece consulta autenticada e somente leitura ao backend existente
do IVOAI. Ele funciona apenas quando iniciado explicitamente e não inicia
executores nem substitui clientes nativos. A paridade administrativa é parcial:
o inventário contém 118 ações da TUI, com seis projeções parciais de consulta e
nenhuma ação completamente coberta.

## Iniciar e autorizar

Use o proprietário local e as raízes de configuração/estado existentes do IVOAI.
Inicie o listener em primeiro plano:

```sh
ivoai admin serve --listen 127.0.0.1:7781
```

Em outro terminal, emita um código de ativação descartável:

```sh
ivoai admin access create --ttl 10m
```

Abra a URL de loopback exibida e informe o código no formulário de autorização.
O comando mostra o código uma vez; mantenha-o fora de logs e relatórios. Não são
solicitadas nem copiadas credenciais dos clientes nativos. A troca OAuth verifica
PKCE, state e CSRF do formulário. O acesso usa cookie HttpOnly/Strict, sem tokens
no armazenamento do navegador ou refresh token em JavaScript.

O listener padrão usa HTTP local. Somente endereços numéricos de loopback são
aceitos; cabeçalhos de encaminhamento não concedem autoridade. Esta entrega não
oferece acesso remoto ou RBAC por organização. Mantenha o listener local. As raízes
privadas existentes devem pertencer ao usuário em execução; o comando não relaxa
suas permissões.

## Consultar e revogar

As sessões mostram estado, identidade do executor/modelo e uso da última execução
registrada, quando disponível. A entrada em cache já está incluída na entrada.
Medição ausente significa desconhecida, não zero; uso de uma execução não é total
da sessão nem comprovação de economia. Versões de componentes e contagens de
conexões são snapshots de configuração, não sondagens de saúde em tempo real.

A listagem é limitada e pode retornar um subconjunto. A interface identifica
snapshots parciais ou truncados; não afirma uma contagem global completa.
Prompts, traces brutos, saídas de ferramentas, caminhos de trabalho e segredos de
conexões são excluídos.

```sh
ivoai admin access list
ivoai admin access revoke ACCESS_ID
```

A revogação vale nas próximas requisições autenticadas. A ação de encerrar acesso
no navegador revoga seu token e limpa os snapshots exibidos. Interrompa o listener
com Ctrl+C para desativar a superfície. Metadados de grants permanecem no mecanismo
OAuth existente, sob a raiz de estado selecionada.

Esta fatia não configura conectores, edita segredos, aprova trabalho, inicia/para
executores, gerencia evidências/índices ou executa setup, atualização, rollback e
restauração. Essas operações conservam os contratos existentes da CLI/TUI. Não há
scheduler nem autoridade de autorização em JavaScript.

O inventário de paridade ligado ao fonte, as falhas e as provas reais de navegador
local estão no [relatório de validação](https://github.com/ivo-lopes/ivoai/blob/main/release-notes/admin-console-validation.md).
As provas usam sessões semeadas e nenhuma inferência de provedor.
