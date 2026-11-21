import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';
import ts from 'typescript';

const source = await readFile(new URL('../src/lib/botDelegation.ts', import.meta.url), 'utf8');

// 交接解析是纯函数：转译后在 Node 里真跑，而不是比对源码字面。
async function delegationModule() {
  const start = source.indexOf('// #region bot-delegation-logic');
  const end = source.indexOf('// #endregion bot-delegation-logic');
  assert.ok(start > 0 && end > start, 'lib/botDelegation.ts 必须保留可执行区域');
  const { outputText } = ts.transpileModule(
    `${source.slice(start, end)}\nexport { parseBotDelegation, botMentionSuggestions, runningDelegations };\n`,
    { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } },
  );
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}

const bots = [
  { id: 'dev', name: '开发', role: '工程' },
  { id: 'growth', name: '增长', role: '运营' },
  { id: 'dev-helper', name: '开发助手', role: '工程' },
];

test('开头点名另一个 Bot 会解析成真实交接任务', async () => {
  const { parseBotDelegation } = await delegationModule();

  assert.deepEqual(parseBotDelegation('@开发 检查登录流程', bots), {
    bot: bots[0],
    task: '检查登录流程',
    mention: '@开发 检查登录流程',
  });
  // 前后空白不算正文。
  assert.equal(parseBotDelegation('   @开发   看下发布队列  ', bots)?.task, '看下发布队列');
});

test('名字长的 Bot 优先，避免被短名字截断', async () => {
  const { parseBotDelegation } = await delegationModule();
  const target = parseBotDelegation('@开发助手 跑一遍测试', bots);
  assert.equal(target?.bot.id, 'dev-helper');
  assert.equal(target?.task, '跑一遍测试');
});

test('只写 @名字 不派活；正文里提到 Bot 也不派活', async () => {
  const { parseBotDelegation } = await delegationModule();
  assert.equal(parseBotDelegation('@开发', bots), null);
  assert.equal(parseBotDelegation('@开发   ', bots), null);
  assert.equal(parseBotDelegation('帮我问一下 @开发 这件事', bots), null);
  assert.equal(parseBotDelegation('@开发助手X 你好', bots), null);
  assert.equal(parseBotDelegation('普通消息', bots), null);
});

test('输入 @ 时按名字和角色过滤候选', async () => {
  const { botMentionSuggestions } = await delegationModule();
  assert.deepEqual(botMentionSuggestions('@', bots).map((bot) => bot.id), ['dev', 'growth', 'dev-helper']);
  assert.deepEqual(botMentionSuggestions('@工程', bots).map((bot) => bot.id), ['dev', 'dev-helper']);
  assert.deepEqual(botMentionSuggestions('@增长', bots).map((bot) => bot.id), ['growth']);
  assert.deepEqual(botMentionSuggestions('没有 mention', bots), []);
  assert.deepEqual(botMentionSuggestions('@', bots, 1).map((bot) => bot.id), ['dev']);
});

test('状态条只显示正在跑的交接', async () => {
  const { runningDelegations } = await delegationModule();
  const rows = [
    { id: 'a', status: 'running' },
    { id: 'b', status: 'completed' },
    { id: 'c', status: 'failed' },
  ];
  assert.deepEqual(runningDelegations(rows).map((item) => item.id), ['a']);
  assert.deepEqual(runningDelegations(null), []);
});
