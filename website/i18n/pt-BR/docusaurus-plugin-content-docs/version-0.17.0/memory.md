# Memory

O ai-memory é o backend de memória operacional durável. O cliente usa roteamento de
loopback local à sessão para que tokens de servidores upstream nunca sejam expostos a
fontes não relacionadas.

```bash
ivoai memory status
ivoai memory configure
```

Os resultados das ferramentas de Memory são autoritativos e seguem a política
exact-required. Com múltiplos purposes, novas escritas exigem um destino determinístico;
o IVOAI nunca as transmite implicitamente para todos os destinos.
