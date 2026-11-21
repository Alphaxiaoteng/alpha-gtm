import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

test('desktop shell extends the light surface below the hidden macOS title bar', async () => {
  const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
  assert.match(layout, /data-testid="app-shell"[\s\S]{0,180}bg-brand-bg pt-8/);
  assert.doesNotMatch(layout, /data-testid="agent-conversation-sidebar"/);
});
