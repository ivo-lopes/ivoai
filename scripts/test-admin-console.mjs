// Hermetic UI fixtures only. This does not validate OAuth or certify real clients.
// Reuse reviewed, already-installed tooling through IVOAI_ADMIN_UI_NODE_MODULES.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, mkdtemp, chmod, mkdir, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

const modules = process.env.IVOAI_ADMIN_UI_NODE_MODULES;
if (!modules) throw new Error('Set IVOAI_ADMIN_UI_NODE_MODULES to an existing reviewed node_modules directory; no installation is performed.');
const require = createRequire(join(resolve(modules), '.admin-console-qa.cjs'));
const { chromium } = require('@playwright/test');
const AxeBuilder = require('@axe-core/playwright').default;
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const output = await mkdtemp(join(tmpdir(), 'ivoai-admin-ui-fixtures-'));
await chmod(output, 0o700);
let authorized = true;
let unavailable = false;
const methods = [];
const generated = '2026-10-28T15:32:00Z';
const sessions = [
  { id: 'session-a19f', state: 'running', mode: 'orchestrated', executor: 'codex', model: { name: 'gpt-5-codex', source: 'runtime_verified' }, started_at: generated, updated_at: generated, worker_count: 2, task_count: 3, latest_invocation_usage: { input_tokens: 12540, output_tokens: 891, cached_input_tokens: 4608, basis: 'client_reported', source: 'codex_exec_turn_completed', scope: 'cli_invocation', cached_input_included_in_input: true } },
  { id: 'session-b72c', state: 'completed', mode: 'direct', executor: 'claude', model: { name: 'Modelo não confirmado', source: 'unknown' }, started_at: generated, updated_at: generated, worker_count: 0, task_count: 1, latest_invocation_usage: null },
  { id: '<img src=x onerror="window.fixtureInjected=true">', state: 'blocked', mode: 'auto', executor: 'codex', model: { name: '<script>window.fixtureInjected=true</script>', source: 'configured' }, started_at: generated, updated_at: generated, worker_count: 0, task_count: 0, latest_invocation_usage: null },
];
const dto = {
  meta: { schema_version: 1, authentication: 'oauth_pkce_cookie', access: 'local_owner_read_only' },
  overview: { schema_version: 1, version: 'fixture', read_only: true, generated_at: generated, session_count: 3, active_sessions: 1, truncated: false, partial: false, latest_invocation_usage: null },
  sessions: { sessions, truncated: false, partial: false },
  components: { components: [{ name: 'Caveman', version: '3.2.0', installed: true, managed: true }, { name: 'Headroom', version: '', installed: false, managed: false }], partial: false },
  connections: { connections: [{ name: 'codex', configured: true, enabled: true, count: 1 }, { name: 'claude', configured: false, enabled: false, count: 0 }, { name: 'external_mcp', configured: true, enabled: true, count: 2 }], partial: false },
};

const server = createServer(async (request, response) => {
  const url = new URL(request.url, 'http://localhost');
  methods.push({ method: request.method, path: url.pathname });
  response.setHeader('Cache-Control', 'no-store');
  if (url.pathname === '/auth/logout' && request.method === 'POST') { authorized = false; response.writeHead(204).end(); return; }
  if (url.pathname.startsWith('/api/admin/v1/')) {
    response.setHeader('Content-Type', 'application/json');
    const key = url.pathname.slice('/api/admin/v1/'.length);
    if (key !== 'meta' && !authorized) { response.writeHead(401).end('{"error":"unauthorized"}'); return; }
    if (unavailable) { response.writeHead(503).end('{"error":"unavailable"}'); return; }
    if (key.startsWith('sessions/') && key.endsWith('/events')) {
      response.end(JSON.stringify({ schema_version: 1, latest_sequence: 1, oldest_sequence: 1, gap: false, partial: false, events: [{ sequence: 1, observed_at: generated, category: 'compression', operation: 'result', state: 'completed', duration_ms: 25, cache_hit: false, bytes_before: 4000, bytes_after: 500, token_basis: 'heuristic_4_bytes' }] })); return;
    }
    if (Object.hasOwn(dto, key)) { response.end(JSON.stringify(dto[key])); return; }
    response.writeHead(404).end('{"error":"not_found"}'); return;
  }
  const files = { '/': ['index.html', 'text/html'], '/assets/admin.css': ['admin.css', 'text/css'], '/assets/admin.js': ['admin.js', 'application/javascript'] };
  if (!Object.hasOwn(files, url.pathname)) { response.writeHead(404).end(); return; }
  const [file, contentType] = files[url.pathname];
  response.setHeader('Content-Type', contentType);
  response.setHeader('Content-Security-Policy', "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'");
  response.end(await readFile(join(root, 'internal/admin/assets', file)));
});
await new Promise((done) => server.listen(0, '127.0.0.1', done));
const url = `http://127.0.0.1:${server.address().port}`;
let browser;
const results = [];
try {
  const privateDirs = Object.fromEntries(['home', 'config', 'cache', 'runtime', 'tmp'].map((name) => [name, join(output, name)]));
  for (const directory of Object.values(privateDirs)) await mkdir(directory, { mode: 0o700 });
  browser = await chromium.launch({ executablePath: process.env.IVOAI_ADMIN_UI_CHROMIUM || '/usr/bin/chromium', headless: true,
    env: { PATH: '/usr/bin:/bin', HOME: privateDirs.home, XDG_CONFIG_HOME: privateDirs.config, XDG_CACHE_HOME: privateDirs.cache, XDG_RUNTIME_DIR: privateDirs.runtime, TMPDIR: privateDirs.tmp, LANG: 'C.UTF-8' } });
  const context = await browser.newContext({ reducedMotion: 'reduce', locale: 'pt-BR' });
  const page = await context.newPage();
  const failures = [];
  page.on('pageerror', (failure) => failures.push(failure.message));
  await page.goto(url);
  await page.locator('#workspace').waitFor({ state: 'visible' });
  assert.equal(await page.locator('#sessions-body tr').count(), 3);
  assert.equal(await page.evaluate(() => Boolean(window.fixtureInjected)), false);
  assert.equal(await page.locator('#sessions-body img, #sessions-body script').count(), 0);
  assert.match(await page.locator('#sessions-body').innerText(), /Sem medição/);
  await page.locator('#session-search').fill('b72');
  assert.equal(await page.locator('#sessions-body tr').count(), 1);
  await page.locator('#session-search').fill('not-found');
  assert.equal(await page.locator('#sessions-empty').isVisible(), true);
  await page.locator('#session-search').fill('');
  await page.locator('#session-state').selectOption('running');
  assert.equal(await page.locator('#sessions-body tr').count(), 1);
  await page.locator('#session-state').selectOption('all');
  const open = page.getByRole('button', { name: 'Ver detalhes da sessão session-a19f', exact: true });
  await open.focus();
  await page.keyboard.press('Enter');
  await page.locator('#session-dialog').waitFor({ state: 'visible' });
  await page.waitForFunction(() => document.getElementById('events-status').textContent.includes('1 evento'));
  assert.match(await page.locator('#usage-details').innerText(), /não é o total da sessão/);
  assert.match(await page.locator('#usage-details').innerText(), /já está incluída/);
  await page.keyboard.press('Escape');
  assert.equal(await open.evaluate((element) => document.activeElement === element), true);
  results.push('filters, unknown usage, text-only injection defense, keyboard dialog and focus restoration PASS');
  sessions[2].id = 'session-c48e'; // Keep visual fixtures readable after testing adversarial text.
  sessions[2].model.name = 'Modelo configurado';
  await page.locator('#refresh').click();
  await page.waitForFunction(() => document.getElementById('sessions-body').textContent.includes('session-c48e'));
  for (const width of [375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 960 });
    for (const view of ['sessions', 'components', 'connections']) {
      await page.locator(`[data-view="${view}"]`).click();
      await page.locator(`#${view}-view`).waitFor({ state: 'visible' });
      assert.equal(await page.locator(`#${view}-view`).isVisible(), true);
      const overflow = await page.evaluate(() => ({ width: window.innerWidth, content: document.documentElement.scrollWidth, elements: [...document.querySelectorAll('body *')].filter((element) => element.getBoundingClientRect().right > window.innerWidth + 1).slice(0, 8).map((element) => ({ tag: element.tagName, class: element.className, right: element.getBoundingClientRect().right })) }));
      assert.equal(overflow.content <= overflow.width, true, `page overflow ${view} ${width}: ${JSON.stringify(overflow)}`);
      const undersized = await page.evaluate(() => [...document.querySelectorAll('button,input,select,nav a')].filter((element) => element.checkVisibility()).filter((element) => { const bounds = element.getBoundingClientRect(); return bounds.height < 44 || bounds.width < 44; }).map((element) => element.id || element.className));
      assert.deepEqual(undersized, [], `44px targets ${view} ${width}`);
      const violations = (await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()).violations;
      assert.deepEqual(violations.map((violation) => ({ id: violation.id, nodes: violation.nodes.map((n) => n.target) })), [], `axe ${view} ${width}`);
      await page.screenshot({ path: join(output, `${view}-${width}.png`), fullPage: true });
    }
    await page.locator('[data-view="sessions"]').click();
    await page.getByRole('button', { name: 'Ver detalhes da sessão session-a19f', exact: true }).click();
    assert.equal(await page.locator('#session-dialog').evaluate((element) => element.scrollWidth <= element.clientWidth), true, `dialog overflow ${width}`);
    const violations = (await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()).violations;
    assert.deepEqual(violations.map((v) => v.id), [], `axe dialog ${width}`);
    await page.screenshot({ path: join(output, `detail-${width}.png`), fullPage: true });
    await page.keyboard.press('Escape');
    results.push(`viewport ${width}: sessions/components/connections/dialog overflow + axe PASS`);
  }
  unavailable = true;
  await page.locator('#refresh').click();
  await page.waitForFunction(() => !document.getElementById('error').hidden);
  assert.equal(await page.locator('#sessions-body tr').count(), 3);
  unavailable = false;
  authorized = false;
  await page.locator('#refresh').click();
  await page.locator('#sign-in').waitFor({ state: 'visible' });
  assert.equal(await page.locator('#workspace').isVisible(), false);
  assert.equal(await page.locator('#sessions-body').innerText(), '');
  assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
  const accessViolations = (await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()).violations;
  assert.deepEqual(accessViolations.map((v) => v.id), [], 'axe access gate');
  await page.screenshot({ path: join(output, 'access-required.png'), fullPage: true });
  authorized = true;
  await page.reload();
  await page.locator('#workspace').waitFor({ state: 'visible' });
  await page.locator('#sign-out').click();
  await page.locator('#sign-in').waitFor({ state: 'visible' });
  assert.equal(authorized, false);
  assert.deepEqual(methods.filter((entry) => entry.method !== 'GET'), [{ method: 'POST', path: '/auth/logout' }]);
  assert.deepEqual(failures, []);
  results.push('unavailability preserves stale snapshot explicitly, revocation clears data, logout only permitted mutation, no browser storage PASS');
  const report = { scope: 'hermetic_ui_fixtures_only', actual_oauth_exchange: false, actual_backend_store: false, native_client_execution: false, screenshots: output, results };
  await writeFile(join(output, 'results.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 });
  console.log(JSON.stringify(report, null, 2));
} finally {
  await browser?.close();
  await new Promise((done) => server.close(done));
}
