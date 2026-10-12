#!/usr/bin/env python3
"""Reviewed, standard-library-only Graft archive operations."""
import base64
import gzip
import hashlib
import json
import pathlib
import shutil
import sys
import tarfile

NATIVE = ('tree-sitter', 'tree-sitter-typescript', 'tree-sitter-javascript', 'tree-sitter-python', 'tree-sitter-go', 'tree-sitter-r', 'tree-sitter-java', 'tree-sitter-kotlin', 'tree-sitter-swift', 'tree-sitter-php')

def validate_lock(lock):
    if lock.get('lockfileVersion') != 3:
        raise ValueError('expected npm lock version 3')
    result = {}
    for name, entry in lock['packages'].items():
        if not name:
            continue
        if not name.startswith('node_modules/') or '..' in pathlib.PurePosixPath(name).parts:
            raise ValueError('invalid dependency path')
        if not entry.get('resolved', '').startswith('https://registry.npmjs.org/'):
            raise ValueError('dependency must resolve to npm registry')
        integrity = entry.get('integrity', '')
        try:
            digest = base64.b64decode(integrity.removeprefix('sha512-'), validate=True)
        except ValueError as exc:
            raise ValueError('invalid dependency integrity') from exc
        if not integrity.startswith('sha512-') or len(digest) != 64 or not entry.get('license'):
            raise ValueError('missing dependency integrity or license')
        result[name] = {k: entry[k] for k in ('version', 'resolved', 'integrity', 'license')}
    return result

def extract_source(archive, destination):
    destination = pathlib.Path(destination)
    with tarfile.open(archive) as tar:
        members = tar.getmembers()
        roots = set()
        for member in members:
            path = pathlib.PurePosixPath(member.name)
            if path.is_absolute() or '..' in path.parts or not (member.isdir() or member.isfile()):
                raise ValueError('unsafe source archive entry')
            roots.add(path.parts[0])
        if len(roots) != 1:
            raise ValueError('source archive must have one root')
        for member in members:
            parts = pathlib.PurePosixPath(member.name).parts[1:]
            if not parts:
                continue
            target = destination.joinpath(*parts)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with tar.extractfile(member) as src, target.open('wb') as dst:
                    shutil.copyfileobj(src, dst)
                target.chmod(0o755 if member.mode & 0o111 else 0o644)

def clean_native_payload(root):
    for name in NATIVE:
        package = pathlib.Path(root)/'node_modules'/name
        shutil.rmtree(package/'prebuilds', ignore_errors=True)
        for path in package.rglob('*'):
            if path.is_file() and path.suffix in {'.c', '.cc', '.cpp', '.h', '.hpp', '.o', '.a'}:
                path.unlink()
        build = package/'build'
        if not build.exists():
            continue
        keep = {p.name: p.read_bytes() for p in (build/'Release').glob('*.node')}
        shutil.rmtree(build)
        (build/'Release').mkdir(parents=True)
        for name, data in keep.items():
            (build/'Release'/name).write_bytes(data)
    # All installed runtime entry points are JS/MJS; compiled TS sources,
    # declarations and source maps are unnecessary and exceed install file limits.
    for path in pathlib.Path(root).rglob('*'):
        if path.is_file() and path.suffix in {'.ts', '.mts', '.cts', '.map'}:
            path.unlink()
    # npm executable links and build-only helper programs are unused by library imports.
    for path in pathlib.Path(root).rglob('.bin'):
        if path.is_dir():
            shutil.rmtree(path)

def write_file_manifest(root):
    root = pathlib.Path(root)
    listing = {}
    for path in sorted(root.rglob('*')):
        if path.is_symlink():
            raise ValueError('symlinks are not permitted in runtime payload')
        if path.is_file() and path != root/'files.json':
            listing[path.relative_to(root).as_posix()] = {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'size': path.stat().st_size}
    (root/'files.json').write_text(json.dumps(listing, sort_keys=True, indent=2)+'\n')

def archive_payload(root, output):
    root = pathlib.Path(root)
    entries = sorted(root.rglob('*'))
    sizes = [path.stat().st_size for path in entries if path.is_file()]
    if len(entries) > 4096 or sum(sizes) > 512 * 1024**2 or any(size > 256 * 1024**2 for size in sizes):
        raise ValueError('runtime payload exceeds managed extraction limits')
    with open(output, 'wb') as raw, gzip.GzipFile(filename='', fileobj=raw, mode='wb', mtime=0) as gz, tarfile.open(fileobj=gz, mode='w', format=tarfile.PAX_FORMAT) as tar:
        for path in entries:
            if path.is_symlink():
                raise ValueError('symlinks are not permitted in runtime payload')
            info = tar.gettarinfo(str(path), arcname=path.relative_to(root).as_posix())
            info.uid = info.gid = info.mtime = 0
            info.uname = info.gname = ''
            info.mode = 0o755 if path.is_dir() or path == root/'graft-runtime' else 0o644
            if path.is_file():
                with path.open('rb') as stream:
                    tar.addfile(info, stream)
            else:
                tar.addfile(info)

if __name__ == '__main__':
    action, *args = sys.argv[1:]
    if action == 'extract':
        extract_source(*args)
    elif action == 'lock':
        lock = validate_lock(json.loads(pathlib.Path(args[0]).read_text()))
        pathlib.Path(args[1]).write_text(json.dumps(lock, sort_keys=True, indent=2)+'\n')
    elif action == 'package':
        clean_native_payload(args[0])
        write_file_manifest(args[0])
        archive_payload(*args)
    else:
        raise SystemExit('unknown operation')
