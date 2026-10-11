"""Review-only Claude Red inventory. Reads an inert tree; runs no upstream code.

Usage: python3 scripts/claude_red_intake.py /path/to/verified/upstream
Output is a review candidate, never an active catalog or permission grant.
The release curator must verify the immutable revision, license and each policy.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys


def metadata(data, folder):
    text = data.decode("utf-8", errors="strict")
    if "\x00" in text or "\x1b" in text:
        raise ValueError("unsafe_text")
    lines = text.splitlines()
    if not lines or lines[0] != "---":
        raise ValueError("missing_frontmatter")
    try:
        end = lines.index("---", 1)
    except ValueError:
        raise ValueError("unclosed_frontmatter") from None
    if len("\n".join(lines[:end]).encode()) > 65536:
        raise ValueError("oversized_metadata")
    values = {}
    current = None
    for line in lines[1:end]:
        if line.startswith("  ") and current:
            values[current] += " " + line.strip()
            continue
        match = re.fullmatch(r"(name|description):\s*(.+)", line)
        if not match or match[1] in values:
            raise ValueError("unsupported_or_duplicate_metadata")
        key, value = match.groups()
        current = key if value in (">", ">-") else None
        if current:
            value = ""
        elif value.startswith('"'):
            value = json.loads(value)
        elif value.startswith("'"):
            if not value.endswith("'"):
                raise ValueError("invalid_scalar")
            value = value[1:-1].replace("''", "'")
        elif any(c in value for c in "{}[]&*!|>"):
            raise ValueError("unsupported_scalar")
        values[key] = value
    name, description = values.get("name", ""), values.get("description", "").strip()
    if name != folder or not re.fullmatch(r"[a-z0-9]+(?:-[a-z0-9]+)*", name) or len(name)>64:
        raise ValueError("name_path_mismatch")
    if not description or len(description.encode())>2048:
        raise ValueError("invalid_description")
    return name, description


def discover(root):
    root = Path(root)
    base = root / "Skills"
    if not base.is_dir() or base.is_symlink():
        raise ValueError("invalid_skills_root")
    valid, rejected = [], []
    count = 0
    for directory, dirs, files in os.walk(base, followlinks=False):
        dirs.sort()
        for name in list(dirs):
            path = Path(directory) / name
            if path.is_symlink():
                rejected.append({"path": path.relative_to(root).as_posix(), "reason": "symlink"})
                dirs.remove(name)
        for filename in sorted(files):
            if filename != "SKILL.md":
                continue
            count += 1
            if count > 4096:
                raise ValueError("inventory_limit")
            path = Path(directory) / filename
            item = {"path": path.relative_to(root).as_posix()}
            try:
                info = path.lstat()
                if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o111:
                    raise ValueError("unsafe_file_type")
                if info.st_size > 1 << 20:
                    raise ValueError("oversized_document")
                fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
                with os.fdopen(fd, "rb") as stream:
                    data = stream.read((1 << 20) + 1)
                if len(data) > 1 << 20:
                    raise ValueError("oversized_document")
                item["sha256"] = hashlib.sha256(data).hexdigest()
                name, description = metadata(data, path.parent.name)
                item.update(name=name, description=description)
                valid.append(item)
            except (ValueError, OSError) as error:
                reason = str(error)
                if not re.fullmatch(r"[a-z_]+", reason):
                    reason = "invalid_metadata_or_unreadable_file"
                item["reason"] = reason
                rejected.append(item)
    return sorted(valid, key=lambda x: x["path"]), sorted(rejected, key=lambda x: x["path"])


if __name__ == "__main__":
    valid, rejected = discover(sys.argv[1])
    print(json.dumps({"skills": valid, "rejected": rejected}, indent=2, ensure_ascii=False))
