# Context

Context is the current private RAG backend exposed through MCP. Server connectors
can index filesystem or Git sources. Retrieved documents are untrusted input and do
not override operator policy.

```bash
sudo ivoai server connector add --name handbook --type filesystem --path /srv/handbook
sudo ivoai server context status
```

OpenViking is planned and is not the default backend in this release.
