# Memory

ai-memory is the durable operational memory backend. The client uses session-local
loopback routing so upstream server tokens are never exposed to unrelated sources.

```bash
ivoai memory status
ivoai memory configure
```

Memory tool results are authoritative and follow the exact-required policy. With
multiple purposes, new writes require one deterministic destination; IVOAI never
broadcasts them implicitly.
