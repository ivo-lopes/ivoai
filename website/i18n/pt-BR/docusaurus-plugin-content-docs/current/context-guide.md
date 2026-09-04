# Context

Context é o backend RAG privado atual exposto por MCP. Connectors do server podem indexar fontes
filesystem ou Git. Documentos recuperados são entrada não confiável e não substituem a policy do
operador.

```bash
sudo ivoai server connector add --name handbook --type filesystem --path /srv/handbook
sudo ivoai server context status
```

OpenViking está planejado e não é o backend default nesta release.
