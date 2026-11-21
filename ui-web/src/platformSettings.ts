import type { PlatformSettingsField, PlatformSettingsSchema } from './api';

export type PlatformSettingValue = string | boolean;
export type PlatformSettingValues = Record<string, PlatformSettingValue>;
export type PlatformSettingInputHint = {
  placeholder: string;
  help: string;
};

export function platformSettingInputHint(field: Pick<PlatformSettingsField, 'key'>): PlatformSettingInputHint {
  if (field.key === 'location') {
    return {
      placeholder: '建议：杭州滨江天街',
      help: '输入位置后会在平台内模糊匹配，并选择第一个结果；发布前仍可修改。',
    };
  }
  return { placeholder: '', help: '' };
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function validateField(field: PlatformSettingsField, value: unknown): PlatformSettingValue {
  if (field.type === 'boolean') {
    if (typeof value !== 'boolean') throw new Error(`${field.label}必须是布尔值`);
    return value;
  }
  if (field.type === 'enum') {
    if (typeof value !== 'string') throw new Error(`${field.label}必须是选项值`);
    if (!field.options?.some((option) => option.value === value)) throw new Error(`${field.label}包含无效选项`);
    return value;
  }
  if (field.type === 'string') {
    if (typeof value !== 'string') throw new Error(`${field.label}必须是文本`);
    const normalized = value.trim();
    if (normalized.length > 128 || /[\r\n\0]/.test(normalized)) throw new Error(`${field.label}最多 128 个字符且不能换行`);
    return normalized;
  }
  throw new Error(`不支持的设置类型：${field.type}`);
}

function validateObject(
  schema: PlatformSettingsSchema,
  source: Record<string, unknown>,
  requireRequired = true,
  contentType?: 'video' | 'graph_text' | 'article',
): PlatformSettingValues {
  const fields = new Map(schema.fields.filter((field) => field.type !== 'manual').map((field) => [field.key, field]));
  for (const key of Object.keys(source)) {
    if (!fields.has(key)) throw new Error(`未知设置：${key}`);
  }
  const values: PlatformSettingValues = {};
  for (const field of schema.fields) {
    if (field.type === 'manual') continue;
    if (contentType && field.content_types?.length && !field.content_types.includes(contentType)) continue;
    const value = Object.hasOwn(source, field.key) ? source[field.key] : field.default;
    if (value === undefined) {
      if (field.required && requireRequired) throw new Error(`${field.label}不能为空`);
      continue;
    }
    const validated = validateField(field, value);
    if (field.required && requireRequired && typeof validated === 'string' && !validated) throw new Error(`${field.label}不能为空`);
    values[field.key] = validated;
  }
  return values;
}

function decodePlatformSettings(settingsJSON: string): Record<string, unknown> {
  let decoded: unknown;
  try {
    decoded = JSON.parse(settingsJSON.trim() || '{}');
  } catch {
    throw new Error('发布设置必须是 JSON 对象');
  }
  if (!isObject(decoded)) throw new Error('发布设置必须是 JSON 对象');
  return decoded;
}

export function parsePlatformSettings(schema: PlatformSettingsSchema, settingsJSON: string): PlatformSettingValues {
  return validateObject(schema, decodePlatformSettings(settingsJSON));
}

export function parsePlatformSettingsDraft(schema: PlatformSettingsSchema, settingsJSON: string): PlatformSettingValues {
  return validateObject(schema, decodePlatformSettings(settingsJSON), false);
}

export function serializePlatformSettings(
  schema: PlatformSettingsSchema,
  values: Record<string, unknown>,
  contentType?: 'video' | 'graph_text' | 'article',
): string {
  return JSON.stringify(validateObject(schema, values, true, contentType));
}

export function serializePlatformSettingsDraft(
  schema: PlatformSettingsSchema,
  values: Record<string, unknown>,
  contentType?: 'video' | 'graph_text' | 'article',
): string {
  return JSON.stringify(validateObject(schema, values, false, contentType));
}

export function withoutRememberedOptionalDeclarations(settingsJSON: string): string {
  try {
    const decoded = decodePlatformSettings(settingsJSON);
    for (const key of ['self_declaration', 'declaration', 'content_source', 'ai_generated', 'altered_content']) {
      delete decoded[key];
    }
    return JSON.stringify(decoded);
  } catch {
    return '{}';
  }
}

export function summarizePlatformSettings(
  schema: PlatformSettingsSchema,
  settingsJSON: string,
  contentType?: 'video' | 'graph_text' | 'article',
): string {
  const values = parsePlatformSettings(schema, settingsJSON);
  return schema.fields
    .filter((field) => field.type !== 'manual' && (!contentType || !field.content_types?.length || field.content_types.includes(contentType)))
    .map((field) => {
    const value = values[field.key];
    const label = field.type === 'boolean'
      ? (value ? '开启' : '关闭')
      : field.type === 'enum'
        ? (field.options?.find((option) => option.value === value)?.label ?? String(value))
        : (String(value) || '未设置');
    return `${field.label}：${label}`;
  }).join(' · ');
}
