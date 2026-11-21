import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

test('Studio is a Media-only workspace with creation, assets, and editorial scheduling handoff', async () => {
  const layout = await read('../src/components/Layout.tsx');
  const store = await read('../src/store.ts');
  const mode = await read('../src/workspaceMode.ts');
  const app = await read('../src/App.tsx');
  const workspace = await read('../src/components/StudioWorkspaceView.tsx');

  assert.match(layout, /id: 'studio'/);
  assert.match(layout, /label: 'Studio'/);
  assert.match(layout, /workspace\.id !== 'shop' && workspace\.id !== 'monitor'/);
  assert.match(store, /'studio'/);
  assert.match(mode, /SHOP_TABS = new Set\([^)]*'shop'[^)]*\)/);
  assert.doesNotMatch(mode, /SHOP_TABS[^\n]*'studio'/);
  assert.match(app, /case 'studio':\s*return <StudioWorkspaceView/);
  assert.match(workspace, /CreationWorkbenchView/);
  assert.match(workspace, /StudioView/);
  assert.match(workspace, /排期交接/);
});

test('Studio schedule handoffs require a reviewed workflow, wall-clock time, and browser IANA timezone', async () => {
  const workspace = await read('../src/components/StudioWorkspaceView.tsx');
  const api = await read('../src/api.ts');

  assert.match(workspace, /workflow\.stage === 'review'/);
  assert.match(workspace, /type="datetime-local"/);
  assert.match(workspace, /Intl\.DateTimeFormat\(\)\.resolvedOptions\(\)\.timeZone/);
  assert.match(workspace, /timezone/);
  assert.match(workspace, /api\.createStudioScheduleHandoff/);
  assert.match(workspace, /api\.cancelStudioScheduleHandoff/);
  for (const status of ['ready', 'frozen', 'superseded', 'cancelled', 'expired']) {
    assert.match(workspace, new RegExp(status));
  }
  assert.doesNotMatch(workspace, /getAccounts|AccountAsset|approvePublishPlan|setInterval/);
  assert.match(api, /export interface StudioScheduleHandoff/);
  assert.match(api, /getStudioScheduleHandoffs/);
  assert.match(api, /createStudioScheduleHandoff/);
  assert.match(api, /getStudioScheduleHandoff/);
  assert.match(api, /cancelStudioScheduleHandoff/);
  assert.match(api, /freezePublishPlanHandoff/);
});

test('Media content exposes Studio provenance without copying a handoff into the content or Shop domains', async () => {
  const content = await read('../src/components/LocalContentView.tsx');

  assert.match(content, /domain === 'media'/);
  assert.match(content, /api\.getStudioScheduleHandoffs/);
  assert.match(content, /来自 Studio/);
  assert.match(content, /original_local_time/);
  assert.match(content, /timezone/);
  assert.match(content, /handoff\.status/);
  assert.doesNotMatch(content, /createStudioScheduleHandoff|updateContent\([^)]*handoff/);
});

test('one-click publish freezes ready Studio handoffs without recalculating or updating a content planned_at', async () => {
  const publish = await read('../src/components/PublishView.tsx');

  assert.match(publish, /api\.getStudioScheduleHandoffs/);
  assert.match(publish, /api\.freezePublishPlanHandoff/);
  assert.match(publish, /调整排期请返回 Studio/);
  assert.match(publish, /PENDING VERIFICATION/);
  assert.match(publish, /native_schedule_verified/);
  assert.match(publish, /handoffId/);
  assert.match(publish, /if \(selectedStudioHandoff\)[\s\S]{0,1200}freezePublishPlanHandoff[\s\S]{0,900}return;/);
  assert.doesNotMatch(publish, /freezePublishPlanHandoff[\s\S]{0,500}updateContent\(/);
  assert.doesNotMatch(publish, /freezePublishPlanHandoff[\s\S]{0,500}nextNativePublishAt/);
});

test('a stale selected Studio handoff fails closed before the legacy publish path', async () => {
  const publish = await read('../src/components/PublishView.tsx');
  const staleGuard = publish.match(/if \(studioHandoffSelectionInvalid\) \{([\s\S]*?)\n    \}\n    const validation = validateContent/);

  assert.match(publish, /const studioHandoffSelectionInvalid = handoffId > 0 && !selectedStudioHandoff/);
  assert.match(publish, /if \(studioHandoffSelectionInvalid\) \{[\s\S]{0,240}Studio 交接已失效[\s\S]{0,160}return;[\s\S]{0,180}const validation = validateContent/);
  assert.ok(staleGuard, 'the stale Studio handoff guard must return before legacy validation');
  assert.doesNotMatch(staleGuard[1], /updateContent|nextNativePublishAt|createPublishPlan/);
  assert.match(publish, /data-testid="publish-studio-handoff-invalid"/);
  assert.match(publish, /不会改写原内容或重新计算排期/);
});

test('a selected Studio handoff has a source-visible read-only path with no content override or template save escape hatch', async () => {
  const publish = await read('../src/components/PublishView.tsx');
  const readOnlyCopy = publish.match(/const studioHandoffCopy = selectedStudioHandoff \? \(([\s\S]*?)\n\s*\) : null;/);
  const editableOverrides = publish.match(/const editablePlatformOverrides = !selectedStudioHandoff \? \(([\s\S]*?)\n\s*\) : null;/);
  const freezeFunction = publish.match(/const saveContentAndFreeze = async \(\) => \{([\s\S]*?)\n\s*const prepareFrozenPlan = async/);
  const handoffFreeze = freezeFunction?.[1].match(/if \(selectedStudioHandoff\) \{([\s\S]*?)\n\s*\}\n\s*const selected = selectedContent/);

  assert.ok(readOnlyCopy, 'Studio handoffs need an explicit read-only copy branch');
  assert.ok(editableOverrides, 'legacy content overrides must stay in their own non-handoff branch');
  assert.ok(freezeFunction, 'the freeze function must remain source-visible to inspect its handoff branch');
  assert.ok(handoffFreeze, 'Studio handoffs need a dedicated freeze branch');
  assert.match(readOnlyCopy[1], /data-testid=\{`publish-studio-handoff-copy-\$\{code\}`\}/);
  assert.match(readOnlyCopy[1], /平台文案已从 Studio 冻结/);
  assert.match(readOnlyCopy[1], /返回 Studio 修改并创建新的交接/);
  assert.doesNotMatch(readOnlyCopy[1], /publish-platform-(?:title|tags|body)|publish-suggest-tags|updateOverride|saveDefaultTemplate/);
  assert.match(editableOverrides[1], /publish-platform-title-\$\{code\}/);
  assert.match(editableOverrides[1], /publish-platform-body-\$\{code\}/);
  assert.match(editableOverrides[1], /updateOverride/);
  assert.match(publish, /allowTemplateSave=\{!selectedStudioHandoff\}/);
  assert.match(publish, /allowTemplateSave \? <div/);
  assert.doesNotMatch(handoffFreeze[1], /content\.variant\.upsert|variant_id|platformOverrides|api\.updateContent|api\.createPublishPlan/);
  assert.match(handoffFreeze[1], /api\.freezePublishPlanHandoff/);
  assert.match(handoffFreeze[1], /handoffId: selectedStudioHandoff\.id/);
  assert.match(handoffFreeze[1], /concurrency: 1/);
  assert.match(handoffFreeze[1], /targets: selectedAssets\.map/);
});
