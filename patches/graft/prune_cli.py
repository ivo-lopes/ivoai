#!/usr/bin/env python3
"""Remove unreachable YAML CLI dependencies from the reviewed library runtime."""
import json
import pathlib
import re
import shutil
import sys

# This is a closed reviewed dependency graph, not a general JS dependency solver.
FORBIDDEN = re.compile(r'''["'](?:argparse|sprintf-js)(?:/[^"']*)?["']''')

def prune(root):
    root = pathlib.Path(root)
    modules = root / 'node_modules'
    yaml = modules / 'js-yaml'
    manifest = json.loads((yaml / 'package.json').read_text())
    if manifest['version'] != '3.15.2' or manifest.get('dependencies', {}).get('argparse') != '^1.0.7':
        raise ValueError('YAML dependency graph requires review')
    allowed = modules / 'argparse'
    cli = yaml / 'bin'
    for path in (p for directory in [modules, root/'src', root/'dist'] for p in directory.rglob('*')):
        if path.is_symlink():
            # npm command links are removed by the existing packaging pass.
            if '.bin' in path.parts:
                continue
            raise ValueError('unexpected dependency symlink')
        if path.suffix not in {'.js', '.mjs', '.cjs', '.ts', '.mts', '.cts'} or not path.is_file():
            continue
        if path.is_relative_to(allowed) or path.is_relative_to(cli):
            continue
        if FORBIDDEN.search(path.read_text(errors='strict')):
            raise ValueError('new runtime dependency on removed CLI packages')
    if not allowed.is_dir() or not cli.is_dir():
        raise ValueError('expected pinned CLI packages')
    shutil.rmtree(allowed)  # includes its private sprintf-js dependency
    shutil.rmtree(cli)
    manifest['dependencies'].pop('argparse')
    manifest.pop('bin', None)
    (yaml / 'package.json').write_text(json.dumps(manifest, indent=2)+'\n')
    for path in modules.rglob('package.json'):
        if json.loads(path.read_text()).get('name') in {'argparse', 'sprintf-js'}:
            raise ValueError('removed CLI package remains in runtime')
    # Keep the original reviewed build lock separately. Runtime inventory must
    # describe shipped bytes and must not pretend the source lock was clean.
    lockpath = root / 'package-lock.json'
    lock = json.loads(lockpath.read_text())
    for name in list(lock['packages']):
        if name == 'node_modules/argparse' or name.startswith('node_modules/argparse/'):
            del lock['packages'][name]
    lock['packages']['node_modules/js-yaml']['dependencies'].pop('argparse')
    lock['packages']['node_modules/js-yaml'].pop('bin', None)
    lockpath.write_text(json.dumps(lock, indent=2)+'\n')

if __name__ == '__main__':
    prune(sys.argv[1])
