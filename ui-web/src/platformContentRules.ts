export type PlatformContentRule = {
  titleRequired?: boolean;
  bodyRequired?: boolean;
  titleMax?: number;
  bodyMax?: number;
  minTags?: number;
  titleUnit?: 'characters' | 'utf16';
  bodyUnit?: 'bytes' | 'utf16';
  forbiddenTitleCharacters?: string[];
	forbiddenBodyCharacters?: string[];
  source: 'official' | 'console_verified' | 'contract_verified' | 'unverified';
  note: string;
  sourceURL?: string;
};

export const minimumPublishLeadMS = 15 * 60 * 1000;

export function validatePlannedPublishAt(value: string | undefined, nowMS = Date.now()) {
  if (!value) return '';
  const plannedMS = Date.parse(value);
  if (!Number.isFinite(plannedMS)) return '排期时间无效';
  if (plannedMS < nowMS + minimumPublishLeadMS) return '排期时间至少提前 15 分钟';
  return '';
}

// XHS is currently the only prepare worker with reviewed setting selectors.
// Other platform schemas remain useful as reference/template data elsewhere,
// but must not be presented as automatically applied by One-click Publish.
export function hasControlledPlatformSettings(platformCode: string) {
	return platformCode === 'xhs';
}

// Only stable, public first-party limits belong here. Creator-console hints
// that have not been verified against public platform documentation remain
// informational and must never silently truncate user content.
const officialRules: Record<string, Partial<Record<'video' | 'graph_text' | 'article', PlatformContentRule>>> = {
  xhs: {
    video: { titleRequired: true, bodyRequired: true, source: 'console_verified', note: '当前小红书创作后台字段已实机核对。' },
    graph_text: { titleRequired: true, bodyRequired: true, source: 'console_verified', note: '当前小红书创作后台字段已实机核对。' },
  },
  douyin: {
    video: { titleRequired: true, bodyRequired: true, titleMax: 30, bodyMax: 1000, source: 'console_verified', note: '当前抖音创作后台视频发布页显示标题 30、作品描述 1000。' },
    graph_text: { titleRequired: true, bodyRequired: true, titleMax: 30, bodyMax: 1000, source: 'console_verified', note: '当前抖音创作后台图文发布沿用作品标题和作品描述契约。' },
  },
  kuaishou: {
    video: { titleRequired: false, bodyRequired: true, source: 'contract_verified', note: '快手作品标题可选；描述和素材在准备阶段校验。' },
    graph_text: { titleRequired: false, bodyRequired: true, source: 'contract_verified', note: '快手作品标题可选；描述和图片在准备阶段校验。' },
  },
  wechat_video: {
    video: { titleRequired: true, bodyRequired: true, source: 'contract_verified', note: '视频号字段契约已交叉核对；提交控件仍需当前页面二次确认。' },
  },
  wechat_mp: {
    article: { titleRequired: true, bodyRequired: true, source: 'console_verified', note: '微信公众号文章标题和正文由当前编辑器校验。' },
  },
  youtube: {
    video: {
      titleRequired: true,
      bodyRequired: false,
      titleMax: 100,
      bodyMax: 5000,
      titleUnit: 'characters',
      bodyUnit: 'bytes',
      forbiddenTitleCharacters: ['<', '>'],
			forbiddenBodyCharacters: ['<', '>'],
      source: 'official',
      note: 'YouTube 官方：标题最多 100 个字符；简介最多 5000 字节，标题和简介不能包含 < 或 >。',
      sourceURL: 'https://developers.google.com/youtube/v3/docs/videos',
    },
  },
  tiktok: {
    video: {
      titleRequired: false,
      bodyRequired: true,
      titleMax: 2200,
      titleUnit: 'utf16',
      source: 'official',
      note: 'TikTok 官方 Direct Post：视频 caption 最多 2200 个 UTF-16 单位。',
      sourceURL: 'https://developers.tiktok.com/doc/content-posting-api-reference-direct-post',
    },
    graph_text: {
      titleRequired: false,
      bodyRequired: true,
      titleMax: 90,
      bodyMax: 4000,
      titleUnit: 'utf16',
      bodyUnit: 'utf16',
      source: 'official',
      note: 'TikTok 官方 Photo Post：标题最多 90、描述最多 4000 个 UTF-16 单位。',
      sourceURL: 'https://developers.tiktok.com/doc/content-posting-api-reference-photo-post',
    },
  },
  csdn: {
    article: {
      titleRequired: true,
      bodyRequired: true,
      minTags: 1,
      source: 'console_verified',
      note: '已在当前 CSDN 创作后台验证：进入最终发布设置后，“文章标签”为必填项；至少填写 1 个标签。',
    },
  },
};

export function contentRuleFor(platformCode: string, contentType: 'video' | 'graph_text' | 'article'): PlatformContentRule {
  return officialRules[platformCode]?.[contentType] ?? {
    source: 'unverified',
    note: platformCode === 'wechat_video'
      ? '视频号公开官方标题限制尚未验证：本地不自动删除标点或截断，填写后以当前创作页校验为准。'
      : '公开官方字段限制尚未验证：本地不自动截断，填写后以当前创作页校验为准。',
  };
}

export function countText(value: string) {
  return Array.from(value).length;
}

export function validatePlatformContent(rule: PlatformContentRule, title: string, body: string, tags: readonly string[] = []) {
  if (rule.titleRequired && !title.trim()) return '标题不能为空';
  if (rule.bodyRequired && !body.trim()) return '正文或简介不能为空';
  const titleLength = rule.titleUnit === 'utf16' ? title.length : countText(title);
  const bodyLength = rule.bodyUnit === 'utf16' ? body.length : new TextEncoder().encode(body).length;
  if (rule.titleMax && titleLength > rule.titleMax) return `标题超过 ${rule.titleMax} ${rule.titleUnit === 'utf16' ? '个 UTF-16 单位' : '个字符'}`;
  if (rule.bodyMax && bodyLength > rule.bodyMax) return `正文或简介超过 ${rule.bodyMax} ${rule.bodyUnit === 'utf16' ? '个 UTF-16 单位' : '字节'}`;
  const forbidden = rule.forbiddenTitleCharacters?.find((character) => title.includes(character));
  if (forbidden) return `标题不能包含 ${forbidden}`;
	const forbiddenBody = rule.forbiddenBodyCharacters?.find((character) => body.includes(character));
	if (forbiddenBody) return `正文或简介不能包含 ${forbiddenBody}`;
  if (rule.minTags && tags.filter((tag) => tag.trim()).length < rule.minTags) return `至少填写 ${rule.minTags} 个标签`;
  return '';
}
