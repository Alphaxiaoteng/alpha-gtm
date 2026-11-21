import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const drawer = await readFile(new URL('../src/components/AgentDrawer.tsx', import.meta.url), 'utf8');
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

test('Codex Drawer exposes the Registry-only control boundary and tool lifecycle', () => {
  assert.match(api, /capabilities:\s*string\[\]/);
  assert.match(drawer, /providerID === 'codex'/);
  assert.match(drawer, /受控执行/);
  assert.doesNotMatch(drawer, /Codex CLI ·/);
  assert.match(drawer, /仅通过 Registry Skill/);
  assert.match(drawer, /不提供终端、源码写入或浏览器控制权限/);
  assert.match(drawer, /tool\.proposed/);
  assert.match(drawer, /approval\.required/);
  assert.match(drawer, /tool\.completed/);
  assert.match(drawer, /tool\.failed/);
  assert.match(drawer, /data-testid="agent-codex-tool-status"/);
});

test('Skill completion is catalog-driven, including one-click-publish when enabled', () => {
  assert.match(drawer, /api\.skillsList\(\)/);
  assert.match(drawer, /skill\.enabled && skill\.id\.startsWith\(skillToken\[1\]\)/);
  assert.match(drawer, /skill\.runtime === 'registry'/);
  assert.doesNotMatch(drawer, /skill\.id === 'one-click-publish'/);
});
