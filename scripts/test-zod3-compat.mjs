import assert from 'node:assert/strict';
import {createRequire} from 'node:module';
import {readFile, writeFile} from 'node:fs/promises';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
import {spawnSync} from 'node:child_process';
import test from 'node:test';

const fixture = process.env.IVOAI_ZOD_FIXTURE;
assert.ok(fixture, 'use scripts/test-zod3-compat.sh');
const modules = await import(pathToFileURL(path.join(fixture, 'entry.mjs')));
const require = createRequire(path.join(fixture, 'entry.cjs'));
const surfaces = [
  ['root ESM', modules.root.z], ['V3 ESM', modules.v3.z],
  ['root CJS', require('zod').z], ['V3 CJS', require('zod/v3').z],
];

for (const [name, z] of surfaces) {
  test(`${name}: reject oversized constrained arrays/sets before any element or outer refinement`, async () => {
    for (const kind of ['array.max', 'array.length', 'array.nonempty.max', 'set.max', 'set.size']) {
      let visited = 0, outer = 0;
      const element = z.string().refine(() => { visited++; return true; });
      const schemas = {
        'array.max': z.array(element).max(2, 'upper bound'),
        'array.length': z.array(element).length(2, 'upper bound'),
        'array.nonempty.max': z.array(element).nonempty().max(2, 'upper bound'),
        'set.max': z.set(element).max(2, 'upper bound'),
        'set.size': z.set(element).size(2, 'upper bound'),
      };
      const schema = schemas[kind].refine(() => { outer++; return true; });
      const input = kind.startsWith('set') ? new Set(Array.from({length: 10000}, (_, i) => `v${i}`)) : Array(10000).fill('v');
      const result = schema.safeParse(input);
      assert.equal(result.success, false);
      assert.equal(visited, 0, kind);
      assert.equal(outer, 0, kind);
      assert.equal(result.error.issues.length, 1);
      assert.equal(result.error.issues[0].code, 'too_big');
      assert.equal(result.error.issues[0].message, 'upper bound');
      const invalid = kind.startsWith('set') ? new Set(Array.from({length: 10000}, (_, i) => i)) : Array(10000).fill(1);
      const invalidResult = schemas[kind].safeParse(invalid);
      assert.equal(invalidResult.success, false);
      assert.equal(invalidResult.error.issues.length, 1, `${kind}: no per-element issue aggregation`);
      assert.equal(invalidResult.error.issues[0].code, 'too_big');
      const asynchronous = schemas[kind].superRefine(async () => { outer++; });
      assert.equal((await asynchronous.safeParseAsync(input)).success, false);
      assert.equal(visited, 0, `${kind} async`);
      assert.equal(outer, 0, `${kind} async`);
    }
  });

  test(`${name}: nested error paths, contextual error maps and invalid types remain V3`, () => {
    let visited = 0;
    const schema = z.object({items: z.array(z.string().refine(() => { visited++; return true; })).max(1)});
    let contextSeen = false;
    const errorMap = (issue, ctx) => {
      contextSeen = typeof ctx.defaultError === 'string' && Array.isArray(ctx.data);
      return {message: `bounded-${issue.code}`};
    };
    const result = schema.safeParse({items: ['a', 'b']}, {errorMap});
    assert.equal(result.success, false);
    assert.equal(contextSeen, true);
    assert.equal(visited, 0);
    assert.deepEqual(result.error.issues[0].path, ['items']);
    assert.equal(result.error.issues[0].message, 'bounded-too_big');
    const exact = z.array(z.string()).length(1).safeParse(['a', 'b']);
    assert.equal(exact.error.issues[0].exact, true);
    assert.equal(exact.error.issues[0].maximum, 1);
    assert.equal(z.array(z.string()).max(2).safeParse({length: 100}).error.issues[0].code, 'invalid_type');
    assert.equal(z.set(z.string()).max(2).safeParse(['a']).error.issues[0].code, 'invalid_type');
  });

  test(`${name}: admitted outputs, defaults, transforms, refinements and minima retain semantics`, async () => {
    let visited = 0;
    const element = z.string().transform(v => v.toUpperCase()).refine(() => {visited++; return true;});
    const schema = z.object({items: z.array(element).nonempty().max(3), count: z.number().default(2)}).strict();
    assert.deepEqual(schema.parse({items: ['a', 'b']}), {items: ['A', 'B'], count: 2});
    assert.equal(visited, 2);
    assert.deepEqual(await schema.parseAsync({items: ['a']}), {items: ['A'], count: 2});
    assert.equal(schema.safeParse({items: []}).error.issues[0].code, 'too_small');
    assert.equal(z.array(z.string()).length(2).safeParse(['a']).error.issues[0].code, 'too_small');
    assert.equal(z.set(z.string()).size(2).safeParse(new Set(['a'])).error.issues[0].code, 'too_small');
    assert.equal(schema.safeParse({items: ['a'], unknown: true}).success, false);
    assert.deepEqual([...z.set(element).max(2).parse(new Set(['a', 'b']))], ['A', 'B']);
    assert.equal(z.string().ip({version: 'v4'}).safeParse('127.0.0.1').success, true);
    assert.equal(z.string().ip({version: 'v4'}).safeParse('invalid').success, false);
    assert.equal(z.function().args(z.number()).returns(z.number()).implement(v => v * 2)(3), 6);
  });

  test(`${name}: nonempty without an upper bound intentionally remains unbounded`, () => {
    let visited = 0;
    const result = z.array(z.string().refine(() => {visited++; return true;})).nonempty().safeParse(Array(200).fill('v'));
    assert.equal(result.success, true);
    assert.equal(visited, 200);
    assert.equal(z.array(z.string()).nonempty().safeParse([]).success, false);
  });
}

test('published V4 exports retain the upstream fix for arrays, maps and sets', () => {
  for (const z of [modules.v4.z, require('zod/v4').z]) {
    for (const kind of ['array', 'map', 'set']) {
      let visited = 0;
      const element = z.string().refine(() => {visited++; return true;});
      const schema = kind === 'array' ? z.array(element).max(1) : kind === 'set' ? z.set(element).max(1) : z.map(element, element).max(1);
      const input = kind === 'array' ? ['a', 'b'] : kind === 'set' ? new Set(['a', 'b']) : new Map([['a', 'a'], ['b', 'b']]);
      assert.equal(schema.safeParse(input).success, false);
      assert.equal(visited, 0);
    }
  }
});

test('fork identity and license are explicit; entry points share V3 constructors', async () => {
  const metadata = require('zod/package.json');
  assert.equal(metadata.name, '@ivoai/zod3-compat');
  assert.equal(metadata.version, '3.25.76-ivoai.1');
  assert.equal(metadata.license, 'MIT');
  assert.equal(metadata.scripts, undefined);
  assert.equal(modules.root.z.ZodArray, modules.v3.z.ZodArray);
  assert.equal(require('zod').ZodArray, require('zod/v3').ZodArray);
  assert.match(await readFile(path.join(fixture, 'package/LICENSE'), 'utf8'), /Copyright \(c\) 2025 Colin McDonnell/);
});

test('published mini aliases retain string parsing in ESM and CJS', () => {
  for (const mini of [modules.mini, modules.v4Mini, modules.v4MiniNested,
    require('zod/mini'), require('zod/v4-mini'), require('zod/v4/mini')]) {
    assert.equal(mini.string().parse('mini'), 'mini');
    assert.equal(mini.string().safeParse(123).success, false);
  }
});

test('published compile entry retains its upstream side-effect-only export contract', () => {
  assert.deepEqual(Object.keys(modules.compile), []);
  assert.deepEqual(Object.keys(require('zod/compile')), []);
});

test('published locale entry points retain English error maps in ESM and CJS', () => {
  for (const locales of [modules.locales, modules.v4Locales,
    require('zod/locales'), require('zod/v4/locales')]) {
    assert.equal(typeof locales.en, 'function');
    const errorMap = locales.en().localeError;
    assert.equal(typeof errorMap, 'function');
    assert.equal(errorMap({code: 'invalid_type', expected: 'string', input: 123}),
      'Invalid input: expected string, received number');
  }
});

test('published core retains its direct parser and string constructor in ESM and CJS', () => {
  for (const core of [modules.core, require('zod/v4/core')]) {
    const schema = new core.$ZodString({type: 'string'});
    assert.equal(core.parse(schema, 'core'), 'core');
    assert.throws(() => core.parse(schema, 123));
  }
});

const tsc = process.env.IVOAI_ZOD_TSC;
if (tsc) {
  test('root/V3/V4/mini declarations type-check for ESM and CJS consumers', async () => {
    const source = `import zDefault, {z, ZodArray} from "zod";
import {z as z3} from "zod/v3";
import {z as z4} from "zod/v4";
import * as mini from "zod/mini";
const schema = z.object({items: z.array(z.string()).nonempty().max(3), count: z.number().default(2)});
const accepted: z.infer<typeof schema> = {items: ["x"], count: 2};
// @ts-expect-error V3 nonempty arrays retain a required first element.
const rejected: z.infer<typeof schema> = {items: [], count: 2};
const constructor: typeof z3.ZodArray = ZodArray;
const ip: string = zDefault.string().ip({version: "v4"}).parse("127.0.0.1");
const fn: (value: number) => number = z.function().args(z.number()).returns(z.number()).implement(value => value * 2);
const value4: string = z4.string().parse("v4");
const minimal: string = mini.string().parse("mini");
void [accepted, rejected, constructor, ip, fn, value4, minimal];
`;
    const files = ['types-consumer.mts', 'types-consumer.cts'].map(file => path.join(fixture, file));
    await Promise.all(files.map(file => writeFile(file, source)));
    const result = spawnSync(path.resolve(tsc), [
      '--noEmit', '--strict', '--target', 'ES2022', '--module', 'NodeNext',
      '--moduleResolution', 'NodeNext', ...files,
    ], {cwd: fixture, encoding: 'utf8', timeout: 60000, maxBuffer: 1 << 20});
    assert.ifError(result.error);
    assert.equal(result.status, 0, `TypeScript declaration check failed:\n${result.stdout}\n${result.stderr}`);
  });
} else {
  console.log('ZOD_TYPES=NOT_RUN (provide IVOAI_ZOD_TSC from an existing reviewed TypeScript installation)');
}

const consumer = process.env.IVOAI_ZOD_SECURITY_VALIDATOR;
if (consumer) {
  test('actual Ruflo security validator source imports and preserves validation via the V3 fork', async () => {
    // The caller provides the reviewed file from an isolated locked Ruflo prefix.
    // Only this pure schema module is evaluated, never the CLI or an installer.
    const destination = path.join(fixture, 'security-validator.mjs');
    await writeFile(destination, await readFile(consumer));
    const security = await import(pathToFileURL(destination));
    assert.equal(security.IdentifierSchema.safeParse('task_123').success, true);
    assert.equal(security.IdentifierSchema.safeParse('../invalid').success, false);
    assert.equal(security.IPv4Schema.safeParse('127.0.0.1').success, true);
    assert.equal(security.IPv4Schema.safeParse('invalid').success, false);
    assert.equal(security.IdentifierSchema.safeParse(123).success, false);
  });
} else {
  console.log('RUFLO_ZOD_CONSUMER=NOT_RUN (provide IVOAI_ZOD_SECURITY_VALIDATOR from reviewed isolated prefix)');
}
