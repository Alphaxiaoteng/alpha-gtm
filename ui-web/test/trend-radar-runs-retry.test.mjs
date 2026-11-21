import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const crawler = await readFile(new URL('../src/components/CrawlerView.tsx', import.meta.url), 'utf8');
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

test('runs page retries only failed calls after an explicit confirmation', () => {
  // 重试按钮只在有失败项、且这一轮已经结束时出现。
  assert.match(crawler, /重试失败项/);
  assert.match(crawler, /run\.state !== 'running' && run\.failed > 0/);
  // 与后端同一口径：按词取最后一次尝试，成功过的词不进重试清单。
  assert.match(crawler, /function pendingRunTerms\(run: TrendRadarRun\): string\[\]/);
  assert.match(crawler, /status !== 'succeeded'/);
  assert.match(crawler, /api\.retryTrendRadarRun\(run\.id\)/);
  assert.match(crawler, /window\.confirm/);
  assert.match(crawler, /只会重跑失败项，成功项不会再调用（不重复扣额度）/);
  assert.match(
    crawler,
    /已重试 \$\{result\.retried_terms\.length\} 项：补齐 \$\{result\.recovered_terms\.length\} 项；仍有 \$\{result\.pending_terms\.length\} 项失败/,
  );
});

test('run detail shows attempt numbers and the export batch id', () => {
  assert.match(crawler, /第 \{attempts\[index\] \?\? 1\} 次/);
  assert.match(crawler, /批次（导出「批次」列）：\{run\.id\}/);
  assert.match(crawler, /含重试 \$\{retriedCalls\} 次/);
  assert.match(crawler, /重试于 \$\{formatTime\(run\.last_retry_at\)\}/);
  assert.match(api, /\/api\/trend-radar\/runs\/\$\{encodeURIComponent\(runID\)\}\/retry/);
  assert.match(api, /body: JSON\.stringify\(\{ confirm: true \}\)/);
});
