#!/usr/bin/env python3
import importlib.util
import json
import pathlib
import tempfile
import unittest
HERE=pathlib.Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('prune_cli',HERE/'prune_cli.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
class PruneCLI(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=pathlib.Path(self.temp.name);self.modules=self.root/'node_modules'
        for directory in ['js-yaml/bin','argparse/node_modules/sprintf-js','gray-matter']:
            (self.modules/directory).mkdir(parents=True)
        yaml={'name':'js-yaml','version':'3.15.2','dependencies':{'argparse':'^1.0.7','esprima':'^4.0.0'},'bin':{'js-yaml':'bin/js-yaml.js'}}
        (self.modules/'js-yaml/package.json').write_text(json.dumps(yaml))
        (self.modules/'js-yaml/bin/js-yaml.js').write_text("require('argparse')")
        (self.modules/'argparse/index.js').write_text("require('sprintf-js')")
        (self.root/'package-lock.json').write_text(json.dumps({'packages':{'node_modules/js-yaml':yaml,'node_modules/argparse':{},'node_modules/argparse/node_modules/sprintf-js':{}}}))
    def test_prunes_only_unreachable_cli_and_records_runtime_lock(self):
        module.prune(self.root)
        self.assertFalse((self.modules/'argparse').exists());self.assertFalse((self.modules/'js-yaml/bin').exists())
        lock=json.loads((self.root/'package-lock.json').read_text())
        self.assertEqual(list(lock['packages']),['node_modules/js-yaml'])
        self.assertEqual(lock['packages']['node_modules/js-yaml']['dependencies'],{'esprima':'^4.0.0'})
    def test_new_imports_fail_before_mutation(self):
        for statement in ["require('argparse')",'import x from "sprintf-js"',"import('argparse/index.js')"]:
            with self.subTest(statement=statement):
                (self.modules/'gray-matter/index.js').write_text(statement)
                with self.assertRaisesRegex(ValueError,'new runtime dependency'):module.prune(self.root)
                self.assertTrue((self.modules/'argparse').exists())
    def test_changed_yaml_version_requires_review(self):
        path=self.modules/'js-yaml/package.json';data=json.loads(path.read_text());data['version']='4.0.0';path.write_text(json.dumps(data))
        with self.assertRaisesRegex(ValueError,'requires review'):module.prune(self.root)
if __name__=='__main__':unittest.main()
