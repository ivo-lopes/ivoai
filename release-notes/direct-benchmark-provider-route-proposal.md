# Provider-route admission: proposal only, real cohorts blocked

Including normal user configuration and observing CHATGPT authentication do not
prove the selected inference route or absence of paid/custom routing. This change
implements no provider guard and makes no auth/status/config/account RPC or real
model call. The separate authorized offline MCP proof does not authorize a real
subscription cohort. The MCP helper remains disconnected from the live benchmark.

Research used IVOAI Memory first and Context second (empty), then the OpenAI Docs
skill and official [configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference),
[App Server reference](https://learn.chatgpt.com/docs/app-server) and
[authentication reference](https://learn.chatgpt.com/docs/auth). Current docs
separate authentication, selected provider and provider configuration. They expose
config/read for effective configuration and account/read for account metadata;
they do not attest the behavior of the reviewed0.162.1 ELF.

Independent source research pinned official rust-v0.162.1 annotated tag7a078f to
commit092d3acd6bec3e3a14bdc7e7a2810ab628ab759d. Selected files were inspected in its
private source cache; these are source findings, not runtime/authentication tests
or a reproducible-build attestation linking source to ELF628e:

- core/src/config/mod.rs:3826 passes top-level openai_base_url into built-in
  providers. model-provider-info/src/lib.rs:426–445 prefers an explicit base URL
  over the auth-dependent default. Selecting builtin openai alone is insufficient.
- login/src/auth/manager.rs:1369–1383 and1449–1455 can log out all credential stores
  on a forced-login mismatch. exec/src/lib.rs:657 invokes enforcement. Do not use
  forced_login_method as a harmless read guard. Included normal configuration can
  itself carry restrictions, so metadata/startup behavior needs its own accurate
  side-effect description; this does not impose a blanket ban on native clients.
- app-server account_processor/workspace_routing.rs:157–164 still calls read_account
  after its optional refresh step. Independent source review found possible
  accounts/check and recovery paths. refreshToken=false is not an offline or
  zero-refresh-side-effect guarantee.
- Effective config/read includes additional configuration, potentially containing
  secrets. Raw responses must not be dumped, persisted, copied or hashed. The proposed
  guard instead uses bounded RAM-only parsing and an allowlisted redacted metadata
  projection. No such RPC or manual file read was performed.

The proposed guard must establish **effective** provider identity, canonical
approved inference endpoint and applicable routing requirements independently of
auth mode, with the same binary/prefix/cwd/environment that will run the comparative attempt.
Custom provider, custom endpoint/header/credential command, unknown requirements,
unavailable official projection or changed scope must stop this cohort admission before a model call;
there is no fallback. Do not infer this state from env flags or an old login check.

A next separately reviewed implementation needs source-proven fixed launch
controls and an official minimal sanitized projection of routing, requirements
and account facts in bounded RAM, never raw persistent evidence. Normal OAuth
refresh/cache performed by the official client remains within the user's existing
authentication authorization; it is distinct from agent token extraction/manual
rotation/logout, which is prohibited. Describe normal refresh accurately rather
than promising an offline or zero-effect account read. Failure to establish the
required facts leaves this cohort admission BLOCKED, not every independent client
or development front. No invented alias, personal config
reader, credential extraction, proxy or alternate auth client is proposed.

Before admitting this helper to a real comparative cohort, an offline synthetic contract must discriminate at
least custom-provider selection, builtin plus custom top-level URL, conflicting
requirements, unsupported/missing/stale facts and auth mismatch, while preserving
all sentinel credentials and settings. Synthetic assertions cannot certify a real
subscription route. Metadata calls and real inference are outside this source-only increment; no RPC
is made now. Future work follows the root-reviewed call scope and the user's
existing official-client authentication authorization, without manual credential
inspection or new authentication machinery.
