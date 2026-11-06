#!/usr/bin/env python3
"""Small reviewed Caveman source/archive operations, without upstream installers."""
import gzip
import hashlib
import json
import pathlib
import shutil
import sys
import tarfile

VERSION = '4.0.2-ivoai.1'
UPSTREAM = 'b4531dfc05a3ef0ba93b57e8bfa5f48564a4b4d6'
SOURCE = '29ef4c5bccee70dbc486bae266f966b8bd53132c6e12e27e9b06b36cf35c51f4'

def digest(path):
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()

def extract(archive, destination):
    destination = pathlib.Path(destination)
    with tarfile.open(archive) as tar:
        members = tar.getmembers()
        roots = set()
        for member in members:
            path = pathlib.PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or not (member.isfile() or member.isdir()):
                raise ValueError('unsafe build archive entry')
            roots.add(path.parts[0])
        if len(roots) != 1:
            raise ValueError('build archive must have exactly one root')
        for member in members:
            parts = pathlib.PurePosixPath(member.name).parts[1:]
            if not parts:
                continue
            target = destination.joinpath(*parts)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with tar.extractfile(member) as stream, target.open('wb') as output:
                    shutil.copyfileobj(stream, output)
                target.chmod(0o755 if member.mode & 0o111 else 0o644)

def decode_stream(body):
    decoder = json.JSONDecoder()
    while body.strip():
        item, length = decoder.raw_decode(body.lstrip())
        body = body.lstrip()[length:]
        yield item

def licenses(deps, destination):
    """Copy exact linked modules' own notices, never infer license from audit output."""
    destination = pathlib.Path(destination)
    found = {}
    for item in decode_stream(pathlib.Path(deps).read_text()):
        module = item.get('Module') or {}
        if module.get('Main') or not module:
            continue
        if module.get('Replace'):
            raise ValueError('unreviewed replaced Go module')
        name, version = module['Path'], module['Version']
        if name in found:
            continue
        root = pathlib.Path(module['Dir'])
        notices = sorted(p for p in root.iterdir() if p.is_file() and p.name.upper().startswith(('LICENSE', 'COPYING', 'NOTICE', 'AUTHORS')))
        if not notices:
            raise ValueError('missing linked module license: ' + name)
        paths = []
        for source in notices:
            relative = pathlib.PurePosixPath(name, version, source.name)
            if relative.is_absolute() or '..' in relative.parts:
                raise ValueError('unsafe linked module path')
            target = destination.joinpath(*relative.parts)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, target)
            paths.append({'path': str(relative), 'sha256': digest(source)})
        found[name] = {'version': version, 'sum': module.get('Sum'), 'notices': paths}
    if not found:
        raise ValueError('empty linked module inventory')
    (destination/'modules.json').write_text(json.dumps(found, indent=2, sort_keys=True)+'\n')

def package(payload, output, arch, repo):
    payload, output, repo = map(pathlib.Path, (payload, output, repo))
    if arch not in ('amd64', 'arm64'):
        raise ValueError('unsupported platform')
    patch = digest(repo/'patches/caveman/dependency-lock.patch')
    files = {}
    for path in sorted(payload.rglob('*')):
        if path.is_symlink():
            raise ValueError('symlink in runtime payload')
        if path.is_file():
            files[path.relative_to(payload).as_posix()] = {'sha256': digest(path), 'size': path.stat().st_size}
    if not {'caveman-proxy', 'caveman-mcp', 'LICENSE', 'NOTICE', 'provenance.json'}.issubset(files):
        raise ValueError('incomplete runtime payload')
    (payload/'files.json').write_text(json.dumps(files, indent=2, sort_keys=True)+'\n')
    entries = sorted(payload.rglob('*'))
    if len(entries) > 4096 or sum(p.stat().st_size for p in entries if p.is_file()) > 256*1024**2:
        raise ValueError('runtime exceeds extraction bounds')
    with output.open('wb') as raw, gzip.GzipFile(filename='', fileobj=raw, mode='wb', mtime=0) as compressed, tarfile.open(fileobj=compressed, mode='w', format=tarfile.PAX_FORMAT) as tar:
        for path in entries:
            info = tar.gettarinfo(str(path), arcname=path.relative_to(payload).as_posix())
            info.uid = info.gid = info.mtime = 0
            info.uname = info.gname = ''
            info.mode = 0o755 if path.is_dir() or path.name in ('caveman-proxy', 'caveman-mcp') else 0o644
            if path.is_file():
                with path.open('rb') as stream: tar.addfile(info, stream)
            else: tar.addfile(info)
    sha = digest(output)
    identity = hashlib.sha256((SOURCE+'\n'+patch+'\n'+sha+'\n').encode()).hexdigest()
    metadata = {'version': VERSION, 'platform': 'linux/'+arch, 'revision': identity, 'sha256': sha,
                'archive': output.name, 'url': '', 'upstream_revision': UPSTREAM,
                'source_sha256': SOURCE, 'patch_sha256': patch}
    output.with_name(output.name.replace('.tar.gz', '.metadata.json')).write_text(json.dumps(metadata, sort_keys=True, indent=2)+'\n')

def cleanup(directory):
    root = pathlib.Path(directory)
    if root.is_symlink() or not root.name.startswith('ivoai-caveman-build.'):
        raise ValueError('cleanup requires the private Caveman build directory')
    if not root.exists():
        return
    for entry in root.rglob('*'):
        if not entry.is_symlink():
            entry.chmod(entry.stat().st_mode | 0o200)
    shutil.rmtree(root)

if __name__ == '__main__':
    action, *args = sys.argv[1:]
    if action == 'extract': extract(*args)
    elif action == 'licenses': licenses(*args)
    elif action == 'package': package(*args)
    elif action == 'cleanup': cleanup(*args)
    else: raise SystemExit('unknown operation')
