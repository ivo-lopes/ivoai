#!/usr/bin/env python3
"""External immutable object identity; no upstream execution."""
import hashlib
import json
import pathlib
import sys
payload, archive = map(pathlib.Path, sys.argv[1:3])
p = json.loads((payload/'provenance.json').read_text())
sha = hashlib.sha256(archive.read_bytes()).hexdigest()
revision = hashlib.sha256(f"{p['sourceSHA256']}\n{p['patchSHA256']}\n{sha}\n".encode()).hexdigest()
metadata = dict(version=p['managedVersion'], platform=f"linux/{p['architecture']}", revision=revision, sha256=sha, archive=archive.name,
                upstream_revision=p['revision'], source_sha256=p['sourceSHA256'], patch_sha256=p['patchSHA256'],
                node_version=p['nodeVersion'], node_sha256=p['nodeArchiveSHA256'])
(archive.parent/f"managed-graft-{p['architecture']}.json").write_text(json.dumps(metadata, sort_keys=True, indent=2)+'\n')
