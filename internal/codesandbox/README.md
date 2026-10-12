# Disposable Node helper sandbox

`Exec(node, args, readPaths, writePaths)` is called only by a dedicated internal
helper subprocess. It replaces that process; on any returned error the helper
must exit immediately. Never call it in the main application or from an untrusted
plugin. No shell command is involved. Arguments exclude `argv[0]`.

The Linux amd64/arm64 implementation requires Landlock ABI 3 or later, seccomp,
`no_new_privs`, and `close_range`. Missing support fails closed. It locks the
calling OS thread, drops its capabilities, restricts filesystem access, marks
all descriptors above stderr close-on-exec, filters syscalls and execs Node.
Root execution is rejected; callers running as root must arrange a separate
unprivileged helper. Other architectures/platforms fail closed.

The caller authorizes existing absolute paths. Read grants include directory
listing and file reading; write grants additionally allow regular files,
directories, symlinks, removal, rename/link within authorized scope and
truncation. Only Node and the exact ELF interpreter embedded in that executable
receive execute grants. Runtime libraries must be explicitly readable. Device
creation is never granted; on ABI 5+ device ioctl access is also restricted.

Directories grant their entire subtree, including existing mount points. Do not
grant `/`, the user's home, credentials directories, or a writable Node runtime.
Use a reviewed immutable runtime and private per-project cache. Scope selection,
canonical identity checks, private permissions and protection against concurrent
changes to grant paths remain the caller's responsibility. Landlock does not
hide all filesystem metadata. Timestamp, mode, ownership and extended-attribute
mutation syscalls are denied separately.

Network socket creation/use and process creation are denied. `clone3` returns
ENOSYS for libc fallback; legacy clone permits only threads sharing address space
and signal handlers, with no namespace creation or process exit signal. Node
worker threads remain available. io_uring, ptrace, cross-process memory/descriptor
access and namespace/mount operations are blocked. The filter checks syscall
architecture and rejects x32. This is a denylist filter, not a universal syscall
allowlist or a replacement for kernel security updates.

Pass a minimal environment with no provider credentials, NODE_OPTIONS or preload
variables. Pass only trusted files or pipes as stdin/stdout/stderr, never network
sockets. The API inherits these three descriptors and the environment deliberately.
Apply wall-clock, output, CPU and memory limits externally. ELF loading and path
validation happen before confinement and must receive trusted inputs. Other Go
threads are trusted setup code; exec removes them before any Node code executes.

The tested Node 26.11.1 needs `--openssl-config=/dev/null` and a read grant for
`/dev/null` to avoid reading the user's/system OpenSSL configuration. The test
also grants `/lib`, `/lib64`, `/usr/lib`, `/dev/urandom`, its read fixture and a
private writable directory; production should narrow library grants where
practical. Graft's Git/LSP subprocesses cannot run inside this sandbox: collect
trusted Git information separately or use its filesystem fallback, and leave
LSP disabled. This package does not launch or configure Graft.

Validation:

```
CODESANDBOX_TEST_NODE=/absolute/reviewed/node go test ./internal/codesandbox
GOOS=linux GOARCH=arm64 go test -c -o /tmp/codesandbox-arm64.test ./internal/codesandbox
```

The Node test uses a helper process and checks authorized reads/writes, external
read/write denial, a symlink escape, child-process denial, TCP/UDP denial and a
successful worker thread. The unavailable-kernel test installs a helper-local
filter that makes Landlock return ENOSYS and verifies refusal. ARM64 compilation
is checked separately; compilation alone is not runtime validation on ARM64.
