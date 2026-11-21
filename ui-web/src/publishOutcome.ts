export type PublishOutcomeKind = 'completed' | 'needs_attention' | 'failed';

export interface PublishOutcome {
  kind: PublishOutcomeKind;
  message: string;
  jobID?: number;
  status?: string;
}

export interface PublishIntent {
  label: string;
  description: string;
  tone: 'safe' | 'review' | 'unknown';
}

interface ToolResultLike {
  ok: boolean;
  message?: string;
  error?: string;
  data?: Record<string, unknown>;
}

const needsAttentionStatuses = new Set([
  'manual_takeover',
  'awaiting_review',
  'login_required',
  'submitted_pending_verification',
  'result_unknown',
]);
const inProgressStatuses = new Set(['pending', 'running']);

function stringField(value: unknown) {
  return typeof value === 'string' ? value.trim() : '';
}

function positiveInteger(value: unknown) {
  return typeof value === 'number' && Number.isInteger(value) && value > 0 ? value : undefined;
}

// mode 在创建任务时写死；在任务页同时显示它，避免 dry_run 的 success 被误看成已发布。
export function describePublishIntent(mode: string): PublishIntent {
  switch (mode) {
    case 'dry_run':
      return { label: '演练 · 未发布', description: '仅验证参数，不会创建正式发布记录。', tone: 'safe' };
    case 'submit':
    case 'auto_submit':
      return { label: '尝试自动提交', description: '仅在明确平台证据成立时提交；异常会记录、通知并继续其他平台。', tone: 'review' };
    case 'prepare':
    case 'browser':
    case 'api':
    case 'hybrid':
      return { label: '仅准备 · 不自动提交', description: '会进入创作后台并自动预填，不会自动点击发布。', tone: 'review' };
    default:
      return { label: '未知执行意图', description: '旧任务缺少可识别意图，请先审查事件与证据。', tone: 'unknown' };
  }
}

export function classifyPublishOutcome(result: ToolResultLike): PublishOutcome {
  const data = result.data ?? {};
  const status = stringField(data.status);
  const jobID = positiveInteger(data.job_id);
  const detail = stringField(data.message) || stringField(result.message) || stringField(result.error);

  if (needsAttentionStatuses.has(status)) {
    const prefix = status === 'awaiting_review'
      ? '任务等待最终提交确认'
      : status === 'login_required'
        ? '平台需要重新登录'
        : status === 'submitted_pending_verification'
          ? '平台已提交，正在核验'
          : status === 'result_unknown'
            ? '发布结果待后台核验'
            : '历史任务等待核验';
    return {
      kind: 'needs_attention',
      status,
      jobID,
      message: `${prefix}${jobID ? `（任务 #${jobID}）` : ''}${detail ? `：${detail}` : ''}`,
    };
  }

  if (inProgressStatuses.has(status)) {
    return {
      kind: 'needs_attention',
      status,
      jobID,
      message: `相同请求仍在执行${jobID ? `（任务 #${jobID}）` : ''}${detail ? `：${detail}` : ''}`,
    };
  }

  if (result.ok) {
    return {
      kind: 'completed',
      status,
      jobID,
      message: detail || (status === 'dry_run_ok' ? '演练完成' : '发布任务已完成'),
    };
  }

  return {
    kind: 'failed',
    status,
    jobID,
    message: detail || '发布任务未完成',
  };
}
