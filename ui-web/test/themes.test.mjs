import assert from 'node:assert/strict';
import { Buffer } from 'node:buffer';
import { readFile } from 'node:fs/promises';
import { before, beforeEach, test } from 'node:test';
import ts from 'typescript';

let getStoredSkin;

class MemoryStorage {
  #items = new Map();
  getItem(key) { return this.#items.has(key) ? this.#items.get(key) : null; }
  setItem(key, value) { this.#items.set(String(key), String(value)); }
  removeItem(key) { this.#items.delete(key); }
  clear() { this.#items.clear(); }
}

before(async () => {
  const source = await readFile(new URL('../src/themes.ts', import.meta.url), 'utf8');
  const testableSource = source
    .replace(/^import type \{ FontId \} from '\.\/fonts';$/m, '')
    .replace(/^import \{ applyFont \} from '\.\/fonts';$/m, 'const applyFont = () => {};');
  const compiled = ts.transpileModule(testableSource, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const moduleURL = `data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`;
  ({ getStoredSkin } = await import(moduleURL));
});

beforeEach(() => {
  globalThis.localStorage = new MemoryStorage();
});

test('skin selection is respected across reloads', () => {
  localStorage.setItem('alpha-nexus-egolite-skin', 'mario');
  assert.equal(getStoredSkin(), 'mario');
  localStorage.setItem('alpha-nexus-egolite-skin', 'hermes');
  assert.equal(getStoredSkin(), 'hermes');
});

test('unknown and legacy values fall back to the light default', () => {
  localStorage.setItem('alpha-nexus-egolite-skin', 'neon');
  assert.equal(getStoredSkin(), 'pixel');

  localStorage.clear();
  localStorage.setItem('alpha-nexus-egolite-skin', 'not-a-skin');
  assert.equal(getStoredSkin(), 'claude');

  localStorage.clear();
  assert.equal(getStoredSkin(), 'claude');
});

test('skin v6 keeps every explicit user choice including light and dark', () => {
  localStorage.setItem('alpha-nexus-egolite-skin-rev', '5');
  localStorage.setItem('alpha-nexus-egolite-skin', 'hermes');
  assert.equal(getStoredSkin(), 'hermes');

  localStorage.setItem('alpha-nexus-egolite-skin', 'claude');
  assert.equal(getStoredSkin(), 'claude');

  localStorage.setItem('alpha-nexus-egolite-skin', 'alpha');
  assert.equal(getStoredSkin(), 'alpha');
});

test('dark Game CSS targets the live Agent DOM contract', async () => {
  const css = await readFile(new URL('../src/index.css', import.meta.url), 'utf8');
  const agent = await readFile(new URL('../src/components/AgentView.tsx', import.meta.url), 'utf8');
  assert.match(agent, /className="agent-studio claude-desk/);
  assert.match(agent, /agent-message-user/);
  assert.match(agent, /agent-send/);
  assert.match(css, /\[data-skin="mario"\] \.agent-studio \.agent-message-user/);
  assert.match(css, /button\[class~="bg-violet-600"\]/);
  assert.match(css, /button\[class~="bg-\[var\(--brand-accent\)\]"\]/);
  assert.match(css, /button\[class~="bg-red-600"\]/);
  assert.match(css, /--brand-success:\s*#15803d/);
  assert.doesNotMatch(css, /--color-emerald-500:\s*var\(--color-gray-500\)/);
});

test('desktop shell restores the saved skin without exposing a theme picker in settings', async () => {
  const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');
  const main = await readFile(new URL('../src/main.tsx', import.meta.url), 'utf8');
  const settings = await readFile(new URL('../src/components/SettingsView.tsx', import.meta.url), 'utf8');
  assert.match(layout, /getStoredSkin/);
  assert.match(layout, /alpha-nexus-egolite:skin-changed/);
  assert.match(main, /applySkin\(getStoredSkin\(\)\)/);
  assert.doesNotMatch(settings, /SKINS\.map/);
  assert.doesNotMatch(settings, /settings-skins/);
});
