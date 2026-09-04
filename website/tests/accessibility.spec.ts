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

const locales = [
  {name: 'en', prefix: ''},
  {name: 'pt-BR', prefix: '/pt-BR'},
] as const;

for (const locale of locales) {
  for (const colorScheme of ['light', 'dark'] as const) {
    for (const viewport of [
    {name: 'mobile', width: 375, height: 812},
    {name: 'tablet', width: 768, height: 900},
    {name: 'laptop', width: 1024, height: 900},
    {name: 'desktop', width: 1440, height: 1000},
  ]) {
      test.describe(`${locale.name} ${colorScheme} ${viewport.name}`, () => {
      test.use({colorScheme, viewport});

        for (const route of routes) {
        test(`${route} has no serious accessibility regression`, async ({page}) => {
          await page.goto(`${locale.prefix}${route || '/'}`, {waitUntil: 'networkidle'});
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

test('IVOAI wordmark uses the self-hosted Bpmf Huninn brand face', async ({page}) => {
  await page.goto('/', {waitUntil: 'networkidle'});
  await expect(page.locator('.navbar__brand')).toHaveAccessibleName('IVOAI home');
  await expect(page.locator('.hero__title')).toHaveAccessibleName('IVOAI');
  const typography = await page.locator('.hero__title .ivoai-wordmark').evaluate((node) => ({
    family: getComputedStyle(node).fontFamily,
    status: document.fonts.status,
  }));
  expect(typography.family).toContain('Bpmf Huninn');
  expect(typography.status).toBe('loaded');
  const lambda = page.locator('.hero__title .ivoai-wordmark__lambda path');
  await expect(lambda).toHaveAttribute('d', 'M2 22 L10 2 L18 22');
  expect(await lambda.evaluate((node) => ({
    linecap: getComputedStyle(node).strokeLinecap,
    linejoin: getComputedStyle(node).strokeLinejoin,
  }))).toEqual({linecap: 'round', linejoin: 'round'});
  const fontRequestHosts = await page.evaluate(() => performance.getEntriesByType('resource')
    .filter((entry) => /\.(?:woff2?|ttf)(?:\?|$)/.test(entry.name))
    .map((entry) => new URL(entry.name).host));
  expect(new Set(fontRequestHosts)).toEqual(new Set([new URL(page.url()).host]));
});

test('self-hosted search returns an indexed documentation result', async ({page}) => {
  await page.goto('/', {waitUntil: 'networkidle'});
  const search = page.getByRole('textbox', {name: 'Search'});
  await search.fill('multi-server');
  await expect(page.getByRole('option', {name: /Multi-server/i}).first()).toBeVisible({timeout: 10_000});
});

test('Portuguese self-hosted search returns Portuguese documentation', async ({page}) => {
  await page.goto('/pt-BR/', {waitUntil: 'networkidle'});
  const search = page.locator('.navbar__search-input').first();
  await search.fill('memória');
  await expect(page.getByRole('option', {name: /Memória/i}).first()).toBeVisible({timeout: 10_000});
});

for (const locale of locales) {
  for (const theme of ['light', 'dark'] as const) {
    test(`${locale.name} ${theme} uses one semantic document canvas`, async ({page}) => {
      await page.goto(`${locale.prefix}/docs/troubleshooting`, {waitUntil: 'networkidle'});
      await page.evaluate((value) => document.documentElement.setAttribute('data-theme', value), theme);
      const colors = await page.evaluate(() => {
        const selectors = ['html', 'body', '#__docusaurus', '.main-wrapper', '.theme-doc-sidebar-container', '.theme-doc-toc-desktop'];
        const canvas = getComputedStyle(document.documentElement).getPropertyValue('--ivoai-canvas').trim();
        return {
          canvas,
          backgrounds: selectors.map((selector) => {
            const node = document.querySelector(selector);
            return node ? getComputedStyle(node).backgroundColor : null;
          }).filter(Boolean),
          height: document.documentElement.scrollHeight,
          viewport: window.innerHeight,
          code: getComputedStyle(document.querySelector('pre')!).backgroundColor,
        };
      });
      expect(colors.height).toBeGreaterThan(colors.viewport * 1.5);
      expect(new Set(colors.backgrounds).size, JSON.stringify(colors)).toBe(1);
      expect(colors.code).not.toBe(colors.backgrounds[0]);
    });
  }
}

for (const theme of ['light', 'dark'] as const) {
  test(`${theme} semantic palette meets contrast floors`, async ({page}) => {
    await page.goto('/docs/quickstart', {waitUntil: 'networkidle'});
    await page.evaluate((value) => document.documentElement.setAttribute('data-theme', value), theme);
    const ratios = await page.evaluate(() => {
      const style = getComputedStyle(document.documentElement);
      const rgb = (value: string) => {
        const probe = document.createElement('span');
        probe.style.color = value;
        document.body.append(probe);
        const match = getComputedStyle(probe).color.match(/[\d.]+/g)!.slice(0, 3).map(Number);
        probe.remove();
        return match;
      };
      const luminance = (value: string) => {
        const channels = rgb(value).map((channel) => channel / 255).map((channel) => channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4);
        return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
      };
      const contrast = (foreground: string, background: string) => {
        const a = luminance(foreground);
        const b = luminance(background);
        return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
      };
      const token = (name: string) => style.getPropertyValue(name).trim();
      return {
        text: contrast(token('--ivoai-text'), token('--ivoai-canvas')),
        muted: contrast(token('--ivoai-text-muted'), token('--ivoai-canvas')),
        link: contrast(token('--ivoai-primary-strong'), token('--ivoai-canvas')),
        focus: contrast(token('--ivoai-focus'), token('--ivoai-canvas')),
      };
    });
    expect(ratios.text).toBeGreaterThanOrEqual(4.5);
    expect(ratios.muted).toBeGreaterThanOrEqual(4.5);
    expect(ratios.link).toBeGreaterThanOrEqual(4.5);
    expect(ratios.focus).toBeGreaterThanOrEqual(3);
    const codeCommentRatio = await page.locator('.token.comment').first().evaluate((node) => {
      const foreground = getComputedStyle(node).color;
      const background = getComputedStyle(node.closest('pre') as Element).backgroundColor;
      const rgb = (value: string) => value.match(/[\d.]+/g)!.slice(0, 3).map((part) => Number(part) / 255);
      const luminance = (value: string) => {
        const channels = rgb(value).map((channel) => channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4);
        return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
      };
      const a = luminance(foreground);
      const b = luminance(background);
      return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
    });
    expect(codeCommentRatio).toBeGreaterThanOrEqual(4.5);
  });
}

test('locale selector preserves the current page and dark theme', async ({page}) => {
  await page.goto('/docs/quickstart', {waitUntil: 'networkidle'});
  await page.evaluate(() => {
    localStorage.setItem('theme', 'dark');
    document.documentElement.setAttribute('data-theme', 'dark');
  });
  const language = page.getByRole('button', {name: 'English'}).first();
  await language.focus();
  await page.keyboard.press('Enter');
  const portuguese = page.getByRole('link', {name: 'Português (Brasil)'}).first();
  await expect(portuguese).toBeVisible();
  await portuguese.click();
  await expect(page).toHaveURL(/\/pt-BR\/docs\/quickstart$/);
	await expect(page.getByRole('heading', {level: 1, name: 'Início rápido'})).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.dataset.theme)).toBe('dark');
});

test('locale and version routes remain composable', async ({page}) => {
  await page.goto('/pt-BR/docs/quickstart', {waitUntil: 'networkidle'});
  await expect(page.locator('html')).toHaveAttribute('lang', 'pt-BR');
	await expect(page.getByRole('heading', {level: 1, name: 'Início rápido'})).toBeVisible();
	await expect(page.getByRole('button', {name: '0.9.1'}).first()).toBeVisible();
  const language = page.getByRole('button', {name: 'Português (Brasil)'}).first();
  await language.click();
  await page.getByRole('link', {name: 'English'}).first().click();
	await expect(page).toHaveURL(/\/docs\/quickstart$/);
});

test('Portuguese accessibility labels are localized', async ({page}) => {
  await page.goto('/pt-BR/docs/quickstart', {waitUntil: 'networkidle'});
  await expect(page.getByRole('textbox', {name: 'Buscar'})).toBeVisible();
  await page.goto('/pt-BR/docs/cli-reference', {waitUntil: 'networkidle'});
  await expect(page.getByRole('region', {name: 'Tabela rolável'}).first()).toBeVisible();
});

for (const locale of locales) {
  for (const theme of ['light', 'dark'] as const) {
    for (const viewport of [
      {name: 'mobile', width: 375, height: 812},
      {name: 'desktop', width: 1440, height: 1000},
    ]) {
      test(`${locale.name} ${theme} ${viewport.name} footer is aligned`, async ({page}) => {
        await page.setViewportSize(viewport);
        await page.goto(`${locale.prefix}/docs/quickstart`, {waitUntil: 'networkidle'});
        await page.evaluate((value) => document.documentElement.setAttribute('data-theme', value), theme);
        const layout = await page.locator('footer.footer').evaluate((footer) => {
          const box = (element: Element) => {
            const rect = element.getBoundingClientRect();
            return {bottom: rect.bottom, left: rect.left, right: rect.right, top: rect.top, width: rect.width};
          };
          const container = footer.querySelector('.container')!;
          const columns = [...footer.querySelectorAll('.footer__col')].map(box);
          const copyright = footer.querySelector('.footer__copyright')!;
          const links = [...footer.querySelectorAll('.footer__link-item')].map(box);
          const titles = [...footer.querySelectorAll('.footer__title')].map((title) => ({
            ...box(title),
            clip: getComputedStyle(title).clip,
            cssHeight: Number.parseFloat(getComputedStyle(title).height),
            cssWidth: Number.parseFloat(getComputedStyle(title).width),
            position: getComputedStyle(title).position,
          }));
          return {
            columns,
            container: box(container),
            copyright: box(copyright),
            links,
            overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
            titles,
            viewportWidth: window.innerWidth,
          };
        });

        expect(Math.abs(layout.container.left - (layout.viewportWidth - layout.container.right))).toBeLessThanOrEqual(1);
        expect(layout.overflow).toBeLessThanOrEqual(1);
        expect(layout.columns).toHaveLength(2);
        expect(layout.columns.every((column) => column.left >= layout.container.left && column.right <= layout.container.right)).toBe(true);
        expect(layout.copyright.left).toBeGreaterThanOrEqual(layout.container.left);
        expect(layout.copyright.right).toBeLessThanOrEqual(layout.container.right);
        if (viewport.name === 'desktop') {
          expect(layout.links).toHaveLength(4);
          expect(Math.max(...layout.links.map((link) => link.top)) - Math.min(...layout.links.map((link) => link.top))).toBeLessThanOrEqual(1);
          expect(Math.max(...layout.links.map((link) => link.bottom))).toBeLessThan(layout.copyright.top);
          expect(layout.titles.every((title) => title.position === 'absolute'
            && title.cssWidth <= 1
            && title.cssHeight <= 1
            && title.clip !== 'auto')).toBe(true);
        } else {
          expect(Math.abs(layout.columns[0].left - layout.columns[1].left)).toBeLessThanOrEqual(1);
          expect(layout.columns[1].top).toBeGreaterThan(layout.columns[0].bottom);
          expect(layout.columns[1].top - layout.columns[0].bottom).toBeLessThanOrEqual(32);
        }
      });
    }
  }
}

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
