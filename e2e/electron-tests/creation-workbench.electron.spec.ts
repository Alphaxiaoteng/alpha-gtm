import { _electron as electron, expect, test, type ElectronApplication, type Page } from '@playwright/test';
import { mkdtemp, rm } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';

const root = path.resolve(__dirname, '../..');
const electronExecutable = path.join(
  root,
  'electron/node_modules/electron/dist/Electron.app/Contents/MacOS/Electron',
);
const goBinary = path.join(root, '.alpha-nexus-egolite-run/alpha-nexus-egolite');

async function runCreationSOP(page: Page, title: string) {
  await expect(page.getByTestId('app-shell')).toBeVisible({ timeout: 60_000 });
  await page.getByTestId('nav-creation').click();
  await expect(page.getByTestId('creation-workbench')).toBeVisible();

  await page.getByLabel('选题标题').fill(title);
  await page.getByLabel('原始证据与方向').fill('Electron 壳级 E2E：验证工作台、Go sidecar 与本地 SQLite；不调用外部模型。');
  await page.getByRole('button', { name: '生成选题方案' }).click();
  await expect(page.getByLabel('Agent 选题方案')).not.toHaveValue('');
  await page.getByRole('button', { name: /进入文案/ }).click();
  await page.getByRole('button', { name: '生成文案' }).click();
  await expect(page.getByLabel('文案草稿')).not.toHaveValue('');
  await page.getByRole('button', { name: /进入编导脚本/ }).click();
  await page.getByRole('button', { name: '生成脚本' }).click();
  await expect(page.getByPlaceholder('钩子、推进、案例、转折和结尾')).not.toHaveValue('');
  await page.getByRole('button', { name: '生成分镜' }).click();
  await expect(page.getByPlaceholder('镜号、时长、画面、景别、旁白与所需素材')).not.toHaveValue('');
  await page.getByRole('button', { name: /审核并入库/ }).click();
  await page.getByTestId('creation-review').click();
	await page.getByTestId('creation-manual-review-confirmation').check();
  await page.getByRole('button', { name: '保存到脚本库' }).click();
  await expect(page.getByText('脚本与分镜已保存到本地脚本库')).toBeVisible();
  await page.getByRole('button', { name: '存入内容库' }).click();
  await expect(page.getByText('已按人工确认存入内容库，状态为本地草稿')).toBeVisible();
}

test('real Electron shell persists the creation SOP through its supervised sidecar without creating an EgoLite Space', async () => {
  const isolatedHome = await mkdtemp(path.join(os.tmpdir(), 'alpha-nexus-electron-e2e-'));
  const dataDir = path.join(isolatedHome, 'data');
  let app: ElectronApplication | undefined;
  try {
    app = await electron.launch({
      executablePath: electronExecutable,
      args: [path.join(root, 'electron')],
      env: {
        HOME: isolatedHome,
        PATH: process.env.PATH || '/usr/bin:/bin',
        TMPDIR: process.env.TMPDIR || os.tmpdir(),
        LANG: process.env.LANG || 'zh_CN.UTF-8',
        NODE_ENV: 'test',
        ALPHA_NEXUS_E2E_USER_DATA: path.join(isolatedHome, 'electron-user-data'),
        ALPHA_NEXUS_DATA_DIR: dataDir,
        ALPHA_NEXUS_GO_BINARY: goBinary,
        ALPHA_NEXUS_DISABLE_PAID_MEDIA: '1',
      },
    });
    const page = await app.firstWindow({ timeout: 60_000 });
		await expect(page.getByTestId('app-shell')).toBeVisible({ timeout: 60_000 });
		expect(await page.evaluate(() => Boolean((window as unknown as { electronAPI?: unknown }).electronAPI))).toBe(true);

    const title = 'Electron E2E｜本地知识驱动的三秒钩子';
    await runCreationSOP(page, title);

    const persisted = await page.evaluate(async (expectedTitle) => {
      const [scriptsResponse, contentsResponse, spacesResponse] = await Promise.all([
        fetch('/api/scripts', { cache: 'no-store' }),
        fetch('/api/contents', { cache: 'no-store' }),
        fetch('/api/ego/spaces', { cache: 'no-store' }),
      ]);
      const scripts = await scriptsResponse.json() as Array<{ title: string; body: string; content_type: string }>;
      const contents = await contentsResponse.json() as Array<{ title: string; body: string; status: string }>;
      const spaces = await spacesResponse.json() as unknown[];
      return {
        responsesOK: scriptsResponse.ok && contentsResponse.ok && spacesResponse.ok,
        script: scripts.find((item) => item.title === expectedTitle),
        content: contents.find((item) => item.title === expectedTitle),
        spaces,
      };
    }, title);

    expect(persisted.responsesOK).toBe(true);
    expect(persisted.script?.content_type).toBe('video');
    expect(persisted.script?.body).toContain('## 分镜');
    expect(persisted.script?.body).toContain('## 审核记录');
    expect(persisted.content?.status).toBe('draft');
    expect(persisted.content?.body).toContain('## 分镜');
    expect(persisted.content?.body).toContain('## 审核记录');
    expect(persisted.spaces).toEqual([]);
  } finally {
    await app?.close().catch(() => undefined);
    await rm(isolatedHome, { recursive: true, force: true });
  }
});
