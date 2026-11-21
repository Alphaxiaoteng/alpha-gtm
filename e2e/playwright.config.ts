import { defineConfig, devices } from '@playwright/test';
import path from 'path';

const root = path.resolve(__dirname, '..');
const apiPort = Number(process.env.ALPHA_NEXUS_E2E_API_PORT || 17325);
const webPort = Number(process.env.ALPHA_NEXUS_E2E_WEB_PORT || 5174);
const dataDir = path.join(root, '.e2e-data');
const apiURL = `http://127.0.0.1:${apiPort}`;
const e2eBinary = path.join(root, 'dist', 'alpha-nexus-e2e');
const nodeBinary = process.env.ALPHA_NEXUS_E2E_NODE || process.execPath;
const quotedNodeBinary = JSON.stringify(nodeBinary);

export default defineConfig({
  testDir: './tests',
  testIgnore: '**/*.real.spec.ts',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }]],
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL: `http://127.0.0.1:${webPort}`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    locale: 'zh-CN',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: [
    {
      command: `rm -rf "${dataDir}" && ALPHA_NEXUS_DISABLE_PAID_MEDIA=1 "${e2eBinary}" --data-dir "${dataDir}" ui serve --port ${apiPort}`,
      url: `${apiURL}/api/overview`,
      reuseExistingServer: false,
      timeout: 60_000,
      cwd: root,
    },
    {
      command: `VITE_API_PROXY_TARGET=${apiURL} VITE_TRUST_LOCAL_PROXY_ORIGIN=1 ${quotedNodeBinary} scripts/run-vite.mjs --host 127.0.0.1 --port ${webPort} --strictPort`,
      url: `http://127.0.0.1:${webPort}`,
      reuseExistingServer: false,
      timeout: 60_000,
      cwd: path.join(root, 'ui-web'),
    },
  ],
});
