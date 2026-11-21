import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const app = await readFile(new URL('../src/App.tsx', import.meta.url), 'utf8');
const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
const store = await readFile(new URL('../src/store.ts', import.meta.url), 'utf8');
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');
const drawer = await readFile(new URL('../src/components/AgentDrawer.tsx', import.meta.url), 'utf8');

test('Skill library lives inside Settings while legacy navigation remains compatible', async () => {
  const view = await readFile(new URL('../src/components/SkillLibraryView.tsx', import.meta.url), 'utf8');
  const settings = await readFile(new URL('../src/components/EgoSettingsView.tsx', import.meta.url), 'utf8');
  assert.match(store, /'skills'/);
  assert.doesNotMatch(layout, /id: 'skills'/);
  assert.match(layout, /matches: \['settings', 'proxies', 'skills', 'logs'\]/);
  assert.match(app, /case 'skills':/);
  assert.match(app, /<EgoSettingsView initialSection="skills" \/>/);
  assert.match(settings, /\{ id: 'skills', label: 'Skill 库'/);
  assert.match(settings, /data-testid=\{`settings-section-\$\{item\.id\}`\}/);
  assert.match(settings, /<SkillLibraryView embedded \/>/);
  assert.match(view, /embedded\?: boolean/);
  assert.match(view, /data-testid="skill-library"/);
  assert.match(view, /data-testid="skill-create"/);
  assert.match(view, /data-testid="skill-import"/);
  assert.match(view, /data-testid="skill-create-with-agent"/);
  assert.match(view, /skillSetEnabled/);
  assert.match(view, /内置 Skill 随应用启动自动更新/);
  assert.match(view, /自定义 Skill 仅保存在本机且永不覆盖/);
});

test('Agent uses slash for Skill invocation plus conversational install', () => {
  assert.match(api, /skillsList/);
  assert.match(api, /skillCreate/);
  assert.match(api, /skillImport/);
  assert.match(api, /skillSetEnabled/);
  assert.match(drawer, /data-testid="agent-skill-suggestions"/);
  assert.match(drawer, /skillImport/);
  assert.match(drawer, /\/\$\{skill\.id\}/);
  assert.doesNotMatch(drawer, /@\$\{skill\.id\}/);
});
