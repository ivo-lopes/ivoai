# G1a — combined main benchmark source epoch

Local merge `7a3e8bccadfdc2b313c7a446a708b6bab267b5e6` integrates main `a1ccf4ca33a1e1f748810add9e03fe61fb963831` into the G1a branch after classification correction `441bf912e33cbdeacfd0b6070a4c07acd5c3b11a`. The merge had no conflicts. The Memory issuer is byte-for-byte identical to the corrected source: the full `MemoryRetentionDecision`, including sensitivity, remains part of the scope/cache domain.

The incoming main changes are benchmark fixtures, executable edit oracles, process observations, scripts and evidence. This merge adds no App/scheduler/plan/worker invocation of the Memory snapshot SPI and no new retention policy, authentication client, credentials, production ingestion or native-runner behavior. Imported opt-in live benchmark helpers were not executed as authenticated/model sessions in this validation.

The initial G1a and classification-correction manifests, receipts and adverse reproductions remain unchanged. Their historical results are not reassigned to this combined source. The [new epoch receipt](evidence/strict-memory-response-snapshot/epoch-main-a1cc/validation.json) records full regular Go tests, Python discovery, affected-package race, mandatory source-pinned rollback, vet and the independent sensitivity regression against the combined tree. Exact final-head hosted CI remains required; this is not release certification, institutional runtime admission or economic proof.

G1a remains an unwired host SPI. Existing limits on unknown retention, page completeness, local-versus-remote revocation, protected binding loss and old-Core protected-disk quota/GC behavior still apply. The v0.14.0 tag and personal/production configuration were not changed.
