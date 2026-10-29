#!/usr/bin/env python3
"""Offline post-download verification; never runs downloaded code or contacts a service."""
import argparse
import base64
import hashlib
import gzip
import json
import os
from pathlib import Path
import re
import resource
import signal
import shutil
import stat
import struct
import subprocess
import sys
import tarfile
import zipfile

TAG = 'v0.14.0'
EXPECTED = {'checksums.txt', 'ivoai-memory-context.zip'} | {
    name.format(arch=arch) for arch in ('amd64', 'arm64') for name in (
        'ivoai_linux_{arch}.tar.gz', 'ivoai-opencode_linux_{arch}.tar.gz',
        'managed-opencode-{arch}.json', 'ivoai-graft_linux_{arch}.tar.gz',
        'managed-graft-{arch}.json')}
MiB = 1 << 20
MAX_CORE_BYTES = 256 * MiB
MAX_GZIP_BYTES = MAX_CORE_BYTES + 65536
MAX_ZIP_BYTES = 512 << 10
ASSET_LIMITS = {name: (65536 if name == 'checksums.txt' or name.endswith('.json') else
                      MAX_ZIP_BYTES if name.endswith('.zip') else
                      128 * MiB if name.startswith('ivoai_linux_') else
                      256 * MiB if name.startswith('ivoai-opencode_') else
                      512 * MiB) for name in EXPECTED}
HEX64 = re.compile(r'^[a-f0-9]{64}$')
GRAFT_FILES = ['scripts/build-managed-graft.sh', 'patches/graft/build.py',
 'patches/graft/provenance.py', 'patches/graft/check-runtime.mjs',
 'patches/graft/tree-sitter-node26.patch', 'patches/graft/metadata.py',
 'patches/graft/graft-runtime', 'patches/graft/check-abi.py',
 'patches/graft/bounded-parse-lifecycle.patch', 'patches/graft/check-parse-lifecycle.mjs',
 'patches/graft/viewer-escaping.patch', 'patches/graft/test_viewer_escaping.py',
 'patches/graft/go-package-calls.patch', 'patches/graft/check-go-package-calls.mjs',
 'patches/graft/dependency-lock.patch', 'patches/graft/check-dependencies.mjs',
 'patches/graft/check-yaml-library.mjs', 'patches/graft/prune_cli.py',
 'patches/graft/prepare_grammars.py']
NODE_SHA = {'amd64': '15fc043a51a5783f2f4b634561091658f5a7f0e3db03f997dca3a8033a33e45b',
 'arm64': '170454fb022f5e9b5f6cc76c25fa32ef5100895be44eb04e92f9780591491bbf'}
BUN_SHA = {'amd64': '951ee2aee855f08595aeec6225226a298d3fea83a3dcd6465c09cbccdf7e848f',
 'arm64': 'a27ffb63a8310375836e0d6f668ae17fa8d8d18b88c37c821c65331973a19a3b'}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    value = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            value.update(block)
    return value.hexdigest()


def unique_json(text):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate JSON key')
            result[key] = value
        return result
    return json.loads(text, object_pairs_hook=pairs)


def regular(path, limit=2 << 30):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode), 'asset must be a regular non-symlink file: ' + path.name)
    require(0 < info.st_size <= limit, 'invalid asset size: ' + path.name)
    return info


def manifest_hashes(assets):
    require({p.name for p in assets.iterdir()} == EXPECTED, 'asset directory must contain exactly the 12 expected files')
    sizes = [regular(assets / name, ASSET_LIMITS[name]).st_size for name in EXPECTED]
    require(sum(sizes) <= 2 << 30, 'total compressed asset budget exceeded')
    hashes = {name: digest(assets / name) for name in sorted(EXPECTED)}
    regular(assets / 'checksums.txt', 65536)
    listed = {}
    for line in (assets / 'checksums.txt').read_text('ascii').splitlines():
        match = re.fullmatch(r'([a-f0-9]{64})  ([A-Za-z0-9_.-]+)', line)
        require(match is not None, 'unsafe or malformed checksum entry')
        value, name = match.groups()
        require(name in EXPECTED - {'checksums.txt'} and name not in listed, 'unexpected or duplicate checksum name')
        listed[name] = value
    require(set(listed) == EXPECTED - {'checksums.txt'}, 'checksums must name exactly 11 payloads')
    for name, value in listed.items():
        require(hashes[name] == value, 'offline checksum mismatch: ' + name)
    return hashes


def source_contract(source, commit):
    require(re.fullmatch('[a-f0-9]{40}', commit) is not None, 'expected source commit must be a full lowercase SHA')
    def git(*args):
        return subprocess.check_output(['git', '-C', str(source), *args], text=True).strip()
    require(not git('status', '--porcelain'), 'approved source tree must be clean')
    require(git('rev-parse', 'HEAD^{tree}') == git('rev-parse', commit + '^{tree}'), 'source checkout differs from approved commit tree')
    mod = (source / 'go.mod').read_text()
    module = re.search(r'^module (\S+)$', mod, re.M).group(1)
    go_version = re.search(r'^go (\S+)$', mod, re.M).group(1)
    require(module == 'github.com/ivo-lopes/ivoai' and go_version == '1.27.3', 'unexpected module/toolchain contract')
    require(not re.search(r'^\s*(replace|exclude)\b', mod, re.M), 'unexpected module replacement/exclusion')
    requires = {}
    in_require = False
    for line in mod.splitlines():
        line = line.split('//', 1)[0].strip()
        if line == 'require (':
            in_require = True
        elif in_require and line == ')':
            in_require = False
        elif in_require and line:
            name, version = line.split()
            require(name not in requires, 'duplicate module requirement')
            requires[name] = version
        elif line.startswith('require '):
            name, version = line.split()[1:]
            require(name not in requires, 'duplicate module requirement')
            requires[name] = version
    sums = {}
    for line in (source / 'go.sum').read_text().splitlines():
        name, version, value = line.split()
        if not version.endswith('/go.mod'):
            sums[(name, version)] = value
    return {'module': module, 'go_version': 'go' + go_version,
            'requires': requires, 'sums': sums, 'source_commit': commit,
            'go_mod_sha256': digest(source / 'go.mod'), 'go_sum_sha256': digest(source / 'go.sum')}


def expected_patches(source):
    opencode = hashlib.sha256(b''.join((source / name).read_bytes() for name in (
        'patches/opencode/1.19.0-clipboard-errors.patch',
        'patches/opencode/1.19.0-plugin-sdk-version.patch',
        'patches/opencode/1.19.0-managed-local-plugins.patch'))).hexdigest()
    files = GRAFT_FILES + [p.relative_to(source).as_posix() for p in
                            sorted((source / 'patches/graft/grammar-licenses').iterdir()) if p.is_file()]
    values = {name: digest(source / name) for name in files}
    graft = hashlib.sha256(''.join(name + ' ' + value + '\n' for name, value in sorted(values.items())).encode()).hexdigest()
    return {'opencode': opencode, 'graft': graft}


def component_metadata(assets, component, arch, hashes, patches):
    name = 'managed-' + component + '-' + arch + '.json'
    regular(assets / name, 65536)
    metadata = unique_json((assets / name).read_text('utf-8'))
    common = {'version', 'platform', 'revision', 'sha256', 'archive', 'upstream_revision', 'source_sha256', 'patch_sha256'}
    if component == 'opencode':
        require(set(metadata) == common | {'bun_version', 'bun_sha256'}, 'unexpected OpenCode metadata schema')
        require(metadata['version'] == '1.19.0-ivoai.3', 'wrong OpenCode version')
        require(metadata['upstream_revision'] == '733b2c627e5aec39cb33cc22373bdce33053ef2c', 'wrong OpenCode source revision')
        require(metadata['source_sha256'] == 'ab433f7a5fc9e54c90da18bd3457298009f426424805f043aa32c03543b4a192', 'wrong OpenCode source digest')
        require(metadata['bun_version'] == '1.3.14' and metadata['bun_sha256'] == BUN_SHA[arch], 'wrong reviewed Bun identity')
    else:
        require(set(metadata) == common | {'node_version', 'node_sha256'}, 'unexpected Graft metadata schema')
        require(metadata['version'] == '0.21.1-ivoai.1', 'wrong Graft version')
        require(metadata['upstream_revision'] == 'fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad', 'wrong Graft source revision')
        require(metadata['source_sha256'] == 'd39598390324ad0076a88ab593b63ad2ba352370ea07c073e31f11327979f8e1', 'wrong current Graft source digest')
        require(metadata['node_version'] == '26.11.1' and metadata['node_sha256'] == NODE_SHA[arch], 'wrong reviewed Node identity')
    archive = 'ivoai-' + component + '_linux_' + arch + '.tar.gz'
    require(metadata['platform'] == 'linux/' + arch and metadata['archive'] == archive, 'metadata platform/archive mismatch')
    require(metadata['sha256'] == hashes[archive] and metadata['patch_sha256'] == patches[component], 'archive or patch association mismatch')
    for key in ('sha256', 'source_sha256', 'patch_sha256', 'revision'):
        require(isinstance(metadata[key], str) and HEX64.fullmatch(metadata[key]), 'malformed component digest')
    identity = hashlib.sha256((metadata['source_sha256'] + '\n' + metadata['patch_sha256'] + '\n' + metadata['sha256'] + '\n').encode()).hexdigest()
    require(metadata['revision'] == identity, 'compiled object identity mismatch')
    return metadata


class BoundedGzipReader:
    """Forward-only decompression budget, before tarfile header processing."""
    def __init__(self, stream, limit=MAX_GZIP_BYTES):
        self.stream, self.limit, self.position = stream, limit, 0

    def tell(self):
        return self.position

    def read(self, size=-1):
        require(0 <= size <= MiB, 'unbounded or oversized decompression read')
        require(self.position + size <= self.limit, 'Core decompression budget exceeded')
        data = self.stream.read(size)
        self.position += len(data)
        return data

    def seek(self, offset, whence=0):
        require(whence == 0 and self.position <= offset <= self.limit,
                'backward or out-of-budget tar seek')
        while self.position < offset:
            require(bool(self.read(min(MiB, offset - self.position))), 'truncated tar seek')
        return self.position


class CoreTarInfo(tarfile.TarInfo):
    def _proc_member(self, stream):
        # Reject PAX, GNU long names, sparse files, links and directories before
        # their internal processors can read extension payloads.
        require(self.type in (tarfile.REGTYPE, tarfile.AREGTYPE), 'Core tar extension or nonregular header refused')
        return super()._proc_member(stream)


def safe_core(archive, destination, arch):
    regular(archive, 128 * MiB)
    with open(archive, 'rb') as compressed, gzip.GzipFile(fileobj=compressed, mode='rb') as decoded:
        bounded = BoundedGzipReader(decoded)
        with tarfile.open(fileobj=bounded, mode='r:', tarinfo=CoreTarInfo) as stream:
            member = stream.next()
            require(member is not None, 'empty Core archive')
            require(member.name == 'ivoai' and member.isreg() and 0 < member.size <= MAX_CORE_BYTES,
                    'unsafe Core archive member')
            require(member.mode & 0o111, 'Core archive member lacks executable mode')
            flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, 'O_NOFOLLOW', 0)
            with os.fdopen(os.open(destination, flags, 0o700), 'wb') as target:
                with stream.extractfile(member) as payload:
                    remaining = member.size
                    while remaining:
                        block = payload.read(min(MiB, remaining))
                        require(bool(block), 'truncated Core payload')
                        target.write(block)
                        remaining -= len(block)
            require(stream.next() is None, 'Core archive must contain exactly one member')
    with open(destination, 'rb') as extracted:
        header = extracted.read(64)
    require(header[:6] == b'\x7fELF\x02\x01' and len(header) == 64, 'Core is not ELF64 little endian')
    require(struct.unpack_from('<H', header, 16)[0] == 2, 'unexpected Core ELF type')
    require(struct.unpack_from('<H', header, 18)[0] == {'amd64': 62, 'arm64': 183}[arch], 'Core ELF architecture mismatch')


def skill_contents(path, expected):
    size = regular(path, MAX_ZIP_BYTES).st_size
    require(len(expected) <= 256 << 10, 'reviewed skill exceeds fixed content budget')
    require(size >= 22, 'short ZIP')
    with open(path, 'rb') as stream:
        stream.seek(-22, 2)
        end = stream.read(22)
        if size >= 42:
            stream.seek(-42, 2)
            require(stream.read(4) != b'PK\x06\x07', 'ZIP64 locator refused')
    signature, disk, directory_disk, count_disk, count, directory_size, directory_offset, comment = struct.unpack('<4s4H2LH', end)
    require(signature == b'PK\x05\x06' and disk == directory_disk == 0 and count_disk == count == 2,
            'ZIP EOCD/count mismatch')
    require(comment == 0 and directory_size <= 4096 and directory_offset + directory_size == size - 22,
            'ZIP central-directory budget/layout mismatch')
    with zipfile.ZipFile(path) as archive:
        require(archive.namelist() == ['ivoai-memory-context/', 'ivoai-memory-context/SKILL.md'], 'unexpected skill archive entries')
        directory, member = archive.infolist()
        for entry in (directory, member):
            require(entry.flag_bits == 0 and entry.compress_type == zipfile.ZIP_STORED and not entry.extra and not entry.comment,
                    'noncanonical ZIP flags/compression/extra fields')
        require(directory.file_size == directory.compress_size == 0 and stat.S_IFMT(directory.external_attr >> 16) == stat.S_IFDIR,
                'unsafe skill directory')
        require(member.file_size == member.compress_size == len(expected) and stat.S_IFMT(member.external_attr >> 16) == stat.S_IFREG,
                'unsafe skill member or size')
        require(archive.read(member) == expected, 'skill content differs from reviewed source')


def reader_budget():
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_AS, (4 << 30, 4 << 30))
    resource.setrlimit(resource.RLIMIT_CPU, (20, 20))
    resource.setrlimit(resource.RLIMIT_FSIZE, (2 << 20, 2 << 20))


def compiler_budget():
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_AS, (4 << 30, 4 << 30))
    resource.setrlimit(resource.RLIMIT_CPU, (120, 120))
    resource.setrlimit(resource.RLIMIT_FSIZE, (64 << 20, 64 << 20))


def verify_info(info, contract, arch):
    require(info['GoVersion'] == contract['go_version'], 'Core Go toolchain mismatch')
    require(info['Path'] == contract['module'] + '/cmd/ivoai', 'Core command path mismatch')
    require(info['Main']['Path'] == contract['module'] and info['Main']['Version'] == TAG, 'Core main module/version mismatch')
    require(not info['Main'].get('Replace'), 'Core main module replacement')
    settings = {}
    for item in info['Settings']:
        require(item['Key'] not in settings, 'duplicate Core build setting')
        settings[item['Key']] = item['Value']
    required = {'-buildmode': 'exe', '-compiler': 'gc', '-trimpath': 'true',
                'CGO_ENABLED': '0', 'GOOS': 'linux', 'GOARCH': arch, 'vcs': 'git',
                'vcs.revision': contract['source_commit'], 'vcs.modified': 'false'}
    if arch == 'amd64':
        required['GOAMD64'] = 'v1'
    for key, value in required.items():
        require(settings.get(key) == value, 'Core build setting mismatch: ' + key)
    dependencies = {}
    for dep in info['Deps']:
        require(not dep.get('Replace') and dep['Path'] not in dependencies, 'replacement or duplicate dependency')
        require(contract['requires'].get(dep['Path']) == dep['Version'], 'Go dependency version mismatch')
        require(contract['sums'].get((dep['Path'], dep['Version'])) == dep['Sum'], 'Go dependency checksum mismatch')
        dependencies[dep['Path']] = {'version': dep['Version'], 'sum': dep['Sum']}
    require(set(dependencies) == set(contract['requires']), 'Core dependency set differs from reviewed module requirements')
    return {'go_version': info['GoVersion'], 'main_module': info['Main'], 'settings': settings, 'dependencies': dependencies}


def embedded_association(binary, metadata):
    blob = binary.read_bytes()
    result = {}
    for component, document in metadata.items():
        body = {**document, 'url': 'https://github.com/ivo-lopes/ivoai/releases/download/' + TAG + '/' + document['archive']}
        # Exact serialization of release.yml's jq -c plus its trailing newline.
        encoded = base64.b64encode((json.dumps(body, separators=(',', ':'), ensure_ascii=False) + '\n').encode())
        count = blob.count(encoded)
        require(count > 0, 'Core lacks exact embedded ' + component + ' release payload')
        result[component] = {'metadata': body, 'base64_payload_occurrences': count,
                             'method': 'exact expected base64 payload in ELF; no execution or pointer-binding claim'}
    return result


def api_hashes(path, hashes, assets):
    if path is None:
        return {'provided': False, 'verified_sha256': [], 'missing_sha256': sorted(EXPECTED)}
    regular(path, 2 << 20)
    api = unique_json(path.read_text())
    require(api.get('tag_name') == TAG and api.get('draft') is False, 'API tag/publication mismatch')
    entries = api.get('assets', [])
    require(len(entries) == 12 and {item['name'] for item in entries} == EXPECTED, 'API asset set mismatch')
    verified, missing = [], []
    for item in entries:
        name = item['name']
        require(item['size'] == (assets / name).stat().st_size, 'API asset size mismatch')
        value = item.get('digest')
        if value:
            require(value == 'sha256:' + hashes[name], 'API asset digest mismatch: ' + name)
            verified.append(name)
        else:
            missing.append(name)
    return {'provided': True, 'api_file_sha256': digest(path), 'release_id': api.get('id'),
            'verified_sha256': sorted(verified), 'missing_sha256': sorted(missing)}


def run(args, report):
    assets, source = Path(args.assets).resolve(strict=True), Path(args.source_tree).resolve(strict=True)
    output = Path(args.output).absolute()
    output.mkdir(mode=0o700, parents=False, exist_ok=False)
    report['private_output_created'] = True
    report.update({'output': str(output), 'tag': TAG, 'source_commit': args.source_commit,
                   'network_calls': False, 'downloaded_code_executed': False})
    hashes = manifest_hashes(assets)
    report['asset_limits_bytes'] = ASSET_LIMITS
    report['core_decompressed_limit_bytes'] = MAX_GZIP_BYTES
    report['resource_limits'] = {'python_soft_address_space': 1 << 30, 'wall_seconds': 300, 'reader_address_space': 4 << 30, 'reader_cpu_seconds': 20, 'reader_wall_seconds': 30, 'reader_output_bytes': 2 << 20, 'core_dumps': False, 'gzip_read_bytes': MiB, 'zip_directory_bytes': 4096, 'zip_entries': 2}
    report['asset_sha256'] = hashes
    report['api'] = api_hashes(Path(args.api_json).resolve(strict=True) if args.api_json else None, hashes, assets)
    contract = source_contract(source, args.source_commit)
    report['reviewed_source'] = {k: v for k, v in contract.items() if k not in ('requires', 'sums')}
    patches = expected_patches(source)
    report['patch_sha256'] = patches
    go = shutil.which(args.go) if not Path(args.go).is_absolute() else args.go
    require(go is not None, 'reviewed Go toolchain required')
    env = {'PATH': str(Path(go).parent) + ':/usr/bin:/bin', 'HOME': str(output / 'home'),
           'GOCACHE': str(output / 'go-cache'), 'GOPATH': str(output / 'go-path'),
           'GOENV': 'off', 'GOWORK': 'off', 'GO111MODULE': 'off', 'GOTOOLCHAIN': 'local',
           'GOPROXY': 'off', 'GOSUMDB': 'off', 'CGO_ENABLED': '0', 'GOMEMLIMIT': '64MiB'}
    require(subprocess.check_output([go, 'version'], env=env, text=True, timeout=30, preexec_fn=compiler_budget).split()[2] == contract['go_version'], 'reader Go toolchain mismatch')
    helper = Path(__file__).with_name('buildinfo_reader.go')
    reader = output / 'buildinfo-reader'
    subprocess.run([go, 'build', '-trimpath', '-o', str(reader), str(helper)], env=env, cwd=output, check=True, timeout=120, preexec_fn=compiler_budget)
    report['reader_source_sha256'] = digest(helper)
    report['cores'] = {}
    for arch in ('amd64', 'arm64'):
        metadata = {component: component_metadata(assets, component, arch, hashes, patches)
                    for component in ('opencode', 'graft')}
        binary = output / ('ivoai-' + arch)
        safe_core(assets / ('ivoai_linux_' + arch + '.tar.gz'), binary, arch)
        metadata_output = output / ('buildinfo-' + arch + '.json')
        metadata_error = output / ('buildinfo-' + arch + '.stderr')
        with open(metadata_output, 'xb') as stdout, open(metadata_error, 'xb') as stderr:
            result = subprocess.run([str(reader), str(binary)], env=env, stdout=stdout, stderr=stderr,
                                    timeout=30, preexec_fn=reader_budget)
        require(result.returncode == 0, 'bounded Go metadata reader refused input')
        regular(metadata_output, 2 << 20)
        checked = verify_info(unique_json(metadata_output.read_text()), contract, arch)
        checked['binary_sha256'] = digest(binary)
        checked['embedded_components'] = embedded_association(binary, metadata)
        report['cores'][arch] = checked
    skill_contents(assets / 'ivoai-memory-context.zip', (source / 'skills/ivoai-memory-context/SKILL.md').read_bytes())
    report['checksums_payloads_verified'] = 11
    report['assets_count'] = 12
    report['pass'] = True
    report['limits'] = ['Checksum/metadata association, not a new source attestation or binary reproducibility proof.',
        'Expected embedded bytes are checked; runtime variable binding/behavior is certified separately by Release tests.',
        'No runtime, TUI, no-AVX or ARM execution occurs in this offline verifier.',
        'API digests absent from supplied JSON remain unknown; checksums.txt has no offline self-digest anchor.']
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--assets', required=True)
    parser.add_argument('--source-tree', required=True)
    parser.add_argument('--source-commit', required=True)
    parser.add_argument('--output', required=True, help='new private output directory; must not exist')
    parser.add_argument('--go', default='go')
    parser.add_argument('--api-json', help='REST release JSON saved outside assets directory; optional')
    args = parser.parse_args()
    os.umask(0o077)
    resource.setrlimit(resource.RLIMIT_AS, (1 << 30, resource.getrlimit(resource.RLIMIT_AS)[1]))
    def timed_out(*_):
        raise TimeoutError('offline verifier wall-clock budget exceeded')
    signal.signal(signal.SIGALRM, timed_out)
    signal.alarm(300)
    report = {'pass': False, 'verifier_sha256': digest(Path(__file__))}
    try:
        output = run(args, report)
    except Exception as error:
        report['error'] = str(error)
        output = Path(args.output)
        if report.get('private_output_created') and output.is_dir() and output.stat().st_uid == os.getuid():
            (output / 'verification.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps({'pass': False, 'error': str(error)}))
        return 1
    (output / 'verification.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'pass': True, 'assets': 12, 'payload_checksums': 11,
                      'api_digests': len(report['api']['verified_sha256']),
                      'api_missing_digests': len(report['api']['missing_sha256']),
                      'report': str(output / 'verification.json')}))
    return 0


if __name__ == '__main__':
    sys.exit(main())
