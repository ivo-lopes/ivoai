// Library-only smoke gate. Does not load the upstream CLI, telemetry or MCP.
import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
import {stat, readdir} from 'node:fs/promises';
const require = createRequire('/build/package.json');
const Parser = require('tree-sitter');
for (const [name, variants] of [
  ['tree-sitter-typescript', ['typescript', 'tsx']],
  ['tree-sitter-javascript'], ['tree-sitter-python'], ['tree-sitter-go'],
  ['tree-sitter-r'], ['tree-sitter-java'], ['tree-sitter-kotlin'],
  ['tree-sitter-swift'], ['tree-sitter-php', ['php', 'php_only']],
]) {
  const module = require(name);
  for (const grammar of variants ? variants.map(v => module[v]) : [module]) {
    const parser = new Parser();
    parser.setLanguage(grammar);
    const tree = parser.parse('');
    assert(tree.rootNode);
  }
}
await import('/build/dist/graph/build.js');
const generic = await import('/build/dist/graph/generic.js');
await generic.warmGenericGrammars(generic.GENERIC_LANGS.map(x => x.name));
for (const {name} of generic.GENERIC_LANGS) assert(generic.isWarm(name), `Missing WASM grammar: ${name}`);
const container = await import('/build/dist/graph/container.js');
await container.warmContainerGrammars(container.CONTAINER_LANGS.map(x => x.name));
for (const {name} of container.CONTAINER_LANGS) assert(container.isContainerWarm(name), `Missing container grammar: ${name}`);
for (const file of ['index.html', 'app.js', 'style.css']) assert((await stat(`/build/dist/viewer/${file}`)).size > 0);
assert((await readdir('/build/dist/graph/queries')).some(x => x.endsWith('.scm')));
console.log('Graft library, native/WASM grammars, queries and viewer smoke passed');
