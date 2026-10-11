import pathlib
import subprocess
import sys
import tempfile
import unittest

CHECK = pathlib.Path(__file__).with_name('check-embedded-docs.py')


class EmbeddedDocsComparison(unittest.TestCase):
    def setUp(self):
        self.work = tempfile.TemporaryDirectory()
        self.addCleanup(self.work.cleanup)
        self.generated = pathlib.Path(self.work.name) / 'generated'
        self.embedded = pathlib.Path(self.work.name) / 'embedded'
        for root, digest in [(self.generated, '11111111'), (self.embedded, '22222222')]:
            for locale in ['', 'pt-BR/']:
                for name, content in {
                    f'{locale}assets/js/runtime.{digest}.js': digest,
                    f'{locale}index.html': f'<script src="assets/js/runtime.{digest}.js"></script>',
                    f'{locale}assets/css/style.css': 'body { color: black }',
                }.items():
                    path = root / name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text(content)

    def check(self):
        return subprocess.run([sys.executable, str(CHECK), str(self.generated), str(self.embedded)], capture_output=True).returncode

    def test_root_and_locale_javascript_hashes_may_differ(self):
        self.assertEqual(self.check(), 0)

    def test_localized_css_still_must_match(self):
        (self.generated / 'pt-BR/assets/css/style.css').write_text('changed')
        self.assertNotEqual(self.check(), 0)

    def test_missing_localized_page_is_rejected(self):
        (self.generated / 'pt-BR/index.html').unlink()
        self.assertNotEqual(self.check(), 0)

    def test_javascript_outside_asset_directory_is_not_excluded(self):
        (self.generated / 'pt-BR/unreviewed.js').write_text('unexpected')
        self.assertNotEqual(self.check(), 0)
