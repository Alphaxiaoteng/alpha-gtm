import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const loading = await readFile(new URL('../src/components/LocalLoadingState.tsx', import.meta.url), 'utf8').catch(() => '');
const app = await readFile(new URL('../src/App.tsx', import.meta.url), 'utf8');
const agent = await readFile(new URL('../src/components/AgentDrawer.tsx', import.meta.url), 'utf8');
const skills = await readFile(new URL('../src/components/SkillLibraryView.tsx', import.meta.url), 'utf8');
const contents = await readFile(new URL('../src/components/LocalContentView.tsx', import.meta.url), 'utf8');
const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const settings = await readFile(new URL('../src/components/SettingsView.tsx', import.meta.url), 'utf8');
const css = await readFile(new URL('../src/index.css', import.meta.url), 'utf8');
const html = await readFile(new URL('../index.html', import.meta.url), 'utf8');

test('shared local loading state has a visual mark and reduced-motion support', () => {
  assert.match(loading, /data-testid=\{testID \|\| 'local-loading-state'\}/);
  assert.match(loading, /local-loading-mark/);
  assert.match(loading, /role="status"/);
  assert.match(loading, /aria-busy="true"/);
  assert.match(css, /@keyframes localLoadingConverge/);
  assert.match(css, /prefers-reduced-motion/);
  assert.match(html, /pre-react-loading-mark/);
  assert.match(html, /preReactConverge/);
});

test('startup, Agent, Skill and content workspace use the shared loading state', () => {
  assert.match(app, /LocalLoadingState/);
  assert.match(agent, /agent-bootstrap-loading/);
  assert.match(agent, /LocalLoadingState/);
  assert.match(skills, /LocalLoadingState/);
  assert.match(contents, /LocalLoadingState/);
  assert.match(layout, /header-local-loading/);
  assert.match(layout, /api\.getBackendHealth/);
  assert.match(layout, /backendHealth\?\.ready/);
  assert.doesNotMatch(layout, /error \? 'bg-red-500' : overview \? 'bg-emerald-500'/);
  assert.match(settings, /正在读取 Agent 接入/);
});
