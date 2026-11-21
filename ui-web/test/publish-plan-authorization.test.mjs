import assert from 'node:assert/strict';
import test from 'node:test';

import {
  prepareFrozenPublishPlan,
  submitPreparedPublishPlan,
} from '../src/publishPlanAuthorization.ts';

function recordingAPI(calls) {
  return {
    async approvePublishPlan(id, snapshotHash, action) {
      calls.push(['approve', id, snapshotHash, action]);
      return { approvalId: `approval-${action}`, nonce: `nonce-${action}` };
    },
    async preparePublishPlan(id, body) {
      calls.push(['prepare', id, body]);
      return { planId: id, status: 'awaiting_review', targets: [] };
    },
    async submitPublishPlan(id, body) {
      calls.push(['submit', id, body]);
      return { planId: id, status: 'submitted_pending_verification', targets: [] };
    },
  };
}

test('preparing a frozen plan cannot also request final-submit approval or submit it', async () => {
  const calls = [];
  const result = await prepareFrozenPublishPlan(recordingAPI(calls), {
    id: 77,
    snapshotHash: 'f'.repeat(64),
  });

  assert.equal(result.status, 'awaiting_review');
  assert.deepEqual(calls, [
    ['approve', 77, 'f'.repeat(64), 'prepare'],
    ['prepare', 77, { snapshotHash: 'f'.repeat(64), approvalId: 'approval-prepare', nonce: 'nonce-prepare' }],
  ]);
});

test('final submit uses a distinct submit-bound approval in a later action', async () => {
  const calls = [];
  const result = await submitPreparedPublishPlan(recordingAPI(calls), {
    id: 77,
    snapshotHash: 'f'.repeat(64),
  });

  assert.equal(result.status, 'submitted_pending_verification');
  assert.deepEqual(calls, [
    ['approve', 77, 'f'.repeat(64), 'submit'],
    ['submit', 77, { snapshotHash: 'f'.repeat(64), approvalId: 'approval-submit', nonce: 'nonce-submit' }],
  ]);
});
