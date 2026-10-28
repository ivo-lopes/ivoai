// Browser proof against TestLiveAdminBrowserFixture's real App/OAuth/Store.
// The rendezvous contains only a disposable test grant; never use personal state.
import assert from 'node:assert/strict';
import { readFile, writeFile, stat, mkdtemp, mkdir, chmod } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';
import { createHash } from 'node:crypto';

const modules = process.env.IVOAI_ADMIN_UI_NODE_MODULES;
const rendezvous = process.env.IVOAI_ADMIN_BROWSER_FIXTURE;
const mode = process.env.IVOAI_ADMIN_BROWSER_PROOF_MODE || 'logout';
assert.ok(modules && rendezvous && ['logout', 'revoke'].includes(mode), 'Set reviewed modules, temporary fixture rendezvous and logout|revoke mode.');
const info = await stat(rendezvous);
assert.ok(info.isFile() && info.size < 4096 && (info.mode & 0o777) === 0o600, 'Fixture rendezvous must be a private bounded regular file.');
const fixture = JSON.parse(await readFile(rendezvous, 'utf8'));
const origin = new URL(fixture.url);
assert.ok(origin.protocol === 'http:' && origin.hostname === '127.0.0.1' && origin.port && origin.pathname === '/', 'Only disposable loopback fixtures are supported.');
assert.equal(typeof fixture.activation_code, 'string');
const require = createRequire(join(resolve(modules), '.admin-live-qa.cjs'));
const { chromium } = require('@playwright/test');
const AxeBuilder = require('@axe-core/playwright').default;
const output = await mkdtemp(join(tmpdir(), 'ia-live-'));
await chmod(output, 0o700);
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const results = [];
const routes = [];
let stage = 'launch';
let browser;
let deniedExternalRequests = 0;
let report;

try {
  const dirs = Object.fromEntries(['h', 'c', 'x', 'r', 't'].map((key) => [key, join(output, key)]));
  for (const directory of Object.values(dirs)) await mkdir(directory, { mode: 0o700 });
  browser = await chromium.launch({ executablePath: process.env.IVOAI_ADMIN_UI_CHROMIUM || '/usr/bin/chromium', headless: true,
    env: { PATH: '/usr/bin:/bin', HOME: dirs.h, XDG_CONFIG_HOME: dirs.c, XDG_CACHE_HOME: dirs.x, XDG_RUNTIME_DIR: dirs.r, TMPDIR: dirs.t, LANG: 'C.UTF-8' } });
  const context = await browser.newContext({ locale: 'pt-BR', reducedMotion: 'reduce', viewport: { width: 1440, height: 960 } });
  await context.route('**/*', async (route) => {
    if (new URL(route.request().url()).origin === origin.origin) await route.continue();
    else { deniedExternalRequests += 1; await route.abort(); }
  });
  const page = await context.newPage();
  let pageErrors = 0;
  page.on('pageerror', () => { pageErrors += 1; });
  page.on('response', (response) => {
    const url = new URL(response.url());
    routes.push({ method: response.request().method(), path: url.pathname, status: response.status() });
  });
  const axe = async (label) => {
    const violations = (await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze()).violations;
    assert.deepEqual(violations.map((v) => v.id), [], `Accessibility ${label}`);
  };
  const safeBrowserState = async () => {
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    assert.equal(await page.evaluate(() => document.cookie), '');
    assert.equal(new URL(page.url()).search, '');
    assert.equal(await page.locator('body').innerText().then((body) => body.includes('CANARY')), false);
  };

  stage = 'unauthorized';
  await page.goto(origin.origin);
  await page.locator('#sign-in').waitFor({ state: 'visible' });
  assert.equal(await page.locator('#workspace').isVisible(), false);
  await axe('access gate');
  results.push('Unauthenticated real API denies protected snapshots; UI exposes only access gate.');

  stage = 'oauth_form';
  await page.locator('#sign-in').click();
  await page.getByLabel('One-time activation code', { exact: true }).waitFor();
  await axe('OAuth form');
  await page.screenshot({ path: join(output, 'oauth-form.png'), fullPage: true });
  await page.getByLabel('One-time activation code', { exact: true }).fill(fixture.activation_code);
  // Do not retain the disposable grant in screenshots, reports, DOM snapshots or logs.
  fixture.activation_code = '';
  stage = 'oauth_exchange';
  await page.getByRole('button', { name: 'Authorize', exact: true }).click();
  await page.locator('#workspace').waitFor({ state: 'visible' });
  await safeBrowserState();
  const cookies = await context.cookies();
  const accessCookies = cookies.filter((cookie) => cookie.name.startsWith('ivoai_admin_') && !cookie.name.endsWith('_oauth_csrf') && !cookie.name.endsWith('_flow'));
  assert.equal(accessCookies.length, 1);
  assert.equal(accessCookies[0].httpOnly, true);
  assert.equal(accessCookies[0].sameSite, 'Strict');
  // Secure=false is expected only for this explicitly HTTP loopback fixture.
  assert.equal(accessCookies[0].secure, false);
  assert.equal(cookies.some((cookie) => cookie.name.endsWith('_flow')), false);
  results.push('Real browser PKCE/state/form/callback exchange passes; access cookie is HttpOnly/Strict, no JS cookies/storage or token URL.');

  stage = 'snapshots';
  assert.equal(await page.locator('#sessions-body tr').count(), 1);
  const sessionID = await page.locator('#sessions-body .session-id').innerText();
  await page.locator('#session-search').fill('absent-session');
  assert.equal(await page.locator('#sessions-empty').isVisible(), true);
  await page.locator('#session-search').fill(sessionID.slice(0, 8));
  assert.equal(await page.locator('#sessions-body tr').count(), 1);
  await page.locator('#session-search').fill('');
  await page.locator('#session-state').selectOption('running');
  assert.equal(await page.locator('#sessions-body tr').count(), 1);
  await page.locator('#session-state').selectOption('all');
  await page.locator('#session-executor').selectOption('codex');
  assert.equal(await page.locator('#sessions-body tr').count(), 1);
  await page.locator('#session-executor').selectOption('all');
  await page.locator('.row-detail').click();
  await page.locator('#session-dialog').waitFor({ state: 'visible' });
  await page.waitForFunction(() => document.getElementById('events-status').textContent.includes('1 evento'));
  assert.match(await page.locator('#usage-details').innerText(), /100[\s\S]*2[\s\S]*60/);
  assert.match(await page.locator('#usage-details').innerText(), /não é o total da sessão/);
  await axe('real session detail');
  await page.screenshot({ path: join(output, 'real-session-detail.png'), fullPage: true });
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('.row-detail').evaluate((element) => document.activeElement === element), true);
  results.push('Seeded Store session, filters, reported invocation usage 100/2/60, real event projection and keyboard drawer pass.');
  for (const width of [375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 960 });
    for (const view of ['sessions', 'components', 'connections']) {
      await page.locator(`[data-view="${view}"]`).click();
      await page.locator(`#${view}-view`).waitFor({ state: 'visible' });
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, `${view} overflow ${width}`);
      await axe(`${view} ${width}`);
      await page.screenshot({ path: join(output, `real-${view}-${width}.png`), fullPage: true });
    }
  }
  await safeBrowserState();
  results.push('Real authenticated sessions/components/connections at 375/768/1024/1440 pass overflow and WCAG A/AA checks.');

  stage = mode;
  if (mode === 'logout') await page.locator('#sign-out').click();
  else {
    await writeFile(rendezvous + '.revoke', '', { mode: 0o600, flag: 'wx' });
    await assert.doesNotReject(async () => {
      for (let i = 0; i < 50; i++) {
        try { await stat(rendezvous + '.revoked'); return; } catch { await new Promise((done) => setTimeout(done, 100)); }
      }
      throw new Error('fixture revocation not acknowledged');
    });
    await page.locator('#refresh').click();
  }
  await page.locator('#sign-in').waitFor({ state: 'visible' });
  assert.equal(await page.locator('#workspace').isVisible(), false);
  for (const id of ['sessions-body', 'components-list', 'connections-list', 'session-facts', 'usage-details', 'events-list']) assert.equal(await page.locator(`#${id}`).innerText(), '');
  await safeBrowserState();
  assert.equal(pageErrors, 0);
  assert.equal(deniedExternalRequests, 0);
  const mutations = routes.filter((route) => route.method !== 'GET');
  assert.deepEqual(mutations.map((route) => ({ method: route.method, path: route.path, status: route.status })), [
    { method: 'POST', path: '/oauth/authorize', status: 303 },
    ...(mode === 'logout' ? [{ method: 'POST', path: '/auth/logout', status: 200 }] : []),
  ]);
  results.push(mode === 'logout' ? 'Real logout revokes access and clears all administrative DOM snapshots.' : 'External grant revocation is observed on refresh and clears all administrative DOM snapshots.');
  stage = 'provenance';
  const source = {};
  for (const path of ['internal/admin/assets/index.html', 'internal/admin/assets/admin.css', 'internal/admin/assets/admin.js', 'internal/admin/server.go', 'internal/admin/read.go', 'internal/admin/server_test.go', 'internal/webauth/http.go', 'internal/webauth/store.go']) source[path] = createHash('sha256').update(await readFile(join(root, path))).digest('hex');
  // axe's package exports intentionally hide package.json; inspect its adjacent
  // installed manifest instead of importing an unsupported package subpath.
  const axeManifest = JSON.parse(await readFile(join(dirname(require.resolve('@axe-core/playwright')), '..', 'package.json'), 'utf8'));
  report = { status: 'PASS', scope: 'real_local_app_oauth_store_with_seeded_session', mode, node: process.version, playwright: require('@playwright/test/package.json').version, axe: axeManifest.version, browser: await browser.version(), actual_provider_execution: false, production_state: false, source_sha256: source, results, routes, denied_external_page_requests: deniedExternalRequests, output };
} catch (failure) {
  // Browser exception text can contain authorize URLs/codes. Retain only stage.
  const code = typeof failure.code === 'string' && /^[A-Z_]{1,64}$/.test(failure.code) ? failure.code : 'CHECK_FAILED';
  report = { status: 'FAIL', stage, failure_code: code, mode, scope: 'real_local_app_oauth_store_with_seeded_session', results, routes, output };
  process.exitCode = 1;
} finally {
  await browser?.close();
  await writeFile(join(output, 'results.json'), JSON.stringify(report, null, 2) + '\n', { mode: 0o600 });
  await writeFile(rendezvous + '.stop', '', { mode: 0o600, flag: 'wx' });
  console.log(JSON.stringify(report, null, 2));
}
