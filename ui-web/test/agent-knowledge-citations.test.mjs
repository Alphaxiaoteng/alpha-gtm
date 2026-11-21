import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

test('Agent Drawer distinguishes retrieved knowledge from explicitly used citations', async () => {
  const drawer = await readFile(new URL('../src/components/AgentDrawer.tsx', import.meta.url), 'utf8');
  const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

  assert.match(api, /used_citations: KnowledgeCitation\[\]/);
  assert.match(drawer, /api\.agentHubKnowledgeTurns\(id\)/);
  assert.match(drawer, /已引用/);
  assert.match(drawer, /已检索/);
  assert.match(drawer, /未在回答中引用/);
});
