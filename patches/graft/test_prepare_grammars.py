#!/usr/bin/env python3
import copy
import hashlib
import importlib.util
import json
import pathlib
import tempfile
import unittest

HERE=pathlib.Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('prepare_grammars',HERE/'prepare_grammars.py')
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

class PrepareGrammars(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=pathlib.Path(self.temp.name)/'source';self.catalog=pathlib.Path(self.temp.name)/'licenses'
        self.catalog.mkdir();(self.root/'src/graph').mkdir(parents=True)
        data=copy.deepcopy(json.loads((HERE/'grammar-licenses/inventory.json').read_text()))
        self.pkg=self.root/'node_modules/tree-sitter-wasm';self.pkg.mkdir(parents=True)
        (self.pkg/'package.json').write_text(json.dumps({'name':'tree-sitter-wasm','version':'1.1.6'}))
        (self.root/'package-lock.json').write_text(json.dumps({'packages':{'node_modules/tree-sitter-wasm':{'version':'1.1.6','integrity':data['bundle']['integrity']}}}))
        generic=[]
        for row in data['grammars']:
            name=row['language'];body=('wasm:'+name).encode();row['wasmSHA256']=hashlib.sha256(body).hexdigest()
            out=self.pkg/'out'/name;out.mkdir(parents=True)
            (out/('tree-sitter-'+name+'.wasm')).write_bytes(body)
            (out/('tree-sitter-'+name+'.wasm.sigstore.json')).write_text('{}')
            (out/'highlights.scm').write_text('unneeded')
            (self.catalog/row['licenseFile']).write_bytes((HERE/'grammar-licenses'/row['licenseFile']).read_bytes())
            if name!='vue':generic.append('{wasm: "'+name+'"}')
        (self.root/'src/graph/generic.ts').write_text(','.join(generic)+'\nfunction requireWasm(x) {}\nfunction loadWasmLanguage(x) {}')
        (self.root/'src/graph/container.ts').write_text('{wasm: "vue"}\nloadWasmLanguage(row.wasm)')
        (self.pkg/'manifest.json').write_text('{}')
        (self.pkg/'out/unused').mkdir();(self.pkg/'out/unused/unknown.wasm').write_text('not-used')
        (self.catalog/'inventory.json').write_text(json.dumps(data))

    def test_keeps_all_runtime_grammars_and_adds_notices(self):
        module.prepare(self.root,self.catalog)
        self.assertEqual(len(list((self.pkg/'out').glob('*/*.wasm'))),16)
        self.assertFalse((self.pkg/'out/unused').exists())
        self.assertEqual(list((self.pkg/'out').rglob('*.scm')),[])
        self.assertEqual(len(list((self.pkg/'THIRD_PARTY_LICENSES').glob('*.LICENSE'))),16)
        self.assertEqual(len(json.loads((self.pkg/'manifest.json').read_text())),16)

    def test_unknown_runtime_grammar_fails_before_pruning(self):
        with (self.root/'src/graph/generic.ts').open('a') as f:f.write('{wasm: "new_language"}')
        with self.assertRaisesRegex(ValueError,'registry'):module.prepare(self.root,self.catalog)
        self.assertTrue((self.pkg/'out/unused').exists())

    def test_changed_binary_fails_before_pruning(self):
        (self.pkg/'out/rust/tree-sitter-rust.wasm').write_text('changed')
        with self.assertRaisesRegex(ValueError,'WASM'):module.prepare(self.root,self.catalog)
        self.assertTrue((self.pkg/'out/unused').exists())

    def test_new_dynamic_loader_caller_requires_review(self):
        (self.root/'src/extra.ts').write_text('loadWasmLanguage(externalInput)')
        with self.assertRaisesRegex(ValueError,'loader'):module.prepare(self.root,self.catalog)

if __name__=='__main__':unittest.main()
