'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync, mkdtempSync, rmSync} = require('node:fs');
const {tmpdir} = require('node:os');
const path = require('node:path');
const {execFileSync} = require('node:child_process');
const {createHash} = require('node:crypto');
const vm = require('node:vm');
const root = path.resolve(__dirname, '..');
const provenance = JSON.parse(readFileSync(path.join(root, 'patches/deepmerge/provenance.json')));
let packageRoot = process.env.IVOAI_DEEPMERGE_UPSTREAM;
let temporary;
if (!packageRoot) {
  const archive = path.join(root, 'internal/components/assets', provenance.archive_file);
  assert.equal(createHash('sha256').update(readFileSync(archive)).digest('hex'), provenance.archive_sha256);
  temporary = mkdtempSync(path.join(tmpdir(), 'ivoai-deepmerge-test-'));
  execFileSync('python3', ['-c',
    'import pathlib,runpy,sys; runpy.run_path(sys.argv[1])["extract_regular"](pathlib.Path(sys.argv[2]).read_bytes(),pathlib.Path(sys.argv[3]))',
    path.join(root, 'patches/zod3-compat/build.py'), archive, temporary]);
  packageRoot = path.join(temporary, 'package');
}
process.on('exit', () => { if (temporary) rmSync(temporary, {recursive:true, force:true}); });
const cjs = require(path.join(packageRoot, 'dist/cjs.js'));
const sandbox = {module: {exports: {}}, exports: {}};
vm.runInNewContext(readFileSync(path.join(packageRoot, 'dist/umd.js'), 'utf8'), sandbox, {timeout:1000});
for (const [name, merge] of [['CJS', cjs], ['UMD', sandbox.module.exports]]) {
  for (const position of ['source', 'target']) {
    test(`${name}: null-prototype ${position} cannot poison the returned prototype`, () => {
      const malicious = Object.assign(Object.create(null), JSON.parse('{"__proto__":{"ivoaiPolluted":true},"safe":7}'));
      const output = position === 'source' ? merge(Object.create(null), malicious) : merge(malicious, {other:8});
      assert.equal(output.safe, 7);
      assert.equal(output.ivoaiPolluted, undefined);
      assert.equal(Object.getPrototypeOf(output).ivoaiPolluted, undefined);
      assert.equal(Object.hasOwn(output, '__proto__'), false);
      assert.equal({}.ivoaiPolluted, undefined);
    });
  }
  test(`${name}: nested and array inputs retain safe values without inherited payload`, () => {
    const value = JSON.parse('{"nested":{"__proto__":{"ivoaiPolluted":true},"safe":1},"items":[{"__proto__":{"ivoaiPolluted":true},"safe":2}]}');
    const output = merge({}, value);
    assert.equal(output.nested.safe, 1);
    assert.equal(output.items[0].safe, 2);
    assert.equal(output.nested.ivoaiPolluted, undefined);
    assert.equal(output.items[0].ivoaiPolluted, undefined);
  });
  test(`${name}: options do not permit direct prototype writes`, () => {
    const source = JSON.parse('{"__proto__":{"ivoaiPolluted":true},"safe":3}');
    const output = merge(Object.create(null), source, {clone:false, isMergeableObject:value => value !== null && typeof value === 'object'});
    assert.equal(output.safe,3);
    assert.equal(output.ivoaiPolluted,undefined);
  });
  test(`${name}: normal merges, callbacks, symbols, dates and input ownership remain compatible`, () => {
    const symbol = Symbol('safe');
    const date = new Date(0);
    const left = {nested:{a:1}, list:[1], date, [symbol]:{a:1}};
    const right = {nested:{b:2}, list:[2], [symbol]:{b:2}};
    const output = merge(left,right);
    assert.equal(JSON.stringify(output.nested),'{"a":1,"b":2}');
    assert.equal(JSON.stringify(output.list),'[1,2]');
    assert.equal(JSON.stringify(output[symbol]),'{"a":1,"b":2}');
    assert.equal(output.date,date);
    assert.equal(JSON.stringify(left.nested),'{"a":1}');
    assert.notEqual(output.nested,left.nested);
    assert.equal(JSON.stringify(merge({list:[1]},{list:[2]},{arrayMerge:(_a,b)=>b})), '{"list":[2]}');
    const custom = merge({nested:{a:1}}, {nested:{b:2}}, {customMerge:key => key === 'nested' ? (a,b)=>({sum:a.a+b.b}) : undefined});
    assert.equal(custom.nested.sum,3);
    assert.equal(JSON.stringify(merge.all([{a:1},{b:2}])),'{"a":1,"b":2}');
  });
}
