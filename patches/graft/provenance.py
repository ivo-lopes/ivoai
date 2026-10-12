#!/usr/bin/env python3
"""Emit stable provenance for the reviewed local toolchain and build inputs."""
import hashlib
import json
import pathlib
import subprocess
import sys
payload, repo = map(pathlib.Path, sys.argv[1:3])
arch, node_sha, flags = sys.argv[3:]
files = ['scripts/build-managed-graft.sh', 'patches/graft/build.py', 'patches/graft/provenance.py', 'patches/graft/check-runtime.mjs', 'patches/graft/tree-sitter-node26.patch', 'patches/graft/metadata.py', 'patches/graft/graft-runtime', 'patches/graft/check-abi.py']
provenance = {
    'managedVersion': '0.21.1-ivoai.1', 'upstreamVersion': '0.21.1',
    'revision': 'fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad',
    'sourceURL': 'https://codeload.github.com/trailhq/Graft/tar.gz/fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad',
    'sourceSHA256': 'ab27f9123eeb59b3987a342a24b7e357ad3412f41c556fde3a76ca9e250d2db1',
    'lockSHA256': 'c05064ea18ed1b538e27cc788f1d6ea2cae00b2c00c79ef552fc1bb4c54fd5f5',
    'nodeVersion': '26.11.1', 'nodeArchiveSHA256': node_sha,
    'platform': 'linux', 'architecture': arch, 'nativeCPUFlags': flags,
    'license': 'MIT', 'npmLifecycleScripts': False,
    'compiler': subprocess.check_output(['g++', '--version'], text=True).splitlines()[0],
    'python': sys.version.split()[0],
    'buildFilesSHA256': {name: hashlib.sha256((repo/name).read_bytes()).hexdigest() for name in files},
}
provenance['patchSHA256'] = hashlib.sha256(''.join(f'{name} {digest}\n' for name, digest in sorted(provenance['buildFilesSHA256'].items())).encode()).hexdigest()
(payload/'provenance.json').write_text(json.dumps(provenance, sort_keys=True, indent=2)+'\n')
