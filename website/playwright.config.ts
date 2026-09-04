import {defineConfig} from '@playwright/test';
import {existsSync} from 'node:fs';

const systemChromium = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || (existsSync('/usr/bin/chromium') ? '/usr/bin/chromium' : undefined);

export default defineConfig({
  testDir: './tests',
  outputDir: './test-results',
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'line' : 'list',
  use: {
    baseURL: 'http://127.0.0.1:3199',
    browserName: 'chromium',
    headless: true,
    launchOptions: systemChromium ? {executablePath: systemChromium} : {},
  },
  webServer: {
    command: 'corepack pnpm serve --host 127.0.0.1 --port 3199',
    url: 'http://127.0.0.1:3199/',
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
