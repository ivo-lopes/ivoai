#!/usr/bin/env python3
"""Test only Graft's HTML escaping helpers; never start its CLI or viewer.

Usage: python3 test_viewer_escaping.py /path/to/patched/graft /path/to/node
The source root is the pinned build staging tree, not the user's repository.
"""
import html.parser
import json
import pathlib
import re
import subprocess
import sys
import unittest

ROOT = pathlib.Path(sys.argv.pop(1)) if len(sys.argv) > 1 else None
NODE = sys.argv.pop(1) if len(sys.argv) > 1 else None


class Attributes(html.parser.HTMLParser):
    def __init__(self):
        super().__init__()
        self.tags = []

    def handle_starttag(self, tag, attrs):
        self.tags.append((tag, attrs))


class ViewerEscaping(unittest.TestCase):
    def test_dynamic_legends_are_text(self):
        source = (ROOT / "viewer" / "main.ts").read_text()
        self.assertIn('${escapeText(key)}', source)
        self.assertIn('${escapeText(type)}', source)

    def test_repository_identifiers_are_one_inert_attribute(self):
        payload = 'file:x" onmouseover="alert(1)\' & <svg/onload=alert(2)>.ts'
        for filename, attribute, expression in [
            ("tree.ts", "data-id", "esc(node.id)"),
            ("detail.ts", "data-go", "esc(other(e))"),
        ]:
            with self.subTest(file=filename):
                source = (ROOT / "viewer" / filename).read_text()
                self.assertIn('${' + expression + '}', source)
                match = re.search(r'function esc\(s: string\): string \{(.*?)\n\}', source, re.S)
                self.assertIsNotNone(match, "review escape helper when upstream changes")
                script = ('function esc(s) {' + match.group(1) + '}\n'
                          + 'process.stdout.write(JSON.stringify(esc(' + json.dumps(payload) + ')));')
                result = subprocess.run([NODE, '--openssl-config=/dev/null', '-e', script],
                                        check=True, text=True, capture_output=True, timeout=10,
                                        env={"PATH": "/nonexistent"})
                escaped = json.loads(result.stdout)
                parser = Attributes()
                parser.feed('<button ' + attribute + '="' + escaped + '">safe</button>')
                self.assertEqual(parser.tags, [('button', [(attribute, payload)])])
                for char in ['&amp;', '&lt;', '&gt;', '&quot;', '&#39;']:
                    self.assertIn(char, escaped)


if __name__ == '__main__':
    if ROOT is None or NODE is None:
        raise SystemExit(__doc__)
    unittest.main()
