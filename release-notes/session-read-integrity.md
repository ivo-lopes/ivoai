# Session enumeration does not initialize or repair storage

`SessionStore.List` previously called `EnsurePrivateDir`, which could create the
session directory or change its permissions during an administrative read.
List and ListBounded now open directories without following links and validate
the actual descriptor's effective-user ownership and private permissions. They
return an empty result for absent storage and reject unsafe storage without
repairing it. Mutation/admission paths retain responsibility for initialization.

Tests compare directory/file ctime, content, modes and the complete tree before
and after List/ListBounded/Active/frontend lookup, checking both absence and an
owned existing journal-backed session. Root/namespace symlinks, ordinary files
and exposed permissions are refused without changing either source or target.
Complete Session race passed in 7.674s, Session/App/Admin packages passed in
1.655s/22.792s/0.050s, and stronger returned-identity focused race checks passed in
1.040s. [Evidence](evidence/session-read-integrity-20261104/validation.json).

This is separate from the immutable e3b43138d public Codex DAG/resume proof;
that historical receipt is preserved and not relabeled as a newer binary run.
