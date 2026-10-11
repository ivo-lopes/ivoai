import pathlib
import re
import sys

generated = pathlib.Path(sys.argv[1])
embedded = pathlib.Path(sys.argv[2])

def javascript_asset(path):
    # Docusaurus emits one asset directory at root and one per locale.
    parts = path.parts
    return parts[:2] == ("assets", "js") or parts[1:3] == ("assets", "js")


def files(root):
    return {
        path.relative_to(root).as_posix(): path
        for path in root.rglob("*")
        if path.is_file() and not javascript_asset(path.relative_to(root))
    }

left, right = files(generated), files(embedded)
if left.keys() != right.keys():
    print("generated only:", ", ".join(sorted(left.keys() - right.keys())), file=sys.stderr)
    print("embedded only:", ", ".join(sorted(right.keys() - left.keys())), file=sys.stderr)
    raise SystemExit("embedded documentation file set is stale")

asset_hash = re.compile(rb"(?<=\.)([0-9a-f]{8})(?=\.js)")
for name in sorted(left):
    a, b = left[name].read_bytes(), right[name].read_bytes()
    if name.endswith(".html"):
        a, b = asset_hash.sub(b"HASHHASH", a), asset_hash.sub(b"HASHHASH", b)
    if a != b:
        raise SystemExit(f"embedded documentation differs: {name}")
