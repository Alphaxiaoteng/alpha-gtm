import { test, expect } from '@playwright/test';

test('imports mixed local knowledge and keeps cited search usable', async ({ page }) => {
  await page.goto('/');
  await page.getByTestId('nav-knowledge').click();
  await expect(page.getByTestId('knowledge-workspace')).toBeVisible();

  await page.getByPlaceholder('知识库名称').fill('E2E 创作知识');
  await page.getByPlaceholder('用途，例如：品牌语气与产品资料').fill('验证本地解析与引用');
  await page.getByRole('button', { name: '创建知识库' }).click();
  await expect(page.getByText('导入到 E2E 创作知识')).toBeVisible();

  await page.getByTestId('knowledge-file-input').setInputFiles([
    {
      name: 'voice.md',
      mimeType: 'text/markdown',
      buffer: Buffer.from('# 品牌语气\n\n短视频开头先说明真实收益，表达自然可信。'),
    },
    {
      name: 'broken.pdf',
      mimeType: 'application/pdf',
      buffer: Buffer.from('not a pdf'),
    },
  ]);
  await page.getByRole('button', { name: '解析并建立索引' }).click();
  await expect(page.getByText('voice.md · 已索引')).toBeVisible();
  await expect(page.getByText(/broken\.pdf · 失败/)).toBeVisible();
  await expect(page.getByTestId('knowledge-index-summary')).toContainText('索引中已有 1 篇资料');
  await expect(page.getByTestId('knowledge-document-library')).toContainText('voice.md');
  await expect(page.getByTestId('knowledge-document-library')).toContainText('broken.pdf');
  await expect(page.getByTestId('knowledge-document-library')).toContainText('已索引');
  await expect(page.getByTestId('knowledge-document-library')).toContainText('解析失败');

  await page.getByPlaceholder('输入创作主题或关键字').fill('自然可信');
  await page.getByRole('button', { name: '检索', exact: true }).click();
  await expect(page.getByText(/\[KB:\d+\] E2E 创作知识 \/ voice\.md \/ 品牌语气/)).toBeVisible();
  await expect(page.getByText('来源路径：voice.md')).toBeVisible();
  await expect(page.getByText(/短视频开头先说明真实收益/)).toBeVisible();
});
