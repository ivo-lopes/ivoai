// Static regression gate over pinned, patched TypeScript. Never imports Graft.
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {createRequire} from 'node:module';
import {resolve} from 'node:path';
const root = resolve(process.argv[2] ?? '/build');
const require = createRequire(resolve(root, 'package.json'));
const ts = require('typescript');
const source = ts.createSourceFile('build.ts', readFileSync(resolve(root, 'src/graph/build.ts'), 'utf8'), ts.ScriptTarget.Latest, true);
let loop;
function visit(node) {
  if (ts.isForOfStatement(node) && node.expression.getText(source) === 'files.entries()') loop = node;
  ts.forEachChild(node, visit);
}
visit(source);
assert(loop, 'parse lifecycle must yield from the async build loop');
const first = loop.statement.statements[0];
assert(ts.isIfStatement(first));
assert.equal(first.expression.getText(source).replace(/\s/g, ''), 'i>0&&i%32===0');
let awaits = 0, immediates = 0, continues = 0, returns = 0;
function inspect(node) {
  if (ts.isAwaitExpression(node)) awaits++;
  if (ts.isCallExpression(node) && node.expression.getText(source) === 'setImmediate') immediates++;
  if (ts.isContinueStatement(node)) continues++;
  if (ts.isReturnStatement(node)) returns++;
  ts.forEachChild(node, inspect);
}
inspect(loop.statement);
assert.equal(awaits, 1, 'native finalizers need an awaited event-loop turn');
assert.equal(immediates, 1);
assert.equal(continues, 4, 'read/encoding/cached-error/cached-success cases must advance');
assert.equal(returns, 0, 'a skipped file must not return from the full graph build');
console.log('Graft bounded parse lifecycle source regression: PASS');
