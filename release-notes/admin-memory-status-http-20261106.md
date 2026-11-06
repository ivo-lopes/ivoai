# Real administrative Memory status over HTTP — 6 November 2026

This follow-up is based on `6179d217206a8941fe4f01eae3dbe87205e02e85`
(PR #76), following the published v0.17.0 source
`f441b81abbbd9cd24f87651837a98f89a40e06ac`. It adds one explicit integration
test and its evidence, without changing production code, release assets,
historical matrix outcomes or personal configuration.

The explicit normal run passed in 2.08s (wall 2.103s); the final race run passed
in 9.05s (wall 10.101s), both with zero skips. `go vet ./internal/admin` passed.
These timings describe individual functional tests under shared-host load and
are not a performance comparison or evidence of session-token savings.

## What the explicit execution proves

`TestAdministrativePositiveMemoryStatusOfficialServer` starts the reviewed official
ai-memory 3.4.2 executable, with a generated private credential and an empty private
data directory, then runs `server.memory.status` through the actual administrative
TCP HTTP handler, CLI parser, registered `servercmd.New` adapter and Core probe.
There is no fabricated dispatch result, HTTP backend or process runner.

The backend executable SHA256 is
`5a8e2c51cb09dadfc157601772b32c79ee63a6cbf9f05caf59c67da385bae6c1`.
The parent verifies it before mounting it into a minimal bubblewrap filesystem;
the test verifies it again before execution. The test refuses to use the fixed
backend port `127.0.0.1:49374` in its parent's network namespace. The official
process and administrative client run in a separate namespace with loopback and
no external route. Personal home directories are not mounted. Only the reviewed
executables, operating-system libraries/tools, private temporary directory and
namespace-local proc/dev are available.

Observed checks:

- The real MCP tools catalog requires the fixture credential and includes
  `memory_status`. This is a catalog/protocol probe, not an invocation of that
  memory tool or a document-retrieval/inference benchmark.
- Two authorized administrative reads return `ai-memory: healthy` through the
  actual CLI adapter. Their configuration and backend credential remain exact.
- A missing administrative cookie returns 401 and missing CSRF returns 403 before
  dispatch. The backend also rejects a wrong bearer credential with 401.
- Replacing only the private credential with a different well-formed value makes
  the Core report a failed status; stopping the owned child does the same. No
  successful fallback is substituted.
- The backend token does not appear in administrative JSON. Child stdout/stderr
  is discarded, so ephemeral credentials cannot enter captured backend logs.

The helper uses the real access store and PKCE exchange, then supplies its cookie,
Origin and CSRF headers to actual HTTP requests. This test is not an additional
browser OAuth round trip; those flows have separate v0.17.0 evidence.

**Read semantics:** `server.memory.status` is a read. Its preview does not issue a
mutation confirmation, and authorized reads may be repeated. Mutation-only
confirmation/NoReplay 409 behavior is deliberately not claimed here. The two
healthy reads plus two dependency failures produce four dispatches; both
administrative admission denials produce none.

## Explicit limits and preconditions

The existing `IVOAI_TEST_MODE=1` guard selects a temporary server filesystem root
and omits the production installation/systemd precondition. It does not replace
the server command adapter, process, bearer authorization, network transport,
MCP request or status decision. This proves the HTTP/CLI/Core/backend composition
under that bounded layout fixture. It does not certify production service
ownership, resource limits, systemd lifecycle, an installed user service, account
scope changes or shared Mindsite synchronization. Embeddings are disabled in this
fixture; no provider inference or paid API is involved.

The test is opt-in through `IVOAI_ADMIN_REAL_MEMORY_BINARY`; absent input is a
skip, not a successful integration. Configured wrong bytes or missing network
isolation fail before a backend process is started. The explicit captured runs
provided the reviewed artifact and private namespace. The existing broad matrix
still correctly records its own unavailable backend precondition.
The captured platform is Linux amd64 with bubblewrap/user namespaces available;
these runs do not certify an ARM backend executable or another operating system.

Passing this case reduces the remaining identified positive administrative HTTP
certification gaps from 18 to 17 after integration of this and PR #76's three
proofs. Context's three additional operations are not included in that count.
IVOAI-189 remains In Progress.

## Reproduction and retained evidence

Use the reviewed Go1.27.3 toolchain and binary, a private build cache, and the
[isolated driver](evidence/admin-memory-status-http-20261106/run-private.py):

```sh
GOMAXPROCS=2 GOMEMLIMIT=768MiB go test -race -c \
  -o /private/admin-race.test ./internal/admin
python3 release-notes/evidence/admin-memory-status-http-20261106/run-private.py \
  --memory-binary /reviewed/ai-memory-3.4.2 \
  --test-binary /private/admin-race.test \
  --output /private/memory-http-proof --label explicit-race
GOMAXPROCS=2 go vet ./internal/admin
```

The driver mounts only the selected inputs and generates/deletes its runtime
directory. It does not read or copy personal credentials. The test terminates
and waits for its owned child before runtime cleanup. There is no Docker image,
model download, global cache cleanup, system service mutation or external API.

An initial failed fixture attempt is retained: the standalone test executable
had not registered the real top-level server command adapter. Registering the
same `servercmd.New` used by the actual CLI resolved that setup error. No product
guard or production behavior was changed. Subsequent results and exact hashes
are recorded in the validation index beside the logs; unfavorable attempts are
not overwritten or counted as successful execution.

Another retained setup failure attempted to mount the race executable before
its linker had finished. Bubblewrap refused the missing input before any test or
backend ran. The driver now requires an existing executable, and the final race
run started only after compilation completed. A separate intentional same-network
namespace invocation failed at the isolation guard before binding the backend
port; that expected rejection is recorded separately from positive integration.
