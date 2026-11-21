import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

const panelPath = '../src/components/BotDetailPanel.tsx';
const apiPath = '../src/api.ts';

test('Bot 详情高级页有可编辑的记忆区', async () => {
  const panel = await read(panelPath);

  assert.match(panel, /data-testid="bot-detail-memory"/);
  assert.match(panel, /data-testid="bot-memory-input"/);
  assert.match(panel, /data-testid="bot-memory-save"/);
  assert.match(panel, /<textarea/);
  assert.match(panel, /api\.getBotMemory\(botID\)/);
  assert.match(panel, /api\.updateBotMemory\(botID, value\)/);
  // 记忆区只在高级 tab 里出现
  assert.match(panel, /\{bot \? <BotMemorySection botID=\{bot\.id\} \/> : null\}/);
});

test('记忆 API 走 /api/agent-bots/{id}/memory', async () => {
  const api = await read(apiPath);

  assert.match(api, /getBotMemory: async \(id: string\)/);
  assert.match(
    api,
    /request<\{ memory\?: string \}>\(`\/api\/agent-bots\/\$\{encodeURIComponent\(id\)\}\/memory`\)/,
  );
  assert.match(api, /updateBotMemory: async \(id: string, content: string\)/);
  assert.match(api, /clearBotMemory: \(id: string\)/);
  assert.match(api, /method: 'DELETE'/);
});
