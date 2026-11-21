import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const css = await readFile(new URL('../src/index.css', import.meta.url), 'utf8');
const layout = await readFile(new URL('../src/components/Layout.tsx', import.meta.url), 'utf8');

const blockAt = (source, marker) => {
  const start = source.indexOf(marker);
  assert.notEqual(start, -1, `missing ${marker}`);
  const brace = source.indexOf('{', start);
  let depth = 0;
  for (let index = brace; index < source.length; index += 1) {
    if (source[index] === '{') depth += 1;
    if (source[index] === '}') depth -= 1;
    if (depth === 0) return source.slice(brace + 1, index);
  }
  assert.fail(`unterminated ${marker}`);
};

test('desktop motion uses one lightweight token set', () => {
  assert.match(css, /--motion-fast:\s*120ms/);
  assert.match(css, /--motion-base:\s*180ms/);
  assert.match(css, /--motion-slow:\s*240ms/);
  assert.match(css, /--motion-ease-standard:/);
  assert.match(css, /--motion-ease-enter:/);
  assert.match(css, /@keyframes workspaceEnter/);
  assert.match(css, /\.ui-workspace-stage\s*\{[\s\S]*animation:\s*workspaceEnter/);
  assert.doesNotMatch(blockAt(css, '@keyframes workspaceEnter'), /(?:width|height|top|left|margin|padding):/);
});

test('workspace and navigation share the motion primitives', () => {
  assert.match(layout, /key=\{activeTab\}/);
  assert.match(layout, /ui-workspace-stage/);
  assert.match(layout, /ui-nav-item/);
  assert.doesNotMatch(layout, /transition-all/);
});

test('reduced motion removes decorative workspace animation', () => {
  const reducedMotion = blockAt(css, '@media (prefers-reduced-motion: reduce)');
  assert.match(reducedMotion, /\.ui-workspace-stage/);
  assert.match(reducedMotion, /animation:\s*none\s*!important/);
});
