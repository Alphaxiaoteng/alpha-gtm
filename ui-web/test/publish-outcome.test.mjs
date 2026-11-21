import assert from 'node:assert/strict';
import { test } from 'node:test';
import { classifyPublishOutcome, describePublishIntent } from '../src/publishOutcome.ts';

test('历史人工状态不会被发布向导当作成功', () => {
  const outcome = classifyPublishOutcome({
    ok: true,
    message: 'publish finished',
    data: { status: 'manual_takeover', job_id: 42, message: '等待人工确认平台结果' },
  });

  assert.deepEqual(outcome, {
    kind: 'needs_attention',
    status: 'manual_takeover',
    jobID: 42,
    message: '历史任务等待核验（任务 #42）：等待人工确认平台结果',
  });
});

test('已完成与失败保留后端消息', () => {
  assert.equal(
    classifyPublishOutcome({ ok: true, data: { status: 'dry_run_ok', message: '演练已完成' } }).kind,
    'completed',
  );
  assert.deepEqual(
    classifyPublishOutcome({ ok: false, error: 'PUBLISH_BROWSER_CDP_UNAVAILABLE', data: { job_id: 3 } }),
    { kind: 'failed', jobID: 3, status: '', message: 'PUBLISH_BROWSER_CDP_UNAVAILABLE' },
  );
});

test('登录、待核验和未知结果不会退回人工接管文案', () => {
  assert.match(
    classifyPublishOutcome({ ok: false, data: { status: 'login_required', job_id: 8 } }).message,
    /平台需要重新登录/,
  );
  assert.match(
    classifyPublishOutcome({ ok: true, data: { status: 'submitted_pending_verification', job_id: 9 } }).message,
    /平台已提交，正在核验/,
  );
  const unknown = classifyPublishOutcome({ ok: false, data: { status: 'result_unknown', job_id: 10 } });
  assert.equal(unknown.kind, 'needs_attention');
  assert.match(unknown.message, /发布结果待后台核验/);
  assert.doesNotMatch(unknown.message, /人工接管/);
});

test('重放中的相同请求不会被误报为已完成', () => {
  assert.deepEqual(
    classifyPublishOutcome({ ok: true, data: { status: 'running', job_id: 7, message: '已恢复同一发布请求的已有任务' } }),
    { kind: 'needs_attention', status: 'running', jobID: 7, message: '相同请求仍在执行（任务 #7）：已恢复同一发布请求的已有任务' },
  );
});

test('发布任务意图明确区分演练、准备与自动提交', () => {
  assert.deepEqual(describePublishIntent('dry_run'), {
    label: '演练 · 未发布',
    description: '仅验证参数，不会创建正式发布记录。',
    tone: 'safe',
  });
  assert.deepEqual(describePublishIntent('prepare'), {
    label: '仅准备 · 不自动提交',
    description: '会进入创作后台并自动预填，不会自动点击发布。',
    tone: 'review',
  });
  assert.deepEqual(describePublishIntent('submit'), {
    label: '尝试自动提交',
    description: '仅在明确平台证据成立时提交；异常会记录、通知并继续其他平台。',
    tone: 'review',
  });
});
