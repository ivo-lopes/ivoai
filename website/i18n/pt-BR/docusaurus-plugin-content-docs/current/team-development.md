# Desenvolvimento em equipe — modo atual

Cada developer instala e faz enrollment individual no servidor Voicecorp, com
client ID, credential e scopes próprios. HOME/XDG, secret store, login dos
providers e quota são individuais. Não compartilhe tokens, Codex/Claude/OpenCode
auth nem histórico nativo. Memory/Context podem compartilhar conhecimento por
purpose; transcripts permanecem pessoais.

Use clone individual, task principal atribuída no Plane, feature branch por
task/developer e PR revisado para integração. Plane é autoridade de ownership;
sessão ativa IVOAI não constitui claim distribuído. Não trabalhe na mesma task
ou paths sobrepostos sem alinhamento. Faça fetch/revalidação antes de integrar.

## Identidade de projeto e limites

Sem `.ivoai.toml`, a identidade cai no host local. `ivoai project init` executado
independentemente em clones usa o path absoluto e produz IDs diferentes, mesmo
com origin igual: GAP conhecido. Para escopo comum, um maintainer inicializa e
revisa um marcador sem secrets e distribui esse mesmo `.ivoai.toml` pelo Git.
Marcadores válidos existentes são preservados. Compare o ID antes de depender
de knowledge project-scoped. Trocar marcador não migra dados históricos.

Sessões, mappings, checkpoints, quota e leases são locais ao XDG. `flock`
protege o mesmo filesystem, não hosts diferentes. Worktrees isolam workers
dentro da orquestração, não humanos distribuídos editando branches/paths iguais.
Handoff humano usa Git + Plane + Memory/Context + resumo bounded, nunca
transcripts completos, prompts de workers, reasoning ou auth files.

## Hooks e offboarding

Use `ivoai memory hooks status|validate|repair` ou Memory Hooks na TUI. Repair
atua apenas em wiring IVOAI-owned e preserva hooks pessoais. Degraded significa
integração opcional indisponível, não agente básico necessariamente bloqueado.
Não cole comandos/env/stderr de hooks em tickets.

No offboarding, o administrador revoga o **client já inscrito** pela operação
autorizada e verifica acesso negado. Revogar código de enrollment não utilizado
não revoga um client já inscrito. Se não houver UI de client revocation, coordene
com o administrador; não edite secrets nem reutilize identidade de outro colega.
Revogação de login do provider é separada.

## Evidência e governança

A fixture `TestTeamCurrentTwoClientIsolation` usa um gateway TLS, dois HOME/XDG,
auth sintética independente, conhecimento comum por purpose, scopes read-only,
revogação individual e leases locais. Não certifica coordenação distribuída live.
`TestTeamCurrentProjectIdentityCrossCloneGap` comprova o GAP e o marcador comum.

Auditoria GitHub read-only em 2026-09-14: rulesets ausentes; endpoint clássico de
main respondeu explicitamente `Branch not protected`; CODEOWNERS/PR templates
ausentes. CI/release workflows existem. Merge/squash/rebase habilitados, exclusão
automática de branch desabilitada. Nenhuma governança foi alterada. Administradores
devem acordar reviews/checks e proteção de tags; workflow não é proteção por si só.

IVOAI-174 permanece Backlog: identidade lógica estável, provenance developer/client,
registry de trabalho ativo, leases distribuídos/TTL/heartbeat, claims Plane-first,
overlap de paths, branch/PR mapping, presença, provenance de artifacts, revalidação
de branch remota, handoff de equipe, RBAC e UX de offboarding. Nada disso foi
implementado silenciosamente na v0.10.4.
