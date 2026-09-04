# Threat model do Skill Control Plane

## 1. Resumo executivo e metodologia

Este documento modela a superfície introduzida pelo Registry, indexação,
dependency/conflict graph, Policy Engine, supply chain, atualização de packs e
Skill Gate do IVOAI. Todo upstream, archive, metadata, body, Context/RAG e output
de worker/tool é tratado como dado não confiável. O ativo protegido não é apenas
o conteúdo da skill: são também a autoridade da policy, as permissões do
executor, a integridade do objeto ativo, a disponibilidade da sessão e a
confidencialidade de credenciais do usuário.

O método usado foi source-backed: foram inspecionados os caminhos reais desde
discovery até ativação, as operações de persistência e os testes existentes. O
gate usa Registry local, ranking metadata-only, resolução de dependências e
policy antes de abrir qualquer body
([gate.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillgate/gate.go#L50)). A promoção é feita somente após
checksum, extração limitada e validações estruturais/policy
([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L207)); o pointer só é
mantido depois de health, integrity e ativação externa concluírem
([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L344)).

Fora do escopo desta versão: executar hooks de skills, conceder shell por skill,
instalar dependências de terceiros, substituir o orquestrador, isolar
semanticamente toda instrução maliciosa dentro de um LLM e fornecer assinatura
independente quando o upstream não a publica.

## 2. Ativos, atores e trust boundaries

### Ativos

- policy e capability allowlist do IVOAI;
- Registry privado e seus IDs, revisions, digests e lifecycle;
- active/previous pointers e manifests imutáveis do supply-chain store;
- TUI oficial de Codex/Claude e configuração de sandbox/tool approval;
- segredos de providers, memória, Context e dados do usuário;
- disponibilidade, reprodutibilidade e rollback das sessões gerenciadas;
- observabilidade bounded sem prompt, body, credential ou ambiente bruto.

### Atores e capacidades

- mantenedor upstream legítimo, comprometido ou malicioso;
- atacante que tomou um repositório, branch, tag ou canal de download;
- archive autor de traversal, link, bomb, executable ou duplicação;
- usuário local capaz de adulterar Registry, pointer ou objeto fora do fluxo;
- skill/dependência transitiva tentando ampliar capability, risco ou autoridade;
- texto não confiável oriundo de skill, Context/RAG, artifact, worker ou tool;
- falha/interrupção do processo durante staging, promoção, leitura ou rollback.

### Fronteiras e fluxos

```text
Internet/upstream (untrusted)
  -> discovery da default branch
  -> resolução para commit imutável
  -> fetch bounded + digest
  -> staging privado (sem execução)
  -> metadata-only index / quarantine
  -> graph + IVOAI Policy
  -> deterministic smoke
  -> immutable object + atomic pointer
  -> Registry activation na mesma transação

Managed session
  -> intent bounded
  -> local Registry only (sem rede)
  -> ranking / graph / policy
  -> authenticated active object
  -> bounded body read + second active-pointer check
  -> official Codex/Claude instruction channel
```

O Registry usa leitura bounded, JSON estrito, normalização, gravação atômica e
rejeição de symlinks no caminho
([store.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skills/store.go#L25)). A indexação lê somente frontmatter
e quarentena symlinks, metadata inválida, IDs duplicados e dependências ausentes
([index.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skills/index.go#L37)). A extração aceita apenas arquivo
regular/diretório, limita bytes e contagem, rejeita links e usa `O_NOFOLLOW`
([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L620)).

## 3. Análise de ameaças e controles

| Cenário | Impacto | Controle e evidência | Risco residual |
|---|---|---|---|
| Upstream/mantenedor comprometido ou takeover | pack malicioso promovido | source solicitado deve coincidir; revisão ativa é commit imutável; digest e trust são campos distintos; policy pode elevar o mínimo de trust ([update.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillupdate/update.go), [policy.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/policy/policy.go#L94)) | commit + digest local provam reprodutibilidade/integridade, não identidade independente |
| Revision, provenance ou digest divergente | troca de artefato/rollback falso | resolver flutuante é recusado; object manifest e stored provenance são revalidados antes/depois de promoção e rollback ([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L344)) | comprometimento simultâneo do upstream e canal sem assinatura independente |
| Tar traversal, symlink, hardlink, executable ou bomb | escrita/execução fora do root ou DoS | path containment, links proibidos, limites compactado/expandido/arquivo/count, modos sanitizados e nenhum código de staging executado ([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L230), [supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L620)) | custo de hashing/parse permanece dentro dos limites configurados |
| Metadata/frontmatter malicioso | panic, privilege inflation, graph poisoning | parser bounded, schema allowlist, quarantine e Registry strict; graph detecta missing/cycle/conflict/authority | termos maliciosos podem influenciar ranking, mas não policy |
| Body com prompt injection | modelo tenta ignorar policy ou pedir shell | body só é aberto depois de graph/policy; policy nunca recebe body; bundle declara precedência do IVOAI; tool/sandbox enforcement permanece fora do texto ([gate.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillgate/gate.go#L85)) | o LLM ainda pode ser semanticamente influenciado dentro das capacidades já concedidas; origem/trust e seleção mínima continuam essenciais |
| Skill pede shell, privilege, sandbox disable ou orchestration authority | execução arbitrária/control-plane takeover | capability não declarada/desconhecida/indisponível falha fechada; destructive e authority são negadas; high risk requer approval ([policy.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/policy/policy.go#L160)) | futura Approval UX precisará exibir escopo com clareza |
| Dependência transitiva ou conflict metadata manipulado | skill não revisada entra por composição | cada tentativa resolve o closure completo e avalia policy para todos os membros; conflito impede a combinação ([gate.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillgate/gate.go#L85)) | classificação IVOAI-owned incorreta ainda é risco humano |
| Registry/pointer/object tampering | ativação de body não registrado | Registry, source, revision e archive digest devem coincidir; active object é autenticado; leitura repete `Active` para detectar troca ([gate.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillgate/gate.go#L170)) | atacante local com controle total do usuário pode adulterar código e dados juntos |
| TOCTOU durante content load | mistura de revisão A/B | bounded regular-file read entre dois checks autenticados do mesmo pointer/revision/digest ([gate.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skillgate/gate.go#L170)) | não é proteção contra comprometimento total do processo/OS |
| Falha/interrupção na promoção | pointer e Registry divergentes | `PromoteWithActivation` desfaz Registry e pointer se apply/validate/journal falhar; recovery reconcilia índice externo ([supplychain.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/supplychain/supplychain.go#L344)) | falha física que impeça qualquer escrita requer intervenção operacional |
| Context/RAG, artifact, worker ou tool tenta alterar policy | policy bypass por conteúdo externo | apenas tipos estruturados do IVOAI chegam ao graph/policy; texto externo não é interpretado como configuração/capability | um executor pode repetir texto enganoso ao usuário; não recebe autoridade adicional |
| Exfiltração por logs/Registry/journal | segredo persistido | evento é allowlist com IDs/reasons bounded; Registry rejeita URL secret-shaped; journals guardam somente provenance operacional ([event.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/observability/event.go), [store.go](https://github.com/ivo-lopes/ivoai/blob/main/internal/skills/store.go#L25)) | body aprovado é enviado ao executor por design e deve ser licenciado/revisado |

Falhas críticas de integrity, provenance, path containment, Registry/pointer,
policy ou graph bloqueiam promoção/ativação. Registry ausente/vazio seleciona zero
skills. Registry corrompido ou componente opcional indisponível degrada para uma
sessão básica sem skill; uma skill explicitamente requerida falha a operação.

## 4. Validação, pressupostos e riscos residuais

Os testes herméticos cobrem A→B/no-change/rollback, checksum e limites,
archives maliciosos, staging sem execução, interrupção, corrupção de previous,
Registry/pointer divergence, TOCTOU, dependency/conflict/authority, policy
deny/approval, prompt injection e observabilidade sem body/secret. Fuzz targets
bounded exercitam frontmatter e archive paths. Nenhum teste normal autentica
provider, usa LLM real ou executa código de terceiro.

Pressupostos:

- o kernel/filesystem honra `O_NOFOLLOW`, rename atômico e permissões privadas;
- o processo e a conta local do usuário não estão totalmente comprometidos;
- adapters de discovery entregam dados não confiáveis ao pipeline, nunca
  autorização implícita;
- a classificação/overlay IVOAI-owned é revisada e commitada como código;
- uma versão ativa sempre é commit-pinned; branch/tag é somente discovery.

Riscos residuais e promotion blockers:

- source sem licença/provenance verificável deve ficar deferred/quarantined;
- política de trust acima de digest local exige attestation/signature realmente
  publicada; não pode ser inventada;
- body aprovado continua sendo linguagem natural não confiável e pode orientar o
  LLM dentro das permissões existentes;
- approval humana completa permanece futura; HIGH não ativa automaticamente;
- mudanças em parser, extractor, pointer, Registry ou event allowlist exigem
  reexecução da suíte adversarial e da matriz v0.5.0;
- qualquer falha que registre body, prompt, raw env, credential ou authorization
  header é blocker de promoção.

Revisão independente automatizada por subagente não foi usada neste ciclo porque
o modo de colaboração ativo não autorizava delegação. Foi feita uma segunda
passagem sequencial focada em boundaries, persistência, TOCTOU e redaction; o
resultado está refletido na tabela e nos testes citados.
