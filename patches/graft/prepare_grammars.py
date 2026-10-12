#!/usr/bin/env python3
"""Keep Graft's closed WASM registry and attach reviewed redistribution notices.

Operate only on disposable build staging, after npm ci and before packaging.
No network, compilation, upstream code execution or signature-verification claim.
"""
import hashlib
import json
import pathlib
import re
import shutil
import sys


def digest(path):
    if path.is_symlink() or not path.is_file():
        raise ValueError('expected a regular input file')
    return hashlib.sha256(path.read_bytes()).hexdigest()


def prepare(root, catalog):
    root, catalog = pathlib.Path(root), pathlib.Path(catalog)
    data = json.loads((catalog / 'inventory.json').read_text())
    rows = {row['language']: row for row in data['grammars']}
    if len(rows) != len(data['grammars']):
        raise ValueError('duplicate grammar inventory')
    declared = set()
    for name in ['generic.ts', 'container.ts']:
        source = (root / 'src/graph' / name).read_text()
        declared.update(re.findall(r'\bwasm:\s*"([a-z_]+)"', source))
    if declared != set(rows):
        raise ValueError('runtime grammar registry differs from reviewed inventory')
    # Registry-based loading is the only admitted path. A new caller must be
    # reviewed before pruning can continue, even if it happens to load old names.
    for source in (root / 'src').rglob('*.ts'):
        if source.name.endswith('.test.ts') or '__tests__' in source.parts:
            continue
        relative = source.relative_to(root).as_posix()
        if re.search(r'\b(?:loadWasmLanguage|requireWasm)\s*\(', source.read_text()):
            if relative not in ['src/graph/generic.ts', 'src/graph/container.ts']:
                raise ValueError('new dynamic WASM loader requires review')
    pkg = root / 'node_modules/tree-sitter-wasm'
    lock = json.loads((root / 'package-lock.json').read_text())['packages']['node_modules/tree-sitter-wasm']
    if lock['version'] != data['bundle']['version'] or lock['integrity'] != data['bundle']['integrity']:
        raise ValueError('WASM bundle pin differs from reviewed inventory')
    if json.loads((pkg / 'package.json').read_text())['version'] != data['bundle']['version']:
        raise ValueError('installed WASM bundle version differs')
    # Validate the complete batch before making any changes.
    for row in rows.values():
        name = row['language']
        if not re.fullmatch(r'[a-z_]+', name) or row['license'] not in ['MIT', 'Apache-2.0', 'CC0-1.0']:
            raise ValueError('unreviewed grammar or license')
        binary = pkg / 'out' / name / ('tree-sitter-' + name + '.wasm')
        if digest(binary) != row['wasmSHA256']:
            raise ValueError('WASM hash differs from reviewed inventory')
        if row['licenseFile'] != name + '.LICENSE' or digest(catalog / row['licenseFile']) != row['licenseSHA256']:
            raise ValueError('grammar license differs from reviewed inventory')
    for path in (pkg / 'out').rglob('*'):
        if path.is_symlink():
            raise ValueError('unexpected symlink in WASM bundle')
    for directory in (pkg / 'out').iterdir():
        if directory.name not in rows:
            if directory.is_dir():
                shutil.rmtree(directory)
            else:
                directory.unlink()
            continue
        keep = {'tree-sitter-' + directory.name + '.wasm', 'tree-sitter-' + directory.name + '.wasm.sigstore.json'}
        for child in directory.iterdir():
            if child.name not in keep:
                if child.is_dir():
                    shutil.rmtree(child)
                else:
                    child.unlink()
    # Queries are Graft's own dist/graph/queries/*.scm; bundled editor queries
    # are never consumed. Preserve the bundle resolver's accurate inventory.
    (pkg / 'manifest.json').write_text(json.dumps({name: [] for name in sorted(rows)}, indent=2) + '\n')
    notices = pkg / 'THIRD_PARTY_LICENSES'
    notices.mkdir(exist_ok=True)
    for row in rows.values():
        shutil.copyfile(catalog / row['licenseFile'], notices / row['licenseFile'])
    shutil.copyfile(catalog / 'inventory.json', notices / 'inventory.json')
    readme = catalog / 'README.md'
    if readme.exists():
        shutil.copyfile(readme, notices / 'README.md')


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit('usage: prepare_grammars.py UPSTREAM_BUILD_ROOT')
    prepare(pathlib.Path(sys.argv[1]), pathlib.Path(__file__).with_name('grammar-licenses'))
