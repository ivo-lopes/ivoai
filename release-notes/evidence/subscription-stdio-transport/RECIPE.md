# Prepared one-run official no-auth qualification — NOT EXECUTED

This recipe is for one negative qualification of the pinned official stdio protocol. It is not an authenticated subscription or model test. It does not run a TUI, the diagnostic NativeRunner, the adaptive Core or a real benchmark cohort. The fake ChatGPT-positive cases are unit fixtures only.

The final Go inputs are bound by `source-freeze-v3.json`, including the refusal of the controlled self-SIGKILL observed before Close. `final-inputs-frozen.json` binds those inputs together with this recipe and the current script. Older v2 freezes, including `recipe-script-freeze.json`, are historical epochs and are not rescored as this final source.

Root must review the final source commit, script hash and exact arguments before authorizing execution. This file grants no authorization. There is no retry, alternate command, paid fallback or flag relaxation after a failure.

The prepared wrapper is `run_offline_once.py`, SHA256 `7a73cb4304829b564bc3a01750a360879ab3552ba37388c3c59d10e5f1402a27`. It checks the expected clean source HEAD before and after compiling the opt-in test, and pins Go1.27.3's executable SHA256. The official main/companion are `628e35487d888a28f0427fc37501da529dd3d81fcbad7a2c3421c1e8670d92e1` and `ec71830690a7c0024916ee0c99fbdc635bc6e747ff665ea4f03a8389bbcb6b30`; neither was executed during preparation.

Only after that review, the executable shape is:

```bash
env -i PATH=/usr/bin:/bin HOME=/home/ivo /usr/bin/python3 -I -S -B \
  <reviewed-source>/release-notes/evidence/subscription-stdio-transport/run_offline_once.py \
  --execute-reviewed-once \
  --source <clean-owned-reviewed-source> \
  --expected-source <exact-reviewed-commit> \
  --binary-dir <reviewed-official-pair-directory> \
  --go /var/tmp/ivoai-validation-toolchain-20261028/go/bin/go
```

The placeholders must be replaced by the reviewed values, never by an automatic newest branch or unverified download. The wrapper performs no install, login, status probe, auth/config copy or personal-state edit. It reads only the supplied binaries/source and creates a new0700 output root under `/var/tmp`. Early input refusals are fixed enums; once preparation starts, `result.json` preserves the outcome. The copied pair and synthetic configuration stay inside that owned root.

The namespace follows the already reviewed user-scope fixture pattern: `bwrap --unshare-all --clearenv --die-with-parent --new-session`, read-only `/usr` and libraries, `/bin -> usr/bin`, private `/proc`, `/dev`, `/tmp` and `/etc`. Only the new case, fixture files and reviewed binaries/test ELF are mounted. No personal HOME, auth, keyring socket, account configuration or real `/etc` is mounted. The test checks a different network-namespace identity, an up loopback interface as the only interface, no IPv4 routes and IPv6 devices restricted to loopback. No AppArmor, host proxy or global sandbox setting is changed.

The fresh user config declares `synthetic_user`, and synthetic `/etc/codex/config.toml` declares `synthetic_system`. Their commands only write a private tripwire and fail. A deliberately unused custom provider points to a namespace-local API trap. The test requires both MCP names in config/read and zero MCP/API trap hits. This proves only declaration and absence of startup effects in this exact preflight; it does not certify all MCP permissions, tool execution or later exec isolation.

The exact existing source constructor is used with `/reviewed/codex`, `/case/repo`, declared `gpt-6.1-sol`, read-only sandbox and explicit fresh HOME/CODEX_HOME environment. It preserves `ProjectTrustArgs`, which marks this repository untrusted, and the reviewed feature denials. It appends the same three canonical provider/base-URL settings as the inventory/exec profile, followed by `app-server --listen stdio://`. It adds no ignore-config flag, forced login, proxy override, model prompt, schema/exec-only option or fabricated auth file. If the pin refuses this constructor, the qualification fails; do not substitute another argument form.

Only initialize, initialized, config/read, configRequirements/read and account/read are permitted, with fixed IDs and exact params. config/read uses includeLayers=false and the exact cwd. The account read has refreshToken=false, which is not a blanket prohibition on normal client-owned OAuth/cache behavior. In this fixture there are no credentials to authenticate with. Config uses snake_case; requirements use camelCase. Layers are omitted when absent; forced-login/workspace fields remain explicit null per the inspected schema. Account and optional workspace routing can be null/omitted for a logged-out client: this must produce `AUTH_DENIED`, not a fabricated positive. The strict b495 mapper is unchanged; any unexpected omission results in failure and review.

The fixed pass condition is a negative `AUTH_DENIED`, both declared MCPs observed, no tripwire hit, no newly created auth file, owned process reaped and actual native Wait0. `AuthenticatedPositiveCertified` is always false. A startup/config/schema error, timeout, missing field, spontaneous nonzero/signal exit, forced cleanup without Wait0 or missing receipt is a preserved FAIL, never a skipped success. Raw stdout/config/account/stderr are not retained, hashed or forwarded. The receipt contains only closed strings and booleans.

The existing probe budget remains15s. Cleanup has100ms EOF grace,300ms TERM grace and1s reap bound, with the leader left unreaped until group cleanup signals finish. The outer test/namespace bounds are30/35s; outer timeout performs owned TERM then KILL cleanup and reports failure. Go compilation is a separate120s source-only preparation bound. No deadline is increased after observing an adverse outcome.

This wrapper/test has been prepared and statically checked, not executed with the official pair. A successful later negative run would qualify only this constructor, schema, refusal and cleanup envelope. It would not certify interactive-subscription provenance, authenticated end-to-end inference, model availability, quotas, TUI parity, universal startup egress, changing managed/cloud policy, TOCTOU elimination, economics or release readiness.
