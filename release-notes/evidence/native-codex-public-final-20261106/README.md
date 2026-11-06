These are byte-identical copies of the coordinator's selected sanitized public
Codex native TUI receipts and concise Go test logs. Original files remain in
/var/tmp/ivoai-consolidation-final-20261106. No authentication files, provider
credentials or raw PTY/TUI captures are included.

The immutable tested production candidate is source
7bce1090ea1eb971326f641460fea4fdee8e6b7b, binary SHA-256
`a133ef9439e4fbac0dd55ef744824167c6b506cdd0abba3b4f8766311b5a76f4`.
This private 0.17.0 candidate is not the published release bundle. The later root
671e52304d8330263d2a2b00ec73f78491922956 differs only in documentation and the
embedded documentation site; that diff was checked before publishing these
copies. The original source-freeze snapshot records the concurrent UX-doc/site
edits before their later commit. No Go/runtime/pin changes are inferred from
those documentation changes or attributed to the candidate after its freeze.

Epoch 1 is NOT_RUN: official model discovery reported unavailable, with zero
admitted/completed turns. The Go test's SKIP/PASS footer does not make that a
successful inference or certification result. The negative receipt is retained.

Epoch 2 is PASS, 102.329 seconds, using the official Codex native TUI and existing
subscription authentication. Three real turns and three closed receipts used
one Core session and the same native thread. The first turn completed an
approved DAG with one worker and changed only math.go; executable baseline tests
failed before the fix and final tests passed. A subsequent read-only turn, normal
exit and cross-process session resume retained the durable DAG history and
completed the third turn normally. Native backend and public wrapper exits
were zero. Final receipt fields dag=false/workers=0 describe the final inactive
projection; completed_DAG_before_readonly_and_resume and archived_workers=1
record the previously completed DAG rather than a missing execution.

The experiment enabled only IVOAI_EXPERIMENTAL_ADAPTIVE_PRIMARY=1 after runtime
admission; scoped capabilities came from Core. The host was shared. Client-
reported token/cache counters are retained exactly as reported, without economic
or causal claims. The global economic gate remains INCOMPLETE.

Copy-integrity SHA-256:

- `public-dag-resume-final-epoch1.json`: `0fe489bfefe0ee5189cfa8201dc0034f33a201385bdb4ca319fe985accb648d4`
- `public-dag-resume-final-epoch1.log`: `f1037421457804a4fc8e2cf7a8d427f4d8d30b56bb436707a5758233d262d330`
- `public-dag-resume-final-epoch2.json`: `6ef998ea3b177c3d0c897a5239d6043e5989200e22352df6bb240f04f447faab`
- `public-dag-resume-final-epoch2.log`: `b962f1b52e8370d8a80bdbc1121b40d053affb37d4dce98fa9eaaf05ba02e42a`
- `source-freeze-dag-final.json`: `30cf48c17f0d0afc415234d5875fb7327845f078f6ee0eeba774d6c15c360d73`
