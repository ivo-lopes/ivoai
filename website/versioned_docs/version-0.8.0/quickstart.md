# Quickstart

## Client

```bash
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sh
ivoai setup
ivoai doctor
ivoai auto
```

Official executor authentication remains inside each executor. IVOAI does not ask
for or copy subscription credentials.

## Add private servers

```bash
ivoai connect server add company-a --url https://ai-a.example.com --purpose company-a --code-stdin
ivoai connect server add company-b --url https://ai-b.example.com --purpose company-b --code-stdin
ivoai connect server list
ivoai auto                         # all enabled sources
ivoai auto --knowledge-source company-a
```

## Server

On a supported Debian 12 or Ubuntu host:

```bash
curl -fsSL https://raw.githubusercontent.com/ivo-lopes/ivoai/main/install.sh | sudo sh
sudo ivoai setup --mode server
sudo ivoai server doctor
```

See [Server setup](setup.md), including LXC prerequisites and docs hosting.
