import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';

test('creation workbench remains mounted while Alpha Agent becomes the main stage', async () => {
  const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
  const app = await readFile(new URL('../src/App.tsx', import.meta.url), 'utf8');
  const studio = await readFile(new URL('../src/components/StudioWorkspaceView.tsx', import.meta.url), 'utf8');
  const drawer = await readFile(new URL('../src/components/AgentDrawer.tsx', import.meta.url), 'utf8');

  assert.doesNotMatch(layout, /id: 'creation'|label: '创作工作台'|label: 'Alpha Agent'/);
  assert.doesNotMatch(layout, /data-testid="agent-conversation-sidebar"/);
  assert.match(layout, /data-testid="module-handoff-agent"/);
  assert.match(app, /case 'studio':\s*return <StudioWorkspaceView/);
  assert.doesNotMatch(app, /CreationWorkbenchView/);
  assert.match(studio, /data-testid="studio-workspace"/);
  assert.match(studio, /CreationWorkbenchView/);
  assert.match(drawer, /data-testid="agent-drawer"/);
  assert.match(app, /<AgentDrawer displayMode="stage" \/>/);
  assert.match(drawer, /data-testid="agent-context-suggestions"/);
  assert.doesNotMatch(drawer, /data-testid="agent-knowledge-folder-button"|data-testid="agent-trend-folder-button"/);
  assert.match(drawer, /data-testid="agent-skill-suggestions"/);
});
