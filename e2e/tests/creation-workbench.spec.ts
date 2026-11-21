import { test, expect } from '@playwright/test';

test('runs the local creation SOP through reviewed script and content persistence', async ({ page }) => {
  const title = 'E2E｜本地知识驱动的三秒钩子';
	const baseResponse = await page.request.post('/api/knowledge/bases', {
		data: { name: 'E2E 编导知识库', description: '隔离测试知识' },
	});
	expect(baseResponse.ok()).toBeTruthy();
	const base = await baseResponse.json() as { id: number };
	const importResponse = await page.request.post('/api/knowledge/imports', {
		multipart: {
			base_id: String(base.id),
			source_name: '编导方法/三秒钩子.md',
			files: {
				name: '三秒钩子.md',
				mimeType: 'text/markdown',
				buffer: Buffer.from('# 三秒钩子\n\n本地知识驱动的短视频开场，需要先给出具体冲突，再承诺一个可执行结果。'),
			},
		},
	});
	expect(importResponse.ok()).toBeTruthy();

  await page.goto('/');
  await page.getByTestId('nav-creation').click();
  await expect(page.getByTestId('creation-workbench')).toBeVisible();

  await page.getByLabel('选题标题').fill(title);
  await page.getByLabel('原始证据与方向').fill('本地 E2E 证据：验证趋势、风格、脚本、分镜与审核均可追溯；不调用外部模型。');
  await page.getByRole('button', { name: '生成选题方案' }).click();
	await expect(page.getByLabel('Agent 选题方案')).toHaveValue(/\[KB:\d+\]/);

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
  await expect(page.getByText('尚未审核。内容库入库前必须审核；审核不会授权或执行发布。')).toHaveCount(0);
	await page.getByTestId('creation-manual-review-confirmation').check();

  await page.getByRole('button', { name: '保存到脚本库' }).click();
  await expect(page.getByText('脚本与分镜已保存到本地脚本库')).toBeVisible();
  await page.getByRole('button', { name: '存入内容库' }).click();
  await expect(page.getByText('已按人工确认存入内容库，状态为本地草稿')).toBeVisible();

  const scriptsResponse = await page.request.get('/api/scripts');
  expect(scriptsResponse.ok()).toBeTruthy();
  const scripts = await scriptsResponse.json() as Array<{ title: string; body: string; content_type: string }>;
  const storedScript = scripts.find((item) => item.title === title);
  expect(storedScript?.content_type).toBe('video');
  expect(storedScript?.body).toContain('## 分镜');
  expect(storedScript?.body).toContain('## 审核记录');
	expect(storedScript?.body).toContain('[KB:');
	expect(storedScript?.body).toContain('编导方法/三秒钩子.md');

  const contentsResponse = await page.request.get('/api/contents');
  expect(contentsResponse.ok()).toBeTruthy();
  const contents = await contentsResponse.json() as Array<{ title: string; body: string; status: string }>;
  const storedContent = contents.find((item) => item.title === title);
  expect(storedContent?.status).toBe('draft');
  expect(storedContent?.body).toContain('## 分镜');
  expect(storedContent?.body).toContain('## 审核记录');
	expect(storedContent?.body).toContain('[KB:');
	expect(storedContent?.body).toContain('编导方法/三秒钩子.md');
});
