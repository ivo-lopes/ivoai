#!/usr/bin/env python3
"""Rebuild the reviewed local compatibility archive; never run upstream hooks."""
import argparse
import base64
import gzip
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import subprocess
import tarfile
import tempfile


MAX_ARCHIVE_MEMBERS = 2000
MAX_MEMBER_BYTES = 8 << 20
MAX_ARCHIVE_BYTES = 32 << 20


def extract_regular(body, destination):
    with tarfile.open(fileobj=io.BytesIO(body), mode="r:gz") as archive:
        members = []
        total = 0
        seen = set()
        # Check each header before advancing over its payload. getmembers()
        # would consume the whole archive before applying these budgets.
        for item in archive:
            if len(members) >= MAX_ARCHIVE_MEMBERS:
                raise ValueError("archive entry budget exceeded")
            path = PurePosixPath(item.name)
            normalized = path.as_posix()
            if path.is_absolute() or ".." in path.parts or not path.parts or path.parts[0] != "package" or normalized in seen:
                raise ValueError("invalid archive path")
            seen.add(normalized)
            if not (item.isdir() or item.isfile()) or item.size > MAX_MEMBER_BYTES:
                raise ValueError("unexpected archive entry")
            total += item.size
            if total > MAX_ARCHIVE_BYTES:
                raise ValueError("archive byte budget exceeded")
            members.append(item)
        # Nothing is written until every admitted member passes validation.
        archive.extractall(destination, members=members, filter="data")


def build(upstream, output):
    here = Path(__file__).resolve().parent
    provenance = json.loads((here / "provenance.json").read_text())
    body = upstream.read_bytes()
    algorithm, expected = provenance["upstream_integrity"].split("-", 1)
    if algorithm != "sha512" or base64.b64encode(hashlib.sha512(body).digest()).decode() != expected or hashlib.sha256(body).hexdigest() != provenance["upstream_sha256"]:
        raise ValueError("upstream archive integrity mismatch")
    patch = here / provenance["patch_file"]
    if hashlib.sha256(patch.read_bytes()).hexdigest() != provenance["patch_sha256"]:
        raise ValueError("reviewed patch integrity mismatch")
    with tempfile.TemporaryDirectory(prefix="ivoai-zod-build-") as temporary:
        root = Path(temporary)
        extract_regular(body, root)
        package = root / "package"
        subprocess.run(["patch", "--batch", "--fuzz=0", "-p1", "-i", str(patch)], cwd=package,
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
        metadata = json.loads((package / "package.json").read_text())
        if metadata["name"] != provenance["fork_name"] or metadata["version"] != provenance["fork_version"] or "scripts" in metadata:
            raise ValueError("unexpected local fork identity")
        # Preserve every upstream source/declaration/V4 payload except explicit patch hunks.
        output.parent.mkdir(parents=True, exist_ok=True)
        with output.open("wb") as target, gzip.GzipFile(filename="", mode="wb", fileobj=target, mtime=0, compresslevel=9) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as archive:
                for file in sorted(package.rglob("*"), key=lambda item: item.relative_to(package).as_posix()):
                    if file.is_dir():
                        continue
                    if not file.is_file() or file.is_symlink():
                        raise ValueError("unexpected patched package entry")
                    data = file.read_bytes()
                    entry = tarfile.TarInfo("package/" + file.relative_to(package).as_posix())
                    entry.size, entry.mode = len(data), 0o644
                    entry.uid = entry.gid = entry.mtime = 0
                    entry.uname = entry.gname = ""
                    archive.addfile(entry, io.BytesIO(data))
    result = output.read_bytes()
    print(json.dumps({"sha256": hashlib.sha256(result).hexdigest(),
                      "integrity": "sha512-" + base64.b64encode(hashlib.sha512(result).digest()).decode(),
                      "bytes": len(result)}, sort_keys=True))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--upstream", required=True, type=Path, help="downloaded official zod@4.6.7 tarball")
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    build(args.upstream.resolve(), args.output.resolve())
