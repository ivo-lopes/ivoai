#!/usr/bin/env python3
"""Emit stable provenance for the reviewed local toolchain and build inputs."""
import hashlib
import json
import pathlib
import subprocess
import sys
payload, repo = map(pathlib.Path, sys.argv[1:3])
arch, node_sha, flags, compiler = sys.argv[3:]
files = ['scripts/build-managed-graft.sh', 'patches/graft/build.py', 'patches/graft/provenance.py', 'patches/graft/check-runtime.mjs', 'patches/graft/tree-sitter-node26.patch', 'patches/graft/metadata.py', 'patches/graft/graft-runtime', 'patches/graft/check-abi.py', 'patches/graft/bounded-parse-lifecycle.patch', 'patches/graft/check-parse-lifecycle.mjs', 'patches/graft/viewer-escaping.patch', 'patches/graft/test_viewer_escaping.py', 'patches/graft/go-package-calls.patch', 'patches/graft/check-go-package-calls.mjs']
files += ['patches/graft/'+name for name in ['dependency-lock.patch', 'check-dependencies.mjs', 'check-yaml-library.mjs', 'prune_cli.py', 'prepare_grammars.py']]
files += [path.relative_to(repo).as_posix() for path in sorted((repo/'patches/graft/grammar-licenses').iterdir()) if path.is_file()]
provenance = {
    'managedVersion': '0.21.1-ivoai.1', 'upstreamVersion': '0.21.1',
    'revision': 'fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad',
    'sourceURL': 'https://codeload.github.com/trailhq/Trail-Code/tar.gz/fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad',
    'sourceSHA256': 'd39598390324ad0076a88ab593b63ad2ba352370ea07c073e31f11327979f8e1',
    'lockSHA256': 'c05064ea18ed1b538e27cc788f1d6ea2cae00b2c00c79ef552fc1bb4c54fd5f5',
    'reviewedBuildLockSHA256': hashlib.sha256((payload/'build-package-lock.json').read_bytes()).hexdigest(),
    'runtimeLockSHA256': hashlib.sha256((payload/'package-lock.json').read_bytes()).hexdigest(),
    'nodeVersion': '26.11.1', 'nodeArchiveSHA256': node_sha,
    'platform': 'linux', 'architecture': arch, 'nativeCPUFlags': flags,
    'upstreamLicense': 'MIT',
    'runtimeLicenseInventory': 'dependencies.json',
    'wasmLicenseInventory': 'node_modules/tree-sitter-wasm/THIRD_PARTY_LICENSES/inventory.json',
    'npmLifecycleScripts': False,
    'compiler': subprocess.check_output([compiler, '--version'], text=True).splitlines()[0],
    'python': sys.version.split()[0],
    'buildFilesSHA256': {name: hashlib.sha256((repo/name).read_bytes()).hexdigest() for name in files},
}
provenance['patchSHA256'] = hashlib.sha256(''.join(f'{name} {digest}\n' for name, digest in sorted(provenance['buildFilesSHA256'].items())).encode()).hexdigest()
(payload/'provenance.json').write_text(json.dumps(provenance, sort_keys=True, indent=2)+'\n')
