import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';

test('foundation exposes a real SQL Link workflow backed by Registry tools', async () => {
  const monitor = await readFile(new URL('../src/components/RuntimeMonitorView.tsx', import.meta.url), 'utf8');
  const view = await readFile(new URL('../src/components/SQLLinkView.tsx', import.meta.url), 'utf8');
  const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

  assert.match(monitor, /<SQLLinkView/);
  assert.match(view, /data-testid="sql-link-view"/);
  for (const stage of ['连接', '选表', '字段映射', '预检', '采集', '质量\/指标', '写入知识库']) {
    assert.match(view, new RegExp(stage));
  }
  assert.match(view, /accept="\.db,\.sqlite,\.sqlite3"/);
  assert.match(view, /只读 SELECT \/ CTE/);
  assert.match(view, /api\.uploadSQLiteSource/);
  assert.match(view, /api\.inspectDataLink/);
  assert.match(view, /api\.preflightDataLink/);
  assert.match(view, /api\.collectDataLink/);
  assert.match(view, /api\.confirmDataLinkReview/);
  assert.match(api, /data\.sql\.list/);
  assert.match(api, /data\.sql\.inspect/);
  assert.match(api, /data\.sql\.preflight/);
  assert.match(api, /data\.sql\.collect/);
  assert.match(api, /data\.sql\.review\.confirm/);
  assert.doesNotMatch(view, /setInterval|cron|schedule/i);
});
