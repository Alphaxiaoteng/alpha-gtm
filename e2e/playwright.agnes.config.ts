import { defineConfig, devices } from '@playwright/test';
import os from 'os';
import path from 'path';

if (process.env.ALPHA_NEXUS_REAL_AGNES_SMOKE !== '1') {
  throw new Error('Set ALPHA_NEXUS_REAL_AGNES_SMOKE=1 only after approving real Agnes calls.');
}

const root = path.resolve(__dirname, '..');
const apiPort = Number(process.env.ALPHA_NEXUS_E2E_API_PORT || 18325);
const webPort = Number(process.env.ALPHA_NEXUS_E2E_WEB_PORT || 5274);
const dataDir = path.join(os.tmpdir(), `alpha-nexus-real-agnes-${process.pid}`);
const apiURL = `http://127.0.0.1:${apiPort}`;
const e2eBinary = path.join(root, 'dist', 'alpha-nexus-e2e');

export default defineConfig({
  testDir: './tests',
  testMatch: '**/*.real.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 20 * 60_000,
  expect: { timeout: 30_000 },
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'playwright-report-agnes' }]],
  use: {
    baseURL: `http://127.0.0.1:${webPort}`,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    locale: 'zh-CN',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      command: `"${e2eBinary}" --data-dir "${dataDir}" ui serve --port ${apiPort}`,
      url: `${apiURL}/api/overview`,
      reuseExistingServer: false,
      timeout: 60_000,
      cwd: root,
    },
    {
      command: `VITE_API_PROXY_TARGET=${apiURL} VITE_TRUST_LOCAL_PROXY_ORIGIN=1 npm run dev -- --host 127.0.0.1 --port ${webPort} --strictPort`,
      url: `http://127.0.0.1:${webPort}`,
      reuseExistingServer: false,
      timeout: 60_000,
      cwd: path.join(root, 'ui-web'),
    },
  ],
});
