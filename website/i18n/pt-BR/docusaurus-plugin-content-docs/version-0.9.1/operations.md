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
