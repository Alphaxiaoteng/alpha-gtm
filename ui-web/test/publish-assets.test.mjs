import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  buildMetaPublishGroups,
  isControlledPrepareAccountAsset,
  isPublishableAccountAsset,
} from '../src/publishAssets.ts';

test('Meta only exposes configured publish targets, not browser login containers', () => {
  assert.equal(isPublishableAccountAsset({ enabled: true, platform_code: 'facebook', asset_type: 'browser_account' }), false);
  assert.equal(isPublishableAccountAsset({ enabled: true, platform_code: 'facebook', asset_type: 'facebook_page' }), true);
  assert.equal(isPublishableAccountAsset({ enabled: true, platform_code: 'instagram', asset_type: 'instagram_account' }), true);
  assert.equal(isPublishableAccountAsset({ enabled: false, platform_code: 'xhs', asset_type: 'browser_account' }), false);
  assert.equal(isPublishableAccountAsset({ enabled: true, platform_code: 'xhs', asset_type: 'browser_account' }), true);
});

test('controlled filling offers browser sessions and fully identified Meta targets', () => {
  assert.equal(isControlledPrepareAccountAsset({ enabled: true, platform_code: 'csdn', asset_type: 'browser_account' }), true);
  assert.equal(isControlledPrepareAccountAsset({ enabled: true, platform_code: 'csdn', asset_type: 'csdn_column' }), false);
  assert.equal(isControlledPrepareAccountAsset({ enabled: false, platform_code: 'csdn', asset_type: 'browser_account' }), false);
  assert.equal(isControlledPrepareAccountAsset({ enabled: true, platform_code: 'facebook', asset_type: 'facebook_page', external_id: '' }), false);
  assert.equal(isControlledPrepareAccountAsset({ enabled: true, platform_code: 'facebook', asset_type: 'facebook_page', external_id: 'page-123' }), true);
  assert.equal(isControlledPrepareAccountAsset({ enabled: true, platform_code: 'instagram', asset_type: 'instagram_account', external_id: 'ig-456' }), true);
});

test('Meta publish selection groups verified Facebook and Instagram targets without inventing missing configuration', () => {
  const accounts = [
    { id: 10, platformCode: 'facebook', name: 'Alpha Page', avatar: '/fb.png', accountGroup: 'alpha', enabled: true, loginState: 'logged_in' },
    { id: 11, platformCode: 'instagram', name: 'Alpha IG', avatar: '/ig.png', accountGroup: 'alpha', enabled: true, loginState: 'unknown' },
  ];
  const assets = [
    { id: 101, account_id: 10, platform_code: 'facebook', asset_type: 'facebook_page', name: 'Alpha Page', external_id: 'page-1', enabled: true },
    { id: 102, account_id: 11, platform_code: 'instagram', asset_type: 'instagram_account', name: 'Alpha IG', external_id: '', enabled: true },
  ];

  const [group] = buildMetaPublishGroups(accounts, assets);
  assert.equal(group.name, 'Alpha Page');
  assert.deepEqual(group.accounts.map((account) => account.id), [10, 11]);
  assert.equal(group.targets.facebook.account.id, 10);
  assert.deepEqual(group.targets.facebook.assets.map((asset) => asset.id), [101]);
  assert.equal(group.targets.facebook.missingReason, '');
  assert.equal(group.targets.instagram.account.id, 11);
  assert.deepEqual(group.targets.instagram.assets, []);
  assert.equal(group.targets.instagram.missingReason, '缺少公开 ID');
});

test('Meta login-container account can own both real targets while absent targets remain explicitly unconfigured', () => {
  const account = { id: 20, platformCode: 'meta', name: 'Alpha Meta', avatar: '/meta.png', accountGroup: '', enabled: true, loginState: 'logged_in' };
  const facebook = { id: 201, account_id: 20, platform_code: 'facebook', asset_type: 'facebook_page', name: 'Alpha Page', external_id: 'page-2', enabled: true };

  const [configured] = buildMetaPublishGroups([account], [facebook]);
  assert.deepEqual(configured.targets.facebook.assets.map((asset) => asset.id), [201]);
  assert.equal(configured.targets.instagram.missingReason, '未配置发布目标');

  const [empty] = buildMetaPublishGroups([account], []);
  assert.equal(empty.targets.facebook.missingReason, '未配置发布目标');
  assert.equal(empty.targets.instagram.missingReason, '未配置发布目标');
});
