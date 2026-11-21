import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

test('shop mode opens a real local operations workspace', async () => {
  const app = await read('../src/App.tsx');
  const layout = await read('../src/components/Layout.tsx');
  const mode = await read('../src/workspaceMode.ts');
  const store = await read('../src/store.ts');

  assert.match(app, /case 'shop':[\s\S]*<ShopWorkspaceView/);
  assert.match(layout, /id: 'shop'[\s\S]*label: '经营总览'/);
  assert.match(mode, /SHOP_TABS[^;]+shop/s);
  assert.match(store, /mode === 'shop' \? 'shop'/);
});

test('shop workspace renders persisted accounts, plans, and real local data-link metrics', async () => {
  const view = await read('../src/components/ShopWorkspaceView.tsx');
  const api = await read('../src/api.ts');
  const drawer = await read('../src/components/AgentDrawer.tsx');

  assert.match(view, /data-testid="shop-workspace"/);
  assert.match(view, /api\.getAccounts/);
  assert.match(view, /api\.getShopListingPlans/);
  assert.match(view, /api\.getDataLinkState/);
  assert.match(view, /api\.getShopContents/);
  assert.match(view, /api\.getShopKnowledgeBases/);
  assert.match(view, /isShopPlatform/);
  assert.match(view, /暂无真实经营数据|尚未接入订单与经营指标/);
  assert.match(view, /真实采集行数/);
  assert.match(view, /质量问题/);
  assert.match(view, /setAgentDraft/);
  assert.doesNotMatch(view, /mock|fake|Math\.random|setInterval/i);
  assert.match(api, /shop\.listing\.list/);
  assert.match(api, /ShopListingPlanSummary/);
  assert.match(drawer, /agentDraftSequenceRef/);
  assert.match(drawer, /agentDraftSequenceRef\.current === bootstrapDraftSequence/);
});

test('shop content and knowledge reuse the media UI but call isolated APIs', async () => {
  const content = await read('../src/components/LocalContentView.tsx');
  const knowledge = await read('../src/components/KnowledgeView.tsx');
  const api = await read('../src/api.ts');

  assert.match(content, /domain.*shop/s);
  assert.match(content, /getShopContents/);
  assert.match(content, /createShopContent/);
  assert.match(knowledge, /getShopKnowledgeBases/);
  assert.match(knowledge, /importShopKnowledge/);
  assert.match(knowledge, /searchShopKnowledge/);
  assert.match(api, /\/api\/shop\/contents/);
  assert.match(api, /\/api\/shop\/knowledge\/bases/);
});
