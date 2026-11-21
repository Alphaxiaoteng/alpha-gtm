import assert from 'node:assert/strict';
import test from 'node:test';
import { duplicateAccountBindingIDs } from '../src/accountBindings.ts';

test('only later browser bindings of the same verified platform account are conflicts', () => {
  const duplicateIDs = duplicateAccountBindingIDs([
    { id: 1, identityId: 1, platformCode: 'douyin', accountIdentifier: 'DS911_', loginState: 'logged_in', enabled: true },
    { id: 13, identityId: 2, platformCode: 'douyin', accountIdentifier: ' DS911_ ', loginState: 'logged_in', enabled: true },
    { id: 14, identityId: 3, platformCode: 'douyin', accountIdentifier: 'DS911_', loginState: 'logged_in', enabled: true },
    { id: 15, identityId: 4, platformCode: 'douyin', accountIdentifier: 'another', loginState: 'logged_in', enabled: true },
    { id: 16, identityId: 5, platformCode: 'tiktok', accountIdentifier: 'DS911_', loginState: 'logged_in', enabled: true },
  ]);

  assert.deepEqual([...duplicateIDs], [13, 14]);
});

test('unverified, disabled and identifier-less accounts are not duplicate bindings', () => {
  const duplicateIDs = duplicateAccountBindingIDs([
    { id: 1, identityId: 1, platformCode: 'douyin', accountIdentifier: '', loginState: 'logged_in', enabled: true },
    { id: 2, identityId: 2, platformCode: 'douyin', accountIdentifier: 'same', loginState: 'unknown', enabled: true },
    { id: 3, identityId: 3, platformCode: 'douyin', accountIdentifier: 'same', loginState: 'logged_in', enabled: false },
  ]);

  assert.equal(duplicateIDs.size, 0);
});
