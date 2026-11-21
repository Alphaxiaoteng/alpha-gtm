import type { EgoSpace, PublishJobSummary, PublishPlanExecution } from './api';

const readySteps = new Set(['ready_to_submit', 'review_or_submit']);
const readyStatuses = new Set(['awaiting_review', 'ready_to_submit']);
const backendVerificationStatuses = new Set(['awaiting_review', 'manual_takeover', 'failed']);

export function isEgoSpaceAvailableForExplicitPublish(space: EgoSpace): boolean {
  if (space.active_job_id > 0 || space.error_code.trim() !== '') return false;
  return space.state === 'ready' && space.ownership === 'agent';
}

// A handoff made for login or manual review must be explicitly returned by the
// user before a publish prepare operation can touch that Space again. Keeping
// this separate from readiness prevents a selectable account from later
// failing with EGO_SPACE_USER_CONTROLLED.
export function canResumeEgoSpaceForExplicitPrepare(space: EgoSpace): boolean {
  return space.active_job_id <= 0
    && space.error_code.trim() === ''
    && space.state === 'manual_takeover'
    && (space.ownership === 'agentDelegatedToUser' || space.ownership === 'user');
}

export function isPublishJobReadyForSubmit(
  job: Pick<PublishJobSummary, 'mode' | 'status' | 'current_step' | 'submit_attempted'>,
): boolean {
  return job.mode === 'prepare'
    && !job.submit_attempted
    && readyStatuses.has(job.status)
    && readySteps.has(job.current_step);
}

export function isPublishPlanExecutionReady(
  execution: PublishPlanExecution | null | undefined,
): boolean {
  return Boolean(
    execution
    && readyStatuses.has(execution.status)
    && execution.targets.length > 0
    && execution.targets.every((target) =>
      readyStatuses.has(target.status) && readySteps.has(target.step)),
  );
}

export function canVerifyXHSCreatorBackend(
  job: Pick<PublishJobSummary, 'platform_code' | 'mode' | 'status' | 'submit_attempted'>,
): boolean {
  return job.platform_code === 'xhs'
    && job.mode === 'submit'
    && job.submit_attempted
    && backendVerificationStatuses.has(job.status);
}
