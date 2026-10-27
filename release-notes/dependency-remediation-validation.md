# Dependency remediation during backend-first validation

The first CI run for PR #31, at `9874dbdf`, stopped in the documentation
production-dependency audit: 23 advisories (one critical, nine high, nine
moderate, four low). Twelve independent jobs passed; two downstream jobs were
skipped. This was not an acceptable promotion result. No merge or release was
performed, and no advisory was excluded to pass the gate.

## Documentation

Exact patch overrides update DOMPurify, fast-uri, follow-redirects, launch-editor,
body-parser, qs, serialize-javascript and undici. The frozen lockfile records
their integrity and the embedded EN/pt-BR site was rebuilt. The production audit
now reports zero advisories; before/after counts and candidate input hashes are
in `evidence/backend-first/dependency-remediation/documentation-audit.json`.
The production build, embedded-site consistency check and docsserver Go tests
passed locally.

An initial accessibility run overlapped a separate consistency rebuild, which
removed the served build temporarily: 149 tests passed and three failed while
pages were unavailable. Those three passed in an isolated rerun. A complete
serialized rerun and exact-head CI remain separate validation results; the
failed run is not counted as a complete passing suite.

## Native-client limitation

Claude Code 2.1.251 administrative authentication status returned exit 1,
`loggedIn=false`, `authMethod=none`. No inference or paid fallback was attempted.
The sanitized result is retained in
`evidence/backend-first/native-clients/claude-auth-status.json`. Runtime and
native TUI parity remain unverified for Claude in this environment.
