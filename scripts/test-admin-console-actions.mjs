// Browser checks for the administrative action contract and prefix routing.
// The backend below is a disposable fixture: no OAuth, Core mutation or proxy
// deployment is certified by this test. Real App checks remain separate.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, mkdtemp, mkdir, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import { createHash } from 'node:crypto';

const modules = process.env.IVOAI_ADMIN_UI_NODE_MODULES;
assert.ok(modules, 'Use the existing reviewed website node_modules; this script installs nothing.');
const require = createRequire(join(resolve(modules), '.admin-actions-qa.cjs'));
const { chromium } = require('@playwright/test');
const AxeBuilder = require('@axe-core/playwright').default;
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const output = await mkdtemp(join(tmpdir(), 'ivoai-admin-actions-'));
const results = [];
const browserVersions = new Set();
const generated = '2026-11-04T18:00:00Z';
const catalog = [
  { id: 'config.show', title: 'Consultar configuração', group: 'configuration', description: 'Consulte os valores sanitizados.', kind: 'read', fields: [], available: true },
  { id: 'config.auto', title: 'Configurar execução automática', group: 'configuration', description: 'Altere a configuração após revisão.', kind: 'mutation', fields: [
    { name: 'enabled', label: 'Habilitar execução automática', type: 'boolean', default: true },
    { name: 'strategy', label: 'Estratégia', type: 'select', required: true, options: [{ value: 'adaptive', label: 'Adaptativa' }, { value: 'explicit', label: 'Explícita' }] },
    { name: 'workers', label: 'Limite de workers', type: 'integer', min: 1, max: 8, required: true, help: 'De 1 a 8 workers.' },
    { name: 'secret', label: 'Código privado da fixture', type: 'password' },
    { name: 'note', label: 'Observação', type: 'textarea' },
  ], confirmation: true, available: true },
  { id: 'launch.codex', title: 'Abrir Codex', group: 'sessions', description: 'Continue no cliente nativo.', kind: 'native_handoff', fields: [], available: true },
  { id: 'policy.sources', title: 'Consultar fontes permitidas', group: 'policy', description: 'Confira a política de fontes.', kind: 'information', fields: [], available: true },
  { id: 'server.restart', title: 'Reiniciar servidor', group: 'server', description: 'Reinicie os serviços existentes.', kind: 'mutation', fields: [], available: false, reason: 'Servidor não configurado nesta fixture.' },
];

for (const prefix of ['', '/ivoai/admin']) {
  let authorized = true;
  let manage = false;
  let committed = 0;
  let previewCount = 0;
  let connectionLoss = false;
  let pending;
  const methods = [];
  const errors = [];
  const served = new Map([['/', ['index.html', 'text/html']], ['/assets/admin.css', ['admin.css', 'text/css']], ['/assets/admin.js', ['admin.js', 'application/javascript']]]);
  const server = createServer(async (req, res) => {
    try {
      const actual = new URL(req.url, 'http://localhost').pathname;
      methods.push({ method: req.method, path: actual });
      if (!actual.startsWith(prefix + '/')) { res.writeHead(404).end(); return; }
      const path = actual.slice(prefix.length);
      res.setHeader('Cache-Control', 'no-store');
      if (served.has(path)) {
        const [file, type] = served.get(path);
        res.setHeader('Content-Type', type);
        res.setHeader('Content-Security-Policy', "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'");
        res.end(await readFile(join(root, 'internal/admin/assets', file))); return;
      }
      res.setHeader('Content-Type', 'application/json');
      const reply = (status, value) => res.writeHead(status).end(JSON.stringify(value));
      if (path === '/api/admin/v1/meta') { reply(200, { schema_version: 1, authentication: 'oauth_pkce_cookie', access: 'local_owner_read_only', base_path: prefix }); return; }
      if (!authorized) { reply(401, { error: 'unauthorized' }); return; }
      if (path === '/auth/logout' && req.method === 'POST') { authorized = false; res.writeHead(204).end(); return; }
      const snapshots = {
        overview: { schema_version: 1, version: 'fixture', read_only: !manage, generated_at: generated },
        sessions: { sessions: [], partial: false }, components: { components: [], partial: false }, connections: { connections: [], partial: false },
        csrf: { csrf_token: 'fixture_csrf_token', access: manage ? 'manage' : 'read' }, actions: { schema_version: 1, actions: catalog },
      };
      const key = path.replace(/^\/api\/admin\/v1\//, '');
      if (req.method === 'GET' && Object.hasOwn(snapshots, key)) { reply(200, snapshots[key]); return; }
      if (req.method === 'POST' && key.startsWith('actions/')) {
        assert.equal(req.headers['x-ivoai-csrf'], 'fixture_csrf_token');
        assert.match(req.headers['content-type'], /application\/json/);
        let body = '';
        for await (const part of req) { body += part; assert.ok(body.length < 65536); }
        const payload = JSON.parse(body);
        assert.match(payload.request_id, /^[a-f0-9-]{36}$/);
        const action = catalog.find((entry) => entry.id === key.slice('actions/'.length));
        assert.ok(action);
        if (action.kind === 'mutation' && !manage) { reply(403, { error: 'write_required' }); return; }
        if (payload.phase === 'preview') {
          assert.equal(action.kind, 'mutation');
          previewCount += 1;
          pending = { action: action.id, input: payload.input, requestID: payload.request_id, token: `confirmation-${previewCount}` };
          reply(200, { action_id: action.id, summary: 'Revise a configuração antes de salvar.', confirmation: pending.token, expires_at: new Date(Date.now() + 120000).toISOString() }); return;
        }
        assert.equal(payload.phase, 'execute');
        if (action.kind === 'mutation') {
          if (!pending) { reply(409, { error: 'confirmation_consumed' }); return; }
          assert.equal(payload.confirmation, pending.token); assert.equal(payload.request_id, pending.requestID); assert.deepEqual(payload.input, pending.input);
          pending = null; committed += 1;
          if (connectionLoss) { res.destroy(); return; }
        }
        const value = { action_id: action.id, status: 'completed', request_id: payload.request_id, output: action.kind === 'mutation' ? 'Configuração atualizada.' : '<img src=x onerror="window.fixtureInjected=true">\n' + 'Linha de resultado administrativo.\n'.repeat(100) };
        if (action.kind === 'mutation') value.one_time_code = 'DISPOSABLE_RESULT_CODE';
        if (action.kind === 'native_handoff') value.status = 'handoff';
        if (action.kind === 'information') value.status = 'information';
        if (action.kind === 'native_handoff') value.handoff = { executable: 'ivoai', args: ['codex', '--cwd', "project '$(touch blocked)'" ] };
        reply(200, value); return;
      }
      reply(404, { error: 'not_found' });
    } catch (failure) { errors.push(String(failure)); if (!res.headersSent) res.writeHead(500); res.end(); }
  });
  await new Promise((done) => server.listen(0, '127.0.0.1', done));
  const origin = `http://127.0.0.1:${server.address().port}`;
  const url = origin + prefix + '/';
  const home = join(output, prefix ? 'prefix' : 'root');
  const dirs = Object.fromEntries(['home', 'config', 'cache', 'runtime', 'tmp'].map((name) => [name, join(home, name)]));
  for (const directory of Object.values(dirs)) await mkdir(directory, { recursive: true, mode: 0o700 });
  let browser;
  try {
    browser = await chromium.launch({ executablePath: process.env.IVOAI_ADMIN_UI_CHROMIUM || '/usr/bin/chromium', headless: true, env: { PATH: '/usr/bin:/bin', HOME: dirs.home, XDG_CONFIG_HOME: dirs.config, XDG_CACHE_HOME: dirs.cache, XDG_RUNTIME_DIR: dirs.runtime, TMPDIR: dirs.tmp, LANG: 'C.UTF-8' } });
    browserVersions.add(await browser.version());
    const context = await browser.newContext({ locale: 'pt-BR', reducedMotion: 'reduce', viewport: { width: 1440, height: 960 } });
    await context.route('**/*', async (route) => { if (new URL(route.request().url()).origin === origin) await route.continue(); else { errors.push('external_request'); await route.abort(); } });
    const page = await context.newPage();
    page.on('pageerror', (failure) => errors.push(String(failure)));
    const axe = async (label) => assert.deepEqual((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()).violations.map((v) => ({ id: v.id, nodes: v.nodes.map((n) => n.target) })), [], label);
    await page.goto(url);
    await page.locator('#workspace').waitFor({ state: 'visible' });
    await page.locator('[data-view="administration"]').click();
    assert.equal(await page.locator('#actions-list article').count(), 5);
    assert.equal(await page.getByRole('button', { name: 'Configurar: Configurar execução automática', exact: true }).isDisabled(), true);
    assert.match(await page.locator('#action-access-notice').innerText(), /permite consultas/);
    await page.getByRole('button', { name: 'Consultar: Consultar configuração', exact: true }).click();
    await page.locator('#action-submit').click();
    await page.locator('#action-result').waitFor({ state: 'visible' });
    assert.match(await page.locator('#action-result-output').innerText(), /<img/);
    assert.equal(await page.evaluate(() => window.fixtureInjected), undefined);
    await axe('scrollable read result');
    const resultText = page.locator('#action-result-output pre');
    assert.equal(await resultText.evaluate((element) => element.scrollHeight > element.clientHeight), true);
    await resultText.focus(); await page.keyboard.press('PageDown');
    await page.waitForFunction(() => document.querySelector('#action-result-output pre').scrollTop > 0);
    await page.keyboard.press('Escape');
    results.push(`${prefix || '/'}: read-only access, text-only result rendering and mutation denial PASS`);
    manage = true;
    await page.locator('#refresh').click();
    await page.waitForFunction(() => document.getElementById('access-mode').textContent === 'Acesso de administração');
    await page.locator('#action-search').fill('execução automática');
    assert.equal(await page.locator('#actions-list article').count(), 1);
    await page.locator('#action-search').fill('');
    await page.locator('#action-group').selectOption('policy');
    assert.equal(await page.locator('#actions-list article').count(), 1);
    await page.locator('#action-group').selectOption('all');
    const openMutation = async () => {
      await page.getByRole('button', { name: 'Configurar: Configurar execução automática', exact: true }).click();
      await page.getByLabel('Limite de workers (obrigatório)', { exact: true }).fill('2');
      await page.getByLabel('Código privado da fixture', { exact: true }).fill('DISPOSABLE_SECRET');
      await page.getByLabel('Observação', { exact: true }).fill('<script>fixtureInjected=true</script>');
      await page.locator('#action-submit').click();
      await page.locator('#action-review').waitFor({ state: 'visible' });
    };
    await openMutation();
    assert.equal(committed, 0); assert.equal(previewCount, 1);
    assert.equal(await page.locator('#action-execute').isDisabled(), true);
    assert.equal(await page.locator('#action-review-values').innerText().then((value) => value.includes('DISPOSABLE_SECRET')), false);
    await page.locator('#action-fields input[name="workers"]').evaluate((element) => { element.value = '7'; });
    await axe('review');
    await page.locator('#action-confirmed').check();
    await page.locator('#action-execute').click();
    await page.locator('#action-result').waitFor({ state: 'visible' });
    assert.equal(committed, 1);
    assert.equal(await page.locator('#one-time-code').inputValue(), 'DISPOSABLE_RESULT_CODE');
    assert.equal(await page.locator('#one-time-code').getAttribute('type'), 'password');
    await page.locator('#reveal-one-time-code').click();
    assert.equal(await page.locator('#one-time-code').getAttribute('type'), 'text');
    assert.equal(await page.locator('#operation-history').innerText().then((value) => value.includes('DISPOSABLE_RESULT_CODE')), false);
    assert.match(await page.locator('#action-result-state').innerText(), /Alteração concluída/);
    await axe('one-time code result');
    await page.waitForFunction(() => document.getElementById('action-fields').childElementCount === 0);
    await page.locator('#action-execute').evaluate((element) => element.click());
    assert.equal(committed, 1);
    assert.equal(await page.evaluate(() => window.fixtureInjected), undefined);
    await page.keyboard.press('Escape');
    await page.locator('#action-dialog').waitFor({ state: 'hidden' });
    await page.waitForFunction(() => document.getElementById('one-time-code').value === '');
    results.push(`${prefix || '/'}: schema fields, immutable reviewed input, explicit single execution and secret clearing PASS`);
    await page.getByRole('button', { name: 'Ver orientação: Abrir Codex', exact: true }).click();
    await page.locator('#action-submit').click();
    await page.locator('#action-result').waitFor({ state: 'visible' });
    assert.match(await page.locator('#action-result-state').innerText(), /não foi executada/);
    assert.match(await page.locator('.native-command').innerText(), /'codex'/);
    assert.equal(committed, 1);
    await page.keyboard.press('Escape');
    for (const width of [375, 768, 1024, 1440]) {
      await page.setViewportSize({ width, height: 960 });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, `catalog overflow ${width}`);
      await axe(`catalog ${width}`);
      const undersized = await page.evaluate(() => [...document.querySelectorAll('button,input,select,nav a')].filter((element) => element.checkVisibility()).filter((element) => { const r = element.getBoundingClientRect(); return r.width < 44 || r.height < 44; }).map((element) => element.id));
      assert.deepEqual(undersized, []);
      await page.evaluate(() => window.scrollTo(0, 0));
      await page.locator('#action-search').focus();
      await page.screenshot({ path: join(home, `administration-${width}.png`), fullPage: true });
      await openMutation();
      assert.equal(await page.locator('#action-dialog').evaluate((element) => element.scrollWidth <= element.clientWidth), true);
      await axe(`review ${width}`);
      await page.locator('#action-confirmed').focus();
      await page.screenshot({ path: join(home, `review-${width}.png`), fullPage: false });
      await page.keyboard.press('Escape');
    }
    results.push(`${prefix || '/'}: 375/768/1024/1440 catalog/review WCAG A/AA, overflow and 44px targets PASS`);
    connectionLoss = true;
    await openMutation();
    await page.locator('#action-confirmed').check();
    await page.locator('#action-execute').click();
    await page.locator('#action-error').waitFor({ state: 'visible' });
    assert.match(await page.locator('#action-error').innerText(), /não será repetida|já foi usada/);
    assert.equal(committed, 2);
    assert.equal(await page.locator('#action-form').isVisible(), false);
    assert.equal(await page.locator('#action-review').isVisible(), false);
    await page.keyboard.press('Escape');
    connectionLoss = false;
    await openMutation();
    authorized = false;
    await page.locator('#action-confirmed').check();
    await page.locator('#action-execute').click();
    await page.locator('#sign-in').waitFor({ state: 'visible' });
    assert.equal(committed, 2);
    assert.equal(await page.locator('#action-dialog').isVisible(), false);
    assert.equal(await page.locator('#actions-list').innerText(), '');
    assert.equal(await page.locator('#operation-history').innerText(), '');
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    authorized = true; manage = false;
    await page.reload();
    await page.locator('#workspace').waitFor({ state: 'visible' });
    assert.equal(await page.locator('#access-mode').innerText(), 'Acesso de leitura');
    await page.locator('#sign-out').click();
    await page.locator('#sign-in').waitFor({ state: 'visible' });
    assert.equal(authorized, false);
    assert.ok(methods.every((entry) => entry.path.startsWith(prefix + '/')), 'all routes stay under configured base path');
    assert.deepEqual(errors, []);
    results.push(`${prefix || '/'}: unknown outcome without replay, authority loss clears state, fresh read-only access and prefix-safe logout PASS`);
  } finally { await browser?.close(); await new Promise((done) => server.close(done)); }
}
const source = {};
for (const path of ['internal/admin/assets/index.html', 'internal/admin/assets/admin.css', 'internal/admin/assets/admin.js', 'scripts/test-admin-console-actions.mjs']) source[path] = createHash('sha256').update(await readFile(join(root, path))).digest('hex');
const axeManifest = JSON.parse(await readFile(join(dirname(require.resolve('@axe-core/playwright')), '..', 'package.json'), 'utf8'));
const report = { node: process.version, playwright: require('@playwright/test/package.json').version, axe: axeManifest.version, browsers: [...browserVersions], source_sha256: source, scope: 'hermetic_administrative_action_ui_contract', actual_oauth_exchange: false, actual_backend_store: false, actual_reverse_proxy: false, native_client_execution: false, screenshots: output, results };
await writeFile(join(output, 'results.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 });
console.log(JSON.stringify(report, null, 2));
