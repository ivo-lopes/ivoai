# Backup e restauração

```bash
sudo ivoai server backup --output /var/lib/ivoai/backups/ivoai-backup.tar.gz
sudo ivoai server restore --input /var/lib/ivoai/backups/ivoai-backup.tar.gz
```

Os backups incluem os dados autoritativos do IVOAI necessários para a restauração, excluem
segredos e índices que podem ser reconstruídos, rejeitam links/traversal e suspendem os serviços
gerenciados durante a operação. Mantenha um backup protegido separado da administração de
enrollment e OAuth conforme a política de gestão de segredos da sua organização.
