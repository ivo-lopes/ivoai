# Operações

Use `ivoai status` para verificar rapidamente a prontidão e `ivoai doctor` para obter
diagnósticos acionáveis. Operadores de servidor também usam `ivoai server status`,
`ivoai server doctor`, backup, restauração, logs, enrollment e comandos do ciclo de
vida do acesso Web.

O serviço de documentação é gerenciado pelo systemd, serve apenas um site Docusaurus
pré-construído e registra logs pelo journald:

```bash
systemctl status ivoai-docs.service
journalctl -u ivoai-docs.service
curl -f http://127.0.0.1:7780/
```

## Diagnóstico e reparo dos hooks gerenciados do ai-memory

O `ivoai doctor` valida os registros de hooks com comprovante de propriedade, a
integridade do binário e dos wrappers, as permissões de execução, links simbólicos,
shebangs e a disponibilidade dos interpretadores. Após revisar os resultados, use
`ivoai doctor --repair-hooks` ou a ação explícita **Repair Memory Hooks** nos
diagnósticos do Dashboard.

A instalação gera as definições revisadas de hooks do ai-memory em um ambiente
isolado de HOME, XDG e diretórios dos provedores e, em seguida, mescla apenas seus
registros exatos. Comprovantes privados registram o hash do binário gerenciado, os
wrappers e os arquivos do pacote verificado. O reparo restaura arquivos ausentes e
permissões de execução apenas dos recursos comprovadamente gerenciados, salva
backups privados e desfaz as gravações anteriores se uma etapa posterior da
transação falhar. As configurações e os hooks de outras ferramentas são
preservados. Repetir a instalação ou o reparo é idempotente.

Registros legados sem comprovante são diagnosticados, mas nunca adotados
silenciosamente por correspondência de nome ou trecho de texto. Revise-os
explicitamente antes de alterá-los. O conteúdo modificado de wrappers ou arquivos
de origem é preservado e exige revisão do operador. Binários gerenciados alterados
precisam de reparo ou atualização do componente antes que os hooks possam ser
restaurados. Um wrapper ausente também exige reparo explícito: o provedor não
consegue executar uma verificação dentro de um arquivo que não existe.

As sessões gerenciadas criam um controle privado de falhas, exclusivo da sessão.
Se o backend de um hook opcional terminar com código 127, a falha será informada
uma vez e os eventos seguintes da mesma sessão deixarão de chamá-lo. Isso não
desabilita hooks de outras ferramentas nem altera configurações globais. O controle
é removido ao encerrar a sessão. O reparo de hooks não reinstala o agente, não
autentica nenhum dos provedores e não migra o banco de dados do ai-memory.
