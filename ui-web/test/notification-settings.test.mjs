import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const settings = await readFile(new URL('../src/components/SettingsView.tsx', import.meta.url), 'utf8');
const api = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

test('settings keeps account notifications compact and human readable', () => {
  assert.match(settings, /data-testid="settings-notifications"/);
  assert.match(settings, /账号掉线立即提醒；一直未恢复时，每天提醒 1 或 2 次；重新登录后也会通知/);
  assert.match(settings, /每 12 小时（每天最多 2 次）/);
  assert.match(settings, /每 24 小时（每天最多 1 次）/);

  for (const kind of ['feishu', 'dingtalk', 'webhook', 'email']) {
    assert.match(settings, new RegExp(`${kind}: \\{`));
    assert.match(settings, new RegExp(`notification-channel-\\$\\{setting\\.kind\\}`));
  }

  assert.match(settings, /type="password"/);
  assert.match(settings, /保存后不回显/);
  assert.doesNotMatch(settings, /open-apis\/bot\/v2\/hook\/[a-zA-Z0-9-]{8,}/);
});

test('notification API never needs a credential on read and separates save from test', () => {
  assert.match(api, /getNotificationSettings:[\s\S]*\/api\/notification-settings/);
  assert.match(api, /updateNotificationPolicy:[\s\S]*reminder_interval_hours/);
  assert.match(api, /updateNotificationChannel:[\s\S]*notification-settings\/\$\{kind\}/);
  assert.match(api, /testNotificationChannel:[\s\S]*notification-settings\/\$\{kind\}\/test/);
  assert.match(api, /webhook_url\?: string/);
  assert.match(api, /password\?: string/);
});
