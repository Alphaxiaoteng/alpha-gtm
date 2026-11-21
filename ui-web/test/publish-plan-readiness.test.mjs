import assert from 'node:assert/strict';
import test from 'node:test';

import {
  canVerifyXHSCreatorBackend,
  canResumeEgoSpaceForExplicitPrepare,
  isEgoSpaceAvailableForExplicitPublish,
  isPublishJobReadyForSubmit,
  isPublishPlanExecutionReady,
} from '../src/publishPlanReadiness.ts';

test('a handed-off Ego Space must be explicitly returned before it becomes publish-ready', () => {
  const space = {
    id: 1,
    identity_id: 1,
    name: 'alpha-nexus/matrix-1',
    ownership: 'agentDelegatedToUser',
    state: 'manual_takeover',
    control_epoch: 1,
    active_job_id: 0,
    last_seen_at: '',
    error_code: '',
  };

  assert.equal(isEgoSpaceAvailableForExplicitPublish(space), false);
  assert.equal(canResumeEgoSpaceForExplicitPrepare(space), true);
  assert.equal(canResumeEgoSpaceForExplicitPrepare({ ...space, active_job_id: 91 }), false);
  assert.equal(canResumeEgoSpaceForExplicitPrepare({ ...space, error_code: 'CHECKPOINT_REQUIRED' }), false);
  assert.equal(isEgoSpaceAvailableForExplicitPublish({ ...space, ownership: 'agent', state: 'ready' }), true);
  assert.equal(isEgoSpaceAvailableForExplicitPublish({ ...space, ownership: 'agent', state: 'manual_takeover' }), false);
  assert.equal(isEgoSpaceAvailableForExplicitPublish({ ...space, ownership: 'agentDelegatedToUser', state: 'lost' }), false);
  assert.equal(canResumeEgoSpaceForExplicitPrepare({ ...space, ownership: 'agent', state: 'ready' }), false);
});

test('only a prepare job at the reviewed submit edge is ready', () => {
  assert.equal(isPublishJobReadyForSubmit({
    mode: 'prepare',
    status: 'awaiting_review',
    current_step: 'review_or_submit',
    submit_attempted: false,
  }), true);
  assert.equal(isPublishJobReadyForSubmit({
    mode: 'prepare',
    status: 'awaiting_review',
    current_step: 'select_target',
    submit_attempted: false,
  }), false);
  assert.equal(isPublishJobReadyForSubmit({
    mode: 'submit',
    status: 'awaiting_review',
    current_step: 'review_or_submit',
    submit_attempted: true,
  }), false);
});

test('plan execution readiness requires every target to reach the submit edge', () => {
  assert.equal(isPublishPlanExecutionReady({
    planId: 126,
    status: 'awaiting_review',
    trace: 'trace',
    targets: [{
      accountId: 9,
      platformCode: 'instagram',
      jobId: 242,
      status: 'awaiting_review',
      step: 'select_target',
      errorCode: 'TARGET_SELECTION_UNCONFIRMED',
    }],
  }), false);
  assert.equal(isPublishPlanExecutionReady({
    planId: 126,
    status: 'awaiting_review',
    trace: 'trace',
    targets: [{
      accountId: 9,
      platformCode: 'instagram',
      jobId: 243,
      status: 'awaiting_review',
      step: 'review_or_submit',
      errorCode: 'AWAITING_HUMAN',
    }],
  }), true);
});

test('only submitted XHS jobs awaiting evidence can run read-only backend verification', () => {
  const submittedXHS = {
    platform_code: 'xhs',
    mode: 'submit',
    status: 'awaiting_review',
    submit_attempted: true,
  };

  for (const status of ['awaiting_review', 'manual_takeover', 'failed']) {
    assert.equal(canVerifyXHSCreatorBackend({ ...submittedXHS, status }), true);
  }
  assert.equal(canVerifyXHSCreatorBackend({ ...submittedXHS, platform_code: 'douyin' }), false);
  assert.equal(canVerifyXHSCreatorBackend({ ...submittedXHS, mode: 'prepare' }), false);
  assert.equal(canVerifyXHSCreatorBackend({ ...submittedXHS, submit_attempted: false }), false);
  assert.equal(canVerifyXHSCreatorBackend({ ...submittedXHS, status: 'published' }), false);
});
