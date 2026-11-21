import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { test } from 'node:test';

const components = [
  'OperationActivityPanel.tsx',
  'BatchView.tsx',
  'MetricsView.tsx',
  'NotificationsView.tsx',
];

test('non-account workspace views only poll the lightweight backend health endpoint', async () => {
  for (const name of components) {
    const source = await readFile(new URL(`../src/components/${name}`, import.meta.url), 'utf8');
    assert.doesNotMatch(source, /refreshInterval/);
  }
  const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
  assert.match(layout, /'healthz'/);
  assert.equal(layout.match(/refreshInterval/g)?.length, 1);
  assert.match(layout, /useSWR\('api\/overview', api\.getOverview\)/);

  const batch = await readFile(new URL('../src/components/BatchView.tsx', import.meta.url), 'utf8');
  assert.doesNotMatch(batch, /streamJobEvents|setTimeout/);

  const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');
  assert.doesNotMatch(api, /streamJobEvents/);
});

test('account workspace only polls persisted passive observer state', async () => {
  const accounts = await readFile(new URL('../src/components/AccountsView.tsx', import.meta.url), 'utf8');
  assert.match(accounts, /getAccountObserverStatus/);
  assert.match(accounts, /refreshInterval/);
  assert.doesNotMatch(accounts, /setInterval|setTimeout|publishAccount|createPublishJob/);
});
