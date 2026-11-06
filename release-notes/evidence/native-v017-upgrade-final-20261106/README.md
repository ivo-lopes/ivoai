This is the complete real-CLI upgrade/rollback qualification of private Core
candidate 0.17.0 compiled from immutable source 7bce1090ea1eb971326f641460fea4fdee8e6b7b
(tree ee351d962b3449f6dc89367f043f0635ee919e50), Linux amd64. It is not the
published/tagged release bundle. The release workflow must repeat its gates on
the final tagged Core, including the final documentation changes.

PASS: published original 0.16.0 → private 0.17.0 → actual rollback to original
0.16.0 → actual reapplication of the same 0.17.0 binary. All eleven named
controls in qualification.json ran. Checksum/version/schema refusal, dry-run
immutability, real migration/setup/Doctor, Graft provenance on both versions,
idempotence, binary/profile preservation and synthetic credentials/ownership
preservation passed. There were 36 fixture requests, zero denied or unexpected
requests, no external networking during transactional phases and no inference.
Provider authentication, quotas and economic gains are not certified by this
fixture. Online preparation fetched only pinned public artifacts/package graphs.
UV_OFFLINE=1 was selected only for the network-isolated transaction phase.

The 1,168.509-second qualification duration includes preparation and the resource
pause; the outer process completed and cleaned its private fixture after
1,201.421 seconds. These are not economic benchmark measurements. The host was
shared, including the coordinator's tests and public native CLI certification.

At wall second 869.719 the watchdog suspended only this private qualification
process tree through pidfds because available disk fell below 4 GiB. Rollback
had passed and the active checkpoint was real_cli_update_after_rollback. The
user requested cleanup before continuing. The coordinator removed disposable
public caches outside this fixture, then explicitly authorized resumption with
more than 14 GiB free. No fixture/profile/baseline/evidence files were removed
or changed to manufacture success. Original processes resumed through the held
pidfds, without restarting or altering deadlines. The authorization arrived
approximately 216 seconds after suspension. The monitor did not record each
SIGCONT timestamp; pause-and-scope.json gives the exact recorded trigger,
authorization and conservative first-resumed-observation bound instead of an
invented precise pause duration. No timeout or test failure occurred after
resumption. The old unsuccessful qualification remains unchanged and linked.

Source freeze records every one of the 5,321 tracked regular files and exact
reviewed Graft/Caveman distribution metadata. After the gate all source hashes,
Core bytes and Core archive bytes matched. Build inspection recorded the exact
revision, vcs.modified=false, Go 1.27.3, CGO_ENABLED=0, GOAMD64=v1 and trimpath.
The candidate binary SHA-256 is
`a133ef9439e4fbac0dd55ef744824167c6b506cdd0abba3b4f8766311b5a76f4`.
Original v0.16 assets, receipts, personal settings and credentials were preserved.

Command (paths below are isolated private artifacts, not user installation):

```sh
GOMAXPROCS=2 GOWORK=off GOENV=off scripts/test-stable-release-upgrade.sh \
  --baseline-core /var/tmp/ivoai-v0160-postpublication-4cds26o9/ivoai \
  --candidate-dir /var/tmp/ivoai-v017-final-upgrade-candidate-20261106/assets \
  --version 0.17.0 \
  --output /var/tmp/ivoai-v017-final-upgrade-epoch2-20261106
```
