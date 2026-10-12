// Library-only regression: no upstream CLI, providers, network or repository access.
import assert from 'node:assert/strict';
import {pathToFileURL} from 'node:url';
import {resolve} from 'node:path';
const root=process.argv[2];
const {extractFile}=await import(pathToFileURL(resolve(root,'dist/graph/extract.js')));
const {resolveEdges}=await import(pathToFileURL(resolve(root,'dist/graph/resolve.js')));
const packages={
 'one/a.go':'package first\nfunc Scale(v int) int { return v*2 }',
 'one/b.go':'package first\nfunc Other() {}',
 'two/a.go':'package second\nfunc Scale(v int) int { return v*3 }',
 'nested/pkg/a.go':'package nested\nfunc Scale(v int) int { return v*4 }',
};
function targets(source, extra={}) {
 const units=Object.entries({...packages,...extra,'app/a.go':source}).map(([p,s])=>extractFile(p,s,'go'));
 const nodes=units.flatMap(u=>u.nodes);
 const edges=resolveEdges(nodes,units.flatMap(u=>u.rawEdges),{goModules:[{module:'example.test/root',dir:'.'},{module:'example.test/nested',dir:'nested'}]});
 const names=new Map(nodes.map(n=>[n.id,`${n.path}:${n.name}`]));
 return edges.filter(e=>e.relation==='calls'&&names.get(e.source)==='app/a.go:Entry').map(e=>names.get(e.target)).sort();
}
function check(name,source,want,extra){assert.deepEqual(targets(source,extra),want,name);}
check('declared package differs from directory','package app\nimport "example.test/root/one"\nfunc Entry(){first.Scale(1)}',['one/a.go:Scale']);
check('explicit aliases disambiguate names','package app\nimport (a "example.test/root/one"; b "example.test/root/two")\nfunc Entry(){a.Scale(1);b.Scale(1)}',['one/a.go:Scale','two/a.go:Scale']);
check('nested module','package app\nimport "example.test/nested/pkg"\nfunc Entry(){nested.Scale(1)}',['nested/pkg/a.go:Scale']);
check('missing import never falls back globally','package app\nfunc Entry(){first.Scale(1)}',[]);
check('parameter shadows','package app\nimport "example.test/root/one"\nfunc Entry(first interface{ Scale(int) }){first.Scale(1)}',[]);
check('short local shadows','package app\nimport "example.test/root/one"\nfunc Entry(){ first:=value; first.Scale(1) }',[]);
check('var shadows','package app\nimport "example.test/root/one"\nfunc Entry(){ var first Thing; first.Scale(1) }',[]);
check('range shadows','package app\nimport "example.test/root/one"\nfunc Entry(){ for _, first := range values {first.Scale(1)} }',[]);
check('if initializer shadows','package app\nimport "example.test/root/one"\nfunc Entry(){ if first:=value; ok {first.Scale(1)} }',[]);
check('nested scope does not shadow outer call','package app\nimport "example.test/root/one"\nfunc Entry(){ {first:=value; first.Scale(1)}; first.Scale(2) }',['one/a.go:Scale']);
check('binding begins after initializer','package app\nimport "example.test/root/one"\nfunc Entry(){ first:=first.Scale(1); first.Scale(2) }',['one/a.go:Scale']);
check('duplicate package definition ambiguous','package app\nimport "example.test/root/one"\nfunc Entry(){first.Scale(1)}',[],{'one/duplicate.go':'package first\nfunc Scale(v int) int{return v}'});
check('external package remains unresolved','package app\nimport "external.invalid/first"\nfunc Entry(){first.Scale(1)}',[]);
check('dot and blank imports not qualifiers','package app\nimport (. "example.test/root/one"; _ "example.test/root/two")\nfunc Entry(){first.Scale(1);second.Scale(2)}',[]);
check('type switch alias shadows','package app\nimport "example.test/root/one"\nfunc Entry(){ switch first := v.(type) {case Thing:first.Scale(1)} }',[]);
check('select receive shadows','package app\nimport "example.test/root/one"\nfunc Entry(){ select {case first := <- ch:first.Scale(1)} }',[]);
check('multiple parameters shadow','package app\nimport "example.test/root/one"\nfunc Entry(a, first Thing){first.Scale(1)}',[]);
check('named result shadows','package app\nimport "example.test/root/one"\nfunc Entry()(first Thing){first.Scale(1);return}',[]);
check('unrelated import preserves typed methods','package app\nimport "example.test/root/one"\ntype Worker struct{}\nfunc(w Worker) Run(){}\nfunc Entry(){w:=Worker{};w.Run()}',['app/a.go:Run']);
check('external path cannot coincide with source file','package app\nimport \"one/a.go\"\nfunc Entry(){first.Scale(1)}',[]);
console.log('Go package call regression: 20 cases passed');
