import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const accountsSource = await readFile(new URL('../src/components/AccountsView.tsx', import.meta.url), 'utf8');
const apiSource = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');

test('account workspace exposes normalized multi-tag management', () => {
  for (const method of ['getAccountTags', 'createAccountTag', 'reorderAccountTags', 'bulkUpdateAccountTags', 'resolveAccountTags']) {
    assert.match(apiSource, new RegExp(method));
  }
  assert.match(accountsSource, /账号标签管理/);
  assert.match(accountsSource, /追加（保留原标签）/);
  assert.match(accountsSource, /移除所选标签/);
  assert.match(accountsSource, /替换（空选时清空）/);
  assert.match(accountsSource, /account-tags-/);
	assert.match(accountsSource, /选择当前筛选/);
	assert.match(accountsSource, /clearSelection/);
});

test('account workspace exposes ordered all, tag, and unclassified filters', () => {
	assert.match(accountsSource, /account-tag-filters/);
	assert.match(accountsSource, /account-tag-filter-all/);
	assert.match(accountsSource, /account-tag-filter-unclassified/);
	assert.match(accountsSource, /account-tag-filter-\$\{tag\.id\}/);
	assert.match(accountsSource, /未分类/);
	assert.match(accountsSource, /moveAccountTag/);
	assert.match(accountsSource, /上移标签/);
	assert.match(accountsSource, /下移标签/);
});

test('account tags support direct card editing and filtered bulk selection', () => {
	assert.match(accountsSource, /account-tags-edit-\$\{acct\.id\}/);
	assert.match(accountsSource, /openSingleTagEditor/);
	assert.match(accountsSource, /account-batch-tags-entry/);
	assert.match(accountsSource, /account-batch-tag-selection/);
	assert.match(accountsSource, /select-filtered-accounts-for-tags/);
	assert.match(accountsSource, /选择当前筛选/);
	assert.match(accountsSource, /修改账号标签/);
	assert.match(accountsSource, /批量设置标签/);
	assert.match(accountsSource, /创建并选中/);
});

test('account workspace supports safe bulk-selection shortcuts', () => {
	assert.match(accountsSource, /e\.key === 'Escape'/);
	assert.match(accountsSource, /e\.key\.toLowerCase\(\) === 'a'/);
	assert.match(accountsSource, /e\.key\.toLowerCase\(\) === 'k'/);
	assert.match(accountsSource, /e\.key\.toLowerCase\(\) === 'r'/);
	assert.match(accountsSource, /Cmd\/Ctrl\+A respects the current search/);
});

test('browser workspace hides the platform filter while add-account keeps platform selection', () => {
	assert.doesNotMatch(accountsSource, /selectedPlatformFilter/);
	assert.doesNotMatch(accountsSource, /Platform Filter/);
	assert.match(accountsSource, /添加自媒体创作者账号/);
	assert.match(accountsSource, /setAddPlatform\(plat\.code\)/);
});
