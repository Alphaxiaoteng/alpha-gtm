import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';
import { findSlop, SLOP_TERMS, describeSlop } from '../src/lib/copyTone.ts';

const GTM_VIEW = new URL('../src/components/GuanyingWorkspaceViews.tsx', import.meta.url);

test('GTM 工作台不再出现营销腔', async () => {
  const source = await readFile(GTM_VIEW, 'utf8');
  const hits = findSlop(source);
  assert.equal(
    hits.length,
    0,
    `GTM 页面仍有 ${hits.length} 处营销腔：\n${hits.map(describeSlop).join('\n')}`,
  );
});

test('规则能抓到真问题，不误报产品固有名词', () => {
  // 这些是原来页面上真实出现过的句子，必须命中。
  const real = [
    '聚焦 4 大成交场景，30 秒直出中英双语报价单。',
    '±5mm 公差瞬时锁定汉德 35T',
    '杜绝整柜退货',
    '一线销售当场选型 & 闪电报价单',
    '缺 Top10 榜单截流',
    '对拉美询盘精准收割',
    '预计提升 3–5 倍线索率',
    '四、小语种降维打击词矩阵',
  ];
  for (const line of real) {
    assert.ok(findSlop(line).length > 0, `应该命中却被放过：${line}`);
  }

  // 产品名、技术词、真实时长都不是营销腔，改掉反而和菜单/参数对不上。
  const fine = [
    '一键发布',
    '一键复制',
    '{ id: \'high\', label: \'深度思考\' }',
    '确定要彻底删除该内容吗？',
    "className=\"h-[calc(100%-33px)] w-full\"",
    '<option value={30_000}>30 秒以内</option>',
    '每 30 秒检查本地 CLI 与模型目录。',
  ];
  for (const line of fine) {
    const hits = findSlop(line);
    assert.equal(hits.length, 0, `不该命中：${line} → ${hits.map((h) => h.term).join(',')}`);
  }
});

test('注释行不计入，规则自己引用的词不会自证违规', () => {
  const source = ['// 这条规则命中「秒出」和「杜绝」', '/* 拦截降维打击 */', '正常文案'].join('\n');
  assert.equal(findSlop(source).length, 0);
});

test('每条规则都带原因和改法，报告可直接给人看', () => {
  for (const term of SLOP_TERMS) {
    assert.ok(term.term.trim().length > 0, `${term.id} 缺少字面词`);
    assert.ok(term.why.length >= 6, `${term.id} 的 why 太短，解释不了为什么像生成的`);
    assert.ok(term.instead.length >= 2, `${term.id} 缺少改法`);
    assert.ok(term.id.length > 0);
  }
});

test('命中带行号，编辑能直接跳到源码', () => {
  const source = ['第一行正常', '第二行有秒出', '第三行正常'].join('\n');
  const hits = findSlop(source);
  assert.equal(hits.length, 1);
  assert.equal(hits[0].line, 2);
  assert.equal(hits[0].term, '秒出');
});
