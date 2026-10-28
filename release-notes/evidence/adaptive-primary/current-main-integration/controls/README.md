# Reproduce the counterfactual controls

These overlays change only a disposable test compilation. They do not edit the
working tree or invoke providers. Use Go 1.27.3 with the repository's verified
module cache and source `6fdd943ce447374568f76c698322c1b80f7b836e`. A later report
commit preserves that source. Do not run the copied `.go.txt` files as a new
package: copy them to a private temporary directory and supply an overlay.

Run this Python program from the repository root to create both overlays:

```python
from pathlib import Path
import json, tempfile, os
root = Path.cwd().resolve()
proof = root / 'release-notes/evidence/adaptive-primary/current-main-integration/controls'
out = Path(tempfile.mkdtemp(prefix='ivoai-adaptive-controls-'))
os.chmod(out, 0o700)
for name, files in {
    'naive-extraction-control': {
        'server.go': 'internal/orchestrator/server.go',
        'artifact_reader.go': 'internal/orchestrator/artifact_reader.go',
    },
    'retirement-order-control': {
        'auto_turn.go': 'internal/app/auto_turn.go',
    },
}.items():
    folder = out / name
    folder.mkdir(mode=0o700)
    replacements = {}
    for filename, original in files.items():
        target = folder / filename
        target.write_bytes((proof / (name + '-' + filename + '.txt')).read_bytes())
        replacements[str(root / original)] = str(target)
    (folder / 'overlay.json').write_text(json.dumps({'Replace': replacements}))
print(out)
```

With the printed directory in `control_dir`, run:

```bash
go test -overlay "$control_dir/naive-extraction-control/overlay.json" \
  ./internal/orchestrator -run '^TestAdmittedPrimaryProtectedReadersRecheckAuthority$' \
  -count=1 -v
go test -overlay "$control_dir/retirement-order-control/overlay.json" \
  ./internal/app -run '^TestNativeCompatibilityDenialPrecedesReceiptRetirement$' \
  -count=1 -v
```

Expected exits are both **1**, with no compilation errors. The first has 14
failed denial subcases and two successful positive subcases. The second has four
failed preservation subcases. Running the same test names without overlays must
pass. Exact source hashes and raw/private versus public log hashes are recorded
in `../validation.json`.

The first replaces only artifact handlers and registrations with the incoming
G0 versions, intentionally omitting the newly composed receipt checks. It does
not claim a historical flaw where receipts did not exist. The second changes
only the ordering of the existing retirement and compatibility checks. No
permission, timeout, assertion, model, pin or credential policy is relaxed.
