#!/usr/bin/env python3
import io
import json
import pathlib
import tempfile
import tarfile
import unittest
import build

class ArchiveTests(unittest.TestCase):
    def test_source_paths_and_special_entries_fail_closed(self):
        for name, kind in [('../escape', tarfile.REGTYPE), ('/absolute', tarfile.REGTYPE), ('root/link', tarfile.SYMTYPE), ('root/device', tarfile.CHRTYPE)]:
            with self.subTest(name=name, kind=kind), tempfile.TemporaryDirectory() as root:
                archive=pathlib.Path(root)/'source.tgz'
                with tarfile.open(archive,'w:gz') as tar:
                    info=tarfile.TarInfo(name);info.type=kind
                    tar.addfile(info)
                with self.assertRaises(ValueError):build.extract(archive,pathlib.Path(root)/'out')

    def test_multiple_source_roots_are_rejected(self):
        with tempfile.TemporaryDirectory() as root:
            archive=pathlib.Path(root)/'source.tgz'
            with tarfile.open(archive,'w:gz') as tar:
                for name in ['first/file','other/file']:tar.addfile(tarfile.TarInfo(name))
            with self.assertRaises(ValueError):build.extract(archive,pathlib.Path(root)/'out')

    def test_clean_source_extracts_only_below_destination(self):
        with tempfile.TemporaryDirectory() as root:
            archive=pathlib.Path(root)/'source.tgz';target=pathlib.Path(root)/'out'
            with tarfile.open(archive,'w:gz') as tar:
                info=tarfile.TarInfo('root/folder/file');info.size=4
                tar.addfile(info,io.BytesIO(b'test'))
            build.extract(archive,target)
            self.assertEqual((target/'folder/file').read_bytes(),b'test')

    def test_missing_notices_and_replaced_modules_rejected(self):
        with tempfile.TemporaryDirectory() as root:
            root=pathlib.Path(root);module=root/'module';module.mkdir();deps=root/'deps.json'
            for value in [{'Path':'example.com/test','Version':'v1.0.0','Dir':str(module)}, {'Replace':{'Path':'attacker'}}]:
                deps.write_text(json.dumps({'Module':value}))
                with self.assertRaises(ValueError):build.licenses(deps,root/'out')

    def test_notices_bound_to_exact_modules(self):
        with tempfile.TemporaryDirectory() as root:
            root=pathlib.Path(root);module=root/'module';module.mkdir();(module/'LICENSE').write_text('MIT test notice');deps=root/'deps.json';dest=root/'out';dest.mkdir()
            value={'Path':'example.com/test','Version':'v1.0.0','Sum':'h1:test','Dir':str(module)}
            deps.write_text(json.dumps({'Module':value}));build.licenses(deps,dest)
            found=json.loads((dest/'modules.json').read_text())
            self.assertEqual(found['example.com/test']['version'],'v1.0.0')
            self.assertEqual(found['example.com/test']['notices'][0]['sha256'],build.digest(module/'LICENSE'))

    def test_cleanup_refuses_non_build_directories(self):
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaises(ValueError):build.cleanup(root)

if __name__=='__main__':unittest.main()
