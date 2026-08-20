package server

const ComposeYAML = `services:
  qdrant:
    image: qdrant/qdrant@sha256:a0e04fe623cb064502cd869cefc1dc7ce359d8edd481063b5bd351c0a0a2c91e
    restart: unless-stopped
    user: "1000:1000"
    env_file: [/etc/ivoai/secrets/qdrant.env]
    ports:
      - "127.0.0.1:6333:6333"
    volumes:
      - /var/lib/ivoai/qdrant:/qdrant/storage
    healthcheck:
      test: ["CMD", "bash", "-c", "</dev/tcp/127.0.0.1/6333"]
      interval: 10s
      timeout: 3s
      retries: 12
    security_opt: ["no-new-privileges:true"]
    networks: [ivoai-internal]

  embeddings:
    container_name: ivoai-embeddings
    image: ghcr.io/huggingface/text-embeddings-inference@sha256:ad950d30878eceb72aaf32024d26fa2b1d04a75304fa0b4776b49aa1941fea07
    restart: unless-stopped
    user: "1000:1000"
    env_file: [/etc/ivoai/secrets/embeddings.env]
    command: ["--model-id", "intfloat/multilingual-e5-small", "--revision", "614241f622f53c4eeff9890bdc4f31cfecc418b3", "--port", "80"]
    ports:
      - "127.0.0.1:8080:80"
    volumes:
      - /var/lib/ivoai/models:/data
    healthcheck:
      test: ["CMD-SHELL", "curl -fsS -H 'Authorization: Bearer '$$API_KEY http://127.0.0.1:80/health"]
      interval: 10s
      timeout: 3s
      retries: 30
    security_opt: ["no-new-privileges:true"]
    networks: [ivoai-internal, model-download]

  ai-memory:
    image: akitaonrails/ai-memory@sha256:1d8a2ca7d7bc2349ba964d2d97dafb683632676460c1e373083f919a18c60d37
    restart: unless-stopped
    command: ["serve", "--transport", "http", "--bind", "0.0.0.0:49374"]
    user: "1000:1000"
    env_file: [/etc/ivoai/secrets/memory.env]
    ports:
      - "127.0.0.1:49374:49374"
    volumes:
      - /var/lib/ivoai/memory:/data
    healthcheck:
      test: ["CMD", "/usr/local/bin/ai-memory", "status"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 5s
    cap_drop: ["ALL"]
    security_opt: ["no-new-privileges:true"]
    networks: [ivoai-internal]

networks:
  ivoai-internal:
    internal: true
  model-download:
    name: ivoai-model-download
`

// ARM64ComposeOverride uses the upstream CPU ARM64 image pinned by registry
// digest. Building TEI from its release tree is intentionally avoided: the
// v1.9.3 tree does not contain the Dockerfile-arm64 previously referenced.
const ARM64ComposeOverride = `services:
  embeddings:
    image: ghcr.io/huggingface/text-embeddings-inference@sha256:2873ddd3029bf6eed09c0befe737d88123107a0b89274b1552d68d1e3bb2a047
`

const GatewayUnit = `[Unit]
Description=ivoai gateway
After=network-online.target ivoai-dependencies.service
Wants=network-online.target
Requires=ivoai-dependencies.service

[Service]
Type=simple
User=ivoai-gateway
Group=ivoai
ExecStart=/usr/local/bin/ivoai server gateway serve
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectProc=invisible
ProcSubset=pid
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=/var/lib/ivoai/enrollment /run/ivoai
ReadOnlyPaths=/etc/ivoai /var/lib/ivoai/context
InaccessiblePaths=/var/lib/ivoai/memory /var/lib/ivoai/qdrant /var/lib/ivoai/models /var/lib/ivoai/corpus
EnvironmentFile=/etc/ivoai/secrets/qdrant.env
EnvironmentFile=/etc/ivoai/secrets/embeddings.env
EnvironmentFile=/etc/ivoai/secrets/memory.env
UMask=0077

[Install]
WantedBy=multi-user.target
`

const ContextUnit = `[Unit]
Description=ivoai context ingestion service
After=network-online.target ivoai-dependencies.service
Requires=ivoai-dependencies.service

[Service]
Type=simple
User=ivoai-context
Group=ivoai
ExecStart=/usr/local/bin/ivoai server context serve
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectProc=invisible
ProcSubset=pid
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
LockPersonality=yes
MemoryDenyWriteExecute=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
ReadWritePaths=/var/lib/ivoai/context /run/ivoai
ReadOnlyPaths=/etc/ivoai
InaccessiblePaths=/etc/ivoai/secrets /var/lib/ivoai/enrollment /var/lib/ivoai/memory
EnvironmentFile=/etc/ivoai/secrets/qdrant.env
EnvironmentFile=/etc/ivoai/secrets/embeddings.env
UMask=0077

[Install]
WantedBy=multi-user.target
`

const DependenciesUnit = `[Unit]
Description=ivoai container dependencies
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/docker compose -f /etc/ivoai/compose.yaml up -d --wait --wait-timeout 840
ExecStartPost=/usr/bin/docker network disconnect ivoai-model-download ivoai-embeddings
ExecStop=/usr/bin/docker compose -f /etc/ivoai/compose.yaml down
NoNewPrivileges=yes
TimeoutStartSec=15min
TimeoutStopSec=2min

[Install]
WantedBy=multi-user.target
`

const DefaultServerConfig = `protocol_version = 1
listen_address = "127.0.0.1:7744"
qdrant_url = "http://127.0.0.1:6333"
qdrant_collection = "ivoai_context_v1"
embedding_url = "http://127.0.0.1:8080"
embedding_dimensions = 384
memory_url = "http://127.0.0.1:49374"
`
