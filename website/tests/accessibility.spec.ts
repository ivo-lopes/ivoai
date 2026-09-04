import AxeBuilder from '@axe-core/playwright';
import {expect, test} from '@playwright/test';

const routes = [
  '/',
  '/docs/quickstart',
  '/docs/cli-reference',
  '/docs/server',
  '/docs/multi-server',
  '/docs/mcp-web',
  '/docs/troubleshooting',
  '/docs/security',
];

for (const colorScheme of ['light', 'dark'] as const) {
  for (const viewport of [
    {name: 'mobile', width: 375, height: 812},
    {name: 'tablet', width: 768, height: 900},
    {name: 'laptop', width: 1024, height: 900},
    {name: 'desktop', width: 1440, height: 1000},
  ]) {
    test.describe(`${colorScheme} ${viewport.name}`, () => {
      test.use({colorScheme, viewport});

      for (const route of routes) {
        test(`${route} has no serious accessibility regression`, async ({page}) => {
          await page.goto(route, {waitUntil: 'networkidle'});
          await page.evaluate((theme) => document.documentElement.setAttribute('data-theme', theme), colorScheme);
          const results = await new AxeBuilder({page}).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa']).analyze();
          const violations = results.violations.filter((violation) => violation.impact === 'critical' || violation.impact === 'serious');
          expect(violations, JSON.stringify(violations, null, 2)).toEqual([]);
          const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
          expect(overflow).toBeLessThanOrEqual(1);
        });
      }
    });
  }
}

test('keyboard focus remains visible and skip navigation works', async ({page}) => {
  await page.goto('/', {waitUntil: 'networkidle'});
  await page.keyboard.press('Tab');
  const focused = page.locator(':focus-visible');
  await expect(focused).toBeVisible();
  await expect(focused).toHaveAttribute('href', /__docusaurus_skipToContent/);
  const outline = await focused.evaluate((node) => getComputedStyle(node).outlineStyle);
  expect(outline).not.toBe('none');
  await page.keyboard.press('Enter');
  await expect(page.locator('#__docusaurus_skipToContent_fallback')).toBeInViewport();
  await expect(focused).not.toBeVisible();
  expect(await page.evaluate(() => window.scrollY)).toBeGreaterThan(0);
});

test('documentation remains usable at 200 percent zoom', async ({page}) => {
  await page.setViewportSize({width: 768, height: 900});
  await page.goto('/docs/cli-reference', {waitUntil: 'networkidle'});
  await page.evaluate(() => { document.documentElement.style.zoom = '2'; });
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(1);
  await expect(page.getByRole('heading', {level: 1})).toBeVisible();
});

test('self-hosted search returns an indexed documentation result', async ({page}) => {
  await page.goto('/', {waitUntil: 'networkidle'});
  const search = page.getByRole('textbox', {name: 'Search'});
  await search.fill('multi-server');
  await expect(page.getByRole('option', {name: /Multi-server/i}).first()).toBeVisible({timeout: 10_000});
});

test('mobile documentation navigation is keyboard operable', async ({page}) => {
  await page.setViewportSize({width: 375, height: 812});
  await page.goto('/docs/quickstart', {waitUntil: 'networkidle'});
  const toggle = page.getByRole('button', {name: 'Toggle navigation bar'});
  await toggle.focus();
  await page.keyboard.press('Enter');
  await expect(toggle).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByRole('link', {name: 'Installation'}).first()).toBeVisible();
});

test('reduced motion preference is honored', async ({page}) => {
  await page.emulateMedia({reducedMotion: 'reduce'});
  await page.goto('/', {waitUntil: 'networkidle'});
  const duration = await page.locator('body').evaluate((node) => getComputedStyle(node).transitionDuration);
  expect(Number.parseFloat(duration)).toBeLessThanOrEqual(0.00001);
});
