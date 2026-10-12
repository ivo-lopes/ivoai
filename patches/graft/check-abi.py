#!/usr/bin/env python3
"""Reject native artifacts exceeding the managed Linux runtime ABI baseline."""
import json
import pathlib
import re
import subprocess
import sys

LIMITS = {'GLIBC': '2.35', 'GLIBCXX': '3.4.30', 'CXXABI': '1.3.13'}

def version_key(version):
    return tuple(map(int, version.split('.')))

def check_versions(output):
    found = {}
    for namespace, version in re.findall(r'\bName: (GLIBCXX|GLIBC|CXXABI)_([A-Za-z0-9_.]+)', output):
        if not re.fullmatch(r'[0-9]+(?:\.[0-9]+)*', version) or version_key(version) > version_key(LIMITS[namespace]):
            raise ValueError(f'{namespace}_{version} exceeds supported {namespace}_{LIMITS[namespace]}')
        if namespace not in found or version_key(version) > version_key(found[namespace]):
            found[namespace] = version
    return found

def audit(root):
    root = pathlib.Path(root)
    results = {}
    for path in sorted(root.rglob('*.node')):
        if path.is_symlink() or not path.is_file():
            raise ValueError('native binding must be a regular file')
        output = subprocess.check_output(['/usr/bin/readelf', '--version-info', '--wide', str(path)], text=True, env={'PATH': '/usr/bin:/bin', 'LC_ALL': 'C'})
        try:
            results[path.relative_to(root).as_posix()] = check_versions(output)
        except ValueError as exc:
            raise ValueError(f'{path.relative_to(root)}: {exc}; rebuild with the Debian 12/GCC 12 compatibility toolchain') from exc
    if len(results) != 10:
        raise ValueError(f'expected ten reviewed native bindings, found {len(results)}')
    (root/'abi.json').write_text(json.dumps({'maximumAllowed': LIMITS, 'nativeRequirements': results}, sort_keys=True, indent=2)+'\n')

if __name__ == '__main__':
    try:
        audit(sys.argv[1])
    except (ValueError, subprocess.CalledProcessError) as exc:
        raise SystemExit(str(exc))
