# Third-party notices

ivoai's own code is MIT-licensed. It installs, links, or uses the following software
at reviewed pins or bounded package rules recorded in the source, lockfiles, and
`manifest/components.yaml`. Nothing here changes the upstream license or terms;
consult the linked project for the complete text.

| Project | Version | License / terms | Role | Upstream |
|---|---:|---|---|---|
| Model Context Protocol Go SDK | 1.8.0 | Apache-2.0 / MIT (file-specific; see upstream LICENSE) | Official Streamable HTTP and MCP protocol implementation linked into the ivoai Web MCP gateway | <https://github.com/modelcontextprotocol/go-sdk> |
| Go toolchain | 1.27.2 | BSD-3-Clause | Ephemeral, checksum-pinned compiler for source-checkout installation when compatible Go is unavailable | <https://go.dev/dl/> |
| OpenAI Codex CLI | 0.162.1 | Apache-2.0 | Official ChatGPT/Codex terminal client | <https://github.com/openai/codex> |
| Anthropic Claude Code | 2.1.296 | Proprietary; Anthropic Consumer or Commercial Terms, depending on account | Official Claude terminal client | <https://github.com/anthropics/claude-code> |
| OpenCode | 1.19.0 (`733b2c627e5aec39cb33cc22373bdce33053ef2c`) | MIT | Pinned managed AUTO frontend; upstream source and notices remain intact | <https://github.com/anomalyco/opencode/releases/tag/v1.19.0> |
| Headroom | 0.40.0 | Apache-2.0 | Optional local context-optimization proxy/wrapper | <https://github.com/headroomlabs-ai/headroom> |
| ai-memory | 2.6.3 | MIT | Persistent cross-session/cross-agent operational memory and hooks | <https://github.com/akitaonrails/ai-memory> |
| Ruflo | 3.56.5 | MIT | Workflow, skills and temporary orchestration state | <https://github.com/ruvnet/ruflo> |
| Node.js | 26.11.1 | MIT | Isolated runtime for the managed Ruflo installation | <https://nodejs.org/> |
| uv | 0.13.0 | Apache-2.0 OR MIT | Isolated Python/tool runtime used to install Headroom | <https://github.com/astral-sh/uv> |
| CPython | 3.14.8 | Python Software Foundation License 2.0 | Managed interpreter for Headroom | <https://www.python.org/downloads/release/python-3148/> |
| Docker Compose | 5.6.0 | Apache-2.0 | Container dependency orchestration for the server | <https://github.com/docker/compose> |
| Docker Engine / CLI | 28.0.0 minimum; exact official Debian APT candidate at installation | Apache-2.0 | Server container runtime; Debian provisioning verifies Docker's reviewed repository key | <https://docs.docker.com/engine/install/debian/> |
| Qdrant | 1.19.2 | Apache-2.0 | Internal server vector database | <https://github.com/qdrant/qdrant> |
| Hugging Face Text Embeddings Inference | 1.9.4 | Apache-2.0 | Local CPU embedding HTTP runtime | <https://github.com/huggingface/text-embeddings-inference> |
| intfloat multilingual-e5-small | revision `614241f622f53c4eeff9890bdc4f31cfecc418b3` | MIT | Local multilingual embedding model | <https://huggingface.co/intfloat/multilingual-e5-small> |
| pelletier/go-toml v2 | 2.4.3 | MIT | TOML configuration parser/encoder linked into ivoai | <https://github.com/pelletier/go-toml> |
| golang.org/x/term | 0.47.0 | BSD-3-Clause | Secure terminal input support linked into ivoai | <https://pkg.go.dev/golang.org/x/term> |
| golang.org/x/sys | 0.49.0 | BSD-3-Clause | Indirect operating-system support linked into ivoai | <https://pkg.go.dev/golang.org/x/sys> |
| Docusaurus | 3.10.2 | MIT | Build-time generator for the embedded product documentation portal | <https://docusaurus.io/> |
| docusaurus-search-local | 0.55.3 | MIT | Build-time local/offline search index and browser search UI | <https://github.com/easyops-cn/docusaurus-search-local> |
| Mermaid layout ELK | 1.0.1 | MIT | Build-time diagram layout support for Docusaurus | <https://github.com/mermaid-js/mermaid> |
| React / React DOM | 19.3.0 | MIT | Documentation portal browser runtime bundled into the static site | <https://react.dev/> |
| Bpmf Huninn (Fontsource package) | 5.3.1 | SIL OFL-1.1 | Self-hosted regular Latin webfont used only for the IVOAI visual wordmark | <https://fonts.google.com/specimen/Bpmf+Huninn> |

Claude Code is not represented as open-source software. ivoai downloads the official external binary and does not redistribute or modify its credential material. Use is governed by [Anthropic's legal and compliance guidance](https://code.claude.com/docs/en/legal-and-compliance), including Consumer Terms for Free/Pro/Max users and Commercial Terms for Team/Enterprise/API users.

The server may also use a user/system-provided Docker Engine, containerd and system libraries. Headroom's isolated environment is resolved from architecture-specific hash-locked constraints, and Ruflo's npm prefix is installed from a complete npm lockfile. Their transitive packages are not redistributed in ivoai release archives and remain separately licensed upstream dependencies.

The documentation and Ruflo use MIT-licensed `braces` 3.0.3 with the local
`3.0.4-ivoai.1` security patch in `patches/braces`. Its embedded archive retains
upstream LICENSE and README; SHA-512 is recorded in both package locks. This
local version is not an upstream release. Dependency audits have no advisory
allowlist. Caveman product 3.2.0 and binary bundle 2.1.0 are Apache-2.0; native
skills retain LICENSE, NOTICE and the historical MIT attribution.

Ruflo's Zod V3 dependency ranges use the explicit local MIT-licensed fork
`@ivoai/zod3-compat@3.25.76-ivoai.1`. It is built from official Zod 4.6.7 with
V3 root exports and early upper-bound rejection backported to V3 arrays/sets;
the patched V4 subtree and upstream license are retained. Official V4 dependency
ranges continue to resolve upstream V4. This is not an upstream V3 release.
The reviewed source identity, patch, checksums, reproduction procedure and limits
are in [patches/zod3-compat](patches/zod3-compat/README.md), with the full MIT
notice in [LICENSE](patches/zod3-compat/LICENSE). A clean dependency audit does
not certify unbounded schemas or arbitrary refinements against resource abuse.

## Claude Red

The native Skill catalog includes 51 unmodified declarative `SKILL.md` files
from [SnailSploit/Claude-Red](https://github.com/SnailSploit/Claude-Red), revision
`739512a8588b28ff3b554e669391c22508602878`, under the MIT License.
Copyright (c) 2024-2025 SnailSploit / Kai Aizen. The full notice is preserved in
`internal/skillcatalog/native/claude-red/LICENSE`. The remaining 28 documents
without required frontmatter are recorded as rejected metadata, not bundled.
Upstream installers, converters, hooks and build tools are not included or run.

## Managed Graft

Graft 0.21.1, revision `fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad`,
is incorporated as the MIT-licensed managed library `0.21.1-ivoai.1`.
Its archive preserves upstream LICENSE, per-package notices, dependency integrity
inventories, build inputs and reviewed patches. The whole archive is not uniformly
MIT: its 16 retained WASM grammars include MIT, Apache-2.0 and CC0-1.0 components.
Their full notices and exact binary hashes are in
[the grammar inventory](patches/graft/grammar-licenses/README.md) and in the payload's
`node_modules/tree-sitter-wasm/THIRD_PARTY_LICENSES` directory.

Prebuilt WASM inputs are pinned by npm SRI and individual SHA-256. The upstream
bundle does not fully attest the source revisions used to compile them; IVOAI
makes no source-reproducibility or independently verified signature claim for
those binaries. Native bindings are built from locked npm sources using reviewed,
network-isolated compilation. Unused grammars/editor queries and the unused
`js-yaml` CLI with its vulnerable `argparse`/`sprintf-js` chain are excluded.
The preserved build lock and the shipped runtime inventory remain distinct.
