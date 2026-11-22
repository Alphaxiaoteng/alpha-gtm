import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

const workspacePath = '../src/components/BotWorkspacePanel.tsx';
const detailPath = '../src/components/BotDetailPanel.tsx';

/** 工作位置数组里单个选项的源码块（按 id 切到下一个 `{ id:` 之前）。 */
function placeBlock(source, id) {
  const arrayStart = source.indexOf('const places: readonly PlaceOption[] = [');
  assert.ok(arrayStart > 0, 'missing places array');
  const arrayEnd = source.indexOf('\n  ];', arrayStart);
  assert.ok(arrayEnd > arrayStart, 'places array should be closed');
  const places = source.slice(arrayStart, arrayEnd);
  const start = places.indexOf(`id: '${id}'`);
  assert.ok(start > 0, `missing place option ${id}`);
  const next = places.indexOf('{ id: ', start + 1);
  return places.slice(start, next === -1 ? undefined : next);
}

test('电脑 tab 复刻 OMB 的 屏幕 | 浏览器 | 数据 | 文件 分段与右侧关闭', async () => {
  const panel = await read(workspacePath);

  assert.match(panel, /type WorkspaceTab = 'screen' \| 'browser' \| 'data' \| 'files'/);
  for (const [id, label] of [
    ['screen', '屏幕'],
    ['browser', '浏览器'],
    ['data', '数据'],
    ['files', '文件'],
  ]) {
    assert.match(panel, new RegExp(`id: '${id}', label: '${label}'`), `missing tab ${id}`);
  }
  // 分段本身要可点（真实切换），不是装饰。
  assert.match(panel, /role="tablist"/);
  assert.match(panel, /role="tab"/);
  assert.match(panel, /data-testid=\{`bot-workspace-tab-\$\{item\.id\}`\}/);
  assert.match(panel, /aria-selected=\{tab === item\.id\}/);
  assert.match(panel, /onClick=\{\(\) => setTab\(item\.id\)\}/);
  // 右侧关闭是真的关闭（回到 BotDetailPanel 的 setActiveBotID('')）。
  assert.match(panel, /data-testid="bot-workspace-close"/);
  assert.match(panel, /onClick=\{onClose\}/);
});

test('屏幕卡片直接显示 EgoLite Space 实时画面，不再要求 macOS 录屏授权', async () => {
  const panel = await read(workspacePath);

  // 真实状态来自已有的 /healthz。
  assert.match(panel, /useSWR\('healthz', api\.getBackendHealth/);
  assert.match(panel, /data-testid="bot-workspace-runtime-state"/);
  assert.match(panel, /data-testid="bot-workspace-screen"/);
  assert.doesNotMatch(panel, /DEV/);

  // 屏幕 = Space 的当前页面，只读、3 秒刷新。
  assert.match(panel, /screenFrame\?\.src/);
  assert.match(panel, /data-testid="bot-workspace-screen-frame"/);
  // 说明文字已精简成一句状态（实时/暂停 + 更新时间），不再向用户解释截图机制。
  assert.match(panel, /bot-workspace-screen-note/);
  // 屏幕 tab 不需要先选 Space：选择器只在浏览器 tab 出现。
  // 切片要取「screen 分支的 JSX 内部」，而不是到下一个字符串出现为止——
  // 后者会把后面整段浏览器分支也算进来，断言必然失败。
  const screenStart = panel.indexOf("{tab === 'screen' ? (");
  assert.ok(screenStart > 0, 'screen 的 JSX 分支应存在');
  const screenBlock = panel.slice(screenStart, panel.indexOf("{tab === 'browser' ? (", screenStart));
  assert.ok(screenBlock.length > 0);
  assert.doesNotMatch(screenBlock, /liveSpacePicker/);
  // 实时画面可以暂停，并显示更新时间。
  assert.match(panel, /data-testid="bot-workspace-screen-pause"/);
  assert.match(panel, /toLocaleTimeString\('zh-CN'/);

  // 不再出现桌面截屏/授权入口，也不调用桌面接口。
  assert.doesNotMatch(panel, /getDesktopScreen/);
  assert.doesNotMatch(panel, /openDesktopScreenPermission/);
  assert.doesNotMatch(panel, /revealDesktopScreenBinary/);
  assert.doesNotMatch(panel, /screen-permission|桌面截屏需要/);
  assert.doesNotMatch(panel, /data-testid="bot-workspace-desktop-permission"/);

  // 用户接管的 Space 不抢控制，只给回退入口。
  assert.match(panel, /data-testid="bot-workspace-screen-user-controlled"/);
  assert.match(panel, /data-testid="bot-workspace-screen-fallback"/);
});

test('「工作位置」整块已移除，面板只保留真实能力', async () => {
  const panel = await read(workspacePath);

  // 六个选项里五个是「未接入」的占位，按产品要求整块删掉。
  assert.doesNotMatch(panel, /bot-workspace-places/);
  assert.doesNotMatch(panel, /工作位置/);
  for (const label of ['自动', '云端桌面', '私有桌面', '这台电脑']) {
    assert.doesNotMatch(panel, new RegExp(`label: '${label}'`), `${label} 不应再出现`);
  }
  // 真实能力仍在：屏幕 / 浏览器 / 数据 / 文件四个分段与 EgoLite 实时画面。
  assert.match(panel, /bot-workspace-screen/);
  assert.match(panel, /bot-workspace-browser/);
  assert.match(panel, /liveSpacePicker/);
});

test('只使用现有只读能力：healthz / ego doctor / ego spaces，不调没有的桌面接口', async () => {
  const panel = await read(workspacePath);

  assert.match(panel, /api\.getEgoDoctor/);
  assert.match(panel, /api\.getEgoSpaces/);
  assert.match(panel, /api\.getBackendHealth/);
  assert.match(panel, /useSWR\('api\/ego\/doctor'/);
  assert.match(panel, /useSWR\('api\/ego\/spaces'/);
  // 没有云端桌面 / 私有桌面 / 截图 / 接管这类不存在的后端。
  assert.doesNotMatch(panel, /computer\/screenshot/);
  assert.doesNotMatch(panel, /computer\/join/);
  assert.doesNotMatch(panel, /local-computer/);
  assert.doesNotMatch(panel, /cloud\//);
  // 只有内置浏览器一件事是真实可用状态。
  assert.match(panel, /data-testid="bot-workspace-browser"/);
  assert.match(panel, /data-testid="bot-workspace-browser-runtime"/);
  // 浏览器不是跳转：这里是实时画面 + 显式接管后取帧。
  assert.match(panel, /接管并看画面/);
  assert.match(panel, /takeOverEgoSpaceScreen|takeBackAndPreview/);
  assert.match(panel, /bot-workspace-screen-frame/);
});

test('浏览器分段轮询真实 Ego Space，页面隐藏时不轮询', async () => {
  const panel = await read(workspacePath);

  // 只有已存在的只读接口；轮询间隔是常量，页面隐藏时不轮询。
  assert.match(panel, /const EGOSPACE_REFRESH_MS = 5_000;/);
  assert.match(
    panel,
    /useSWR\('api\/ego\/spaces', api\.getEgoSpaces, \{[\s\S]{0,200}?refreshInterval: EGOSPACE_REFRESH_MS,[\s\S]{0,160}?refreshWhenHidden: false,/,
    'space list must poll with the shared interval and skip hidden pages',
  );
  // 不用手写 setInterval 循环，也不在组件外开第二套订阅。
  assert.doesNotMatch(panel, /window\.setInterval/);
  assert.doesNotMatch(panel, /new EventSource/);
  // 取帧状态用真实的 screenLoading，不是假转圈；SWR 仍负责列表轮询。
  assert.match(panel, /screenLoading/);
  assert.match(panel, /getEgoSpaceScreen/);
});

test('Space 行极简：只留 profile 名 + 状态 + 登录数，技术字段不上界面', async () => {
  const panel = await read(workspacePath);

  assert.match(panel, /data-testid=\{`bot-workspace-space-\$\{space\.id\}`\}/);
  // 用户视角只关心：这是哪个浏览器（profile 名）、现在什么状态、登录了几家。
  assert.match(panel, /identity\?\.profile_name \|\| identity\?\.name/);
  assert.match(panel, /space\.state/);
  assert.match(panel, /平台已登录/);
  assert.match(panel, /accountsByIdentity\.get\(space\.identity_id\)/);
  // 运维字段不再摆给用户：Space #id / task / ownership / 状态更新时间 / 任务号。
  assert.doesNotMatch(panel, /Space #\$\{space\.space_id\}/);
  assert.doesNotMatch(panel, /space\.task_id/);
  assert.doesNotMatch(panel, /SPACE_OWNERSHIP_LABELS/);
  assert.doesNotMatch(panel, /状态更新 \$\{seen\}/);
  assert.doesNotMatch(panel, /space\.active_job_id/);
  // 状态用真实取值翻译，未知状态原样回退。
  for (const state of ['ready', 'manual_takeover', 'result_unknown', 'verification_failed', 'unbound']) {
    assert.match(panel, new RegExp(`${state}: \\{ label:`), `missing state ${state}`);
  }
});

test('Space 动作只有真实入口，且同一时刻只发一次', async () => {
  const panel = await read(workspacePath);

  // 打开浏览器 = 已存在的 POST /api/ego/identities/{id}/open（服务端复用同一 Space）。
  assert.match(panel, /api\.openEgoBrowser\(identityID, 0\)/);
  // 置前 = 已存在的 POST /api/ego/focus。
  assert.match(panel, /api\.focusEgoLite\(\)/);
  // 单飞：有请求在途时按钮禁用。
  assert.match(panel, /openingIdentityID/);
  assert.match(panel, /disabled=\{[^}]*openingIdentityID !== null/);
  // EgoLite 不可用时不许点。
  assert.match(panel, /doctor\?\.available/);
  // 没有凭空造出来的接管 / 创建 / 截图接口。
  assert.doesNotMatch(panel, /window\.open\(/);
  assert.doesNotMatch(panel, /space\/takeover/);
  assert.doesNotMatch(panel, /space\/create/);
  assert.doesNotMatch(panel, /screenshot/i);
});

test('Space 列表有加载 / 错误 / 空态，并和本机运行时分开表述', async () => {
  const panel = await read(workspacePath);

  assert.match(panel, /spacesLoading/);
  assert.match(panel, /bot-workspace-spaces-loading/);
  assert.match(panel, /bot-workspace-spaces-error/);
  assert.match(panel, /bot-workspace-spaces-empty/);
  assert.match(panel, /还没有/);
  assert.match(panel, /data-testid="bot-workspace-browser-spaces"/);
  // 本机运行时 ≠ Ego 运行时。
  assert.match(panel, /本机运行时|EgoLite/);
  // 只读预览：用户接管时不截图、不抢控制。
  assert.match(panel, /user_controlled|你正在/);
});

test('api.ts 的 EgoSpace 类型补齐后端真实字段', async () => {
  const api = await read('../src/api.ts');
  const start = api.indexOf('export interface EgoSpace {');
  assert.ok(start > 0, 'missing EgoSpace interface');
  const block = api.slice(start, api.indexOf('\n}', start));
  for (const field of ['space_id', 'task_id', 'created_at', 'updated_at', 'last_seen_at', 'error_code']) {
    assert.match(block, new RegExp(`${field}:`), `missing ${field}`);
  }
});

test('数据分段只读现有对话，文件分段如实说没有 per-Bot 文件面板', async () => {
  const panel = await read(workspacePath);

  assert.match(panel, /data-testid="bot-workspace-data"/);
  assert.match(panel, /conversations/);
  assert.match(panel, /最近活动/);
  assert.match(panel, /常用模型/);
  // 数据卡片（OMB 的采集抽屉）没有后端，要写明。
  assert.match(panel, /数据卡片未接入/);
  assert.match(panel, /data-testid="bot-workspace-files"/);
  assert.match(panel, /没有 per-Bot 文件面板/);
});

test('BotDetailPanel 在电脑 tab 里挂载面板，并传真实 Bot 与对话', async () => {
  const detail = await read(detailPath);

  assert.match(detail, /import \{ BotWorkspacePanel \} from '\.\/BotWorkspacePanel'/);
  assert.match(detail, /<BotWorkspacePanel/);
  // 挂在 computer tab 里，不是新开一个 tab。
  const computer = detail.slice(
    detail.indexOf("data-testid=\"bot-detail-computer\""),
    detail.indexOf("{!showSkeleton && tab === 'routines'"),
  );
  assert.ok(computer.length > 0, 'computer tab block should exist');
  assert.match(computer, /<BotWorkspacePanel/);
  assert.match(computer, /bot=\{bot\}/);
  assert.match(computer, /conversations=\{conversations\}/);
  assert.match(computer, /onClose=\{\(\) => setActiveBotID\(''\)\}/);
});
