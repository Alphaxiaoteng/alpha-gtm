import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

test('the default dark game skin keeps the app background pure and untextured', async () => {
  const css = await readFile(new URL('../src/index.css', import.meta.url), 'utf8');
  const mario = css.match(/\[data-skin="mario"\]\s*\{([\s\S]*?)\n\}/)?.[1] || '';
  assert.match(mario, /--brand-bg:\s*#000000/);
  assert.match(mario, /--brand-sidebar:\s*#000000/);
  const sidebar = css.match(/\[data-skin="mario"\] \[data-testid="app-sidebar"\] \{([\s\S]*?)\n\}/)?.[1] || '';
  assert.match(sidebar, /background:\s*var\(--brand-bg\) !important/);
  assert.doesNotMatch(sidebar, /gradient/);
});
