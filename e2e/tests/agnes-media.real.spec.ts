import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

const publicFirstFrameURL = 'https://raw.githubusercontent.com/github/explore/main/topics/golang/golang.png';

async function waitForGeneration(
  request: APIRequestContext,
  mode: 'text_to_image' | 'image_to_image' | 'image_to_video',
  afterID: number,
) {
  let current: any;
  await expect.poll(async () => {
    const response = await request.get('/api/ai-image/generations');
    expect(response.ok()).toBeTruthy();
    const jobs = await response.json() as any[];
    current = jobs.find((job) => job.id > afterID && job.mode === mode);
    if (!current) return 'not_created';
    return current.status === 'failed' ? `failed:${current.error_code || 'GENERATION_FAILED'}` : current.status;
  }, { timeout: 18 * 60_000, intervals: [500, 1000, 2000, 5000] }).toBe('succeeded');
  return current;
}

async function expectDecodableImage(page: Page, assetURL: string) {
  const dimensions = await page.evaluate(async (url) => {
    const response = await fetch(url);
    if (!response.ok) throw new Error(`asset HTTP ${response.status}`);
    const bitmap = await createImageBitmap(await response.blob());
    const result = { width: bitmap.width, height: bitmap.height };
    bitmap.close();
    return result;
  }, assetURL);
  expect(dimensions.width).toBeGreaterThan(0);
  expect(dimensions.height).toBeGreaterThan(0);
}

async function expectPersistedMP4(page: Page, assetURL: string) {
  const artifact = await page.evaluate(async (url) => {
    const response = await fetch(url);
    if (!response.ok) throw new Error(`asset HTTP ${response.status}`);
    const bytes = new Uint8Array(await response.arrayBuffer());
    return {
      contentType: response.headers.get('content-type') || '',
      size: bytes.byteLength,
      box: bytes.byteLength >= 8 ? String.fromCharCode(...bytes.slice(4, 8)) : '',
    };
  }, assetURL);
  expect(artifact.contentType).toContain('video/mp4');
  expect(artifact.size).toBeGreaterThan(8);
  expect(artifact.box).toBe('ftyp');
}

test('real Agnes click-to-generate smoke persists text, image, and video artifacts', async ({ page, request }) => {
  const configResponse = await request.get('/api/ai-image/config');
  expect(configResponse.ok()).toBeTruthy();
  const config = await configResponse.json();
  expect(config.credential_configured, 'Save the Agnes key in macOS Keychain before running this opt-in smoke.').toBe(true);

  const fixtureResponse = await request.get(publicFirstFrameURL);
  expect(fixtureResponse.ok()).toBeTruthy();
  const fixtureType = (fixtureResponse.headers()['content-type'] || '').split(';')[0];
  expect(fixtureType).toBe('image/png');
  const fixture = await fixtureResponse.body();

  await request.put('/api/ai-image/workspace', {
    data: { prompts: [], nodes: [], viewport: { x: 0, y: 0, zoom: 1 } },
  });
  const initialJobsResponse = await request.get('/api/ai-image/generations');
  const initialJobs = await initialJobsResponse.json() as any[];
  let latestID = initialJobs.reduce((maximum, job) => Math.max(maximum, Number(job.id) || 0), 0);
  const publishRequests: string[] = [];
  page.on('request', (outgoing) => {
    if (/\/api\/(publish|jobs\/create)/.test(new URL(outgoing.url()).pathname)) publishRequests.push(outgoing.url());
  });

  await page.goto('/');
  await page.getByTestId('nav-ai-image').click();
  await page.getByTestId('ai-image-model-select').selectOption('text_to_image');
  await page.getByTestId('ai-image-prompt').fill('一件极简金属产品，干净棚拍背景，柔和侧光');
  await expect.poll(async () => (await (await request.get('/api/ai-image/generations')).json() as any[]).length).toBe(initialJobs.length);
  await page.getByTestId('ai-image-produce').click();
  const textImage = await waitForGeneration(request, 'text_to_image', latestID);
  latestID = textImage.id;
  expect(textImage.output_asset?.sha256).toMatch(/^[a-f0-9]{64}$/);
  await expectDecodableImage(page, textImage.output_asset.url);
  await expect(page.locator('[data-canvas-node-kind="image"]')).toHaveCount(1);

  await page.getByTestId('ai-media-file-input').setInputFiles({
    name: 'public-first-frame.png',
    mimeType: fixtureType,
    buffer: fixture,
  });
  await expect(page.locator('[data-canvas-node-kind="image"][data-selected="true"]')).toHaveCount(1);
  await page.getByTestId('ai-image-model-select').selectOption('image_to_image');
  await page.locator('[data-testid^="ai-reference-role-"]').selectOption('product');
  await page.getByTestId('ai-image-prompt').fill('保留参考主体，改为高端社媒产品摄影构图');
  await page.getByTestId('ai-image-produce').click();
  const imageImage = await waitForGeneration(request, 'image_to_image', latestID);
  latestID = imageImage.id;
  expect(imageImage.output_asset?.sha256).toMatch(/^[a-f0-9]{64}$/);
  expect(imageImage.output_asset.sha256).not.toBe(imageImage.references[0].sha256);
  await expectDecodableImage(page, imageImage.output_asset.url);
  await expect(page.locator('[data-canvas-node-kind="image"]')).toHaveCount(3);

  await page.getByTestId('ai-image-model-select').selectOption('image_to_video');
  await expect(page.getByTestId('ai-image-first-frame-url')).toHaveCount(0);
  await expect(page.getByText(/托管派生首帧/)).toBeVisible();
  await page.getByTestId('ai-image-prompt').fill('主体保持稳定，镜头缓慢推近，背景产生轻微自然运动');
  await page.getByTestId('ai-image-produce').click();
  const imageVideo = await waitForGeneration(request, 'image_to_video', latestID);
  expect(imageVideo.output_asset?.kind).toBe('video');
  expect(imageVideo.output_asset?.width).toBeGreaterThan(0);
  expect(imageVideo.output_asset?.height).toBeGreaterThan(0);
  expect(imageVideo.output_asset?.duration_ms).toBeGreaterThan(0);
  await expectPersistedMP4(page, imageVideo.output_asset.url);
  await expect(page.locator('[data-canvas-node-kind="video"]')).toHaveCount(1);
  expect(publishRequests).toEqual([]);
});
