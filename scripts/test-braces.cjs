const assert = require('node:assert/strict');
const path = require('node:path');
const test = require('node:test');
const braces = require(path.resolve(process.env.IVOAI_BRACES_DIR));
const rejected = error => error instanceof SyntaxError && /braces safety limit/.test(error.message);

test('deep strings fail explicitly through every public consumer', () => {
  const patterns = [
    '{'.repeat(4000) + 'x' + '}'.repeat(4000),
    '('.repeat(4000) + 'x' + ')'.repeat(4000),
    '{('.repeat(2000) + 'x' + ')}'.repeat(2000),
    '${' + '{'.repeat(3999) + 'x' + '}'.repeat(4000),
    '{'.repeat(4000), '('.repeat(4000),
  ];
  for (const pattern of patterns) {
    for (const operation of ['parse', 'compile', 'expand', 'stringify']) {
      assert.throws(() => braces[operation](pattern), rejected, operation);
    }
  }
});

test('supplied deep, cyclic and oversized ASTs are bounded', () => {
  let deep = {type: 'text', value: 'x'};
  for (let i = 0; i < 4000; i++) deep = {type: 'brace', nodes: [deep]};
  const cycle = {type: 'root', nodes: []}; cycle.nodes.push(cycle);
  const wide = {type: 'root', ranges: 1, nodes: Array.from({length: 70000}, () => ({type: 'text', value: '1'}))};
  for (const ast of [deep, cycle, wide]) {
    for (const operation of ['compile', 'expand', 'stringify']) {
      assert.throws(() => braces[operation](ast), rejected, operation);
    }
  }
});

test('expansion rejects cyclic supplied parent links', () => {
  const root = {type: 'root', nodes: []};
  const node = {type: 'text', value: 'x'}; node.parent = node;
  root.nodes.push(node);
  assert.throws(() => braces.expand(root), rejected);
});

test('ordinary patterns and shallow siblings retain upstream behavior', () => {
  assert.deepEqual(braces.expand('{a,{b,c}}'), ['a', 'b', 'c']);
  assert.deepEqual(braces.expand('{1..3}'), ['1', '2', '3']);
  assert.deepEqual(braces.expand('{a,b}{c,d}'), ['ac', 'ad', 'bc', 'bd']);
  for (const pattern of ['{a,b', '{}', '${a,b}', '\\{a,b\\}', '"{a,b}"', '[{a,b}]']) {
    assert.equal(braces.stringify(braces.parse(pattern)), braces.stringify(pattern));
  }
  assert.equal(braces.compile('{a,b}'.repeat(1000)), '(a|b)'.repeat(1000));
  assert.doesNotThrow(() => braces.compile('{'.repeat(100) + 'x' + '}'.repeat(100)));
});

test('nesting boundary is fixed and cannot be disabled by options', () => {
  assert.doesNotThrow(() => braces.compile('{'.repeat(255) + 'x' + '}'.repeat(255)));
  assert.throws(() => braces.compile('{'.repeat(256) + 'x' + '}'.repeat(256), {maxDepth: Infinity}), rejected);
  assert.throws(() => braces.parse('{'.repeat(4000) + '1..2,3' + '}'.repeat(4000)), rejected);
});

test('supplied AST values cannot introduce unbounded array traversal', () => {
  let deep = 'x';
  for (let i = 0; i < 10000; i++) deep = [deep];
  const cycle = []; cycle.push(cycle);
  for (const value of [deep, cycle]) {
    for (const operation of ['compile', 'expand', 'stringify']) {
      assert.throws(() => braces[operation]({type: 'root', nodes: [{type: 'text', value}]}), rejected);
    }
  }
});
