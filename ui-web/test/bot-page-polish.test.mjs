import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

const railPath = '../src/components/BotRail.tsx';
const detailPath = '../src/components/BotDetailPanel.tsx';
const settingsPath = '../src/components/BotSettingsPanel.tsx';

test('空列表给三个一键创建的推荐 Bot，点了直接建并打开', async () => {
  const rail = await read(railPath);

  assert.match(rail, /const RECOMMENDED_BOTS: Array</);
  for (const key of ['director', 'analyst', 'xhs']) {
    assert.match(rail, new RegExp(`key: '${key}'`), `missing recommended bot ${key}`);
  }
  for (const name of ['内容编导', '数据复盘', '小红书运营']) {
    assert.match(rail, new RegExp(`name: '${name}'`), `missing recommended name ${name}`);
  }
  // One click posts a real bot and opens it; it must not route to settings.
  assert.match(rail, /data-testid="bot-rail-empty"/);
  assert.match(rail, /data-testid=\{`bot-rail-recommend-\$\{preset\.key\}`\}/);
  assert.match(rail, /const created = await api\.createAgentBot\(\{/);
  assert.match(rail, /await Promise\.all\(\[mutateBots\(\), refreshRail\(\)\]\)/);
  assert.match(rail, /await openBot\(created\)/);
  // 用 next 分支本身作为终点，不依赖缩进与换行——源码格式化会改掉那些空白。
  const emptyStart = rail.indexOf('data-testid="bot-rail-empty"');
  const emptyEnd = rail.indexOf('listBots.length === 0', emptyStart);
  const emptyBlock = rail.slice(emptyStart, emptyEnd > emptyStart ? emptyEnd : rail.length);
  assert.ok(emptyBlock.length > 0, 'empty rail block should exist');
  assert.doesNotMatch(emptyBlock, /setActiveTab\('settings'\)/);
});

test('侧栏加载态是三条骨架行，不是 spinner', async () => {
  const rail = await read(railPath);

  assert.match(rail, /data-testid="bot-rail-skeleton"/);
  assert.match(rail, /\{\[0, 1, 2\]\.map\(\(row\) => \(/);
  assert.match(rail, /animate-pulse/);
  const skeletonBlock = rail.slice(rail.indexOf('data-testid="bot-rail-skeleton"'), rail.indexOf('bot-rail-error'));
  assert.ok(skeletonBlock.length > 0, 'skeleton block should exist');
  assert.doesNotMatch(skeletonBlock, /animate-spin/);
});

test('侧栏请求失败给一行提示 + 重试，且已有数据时不清空列表', async () => {
  const rail = await read(railPath);

  assert.match(rail, /const \{ data: bots = \[\], isLoading, error, mutate: mutateBots \} = useSWR\('api\/agent-bots'/);
  assert.match(rail, /error && bots\.length === 0/);
  assert.match(rail, /data-testid="bot-rail-error"/);
  assert.match(rail, /Bot 列表没读到/);
  assert.match(rail, /data-testid="bot-rail-retry"/);
  assert.match(rail, /void mutateBots\(\);/);
});

test('置顶标记跟在皇冠后面，用 wrapper 承载 testid/aria/title', async () => {
  const rail = await read(railPath);

  assert.match(rail, /import \{[^}]*\bPin\b[^}]*\} from 'lucide-react'/);
  assert.match(rail, /data-testid=\{`bot-rail-pin-\$\{bot\.id\}`\}/);
  assert.match(rail, /aria-label="已置顶"/);
  assert.match(rail, /title="已置顶"/);
  // 12px 下要能读出是图钉，所以比铃铛略大一点、带一点暖色
  assert.match(rail, /<Pin className="h-4 w-4" strokeWidth=\{2\.4\} fill="currentColor"/);
  // lucide 组件本身不接受 DOM props，标记必须在 span 上
  assert.match(rail, /className="flex h-4 w-4 items-center justify-center text-\[#c08a1e\]"\s*>\s*\n\s*<Pin className="h-4 w-4"/);

  // 顺序契约（按参考图排版）：皇冠贴头像左上角，未读点紧跟名字，
  // 置顶图钉与免打扰铃铛收在行尾。
  const crown = rail.indexOf('data-testid={`bot-rail-primary-${bot.id}`}');
  const unread = rail.indexOf('data-testid={`bot-rail-unread-${bot.id}`}');
  const pin = rail.indexOf('data-testid={`bot-rail-pin-${bot.id}`}');
  const muted = rail.indexOf('data-testid={bot.muted ? `bot-rail-muted-${bot.id}` : `bot-rail-unmuted-${bot.id}`}');
  assert.ok(crown > 0 && unread > crown && pin > unread && muted > pin, 'marker order must be 主 Bot → 未读 → 置顶 → 免打扰');

  // 置顶行本身带浅暖底，扫一眼就知道哪行被钉住
  assert.match(rail, /bot\.pinned\s*\n?\s*\? 'bg-brand-surface-raised/);

  // 组内排序仍然是 pinned 优先
  assert.match(rail, /if \(Boolean\(left\.pinned\) !== Boolean\(right\.pinned\)\) return left\.pinned \? -1 : 1;/);
});

test('没做的功能不占导航：例行任务/页面占位已删除，只留能用的两页', async () => {
  const detail = await read(detailPath);

  // 产品思维：没做的东西不摆空 tab，也不写工程词。
  assert.doesNotMatch(detail, /id: 'routines'/);
  assert.doesNotMatch(detail, /id: 'pages'/);
  assert.doesNotMatch(detail, /接线/);
  // 只剩电脑 + 高级两个状态，对齐 OMB：电脑态直接一层 ComputerPanel。
  assert.match(detail, /type DetailTab = 'computer' \| 'advanced'/);
  assert.doesNotMatch(detail, /const TABS/);
  assert.match(detail, /tab === 'advanced' \?/);

  // 电脑态只留实时画面 + 真实操作，不摆模型/对话数/优化建议这类开发视角。
  const computerStart = detail.indexOf('data-testid="bot-detail-computer"');
  const computer = detail.slice(computerStart, detail.indexOf('</aside>', computerStart));
  assert.ok(computer.length > 0, 'computer tab block should exist');
  assert.match(computer, /<BotWorkspacePanel/);
  assert.match(computer, /data-testid="bot-detail-pin"/);
  assert.match(computer, /data-testid="bot-detail-primary"/);
  assert.doesNotMatch(computer, /优化建议/);
  assert.doesNotMatch(computer, /label="对话数"/);
  assert.doesNotMatch(computer, /label="最近活动"/);
  // 高级页不再暴露 Bot ID / 头像路径。
  assert.doesNotMatch(detail, /label="Bot ID"/);
});

test('窄屏下详情面板变成覆盖式抽屉，带可点关闭的遮罩', async () => {
  const detail = await read(detailPath);

  assert.match(detail, /data-testid="bot-detail-backdrop"/);
  assert.match(detail, /onClick=\{\(\) => setActiveBotID\(''\)\}/);
  assert.match(detail, /hidden bg-black\/25 max-\[1100px\]:block/);
  // 窄屏绝对定位，不挤压聊天区；宽屏保持并排
  assert.match(detail, /max-\[1100px\]:absolute/);
  assert.match(detail, /max-\[1100px\]:inset-y-0/);
  assert.match(detail, /max-\[1100px\]:right-0/);
  assert.match(detail, /max-\[1100px\]:z-30/);
  assert.match(detail, /className="flex w-\[340px\] shrink-0/);
  // 面板在遮罩之上
  assert.match(detail, /absolute inset-0 z-20 hidden/);
});

test('详情面板加载中给骨架，不留空白', async () => {
  const detail = await read(detailPath);

  assert.match(detail, /const showSkeleton = isLoading && !bot;/);
  assert.match(detail, /data-testid="bot-detail-skeleton"/);
  assert.match(detail, /aria-busy=\{showSkeleton \|\| undefined\}/);
  assert.match(detail, /showSkeleton \? \(/);
  assert.match(detail, /<DetailSkeleton \/>/);
});

test('设置页把 DSH 供应商/模型数量收进默认收起的 details', async () => {
  const panel = await read(settingsPath);

  assert.match(panel, /data-testid="connect-dsh-details"/);
  assert.match(panel, /DSH 配置 1:1 已同步 ✓/);
  // 数量与默认模型都在折叠区里，不占首屏
  const detailsStart = panel.indexOf('data-testid="connect-dsh-details"');
  const detailsEnd = panel.indexOf('</details>', detailsStart);
  assert.ok(detailsEnd > detailsStart, 'details block should close');
  const detailsBody = panel.slice(detailsStart, detailsEnd);
  assert.match(detailsBody, /\{providerCount\} 个供应商 · \{modelCount\} 个模型/);
  assert.match(detailsBody, /默认 \{catalog\.default_provider\}\/\{catalog\.default_model\}/);
  // 没有 open 属性 = 默认收起
  assert.doesNotMatch(detailsBody, /<details[^>]*\sopen/);
});

// 「置顶」是界面上人造的分区，不是真实分组。对它右键「重命名分组」
// 会把每个置顶 Bot 的 group_name 写成「置顶」，等于凭空造出一个部门
// 并把它们从原部门搬走。
test('置顶伪分区不响应分组右键菜单', async () => {
  const rail = await read(railPath);

  assert.match(rail, /const syntheticGroups = new Set\(\['置顶'\]\)/);
  assert.match(rail, /if \(syntheticGroups\.has\(section\.group\)\) return;/);
  // 守卫必须在 preventDefault 之前，否则仍会吞掉浏览器默认菜单却不弹自己的。
  const guardIdx = rail.indexOf('if (syntheticGroups.has(section.group)) return;');
  const preventIdx = rail.indexOf('event.preventDefault();', guardIdx);
  assert.ok(guardIdx > 0 && preventIdx > guardIdx, 'guard must precede preventDefault');
});

// 复制是 Bot 管理最基本的操作之一：想基于现有人格改一版，不该从零重填。
test('Bot 右键菜单可以复制人格', async () => {
  const rail = await read(railPath);

  assert.match(rail, /async function duplicateBot\(bot: AgentBot\)/);
  assert.match(rail, /data-testid="bot-rail-menu-duplicate"/);
  assert.match(rail, /const created = await api\.createAgentBot\(\{/);
  // 主 Agent 只能有一个；副本不该抢这个位子。
  const dupBlock = rail.slice(rail.indexOf('async function duplicateBot'));
  const body = dupBlock.slice(0, dupBlock.indexOf('async function'));
  assert.doesNotMatch(body, /is_primary/);
});

// Bot 的常规管理操作应当能在侧栏右键里完成，而不是全都藏进设置页。
test('Bot 右键菜单覆盖常规管理操作', async () => {
  const rail = await read(railPath);

  for (const testid of [
    'bot-rail-menu-pin',
    'bot-rail-menu-mute',
    'bot-rail-menu-edit',
    'bot-rail-menu-duplicate',
    'bot-rail-menu-delete',
  ]) {
    assert.match(rail, new RegExp(`data-testid="${testid}"`), `missing ${testid}`);
  }
  // 删除前必须确认，且内置主 Agent 前端先挡一次。
  assert.match(rail, /window\.confirm\(`删除 Bot/);
  assert.match(rail, /isBuiltinAlphaAgent\(bot\)/);
});
