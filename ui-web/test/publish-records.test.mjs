import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

test('task center stays manual-refresh and follows backend action capabilities', async () => {
  const view = await readFile(new URL('../src/components/PublishRecordsView.tsx', import.meta.url), 'utf8');
  const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

  assert.doesNotMatch(view, /refreshInterval|setInterval|setTimeout|streamJobEvents/);
  assert.match(view, /job\.can_cancel/);
  assert.match(view, /job\.can_retry/);
  assert.match(api, /`\/api\/jobs\/\$\{id\}\/cancel`/);
  assert.match(api, /`\/api\/jobs\/\$\{id\}\/retry`/);
  assert.doesNotMatch(view, /\/resume/);
  assert.match(view, /useEffect\(\(\) => \{/);
  assert.match(view, /setSelectedJobID\(visibleJobs\[0\]\.id\)/);
});

test('submitted XHS jobs expose a read-only creator-backend verification action', async () => {
  const view = await readFile(new URL('../src/components/PublishRecordsView.tsx', import.meta.url), 'utf8');
  const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

  assert.match(api, /verifyPublishJobBackend/);
  assert.match(api, /`\/api\/publish-jobs\/\$\{id\}\/verify-published`/);
  assert.match(view, /canVerifyXHSCreatorBackend\(job\)/);
  assert.match(view, /data-testid="publish-backend-verify"/);
  assert.match(view, /仅核验后台（不会再次发布）/);
  assert.match(view, /data-testid="publish-backend-verification-result"/);
  assert.match(view, />状态</);
  assert.match(view, />阶段</);
  assert.match(view, />错误码</);
  assert.match(view, /api\.verifyPublishJobBackend\(selectedJobID\)/);
  assert.doesNotMatch(view, /submitPublishPlan/);
  const verifyHandler = view.match(/const verifyCreatorBackend = async \(\) => \{[\s\S]*?\n  \};/u)?.[0] ?? '';
  assert.match(verifyHandler, /api\.verifyPublishJobBackend/);
  assert.match(verifyHandler, /Promise\.allSettled/);
  assert.doesNotMatch(verifyHandler, /retryJob|submitPublishPlan|runAction\s*\(\s*['"]retry/u);
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
