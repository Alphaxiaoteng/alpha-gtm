import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { runInNewContext } from 'node:vm';
import test from 'node:test';
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';
import { duplicateAccountBindingIDs } from '../src/accountBindings.ts';
import { platformCodesForMode } from '../src/workspaceMode.ts';

const source = await readFile(new URL('../src/components/BrowserWorkspaceView.tsx', import.meta.url), 'utf8');
const require = createRequire(import.meta.url);
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, jsx: ts.JsxEmit.ReactJSX },
}).outputText;
const identities = [{ id: 1, name: '浏览器1' }, { id: 2, name: '浏览器2' }];
const account = { id: 1, identityId: 1, name: '原账号资料', platformCode: 'douyin', accountIdentifier: 'same-id', enabled: true, loginState: 'logged_in' };

function render(responses) {
  const module = { exports: {} };
  runInNewContext(compiled, {
    exports: module.exports,
    require(name) {
      if (name === 'swr') return (key) => ({ mutate: async () => {}, ...responses[key] });
      if (name === '../api') return { api: {} };
      if (name === '../store') return { useUIStore: (select) => select({ productMode: 'media', toast: { show() {} } }) };
      if (name === '../accountBindings') return { duplicateAccountBindingIDs };
      if (name === '../workspaceMode') return { platformCodesForMode };
      // 演示数据默认关闭：这些用例断言的是「空库如实显示为空」，所以恒定返回
      // 关闭，不引入任何演示行。
      if (name === '../lib/demoData') {
        return {
          DEMO_BADGE: '演示数据',
          DEMO_IDENTITIES: [],
          demoAccountsFor: () => [],
          isDemoModeEnabled: () => false,
        };
      }
      if (name.startsWith('./')) return { [name.slice(2)]: () => null };
      return require(name);
    },
  });
  return renderToStaticMarkup(createElement(module.exports.BrowserWorkspaceView));
}

test('missing or partially loaded account data never renders a false empty collection', () => {
  for (const responses of [{}, { 'api/ego/identities': { data: identities } }]) {
    const html = render(responses);
    assert.match(html, /正在读取账号记录/);
    assert.doesNotMatch(html, /还没有账号|为此浏览器添加平台|0\/0/);
    assert.match(html, /aria-label="添加账号"[^>]*disabled/);
  }
  assert.match(render({ 'api/ego/identities': { data: [] }, 'api/accounts': { data: [] } }), /还没有账号/);
});

test('request failure displays retry; stale account records remain visible and cannot be reconfigured', () => {
  const error = new Error('offline');
  const empty = render({ 'api/accounts': { error } });
  assert.match(empty, /账号记录暂时无法读取/);
  assert.match(empty, /重试/);
  assert.doesNotMatch(empty, /还没有账号/);
  const stale = render({ 'api/ego/identities': { data: identities }, 'api/accounts': { data: [account], error } });
  assert.match(stale, /原账号资料/);
  assert.match(stale, /保留上次读取的账号/);
  assert.match(stale, /aria-label="配置 浏览器1 平台"[^>]*disabled/);
});

test('duplicate identity bindings stay logged in and report their conflict separately', () => {
  const html = render({ 'api/ego/identities': { data: identities }, 'api/accounts': { data: [account, { ...account, id: 2, identityId: 2 }] } });
  assert.equal((html.match(/1\/1 已登录/g) ?? []).length, 2);
  assert.match(html, /重复绑定/);
  assert.doesNotMatch(html, /0\/1 已登录/);
});

test('an inconclusive check does not hide previously identified account details', () => {
  const html = render({ 'api/ego/identities': { data: identities }, 'api/accounts': { data: [{ ...account, loginState: 'unknown', loginReasonCode: 'PLATFORM_UNREACHABLE' }] } });
  assert.match(html, /原账号资料/);
  assert.match(html, /待确认/);
  assert.doesNotMatch(html, /登录后自动识别/);
});

test('platform editor freezes its full snapshot until explicitly reopened', () => {
  assert.doesNotMatch(source, /setSelectedPlatforms\(modeAccounts[\s\S]*?\}, \[identityID, modeAccounts\]\)/);
  assert.match(source, /setExpectedPlatformCodes/);
  assert.match(source, /expectedPlatformCodes\.filter/);
  assert.match(source, /api\.configureEgoIdentityPlatforms\(targetIdentityID, ordered, expectedPlatformCodes\)/);
  assert.match(source, /EGO_PLATFORM_CONFIGURATION_CONFLICT/);
});
