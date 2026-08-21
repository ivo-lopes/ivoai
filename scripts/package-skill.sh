#!/usr/bin/env bash
set -Eeuo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
skill_name=ivoai-memory-context
skill_source="$repo_root/skills/$skill_name"
output=${1:-"$repo_root/dist/$skill_name.zip"}

if [[ ! -f "$skill_source/SKILL.md" ]]; then
  printf 'package-skill: missing %s/SKILL.md\n' "$skill_source" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  printf 'package-skill: python3 is required\n' >&2
  exit 1
fi

mkdir -p "$(dirname "$output")"
if [[ -L "$output" || -d "$output" ]]; then
  printf 'package-skill: refusing unsafe output path %s\n' "$output" >&2
  exit 1
fi
output_dir=$(cd "$(dirname "$output")" && pwd)
output_name=$(basename "$output")
temporary=$(mktemp "$output_dir/.${output_name}.tmp.XXXXXX")
cleanup() {
  rm -f -- "$temporary"
}
trap cleanup EXIT INT TERM

python3 - "$skill_source/SKILL.md" "$temporary" "$skill_name" <<'PY'
import pathlib
import sys
import zipfile

source = pathlib.Path(sys.argv[1])
output = pathlib.Path(sys.argv[2])
skill_name = sys.argv[3]
content = source.read_text(encoding="utf-8")

if not content.startswith("---\n") or "\n---\n" not in content[4:]:
    raise SystemExit("package-skill: SKILL.md has invalid frontmatter")
frontmatter = content.split("\n---\n", 1)[0][4:]
fields = {}
for line in frontmatter.splitlines():
    key, separator, value = line.partition(":")
    if separator:
        fields[key.strip()] = value.strip()
if fields.get("name") != skill_name or not fields.get("description"):
    raise SystemExit("package-skill: SKILL.md name/description is invalid")
if any(marker in content for marker in ("TODO", "FIXME", "HACK", "XXX")):
    raise SystemExit("package-skill: unfinished marker found in SKILL.md")

directory = zipfile.ZipInfo(f"{skill_name}/", (2020, 1, 1, 0, 0, 0))
directory.external_attr = 0o40755 << 16
entry = zipfile.ZipInfo(f"{skill_name}/SKILL.md", (2020, 1, 1, 0, 0, 0))
entry.compress_type = zipfile.ZIP_STORED
entry.external_attr = 0o100644 << 16

with zipfile.ZipFile(output, "w") as archive:
    archive.writestr(directory, b"")
    archive.writestr(entry, content.encode("utf-8"))
PY

chmod 0644 "$temporary"
mv -f -- "$temporary" "$output"
trap - EXIT INT TERM

printf 'Created %s\n' "$output"
