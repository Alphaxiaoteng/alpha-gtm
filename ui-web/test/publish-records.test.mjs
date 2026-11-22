import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

test('页面只保留内容记录与发布计划，不再有任务中心', async () => {
  const view = await readFile(new URL('../src/components/PublishRecordsView.tsx', import.meta.url), 'utf8');
  const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

  // 任务中心整块已按产品要求移除（它长期是空态，且与发布记录重复）。
  assert.doesNotMatch(view, /任务中心/);
  assert.doesNotMatch(view, /data-testid="publish-backend-verify"/);
  assert.doesNotMatch(view, /visibleJobs|selectedJobID/);
  // 页面仍是手动刷新，不引入后台轮询。
  assert.doesNotMatch(view, /refreshInterval|setInterval|streamJobEvents/);
  // 内容记录与发布计划两个区块保留。
  assert.match(view, /data-testid="publish-content-records"/);
  assert.match(view, /data-testid="publish-plans"/);
  // 后端取消/重试接口本身仍在（发布页会用），只是不在本页调用。
  assert.match(api, /`\/api\/jobs\/\$\{id\}\/cancel`/);
  assert.match(api, /`\/api\/jobs\/\$\{id\}\/retry`/);
});

test('核验接口仍然保留，但本页不再暴露后台核验入口', async () => {
  const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');
  const view = await readFile(new URL('../src/components/PublishRecordsView.tsx', import.meta.url), 'utf8');

  // 核验 API 仍在（发布链路要用），只是任务中心移除后本页没有调用入口。
  assert.match(api, /verifyPublishJobBackend/);
  assert.match(api, /`\/api\/publish-jobs\/\$\{id\}\/verify-published`/);
  assert.doesNotMatch(view, /verifyCreatorBackend|canVerifyXHSCreatorBackend/);
  // 本页绝不自行提交发布。
  assert.doesNotMatch(view, /submitPublishPlan/);
});

test('remote content cards keep schedule, publish, delete and failure states distinct', async () => {
  const view = await readFile(new URL('../src/components/PublishRecordsView.tsx', import.meta.url), 'utf8');
  const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

  assert.match(view, /发布内容记录/);
  assert.match(view, /排期成功/);
  assert.match(view, /发布成功/);
  assert.match(view, /删除失败/);
  assert.match(view, /item\.error_code/);
  assert.match(view, /function canDeleteRemoteContent/);
	for (const platform of ['douyin', 'youtube', 'tiktok', 'kuaishou', 'wechat_video', 'facebook']) {
		assert.match(view, new RegExp(`['"]${platform}['"]`));
	}
  assert.match(view, /item\.platform_code === 'kuaishou'/);
  assert.match(view, /item\.remote_state === 'published'/);
  assert.match(view, /item\.external_post_id\?\.trim\(\)/);
  assert.match(view, /canDeleteRemoteContent\(item\)/);
  assert.match(api, /confirmPermanentDelete: true/);
  assert.match(api, /\/api\/publish-content-records/);
});
