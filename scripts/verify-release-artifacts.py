#!/usr/bin/env python3
"""Bounded offline release gate. Never executes downloaded Core/components or uses a network.

Archive/checksum/component contracts derive from the preserved v0.15.0 verifier.
This gate additionally checks component TARs and the actual recorded ldflag bindings.
"""
import argparse
import base64
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import shlex
import shutil
import signal
import stat
import struct
import subprocess
import sys
import tarfile
import tempfile
import zipfile

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

NODE_SHA = {'amd64': '5848553868b402d3fabb5b09270649977780a390f832155721025a08b5104455',
 'arm64': '439e9c438acdf6a6ffe47418da709153d66c4bed812e30a473faacb107dd2791'}

BUN_SHA = {'amd64': '951ee2aee855f08595aeec6225226a298d3fea83a3dcd6465c09cbccdf7e848f',
 'arm64': 'a27ffb63a8310375836e0d6f668ae17fa8d8d18b88c37c821c65331973a19a3b'}

def require(condition, message):
    if not condition:
        raise ValueError(message)

def digest(path):
    value = hashlib.sha256()
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_size <= 2 << 30, 'unsafe digest input')
        remaining = before.st_size
        while remaining:
            block = stream.read(min(MiB, remaining))
            require(block, 'digest input truncated')
            value.update(block)
            remaining -= len(block)
        require(not stream.read(1), 'digest input grew')
        after = os.fstat(stream.fileno())
        require(before.st_size == after.st_size and before.st_mtime_ns == after.st_mtime_ns,
                'digest input changed')
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
    require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1,
            'asset must be a regular non-linked file: ' + path.name)
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
        require(metadata['version'] == '0.21.1-ivoai.2', 'wrong Graft version')
        require(metadata['upstream_revision'] == 'fe30ead39d5e6f0c921018d364da2bdbc9d4b3ad', 'wrong Graft source revision')
        require(metadata['source_sha256'] == 'd39598390324ad0076a88ab593b63ad2ba352370ea07c073e31f11327979f8e1', 'wrong current Graft source digest')
        require(metadata['node_version'] == '26.13.0' and metadata['node_sha256'] == NODE_SHA[arch], 'wrong reviewed Node identity')
    archive = 'ivoai-' + component + '_linux_' + arch + '.tar.gz'
    require(metadata['platform'] == 'linux/' + arch and metadata['archive'] == archive, 'metadata platform/archive mismatch')
    require(metadata['sha256'] == hashes[archive] and metadata['patch_sha256'] == patches[component], 'archive or patch association mismatch')
    for key in ('sha256', 'source_sha256', 'patch_sha256', 'revision'):
        require(isinstance(metadata[key], str) and HEX64.fullmatch(metadata[key]), 'malformed component digest')
    identity = hashlib.sha256((metadata['source_sha256'] + '\n' + metadata['patch_sha256'] + '\n' + metadata['sha256'] + '\n').encode()).hexdigest()
    require(metadata['revision'] == identity, 'compiled object identity mismatch')
    return metadata

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
        require(directory.header_offset == 0 and member.header_offset == 30 + len(directory.filename) and
                directory_offset == member.header_offset + 30 + len(member.filename) + len(expected),
                'noncanonical ZIP local layout')
        with path.open('rb') as stream:
            for entry in (directory, member):
                stream.seek(entry.header_offset)
                local = struct.unpack('<4s5H3I2H', stream.read(30))
                require(local[0] == b'PK\x03\x04' and local[2] == 0 and local[3] == 0 and
                        local[6:9] == (entry.CRC, entry.compress_size, entry.file_size) and
                        local[9] == len(entry.filename) and local[10] == 0 and
                        stream.read(local[9]) == entry.filename.encode('ascii'),
                        'ZIP local/central headers disagree')
        require(archive.read(directory) == b'', 'skill directory local header mismatch')
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


class Source:
    """Read reviewed source bytes from Git objects, never from mutable worktree files."""
    def __init__(self, root, commit):
        require(re.fullmatch(r'[a-f0-9]{40}', commit) is not None,
                'source must be the full lowercase commit SHA')
        self.root, self.commit = Path(root).resolve(strict=True), commit
        require(self.git('rev-parse', commit + '^{commit}').decode().strip() == commit,
                'source identity is not an exact commit')

    def git(self, *args):
        value = subprocess.check_output(['git', '-C', str(self.root), *args], timeout=15,
                                        stderr=subprocess.DEVNULL)
        require(len(value) <= 8 * MiB, 'source object/list exceeds budget')
        return value

    def read(self, name):
        return self.git('show', self.commit + ':' + name)

    def contract(self):
        mod = self.read('go.mod').decode()
        module = re.search(r'^module (\S+)$', mod, re.M)
        toolchain = re.search(r'^go (\S+)$', mod, re.M)
        require(module is not None and module[1] == 'github.com/ivo-lopes/ivoai', 'unexpected source module')
        require(toolchain is not None and toolchain[1] == '1.27.3', 'unexpected reviewed Go toolchain')
        require(not re.search(r'^\s*(replace|exclude)\b', mod, re.M), 'module replacement/exclusion refused')
        requires, inside = {}, False
        for line in mod.splitlines():
            line = line.split('//', 1)[0].strip()
            if line == 'require (':
                inside = True
            elif inside and line == ')':
                inside = False
            elif inside and line or line.startswith('require '):
                parts = line.split() if inside else line.split()[1:]
                require(len(parts) == 2 and parts[0] not in requires, 'invalid module requirement')
                requires[parts[0]] = parts[1]
        sums = {}
        for line in self.read('go.sum').decode().splitlines():
            name, version, value = line.split()
            if not version.endswith('/go.mod'):
                sums[(name, version)] = value
        return {'module': module[1], 'go_version': 'go' + toolchain[1],
                'source_commit': self.commit, 'requires': requires, 'sums': sums}

    def patches(self):
        opencode = hashlib.sha256(b''.join(self.read(name) for name in (
            'patches/opencode/1.19.0-clipboard-errors.patch',
            'patches/opencode/1.19.0-plugin-sdk-version.patch',
            'patches/opencode/1.19.0-managed-local-plugins.patch'))).hexdigest()
        licenses = self.git('ls-tree', '-r', '--name-only', self.commit,
                            '--', 'patches/graft/grammar-licenses').decode().splitlines()
        values = {name: hashlib.sha256(self.read(name)).hexdigest()
                  for name in GRAFT_FILES + licenses}
        graft = hashlib.sha256(''.join(name + ' ' + value + '\n'
                                      for name, value in sorted(values.items())).encode()).hexdigest()
        return {'opencode': opencode, 'graft': graft}


def exact_read(stream, size):
    require(0 <= size <= MiB, 'unbounded archive read refused')
    value = stream.read(size)
    require(len(value) == size, 'truncated archive')
    return value


def safe_name(name):
    require(isinstance(name, str) and 0 < len(name.encode('utf-8')) <= 1024,
            'archive path exceeds budget')
    require(not name.startswith('/') and '\\' not in name and '\x00' not in name,
            'unsafe archive path')
    value = name.removesuffix('/')
    require(all(part not in ('', '.', '..') for part in value.split('/')) and
            not any(ord(c) < 32 or ord(c) == 127 for c in name), 'unsafe archive path')
    return value


def pax_path(data):
    # Graft's reviewed Python packager needs only bounded local path records.
    # Reject global/nested headers and overrides of sizes, links or ownership.
    require(len(data) <= 4096, 'PAX header budget exceeded')
    space = data.find(b' ')
    require(0 < space <= 4 and data[:space].isdigit(), 'invalid PAX length')
    require(str(len(data)).encode() == data[:space] and data[space+1:].startswith(b'path=')
            and data.endswith(b'\n'), 'only one exact PAX path record is allowed')
    return data[space+6:-1].decode('utf-8', 'strict')


def scan_tar(path, kind, arch, destination=None):
    """Bound before parsing extensions; never extract paths from an archive."""
    require(kind in ('core', 'opencode', 'graft'), 'unknown archive contract')
    total_limit = MAX_CORE_BYTES if kind == 'core' else 512 * MiB
    entry_limit = 1 if kind == 'core' else 2 if kind == 'opencode' else 4096
    seen, files, total, decoded, pending, headers = set(), set(), 0, 0, None, 0
    with path.open('rb') as raw, gzip.GzipFile(fileobj=raw) as stream:
        while True:
            header = exact_read(stream, 512)
            decoded += 512
            require(decoded <= total_limit + 16 * MiB, 'decompressed archive budget exceeded')
            if header == bytes(512):
                require(pending is None and exact_read(stream, 512) == bytes(512), 'invalid tar terminator')
                # Force gzip CRC validation and reject hidden concatenated TARs.
                tail = stream.read(65537)
                require(len(tail) <= 65536 and not any(tail), 'trailing or oversized tar data')
                break
            headers += 1
            require(headers <= 8192, 'archive header count exceeded')
            member = tarfile.TarInfo.frombuf(header, 'utf-8', 'strict')
            if member.type == tarfile.XHDTYPE:
                require(kind == 'graft' and pending is None and 0 < member.size <= 4096,
                        'tar extension refused or PAX header budget exceeded')
                pending = pax_path(exact_read(stream, member.size))
                require(not any(exact_read(stream, (-member.size) % 512)), 'nonzero PAX padding')
                decoded += member.size + (-member.size) % 512
                continue
            require(member.type in (tarfile.REGTYPE, tarfile.AREGTYPE, tarfile.DIRTYPE),
                    'links, sparse files and tar extensions are refused')
            require(not member.linkname and member.mode & 0o7000 == 0,
                    'archive links or privileged modes refused')
            name = safe_name(pending if pending is not None else member.name)
            pending = None
            require(name not in seen and len(seen) < entry_limit, 'duplicate or excessive archive entries')
            require(not any('/'.join(name.split('/')[:i]) in files
                            for i in range(1, len(name.split('/')))), 'file used as archive parent')
            require(not member.isreg() or not any(old.startswith(name + '/') for old in seen),
                    'archive file collides with existing children')
            seen.add(name)
            require(0 <= member.size <= MAX_CORE_BYTES, 'archive member size exceeds budget')
            if member.isdir():
                require(kind == 'graft' and member.size == 0, 'unexpected archive directory')
                continue
            require(not member.name.endswith('/'), 'regular member cannot have directory path')
            files.add(name)
            total += member.size
            require(total <= total_limit, 'archive payload budget exceeded')
            if kind == 'core':
                require(name == 'ivoai' and member.size > 0 and member.mode & 0o111,
                        'unsafe Core archive member')
            if kind == 'opencode':
                require(name in ('opencode', 'LICENSE') and member.size > 0,
                        'unexpected managed OpenCode member')
            target = None
            try:
                if destination is not None and kind == 'core':
                    fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                    target = os.fdopen(fd, 'wb')
                remaining = member.size
                while remaining:
                    block = exact_read(stream, min(MiB, remaining))
                    if target is not None:
                        target.write(block)
                    remaining -= len(block)
                padding = exact_read(stream, (-member.size) % 512)
                require(not any(padding), 'nonzero tar member padding')
            finally:
                if target is not None:
                    target.close()
            decoded += member.size + (-member.size) % 512
    required = {'ivoai'} if kind == 'core' else {'opencode', 'LICENSE'} if kind == 'opencode' else {
        'graft-runtime', 'provenance.json', 'files.json', 'LICENSE'}
    require(required <= files, 'required archive members missing')
    if destination is not None and kind == 'core':
        with destination.open('rb') as stream:
            header = stream.read(64)
        require(len(header) == 64 and header[:6] == b'\x7fELF\x02\x01', 'Core must be ELF64 little endian')
        require(struct.unpack_from('<HH', header, 16) == (2, {'amd64': 62, 'arm64': 183}[arch]),
                'Core ELF type/architecture mismatch')
    return {'entries': len(seen), 'payload_bytes': total}


def ldflags(settings):
    tokens, bindings = shlex.split(settings.get('-ldflags', '')), {}
    index = 0
    while index < len(tokens):
        token = tokens[index]
        if token == '-X':
            index += 1
            require(index < len(tokens), 'truncated linker binding')
            token = tokens[index]
        elif token.startswith('-X='):
            token = token[3:]
        else:
            index += 1
            continue
        key, sep, value = token.partition('=')
        require(sep and key and key not in bindings, 'duplicate or invalid linker binding')
        bindings[key] = value
        index += 1
    return bindings


def embedded_payloads(binary, tag, metadata):
    require(binary is not None, 'actual ELF required for embedded-byte association')
    regular(binary, MAX_CORE_BYTES)
    blob = binary.read_bytes()
    associations = {}
    for component, document in metadata.items():
        expected = {**document, 'url': 'https://github.com/ivo-lopes/ivoai/releases/download/' + tag + '/' + document['archive']}
        encoded = base64.b64encode((json.dumps(expected, separators=(',', ':'), ensure_ascii=False) + '\n').encode())
        count = blob.count(encoded)
        require(count > 0, 'Core lacks exact same-tag embedded ' + component + ' payload')
        associations[component] = {'url': expected['url'], 'exact_base64_occurrences': count}
    return associations


def verify_info(info, contract, arch, tag, metadata, binary=None):
    require(info['GoVersion'] == contract['go_version'], 'Core Go toolchain mismatch')
    require(info['Path'] == contract['module'] + '/cmd/ivoai', 'Core command path mismatch')
    require(info['Main']['Path'] == contract['module'] and info['Main']['Version'] == tag,
            'Core main module/version mismatch')
    require(not info['Main'].get('Replace'), 'Core main replacement refused')
    settings = {}
    for item in info['Settings']:
        require(item['Key'] not in settings, 'duplicate Core build setting')
        settings[item['Key']] = item['Value']
    required = {'-buildmode': 'exe', '-compiler': 'gc', '-trimpath': 'true', 'CGO_ENABLED': '0',
                'GOOS': 'linux', 'GOARCH': arch, 'vcs': 'git',
                'vcs.revision': contract['source_commit'], 'vcs.modified': 'false'}
    if arch == 'amd64':
        required['GOAMD64'] = 'v1'
    for key, value in required.items():
        require(settings.get(key) == value, 'Core build setting mismatch: ' + key)
    deps = {}
    for dep in info.get('Deps') or []:
        require(not dep.get('Replace') and dep['Path'] not in deps, 'replaced/duplicate Core dependency')
        require(contract['requires'].get(dep['Path']) == dep['Version'] and
                contract['sums'].get((dep['Path'], dep['Version'])) == dep['Sum'],
                'Core dependency differs from source')
        deps[dep['Path']] = dep['Version']
    require(set(deps) == set(contract['requires']), 'Core dependency set differs from source')
    if '-ldflags' in settings:
        bindings = ldflags(settings)
        require(bindings.get('main.version') == tag[1:], 'Core main.version linker binding mismatch')
        expected_keys = {'main.version'}
        for component, document in metadata.items():
            key = contract['module'] + '/internal/components.managed' + ('OpenCode' if component == 'opencode' else 'Graft') + 'Build'
            expected_keys.add(key)
            encoded = bindings.get(key)
            require(isinstance(encoded, str) and len(encoded) <= 16384, 'missing bounded component linker binding')
            body = unique_json(base64.b64decode(encoded, validate=True).decode())
            expected = {**document, 'url': 'https://github.com/ivo-lopes/ivoai/releases/download/' + tag + '/' + document['archive']}
            require(body == expected, 'embedded component metadata/URL differs from same-tag archive')
        require(set(bindings) == expected_keys, 'unexpected linker binding')
        status, associations = 'RECORDED_BINDINGS_MATCH', None
    else:
        # Reviewed Go1.27.3 -trimpath releases omit this optional setting. Keep
        # that unknown explicit: ELF bytes do not prove a runtime variable's
        # pointer binding. Native version/help remains a separate required gate.
        status = 'NOT_RECORDED_BY_TOOLCHAIN'
        associations = embedded_payloads(binary, tag, metadata)
    return {'go_version': info['GoVersion'], 'main_module_version': info['Main']['Version'],
            'observed_build_settings': settings, 'required_build_settings_verified': required,
            'ldflags_status': status, 'embedded_byte_associations': associations,
            'component_association': 'metadata/bytes only; no runtime pointer-binding, source attestation or reproducibility claim',
            'native_version_help_gate': 'REQUIRED_SEPARATELY_NOT_EXECUTED_BY_THIS_VERIFIER'}


def release_tag(version):
    version = version.removeprefix('v')
    require(re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?', version)
            is not None, 'release version must match the reviewed tag grammar')
    return 'v' + version


def api_metadata(path, hashes, directory, tag):
    if path is None:
        return {'provided': False, 'digest_status': 'UNKNOWN_NOT_PROVIDED'}
    regular(path, 2 * MiB)
    document = unique_json(path.read_text())
    require(document.get('tag_name', document.get('tagName')) == tag, 'API release tag mismatch')
    require(document.get('prerelease', document.get('isPrerelease')) is ('-' in tag),
            'API prerelease state must match the release tag')
    require(type(document.get('draft', document.get('isDraft'))) is bool, 'API draft state missing')
    draft = document.get('draft', document.get('isDraft'))
    locator = tag
    html_url = document.get('html_url')
    if html_url is not None:
        canonical = 'https://github.com/ivo-lopes/ivoai/releases/tag/'
        if draft and re.fullmatch(re.escape(canonical) + r'untagged-[a-f0-9]{20}', html_url):
            # GitHub's unpublished locator is transport metadata, not the
            # release tag or a replacement for source/digest verification.
            locator = html_url.removeprefix(canonical)
        else:
            require(html_url == canonical + tag, 'API release page URL mismatch')
    entries = document.get('assets')
    require(isinstance(entries, list) and len(entries) == 12, 'API asset count mismatch')
    seen, missing = set(), []
    for item in entries:
        name = item.get('name')
        require(name in EXPECTED and name not in seen, 'duplicate/unexpected API asset')
        seen.add(name)
        require(type(item.get('size')) is int and item['size'] == (directory/name).stat().st_size,
                'API asset size mismatch')
        if 'browser_download_url' in item:
            require(item['browser_download_url'] ==
                    'https://github.com/ivo-lopes/ivoai/releases/download/' + locator + '/' + name,
                    'API download URL differs from exact release asset locator')
        value = item.get('digest')
        if value is None:
            missing.append(name)
        else:
            require(value == 'sha256:' + hashes[name], 'API asset digest mismatch')
    return {'provided': True, 'draft': document.get('draft', document.get('isDraft')),
            'prerelease': '-' in tag,
            'download_locator': locator,
            'download_locator_status': 'UNPUBLISHED_DRAFT_LOCATOR' if locator != tag else 'RELEASE_TAG',
            'verified_digests': 12-len(missing), 'unknown_digests': sorted(missing)}


BUILDINFO_READER = '''package main
import ("debug/buildinfo"; "encoding/json"; "os")
func main() {
 if len(os.Args)!=2 { os.Exit(2) }
 info,err:=buildinfo.ReadFile(os.Args[1]); if err!=nil { os.Exit(1) }
 if json.NewEncoder(os.Stdout).Encode(info)!=nil { os.Exit(1) }
}
'''


def verify(args):
    tag = release_tag(args.version)
    directory = Path(args.directory).absolute()
    require(stat.S_ISDIR(directory.lstat().st_mode), 'asset directory must not be a symlink')
    if args.output:
        require(not Path(args.output).absolute().is_relative_to(directory),
                'verification report must be outside the exact asset directory')
    hashes = manifest_hashes(directory)
    source = Source(args.source_tree, args.source)
    contract, patches = source.contract(), source.patches()
    api = api_metadata(Path(args.api_json) if args.api_json else None, hashes, directory, tag)
    skill_contents(directory/'ivoai-memory-context.zip', source.read('skills/ivoai-memory-context/SKILL.md'))
    go = shutil.which(args.go)
    require(go is not None, 'reviewed Go toolchain required')
    report = {'pass': False, 'tag': tag, 'source': args.source, 'assets': 12,
              'payload_checksums': 11, 'asset_sha256': hashes, 'api': api, 'cores': {},
              'network_calls': False, 'downloaded_code_executed': False}
    with tempfile.TemporaryDirectory(prefix='ivoai-release-verifier-') as private:
        private = Path(private)
        env = {'PATH': str(Path(go).parent)+':/usr/bin:/bin', 'HOME': str(private),
               'GOCACHE': str(private/'cache'), 'GOPATH': str(private/'go-path'),
               'GOENV': 'off', 'GOWORK': 'off', 'GO111MODULE': 'off', 'GOTOOLCHAIN': 'local',
               'GOPROXY': 'off', 'GOSUMDB': 'off', 'CGO_ENABLED': '0', 'GOMAXPROCS': '2'}
        require(subprocess.check_output([go, 'version'], env=env, timeout=30).decode().split()[2] == contract['go_version'],
                'metadata reader toolchain mismatch')
        helper, reader = private/'reader.go', private/'reader'
        helper.write_text(BUILDINFO_READER)
        subprocess.run([go, 'build', '-trimpath', '-o', str(reader), str(helper)], env=env,
                       cwd=private, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                       check=True, timeout=120, preexec_fn=compiler_budget)
        for arch in ('amd64', 'arm64'):
            metadata = {name: component_metadata(directory, name, arch, hashes, patches)
                        for name in ('opencode', 'graft')}
            inventory = {name: scan_tar(directory/metadata[name]['archive'], name, arch)
                         for name in ('opencode', 'graft')}
            binary = private/('core-'+arch)
            inventory['core'] = scan_tar(directory/('ivoai_linux_'+arch+'.tar.gz'), 'core', arch, binary)
            output = private/('info-'+arch+'.json')
            with output.open('xb') as stream:
                subprocess.run([str(reader), str(binary)], env=env, stdout=stream,
                               stderr=subprocess.DEVNULL, check=True, timeout=30,
                               preexec_fn=reader_budget)
            regular(output, 2 * MiB)
            checked = verify_info(unique_json(output.read_text()), contract, arch, tag, metadata, binary)
            checked['archives'] = inventory
            checked['binary_sha256'] = digest(binary)
            report['cores'][arch] = checked
    report['pass'] = True
    report['limits'] = ['Offline association, not independent source attestation or reproducible builds.',
                        'No Core/native-client/provider/ARM/no-AVX execution.',
                        'Absent API digests remain UNKNOWN; checksums have no offline self-digest anchor.']
    return report


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', required=True)
    parser.add_argument('--version', required=True, help='release version, e.g. 0.16.0 or 0.16.0-rc.1; optional leading v')
    parser.add_argument('--source', required=True)
    parser.add_argument('--source-tree', default=str(Path(__file__).resolve().parent.parent))
    parser.add_argument('--go', default='go')
    parser.add_argument('--api-json')
    parser.add_argument('--output', help='optional new JSON report path outside the asset directory')
    args = parser.parse_args(argv)
    os.umask(0o077)
    resource.setrlimit(resource.RLIMIT_AS, (1 << 30, resource.getrlimit(resource.RLIMIT_AS)[1]))
    def expired(*_):
        raise TimeoutError('offline verification wall budget exceeded')
    signal.signal(signal.SIGALRM, expired)
    signal.alarm(300)
    try:
        report = verify(args)
    except Exception as error:
        report = {'pass': False, 'error': str(error)}
    if args.output:
        with open(args.output, 'x', encoding='utf-8') as stream:
            json.dump(report, stream, indent=2)
            stream.write('\n')
    print(json.dumps(report))
    return 0 if report['pass'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
