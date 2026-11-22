/**
 * 文案语气守卫：把「AI 生成的营销腔」变成可检查的事实。
 *
 * 这里不判断文采，只登记那些**换掉之后信息不变、但读起来像模板**的词。
 * 每一类都在真实页面上出过问题：
 *
 * - 「秒出 / 一键 / 30 秒」把功能说成魔法，用户实测对不上就失去信任。
 * - 「杜绝 / 彻底 / 100%」是无条件承诺，产品做不到。
 * - 「降维打击 / 截流 / 收割 / 弯道超车」是增长黑话，客户看不懂，也把同行写成猎物。
 * - 「赋能 / 抓手 / 闭环 / 生态位」是空心名词，删掉不影响句子。
 * - 「预计提升 3–5 倍」冒充数据；没有来源的倍数必须删或标成假设。
 *
 * 用法：`findSlop(text)` 返回命中项，测试与编辑器都读同一份清单。
 * 规则本身是数据，不是校验器的硬编码，所以新增词只改这个文件。
 */

export interface SlopTerm {
  /** 稳定 ID，测试与报告引用它。 */
  id: string;
  /** 命中的字面词。 */
  term: string;
  /** 为什么这句话读起来像生成的。 */
  why: string;
  /** 人写的时候会怎么表达。 */
  instead: string;
}

/** 分类只是给人看的归组，匹配时不分先后。 */
export const SLOP_TERMS: SlopTerm[] = [
  // 速度神话
  { id: 'speed-magic', term: '秒出', why: '把耗时压成 0，读者会当成夸张。', instead: '列出实际步骤或耗时' },
  { id: 'speed-magic', term: '秒级', why: '同上，且没有说明在什么条件下成立。', instead: '写出实测耗时与前提' },
  { id: 'speed-magic', term: '闪电', why: '形容词替代了具体信息。', instead: '直接说这是报价单/选型结果' },
  { id: 'speed-magic', term: '瞬时', why: '「瞬时」没说清在什么条件下才成立。', instead: '说明按什么条件匹配' },
  { id: 'speed-magic', term: '一键', why: '隐藏了背后的步骤和前置条件。', instead: '写清点击后实际发生什么' },
  // 只认「30 秒 + 动作」的承诺用法；「30 秒以内」是真实时长选项，由 ALLOWED_PHRASES 放行。
  { id: 'speed-magic', term: '30 秒', why: '未测量的承诺时间，容易出现「说 30 秒做了 5 分钟」。', instead: '写实测耗时或不写' },

  // 无条件承诺（「彻底删除」这类真实动作不算，见 ALLOWED_PHRASES）
  { id: 'absolute-claim', term: '杜绝', why: '产品只能降低概率，不能归零。', instead: '说明会提醒/拦截哪些情况' },
  { id: 'absolute-claim', term: '彻底', why: '同上，把风险说成不存在。', instead: '给出适用范围' },
  { id: 'absolute-claim', term: '零风险', why: '「零风险」无法证明，读者会当成免责话术。', instead: '列出已知风险' },

  // 增长黑话
  { id: 'growth-jargon', term: '降维打击', why: '把普通做法包装成战略，读者无法据此行动。', instead: '说明具体做哪几件事' },
  { id: 'growth-jargon', term: '截流', why: '行业黑话，客户通常不这样说话。', instead: '说明要覆盖哪些搜索词' },
  { id: 'growth-jargon', term: '收割', why: '把买家写成猎物，也和实际转化不符。', instead: '写「覆盖/承接这类需求」' },
  { id: 'growth-jargon', term: '弯道超车', why: '口号，不含执行信息。', instead: '列出具体改进项' },
  { id: 'growth-jargon', term: '流量密码', why: '把结果说成秘密，读者拿不到可复制的动作。', instead: '说明做对了什么' },
  { id: 'growth-jargon', term: '黄金', why: '「黄金公式/黄金子目录」是修饰腔。', instead: '直接给公式或目录结构' },
  { id: 'growth-jargon', term: '蓝海', why: '判断而非事实，且没有给出竞争度数据。', instead: '给出关键词或市场数据' },
  { id: 'growth-jargon', term: '内卷', why: '情绪词，不描述竞争状况。', instead: '说明该词的竞争度' },
  { id: 'growth-jargon', term: '护城河', why: '「护城河」是比喻，不指任何具体资产。', instead: '说明具体壁垒' },

  // 空心名词
  { id: 'hollow-noun', term: '赋能', why: '不说明做了什么。', instead: '写出具体动作' },
  { id: 'hollow-noun', term: '抓手', why: '读者不知道具体指什么。', instead: '写出具体手段' },
  { id: 'hollow-noun', term: '闭环', why: '流程术语，读者更关心具体步骤。', instead: '列出见面/报价/跟进的动作' },
  { id: 'hollow-noun', term: '生态位', why: '「生态位」是战略术语，客户看不出产品做什么。', instead: '说明产品定位' },
  { id: 'hollow-noun', term: '沉淀', why: '「沉淀能力/沉淀资产」是套话。', instead: '写「保存为…」' },
  { id: 'hollow-noun', term: '心智', why: '营销术语，读者无法据此判断。', instead: '说明要传递的具体印象' },

  // 假数据
  { id: 'fake-number', term: '预计提升', why: '没有来源的预测被写成结论。', instead: '删掉，或标为假设并给依据' },
  { id: 'fake-number', term: '提升 3–5 倍', why: '无来源倍数。', instead: '删掉或给出实测数据' },
  { id: 'fake-number', term: '提升 3-5 倍', why: '无来源倍数。', instead: '删掉或给出实测数据' },
  { id: 'fake-number', term: '转化率极高', why: '没有分母的评价。', instead: '给出可核对的比例' },
  { id: 'fake-number', term: '极高', why: '没有比较基准的形容词。', instead: '给出具体数值' },
  { id: 'fake-number', term: '权威度极高', why: '评价没有分母，读者无法核对是谁评的。', instead: '列出具体认证与外链数' },

  // 空洞强调
  { id: 'empty-emphasis', term: '不设虚构', why: '这类自证很容易反过来变成修辞。', instead: '直接展示数据来源' },
  { id: 'empty-emphasis', term: '深度', why: '「深度诊断/深度分析」是同义反复。', instead: '说明检查了哪几项' },
  { id: 'empty-emphasis', term: '全方位', why: '「全方位」暗示没有边界，实际总有覆盖不到的地方。', instead: '列出覆盖范围' },
  { id: 'empty-emphasis', term: '一站式', why: '「一站式」不说明包含哪些环节。', instead: '列出包含的功能' },
  { id: 'empty-emphasis', term: '助力', why: '「助力」把具体动作换成了姿态。', instead: '写「帮你/让…可以直接…」' },
  { id: 'empty-emphasis', term: '打造', why: '「打造」掩盖了实际是拼装、采购还是自研。', instead: '写「做出/建成」' },
  { id: 'empty-emphasis', term: '强势', why: '「强势」是自我介绍时的评价，不描述可观察的事实。', instead: '删掉，或写出具体数据' },
  { id: 'empty-emphasis', term: '震撼', why: '「震撼」替读者下结论，而不是给他判断依据。', instead: '删掉，或写出具体数据' },
];

export interface SlopHit {
  id: string;
  term: string;
  why: string;
  instead: string;
  /** 1 起算的行号，方便直接跳到源码。 */
  line: number;
  /** 命中处的整行，已去首尾空白并截断。 */
  excerpt: string;
}

/** 注释行里出现这些词是人写说明，不算页面文案。 */
const COMMENT_ONLY = /^\s*(\/\/|\*|\/\*)/;

/**
 * 这些词本身中性，只有用法不对时才算营销腔。
 *
 * - 「一键发布」「一键复制」是产品固有的功能名，改掉会和菜单、CLI 对不上。
 * - 「深度思考」「深度优先」是模型/算法参数名。
 * - 「彻底删除」描述的是不可撤销操作，是准确说法。
 * - CSS 里的 `100%`、`calc(100%-2rem)` 是布局值。
 *
 * 规则先去掉这些片段，再匹配；摘掉后仍命中的才是真问题。
 */
const ALLOWED_PHRASES: RegExp[] = [
  /一键(发布|复制|匹配(本机)?|选择|抓取|重试|提炼|新建[^，。；\n]{0,8})/g,
  /深度(思考|学习|优先|遍历|拷贝|合并)/g,
  /彻底删除/g,
  /(^|[^\d])100%[^\d]?/g,
  /calc\(100%[^)]*\)/g,
  // 真实时长与轮询间隔：「30 秒以内」「每 30 秒检查」是可核对的量。
  /30 ?秒(以内|内)/g,
  /每 ?30 ?秒/g,
];

/** 去掉允许的用法，剩下的文本才参与匹配。 */
function withoutAllowed(line: string): string {
  return ALLOWED_PHRASES.reduce((rest, pattern) => rest.replace(pattern, ' '), line);
}

/**
 * 找出文本里的营销腔。
 *
 * 逐行扫描而不是整篇匹配：报告要能指向具体某一行，编辑才知道改哪里。
 * 注释行跳过——规则说明本身会引用这些词。
 */
export function findSlop(text: string): SlopHit[] {
  const hits: SlopHit[] = [];
  const lines = text.split('\n');
  for (let index = 0; index < lines.length; index += 1) {
    const raw = lines[index];
    if (COMMENT_ONLY.test(raw)) continue;
    const scannable = withoutAllowed(raw);
    for (const entry of SLOP_TERMS) {
      if (!scannable.includes(entry.term)) continue;
      hits.push({
        id: entry.id,
        term: entry.term,
        why: entry.why,
        instead: entry.instead,
        line: index + 1,
        excerpt: raw.trim().slice(0, 120),
      });
    }
  }
  return hits;
}

/** 报告里给人读的一行摘要。 */
export function describeSlop(hit: SlopHit): string {
  return `${hit.line}:${hit.term} — ${hit.why} 改成「${hit.instead}」`;
}
