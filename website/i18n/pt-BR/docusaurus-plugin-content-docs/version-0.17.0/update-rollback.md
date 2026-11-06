# Atualização e rollback

```bash
ivoai update --dry-run
ivoai update
ivoai update --rollback
```

Antes da ativação, as atualizações validam checksums da release, proveniência dos
componentes, compatibilidade dos schemas e metadados de migração transacional. O
rollback restaura o binário anterior e o estado mutável compatível pertencente ao
IVOAI. Executores externos e seus stores de autenticação permanecem fora da
responsabilidade do IVOAI.
