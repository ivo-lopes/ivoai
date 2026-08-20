# Third-party notices

ivoai's own code is MIT-licensed. It installs or runs the following separately distributed software at exact versions recorded in `manifest/components.yaml`. Nothing here changes the upstream license or terms; consult the linked project for the complete text.

| Project | Version | License / terms | Role | Upstream |
|---|---:|---|---|---|
| Go toolchain | 1.27.0 | BSD-3-Clause | Ephemeral, checksum-pinned compiler for source-checkout installation when compatible Go is unavailable | <https://go.dev/dl/> |
| OpenAI Codex CLI | 0.148.0 | Apache-2.0 | Official ChatGPT/Codex terminal client | <https://github.com/openai/codex> |
| Anthropic Claude Code | 2.1.228 stable | Proprietary; Anthropic Consumer or Commercial Terms, depending on account | Official Claude terminal client | <https://github.com/anthropics/claude-code> |
| Headroom | 0.36.0 | Apache-2.0 | Optional local context-optimization proxy/wrapper | <https://github.com/headroomlabs-ai/headroom> |
| ai-memory | 1.29.0 | MIT | Persistent cross-session/cross-agent operational memory and hooks | <https://github.com/akitaonrails/ai-memory> |
| Ruflo | 3.38.12 | MIT | Workflow, skills and temporary orchestration state | <https://github.com/ruvnet/ruflo> |
| Node.js | 22.18.0 | MIT | Isolated runtime for the managed Ruflo installation | <https://nodejs.org/> |
| uv | 0.12.5 | Apache-2.0 OR MIT | Isolated Python/tool runtime used to install Headroom | <https://github.com/astral-sh/uv> |
| CPython | 3.13.15 | Python Software Foundation License 2.0 | Managed interpreter for Headroom | <https://www.python.org/downloads/release/python-31315/> |
| Docker Compose | 5.5.0 | Apache-2.0 | Container dependency orchestration for the server | <https://github.com/docker/compose> |
| Qdrant | 1.19.0 | Apache-2.0 | Internal server vector database | <https://github.com/qdrant/qdrant> |
| Hugging Face Text Embeddings Inference | 1.9.3 | Apache-2.0 | Local CPU embedding HTTP runtime | <https://github.com/huggingface/text-embeddings-inference> |
| intfloat multilingual-e5-small | revision `614241f622f53c4eeff9890bdc4f31cfecc418b3` | MIT | Local multilingual embedding model | <https://huggingface.co/intfloat/multilingual-e5-small> |
| pelletier/go-toml v2 | 2.2.4 | MIT | TOML configuration parser/encoder linked into ivoai | <https://github.com/pelletier/go-toml> |
| golang.org/x/term | 0.34.0 | BSD-3-Clause | Secure terminal input support linked into ivoai | <https://pkg.go.dev/golang.org/x/term> |
| golang.org/x/sys | 0.35.0 | BSD-3-Clause | Indirect operating-system support linked into ivoai | <https://pkg.go.dev/golang.org/x/sys> |

Claude Code is not represented as open-source software. ivoai downloads the official external binary and does not redistribute or modify its credential material. Use is governed by [Anthropic's legal and compliance guidance](https://code.claude.com/docs/en/legal-and-compliance), including Consumer Terms for Free/Pro/Max users and Commercial Terms for Team/Enterprise/API users.

The server may also use a user/system-provided Docker Engine, containerd and system libraries. Headroom's isolated environment is resolved from architecture-specific hash-locked constraints, and Ruflo's npm prefix is installed from a complete npm lockfile. Their transitive packages are not redistributed in ivoai release archives and remain separately licensed upstream dependencies.
