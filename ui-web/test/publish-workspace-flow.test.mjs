import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';

const rules = await import('../src/platformContentRules.ts');

test('publish workspace keeps the primary flow to three explicit steps', async () => {
  const workspace = await readFile(new URL('../src/components/PublishWorkspaceView.tsx', import.meta.url), 'utf8');
  const publish = await readFile(new URL('../src/components/PublishView.tsx', import.meta.url), 'utf8');
  const apiSource = await readFile(new URL('../src/api.ts', import.meta.url), 'utf8');
  const authorization = await readFile(new URL('../src/publishPlanAuthorization.ts', import.meta.url), 'utf8');

  assert.match(workspace, /<PublishView embedded/);
  assert.match(workspace, /<header/);
  assert.match(workspace, /data-testid="publish-records-entry"/);
  assert.match(workspace, /setActiveTab\('records'\)/);
  assert.match(workspace, /发布记录/);
  // 2026-09 改版后标题为"发布状态统计与分发"，串行承诺改为"严格隔离"表述。
  assert.match(workspace, /预填、人工核对与记录追踪严格隔离/);
  assert.match(workspace, /平台异常会通知并继续其余目标/);
  assert.match(workspace, /api\.getEgoPublishManifests/);
  assert.match(workspace, /api\.getPublishRecords/);
	assert.match(workspace, /capability\.prepare_mode === 'automated' && capability\.prepare_adapter_supported/);
	assert.match(publish, /capability\.prepare_mode === 'automated' && capability\.prepare_adapter_supported/);
	assert.match(workspace, /capability\.submit_adapter_supported && capability\.verify_adapter_supported/);
	assert.match(workspace, /本地提交\+核验适配 \{localSubmitVerifyAdapters\} 项/);
	assert.match(workspace, /首版 \{availablePlatforms\} 个平台 \/ \{capabilityCount\} 种形态/);
  assert.match(workspace, /new Set\(publishRecords\.map\(\(record\) => record\.platform_code\)\)\.size/);
  assert.match(workspace, /真正发布成功 \{publishRecords\.length\} 条 \/ \{publishedPlatforms\} 个平台/);
  assert.doesNotMatch(workspace, /ContentView|StudioView|内容库与高级工具/);
  assert.doesNotMatch(workspace, /1 准备内容与素材|2 选择账号并发布/);
  assert.doesNotMatch(publish, /AlphaGamerLaunchMatrix|deriveAlphaGamerLaunchRows|alpha-gamer-launch-matrix/);
  assert.doesNotMatch(publish, /aria-label="本次发布计划"|冻结前可继续调整/);
  assert.match(publish, /api\.getAccounts, \{ refreshInterval: 5_000, refreshWhenHidden: false \}/);
  assert.match(publish, /api\.getAccountAssets, \{ refreshInterval: 5_000, refreshWhenHidden: false \}/);
  assert.match(publish, /api\.getEgoSpaces, \{ refreshInterval: 5_000, refreshWhenHidden: false \}/);
  assert.match(workspace, /api\.getPublishRecords, \{ refreshInterval: 5_000, refreshWhenHidden: false \}/);
  assert.match(apiSource, /getAccountAssets: \(\) => request<AccountAsset\[]>\('\/api\/account-assets', \{ cache: 'no-store' \}\)/);
  assert.match(apiSource, /getEgoSpaces: \(\) => request<EgoSpace\[]>\('\/api\/ego\/spaces', \{ cache: 'no-store' \}\)/);
  assert.match(apiSource, /getPublishRecords: \(\) => request<PublishRecord\[]>\('\/api\/publish-records', \{ cache: 'no-store' \}\)/);

  for (const testID of [
    'publish-step-accounts',
    'publish-step-content',
    'publish-step-review',
    'publish-family-video',
    'publish-family-graph-text',
    'publish-family-article',
    'publish-next-content',
    'publish-content-list',
    'publish-content-summary',
    'publish-next-review',
    'publish-plan-prepare',
    'publish-plan-submit',
  ]) assert.match(publish, new RegExp(testID));

  assert.match(publish, /label: '选账号'/);
  assert.match(publish, /label: '填内容'/);
  assert.match(publish, /label: '发布'/);
  assert.match(publish, /选账号 → 冻结内容 → EgoLite 打开对应创作页/);
  assert.match(publish, /type PublishFamily = 'video' \| 'graph_text' \| 'article'/);
  assert.match(publish, /视频、图文和文章分别准备/);
  assert.match(publish, /api\.getAccounts/);
  assert.match(publish, /api\.getEgoSpaces/);
  assert.match(publish, /accountsByID\.get\(asset\.account_id\)\?\.enabled !== false/);
  assert.match(publish, /isEgoSpaceAvailableForExplicitPublish/);
  assert.match(publish, /account\.enabled && account\.loginState === 'logged_in' && availableIdentityIDs\.has\(account\.identityId\)/);
  assert.doesNotMatch(publish, /account\.loginReasonCode\?\.startsWith\('VERIFIED_'\)/);
  assert.match(publish, /if \(!assets \|\| accounts === undefined\) return/);
  assert.doesNotMatch(publish, /BROWSER_OPERATION_BUSY/);
  assert.doesNotMatch(publish, /api\.startBrowser/);
  assert.doesNotMatch(publish, /recoverAccount/);
  assert.doesNotMatch(publish, /浏览器已打开|浏览器已关闭|启动并检测|重新检测/);
  assert.match(publish, /❗ Ego Space 未就绪，暂不可发布/);
  assert.match(publish, /disabled=\{!ready\}/);
  assert.doesNotMatch(publish, /❗ 当前测试暂不可发布/);
  assert.doesNotMatch(publish, /metaPublishingPaused/);
  assert.doesNotMatch(publish, /Meta 暂停发布，暂不可勾选/);
  assert.match(publish, /nonMetaControlledFamilyAssets\.map/);
  assert.match(publish, /publish-account-tag-picker/);
  assert.match(publish, /publish-select-by-tags/);
  assert.match(publish, /resolveAccountTags/);
  assert.match(publish, /accountsByID/);
  assert.match(publish, /publish-account-avatar-\$\{asset\.account_id\}/);
  assert.match(publish, /的账号头像/);
  assert.match(publish, /aria-pressed=\{selected\}/);
  assert.match(publish, /ContentCover/);
  assert.match(publish, /focus-visible:outline-brand-accent/);
  assert.doesNotMatch(publish, /<select[^>]*publish-content/);
  assert.match(publish, /api\.updateContent/);
  assert.match(publish, /24 \* 60 \* 60 \* 1000/);
  assert.doesNotMatch(publish, /publish-media-select|新建内容|公共内容|单独修改内容/);
  assert.doesNotMatch(publish, /演练模式|尝试自动提交/);
  assert.match(authorization, /approvePublishPlan\(plan\.id, plan\.snapshotHash, 'prepare'\)/);
  assert.match(authorization, /api\.preparePublishPlan/);
  assert.doesNotMatch(publish, /prepareExecution\.status === 'awaiting_review'/);
  assert.match(publish, /hasPreparedPublishTarget\(prepareExecution\)/);
  assert.match(publish, /execution\?\.targets\.some/);
  assert.match(publish, /if \(!readyForSubmit\)/);
  assert.match(authorization, /approvePublishPlan\(plan\.id, plan\.snapshotHash, 'submit'\)/);
  assert.match(authorization, /api\.submitPublishPlan/);
  assert.match(publish, /execution\.status === 'submitted_pending_verification'/);
  assert.match(publish, /execution\.status === 'result_unknown'/);
  assert.match(publish, /已点击一次，但结果证据不足；已禁止自动重试/);
  assert.match(publish, /prepareFrozenPlan/);
  assert.match(publish, /submitPreparedPlan/);
  assert.doesNotMatch(publish, /submitFrozenPlan|liveSubmitConfirmed/);
  assert.match(publish, /确认并提交一次/);
  assert.doesNotMatch(publish, /填入已选平台（不会发布）/);
  assert.doesNotMatch(publish, /PUBLISH_BROWSER_CLOSE_FAILED/);
  assert.doesNotMatch(publish, /本次临时打开的账号浏览器随后自动关闭|原本已打开的浏览器不关闭/);
});

test('publish workspace counts first-release capability contracts from the manifest', async () => {
  const workspace = await readFile(new URL('../src/components/PublishWorkspaceView.tsx', import.meta.url), 'utf8');

	assert.match(workspace, /manifest\.first_release/);
	assert.match(workspace, /manifest\.capabilities\.length/);
	assert.match(workspace, /capability\.submit_verified && capability\.verify_verified/);
	assert.match(workspace, /首版 \{availablePlatforms\} 个平台 \/ \{capabilityCount\} 种形态/);
	assert.doesNotMatch(workspace, /manifests\.length/);
});

test('Media freeze plans always use serial concurrency', async () => {
  const publish = await readFile(new URL('../src/components/PublishView.tsx', import.meta.url), 'utf8');

  assert.doesNotMatch(publish, /concurrency:\s*Math\.min\(selectedAssets\.length,\s*3\)/);
  assert.match(publish, /concurrency:\s*1/);
});

test('publish records keep plan audit history separate from verified success', async () => {
  const records = await readFile(new URL('../src/components/PublishRecordsView.tsx', import.meta.url), 'utf8');

  assert.match(records, /api\.getPublishPlans/);
  assert.match(records, /data-testid="publish-plans"/);
  assert.match(records, /计划、平台异常与历史状态都是审计轨迹/);
  assert.match(records, /真正发布成功仍以“已确认发布”记录为准/);
  assert.match(records, /frozen: '已冻结未执行'/);
  assert.match(records, /manual_takeover: '历史人工状态'/);
  assert.doesNotMatch(records, /frozen: '已完成'|manual_takeover: '已完成'/);
  assert.match(records, /mutatePlans\(\)/);
});

test('content loading and account changes cannot masquerade as a ready frozen plan', async () => {
  const publish = await readFile(new URL('../src/components/PublishView.tsx', import.meta.url), 'utf8');

  assert.match(publish, /const mediaLoading = Boolean\(selectedContent\) && attachedMedia === undefined && !attachedMediaError/);
  assert.match(publish, /publish-media-loading/);
  assert.match(publish, /正在读取关联素材/);
  assert.match(publish, /publish-media-error/);
  assert.match(publish, /媒体读取失败|素材读取失败/);
  assert.match(publish, /previousSelectedAssetKey/);
  assert.match(publish, /账号选择已变化，请重新确认后再填入平台/);
  assert.match(publish, /setStep\('accounts'\)/);
});

test('platform content uses one switchable platform page with account avatars and native field names', async () => {
  const publish = await readFile(new URL('../src/components/PublishView.tsx', import.meta.url), 'utf8');

  assert.match(publish, /content\.variant\.upsert/);
  assert.match(publish, /variant_id:/);
  assert.match(publish, /platformOverrides/);
  assert.match(publish, /activePlatformCode/);
  assert.match(publish, /role="tablist"/);
  assert.match(publish, /publish-platform-tab-\$\{code\}/);
  assert.match(publish, /publish-platform-tab-avatar-\$\{account\.id\}/);
  assert.match(publish, /selectedPlatformCodes\.map/);
  assert.match(publish, /const activeCode = activePlatformCode/);
  assert.doesNotMatch(publish, /selectedPlatformCodes\.map\(\(code\) => \{\s*const override[\s\S]*publish-platform-content-\$\{code\}/);
  assert.match(publish, /publish-platform-title-/);
  assert.match(publish, /publish-platform-body-/);
	assert.match(publish, /publish-platform-tags-/);
	assert.match(publish, /publish-suggest-tags-/);
	assert.match(publish, /suggestPublishTags\(override\.title, override\.body\)/);
	assert.doesNotMatch(publish, /caption:\s*event\.target\.value/);
  assert.match(publish, /publish-platform-content-/);
  assert.match(publish, /platformContentFields/);
  for (const officialLabel of ['作品标题', '作品描述', '话题', '文章标题', '文章正文', 'Markdown 正文', '文章标签']) {
    assert.match(publish, new RegExp(officialLabel));
  }
  assert.match(publish, /supportsTags: false/);
  assert.match(publish, /contentTagsForPlatform/);
  assert.doesNotMatch(publish, /均使用原内容/);
  assert.doesNotMatch(publish, /publish-platform-toggle-/);
  assert.match(publish, /publish-platform-template-/);
  assert.doesNotMatch(publish, /publish-more-settings-/);
  assert.match(publish, /保存为默认模板/);
  assert.match(publish, /parsePlatformSettingsDraft\(schema, withoutRememberedOptionalDeclarations\(selected\?\.settings_json \|\| '\{\}'\)\)/);
  assert.match(publish, /publish-platform-schema-evidence-/);
  assert.match(publish, /参数依据：/);
  assert.match(publish, /publish-content-rule-evidence-\$\{code\}/);
  assert.match(publish, /required === true/);
  assert.match(publish, /required === false/);
  assert.match(publish, /待平台确认/);
  assert.match(publish, /required=\{rule\.titleRequired === true\}/);
  assert.match(publish, /required=\{rule\.bodyRequired === true\}/);
  assert.match(publish, /const tagsRequired = Boolean\(rule\.minTags\)/);
  assert.match(publish, /platformSettingInputHint/);
  assert.match(publish, /field\.remember/);
  assert.match(publish, /选填 · 记住上次/);
  assert.match(publish, /创作后台实机核对/);
  assert.match(publish, /字段契约已核对/);
  assert.match(publish, /测试夹具，待实机核对/);
  assert.match(publish, /规则版本/);
  assert.match(publish, /api\.createTemplate|api\.updateTemplate/);
  assert.match(publish, /alpha-nexus-publish-defaults-v2/);
  assert.match(publish, /assetIds: \[\]/);
  assert.doesNotMatch(publish, /saveDefaults\(\{ family, assetIds: assetIDs/);
  assert.match(publish, /settingsByTarget/);
  assert.match(publish, /settingsMemoryKey/);
  assert.doesNotMatch(publish, /summarizeFrozenPlatformSettings/);
  assert.doesNotMatch(publish, /发布选项（已冻结）：/);
  assert.match(publish, /选填项按账号记住上次选择/);
  assert.match(publish, /publish-required-summary-/);
  assert.match(publish, /未选择，将使用平台首帧能力并在页面复核/);
  assert.match(publish, /平台页面确认/);
  assert.match(publish, /target\.errorCode/);
  assert.match(publish, /nextNativePublishAt/);
  assert.doesNotMatch(publish, /validatePlannedPublishAt\(selectedContent\.planned_at\)/);
  assert.match(publish, /publish-content-planned-at/);
  assert.match(publish, /publish-silent-upload/);
  assert.match(publish, /后台从冻结本机路径静默上传；不打开平台文件选择器/);
  assert.match(publish, /按当前时间 \+24 小时重新计算/);
  assert.match(publish, /LOGIN_REQUIRED/);
  assert.match(publish, /需要登录：\$\{verificationPlatforms\}/);
  assert.match(publish, /plan\.snapshot\.content\.planned_at/);
  assert.match(publish, /target\.publish_at/);
  assert.match(publish, /if \(!frozenPublishTimeMatches\)/);

  // Tag requirements come from the verified platform rule instead of a UI-only platform name check.
  assert.doesNotMatch(publish, /required=\{code === 'csdn'\}/);
  assert.match(publish, /publish-platform-tags-\$\{code\}/);

  for (const technicalLabel of ['Schema v', 'SHA-256', '串行度', 'Job #', '平台内容版本']) {
    assert.doesNotMatch(publish, new RegExp(technicalLabel));
  }
});

test('final confirmation only surfaces the account, cover, and title', async () => {
  const publish = await readFile(new URL('../src/components/PublishView.tsx', import.meta.url), 'utf8');

  assert.match(publish, /publish-review-content/);
  assert.match(publish, /publish-review-cover/);
  assert.match(publish, /publish-review-title/);
  assert.match(publish, /publish-review-account-/);
  assert.match(publish, /<ContentCover content=\{selectedContent\}/);
  assert.match(publish, /<AccountAvatar/);
  assert.doesNotMatch(publish, /素材指纹：/);
  assert.doesNotMatch(publish, /合规策略：公开/);
  assert.doesNotMatch(publish, /compliance_fields\?\.length/);
});

test('UI reads supported media modes from capability manifests before freezing', async () => {
	const publish = await readFile(new URL('../src/components/PublishView.tsx', import.meta.url), 'utf8');

	assert.match(publish, /publishCapabilityFor/);
	assert.match(publish, /supportsPublishContentType/);
	assert.match(publish, /manifest\.first_release/);
	assert.match(publish, /item\.type === family/);
	assert.doesNotMatch(publish, /当前账号不能一起填充图文/);
	assert.match(publish, /disabled=\{!compatible\}/);
  assert.match(publish, /selectableFamilyAssets/);
	assert.match(publish, /isControlledPrepareAccountAsset/);
  assert.doesNotMatch(publish, /未接入受控填写/);
});

test('content constraints stay separate from capability selection', async () => {
	const publish = await readFile(new URL('../src/components/PublishView.tsx', import.meta.url), 'utf8');
	const contracts = await readFile(new URL('../src/platformContentRules.ts', import.meta.url), 'utf8');
	assert.match(publish, /publishCapabilityFor/);
	assert.doesNotMatch(contracts, /controlledMediaModes|supportsControlledMediaMode/);

	const youtube = rules.contentRuleFor('youtube', 'video');
	assert.match(rules.validatePlatformContent(youtube, 'bad < title', 'body'), /标题不能包含 </);
	assert.match(rules.validatePlatformContent(youtube, 'title', 'bad > body'), /正文或简介不能包含 >/);
	const csdn = rules.contentRuleFor('csdn', 'article');
	assert.match(rules.validatePlatformContent(csdn, 'title', 'body', []), /至少填写 1 个标签/);
	assert.equal(rules.validatePlatformContent(csdn, 'title', 'body', ['AlphaNexus']), '');
	const kuaishou = rules.contentRuleFor('kuaishou', 'video');
	assert.equal(kuaishou.titleRequired, false);
	assert.equal(rules.validatePlatformContent(kuaishou, '', '快手描述'), '');
	const douyin = rules.contentRuleFor('douyin', 'video');
	assert.match(rules.validatePlatformContent(douyin, '', '抖音描述'), /标题不能为空/);
	const meta = rules.contentRuleFor('facebook', 'video');
	assert.equal(meta.source, 'unverified');
	assert.equal(meta.titleRequired, undefined);
	assert.equal(meta.bodyRequired, undefined);
});

// 未验收平台点提交必然被拒（EGO_PUBLISH_SUBMIT_UNVERIFIED，fail-closed 设计）。
// 按钮能点却说不出原因，用户会以为功能坏了，所以提交前就要标明。
test('未验收的平台在提交前被明确指出', async () => {
  const view = await readFile(new URL('../src/components/PublishView.tsx', import.meta.url), 'utf8');

  assert.match(view, /function submitAcceptance\(/);
  assert.match(view, /capability\.submit_verified \? 'verified' : 'unverified'/);
  assert.match(view, /data-testid="publish-submit-unverified-notice"/);
  assert.match(view, /EGO_PUBLISH_SUBMIT_UNVERIFIED/);
});
