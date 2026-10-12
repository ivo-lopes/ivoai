// Review lock resolution and the public API used by OpenAI's optional transport.
// No network connection, credentials, npm lifecycle or upstream CLI is used.
import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
import {resolve,join} from 'node:path';
import {readFileSync} from 'node:fs';
const root=resolve(process.argv[2]);
const require=createRequire(join(root,'package.json'));
const manifest=JSON.parse(readFileSync(join(root,'package.json'),'utf8'));
const lock=JSON.parse(readFileSync(join(root,'package-lock.json'),'utf8'));
assert.equal(manifest.overrides.undici,'6.28.1');
for(const [path,pkg] of Object.entries(lock.packages)) {
 if(path.endsWith('/undici')) {
  assert.equal(pkg.version,'6.28.1');
  assert.equal(pkg.integrity,'sha512-zWpdTVD54H48CIybL0rWQ3ukpb9d23wM7eH5RtfdmeP70cWHNjtfo7P4vZX+5CoDcO53J4Pu5uXp7lNfjc6DRA==');
 }
}
assert.equal(require('undici/package.json').version,'6.28.1');
const {Agent,ProxyAgent,WebSocket}=require('undici');
for(const api of [Agent,ProxyAgent,WebSocket])assert.equal(typeof api,'function');
await new Agent().close();
console.log('reviewed dependency lock and transport API passed');
