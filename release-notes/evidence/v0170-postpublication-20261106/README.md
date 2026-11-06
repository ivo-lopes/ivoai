The public stable release v0.17.0 was downloaded once after the release workflow
42271672675 completed all five jobs successfully. This proof refers to the actual
published assets, not the earlier private a133ef94 candidate.

Release: https://github.com/ivo-lopes/ivoai/releases/tag/v0.17.0
Published 2026-11-06T19:01:22Z; release ID 436876360, latest=true,
draft=false, prerelease=false. Tag commit f441b81abbbd9cd24f87651837a98f89a40e06ac,
Git tree 97684ef5fdb98f9460111b6d490a01faae270110. The remote annotated tag was
checked against this commit, independently of the release target_commitish label.

PASS: exactly twelve public assets, eleven payload checksums and all twelve API
SHA-256 digests. The strict offline verifier came unchanged from the published
tag (SHA-256 2c58d35bdff6f4a0bd8f5cee0fef215aac96afff05e855847e8af765241ab6b3).
It associated both Core build metadata with the exact tag/source, Go 1.27.3,
architecture/build settings and the reviewed Graft/Caveman bytes/metadata. It
validated archives, file inventories and bundled licenses without executing the
downloaded code. Only its bounded local metadata-reader helper was compiled;
no product or dependency rebuild was performed.

After strict PASS, the downloaded amd64 Core executed version and help natively,
then the same two commands under the previously reviewed QEMU Nehalem CPU model
with AVX and AVX2 disabled. All four exited zero; version was 0.17.0 and each help
was 3998 bytes. A private network namespace, cleared environment and private
HOME/XDG/TMP paths excluded personal configuration/authentication and external
network access. No provider, model or inference was run. This is startup coverage,
not full no-AVX inference certification. ARM native startup/upgrade/rollback
passed in the release workflow and was not re-executed locally.

Published Core SHA-256:

- amd64: ecb8011b2ac215a7b6ac9166cd4fc032ef3bf0b2ce1a3dbbda756b93b467160c
- arm64: be9ab3f74b1e8019d386c63e87862ecf0753b4752a758a79490ecf3c98b8ba74

The publication receipt's 31.765 seconds covers this post-publication verification
and startup check, not a session/economic benchmark. Global economics remains
INCOMPLETE; earlier negative and NOT_RUN results remain unchanged. The strict
verifier's network_calls=false/downloaded_code_executed=false describe that
verification phase; separate startup execution occurred only after it passed.

Copies contain no asset binaries, credentials or raw PTY captures. Original
files remain intact locally. Only the private path in the first buildinfo header
was replaced by an artifact label; source-proof.json records original and public
hashes. All other selected copies are byte-identical. SHA256SUMS anchors every
file in this evidence directory except itself. No release, tag or code PR was
modified to publish this evidence.
