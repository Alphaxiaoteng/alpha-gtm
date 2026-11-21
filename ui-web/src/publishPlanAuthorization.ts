export type FrozenPublishPlanReference = {
  id: number;
  snapshotHash: string;
};

type PublishPlanApproval = { approvalId: string; nonce: string };

export type PublishPlanAuthorizationAPI<Result = unknown> = {
  approvePublishPlan: (id: number, snapshotHash: string, action: 'prepare' | 'submit') => Promise<PublishPlanApproval>;
  preparePublishPlan: (id: number, body: { snapshotHash: string; approvalId: string; nonce: string }) => Promise<Result>;
  submitPublishPlan: (id: number, body: { snapshotHash: string; approvalId: string; nonce: string }) => Promise<Result>;
};

export async function prepareFrozenPublishPlan<Result>(
  api: PublishPlanAuthorizationAPI<Result>,
  plan: FrozenPublishPlanReference,
): Promise<Result> {
  const approval = await api.approvePublishPlan(plan.id, plan.snapshotHash, 'prepare');
  return api.preparePublishPlan(plan.id, {
    snapshotHash: plan.snapshotHash,
    approvalId: approval.approvalId,
    nonce: approval.nonce,
  });
}

export async function submitPreparedPublishPlan<Result>(
  api: PublishPlanAuthorizationAPI<Result>,
  plan: FrozenPublishPlanReference,
): Promise<Result> {
  const approval = await api.approvePublishPlan(plan.id, plan.snapshotHash, 'submit');
  return api.submitPublishPlan(plan.id, {
    snapshotHash: plan.snapshotHash,
    approvalId: approval.approvalId,
    nonce: approval.nonce,
  });
}
