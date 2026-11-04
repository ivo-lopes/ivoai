#!/usr/bin/env python3
"""Build the reviewed deepmerge fork from verified bytes, without install hooks."""
import argparse
import base64
import gzip
import hashlib
import io
import json
from pathlib import Path
import runpy
import subprocess
import tarfile
import tempfile


def build(upstream, output):
    here = Path(__file__).resolve().parent
    provenance = json.loads((here / "provenance.json").read_text())
    body = upstream.read_bytes()
    if (hashlib.sha256(body).hexdigest() != provenance["upstream_sha256"] or
            "sha512-" + base64.b64encode(hashlib.sha512(body).digest()).decode() != provenance["upstream_integrity"]):
        raise ValueError("upstream archive integrity mismatch")
    patch = here / provenance["patch_file"]
    if hashlib.sha256(patch.read_bytes()).hexdigest() != provenance["patch_sha256"]:
        raise ValueError("reviewed patch integrity mismatch")
    extract = runpy.run_path(str(here.parent / "zod3-compat" / "build.py"))["extract_regular"]
    with tempfile.TemporaryDirectory(prefix="ivoai-deepmerge-build-") as temporary:
        root = Path(temporary)
        extract(body, root)
        package = root / "package"
        subprocess.run(["patch", "--batch", "--fuzz=0", "-p1", "-i", str(patch)],
                       cwd=package, check=True, stdout=subprocess.DEVNULL)
        metadata = json.loads((package / "package.json").read_text())
        if metadata["name"] != provenance["fork_name"] or metadata["version"] != provenance["fork_version"] or "scripts" in metadata:
            raise ValueError("unexpected fork identity")
        output.parent.mkdir(parents=True, exist_ok=True)
        with output.open("wb") as target, gzip.GzipFile(filename="", mode="wb", fileobj=target, mtime=0, compresslevel=9) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as archive:
                for name in ["dist/cjs.js", "dist/umd.js", "index.d.ts", "index.js", "license.txt", "package.json", "readme.md"]:
                    path = package / name
                    if not path.is_file() or path.is_symlink():
                        raise ValueError("unexpected package member")
                    data = path.read_bytes()
                    entry = tarfile.TarInfo("package/" + name)
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
    parser.add_argument("--upstream", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    build(args.upstream.resolve(), args.output.resolve())
