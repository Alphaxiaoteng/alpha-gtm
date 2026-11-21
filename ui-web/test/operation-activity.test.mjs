import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';

test('Agent operation activity panel consumes only the safe timeline endpoint', async () => {
  const panel = await readFile(new URL('../src/components/OperationActivityPanel.tsx', import.meta.url), 'utf8');
  const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

  assert.match(api, /getOperationEvents/);
  assert.match(api, /\/api\/operations\/events/);
  assert.match(panel, /data-testid="operation-activity"/);
  assert.match(panel, /data-testid="operation-refresh"/);
  assert.doesNotMatch(panel, /refreshInterval|setInterval/);
  assert.match(panel, /不记录工具参数、完整网址、CDP 包、Cookie、凭证或截图/);
  assert.doesNotMatch(panel, /cdpUrl/);
  assert.doesNotMatch(panel, /screenshot_path/);
  assert.match(panel, /active\.length \?/);
  assert.match(panel, /events\?\.length \?/);
  assert.doesNotMatch(panel, /\.map\([\s\S]{0,500}\) \|\| <div/);
});

test('global shell keeps one Agent handoff and no duplicate activity controls', async () => {
  const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');

  assert.doesNotMatch(layout, /data-testid="operation-activity-toggle"/);
  assert.doesNotMatch(layout, /data-testid="content-calendar-toggle"/);
  assert.doesNotMatch(layout, /运行 Space:/);
  assert.match(layout, /data-testid="module-handoff-agent"/);
  assert.doesNotMatch(layout, /data-testid="agent-conversation-sidebar"/);
});
