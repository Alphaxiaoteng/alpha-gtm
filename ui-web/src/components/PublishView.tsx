import { useEffect, useMemo, useRef, useState } from 'react';
import useSWR from 'swr';
import { ArrowLeft, ArrowRight, Check, CircleUserRound, FileText, Film, Images, Save } from 'lucide-react';
import { api, type Account, type AccountAsset, type ContentItem, type EgoPublishManifest, type PlatformSettingsSchema, type PlatformTemplate, type PublishPlan, type PublishPlanExecution, type StudioScheduleHandoff } from '../api';
import { parsePlatformSettingsDraft, platformSettingInputHint, serializePlatformSettings, serializePlatformSettingsDraft, summarizePlatformSettings, withoutRememberedOptionalDeclarations, type PlatformSettingValues } from '../platformSettings';
import { suggestPublishTags } from '../publishTags';
import { contentRuleFor, countText, validatePlannedPublishAt, validatePlatformContent } from '../platformContentRules';
import { buildMetaPublishGroups, isControlledPrepareAccountAsset, type MetaPublishPlatformCode } from '../publishAssets';
import { canResumeEgoSpaceForExplicitPrepare, isEgoSpaceAvailableForExplicitPublish } from '../publishPlanReadiness';
import { prepareFrozenPublishPlan, submitPreparedPublishPlan } from '../publishPlanAuthorization';
import { useUIStore } from '../store';
import { AccountAvatar } from './AccountAvatar';
import { ContentCover } from './ContentCover';
import { PlatformIcon } from './PlatformIcon';

type PublishFamily = 'video' | 'graph_text' | 'article';
type PublishStep = 'accounts' | 'content' | 'review';
type ContentType = ContentItem['type'];
type PlatformOverride = {
	title: string;
	body: string;
	tags: string;
};
type PlatformContentFields = {
  titleLabel: string;
  bodyLabel: string;
  tagsLabel: string;
  tagsHelp: string;
  supportsTags: boolean;
};

type PersistedDefaults = {
  family?: PublishFamily;
  assetIds?: number[];
  settingsByPlatform?: Record<string, string>;
  settingsByTarget?: Record<string, string>;
  templateByPlatform?: Record<string, number>;
};

type ResolvedPlatformSettings = {
  schema?: PlatformSettingsSchema;
  values: PlatformSettingValues | null;
  normalizedJSON: string;
  summary: string;
  error: string;
};

// Reset only local form memory so newly frozen plans receive the reviewed
// YouTube public/not-for-kids defaults instead of stale saved values.
const publishDefaultsStorageKey = 'alpha-nexus-publish-defaults-v2';
const nativeScheduleOffsetMs = 24 * 60 * 60 * 1000;
const platformContentFields: Record<string, PlatformContentFields> = {
  xhs: { titleLabel: '标题', bodyLabel: '正文', tagsLabel: '话题', tagsHelp: '自动提取，可修改；发布时追加为 #话题', supportsTags: true },
  douyin: { titleLabel: '作品标题', bodyLabel: '作品描述', tagsLabel: '话题', tagsHelp: '自动提取，可修改；发布时追加为 #话题', supportsTags: true },
  kuaishou: { titleLabel: '作品标题', bodyLabel: '作品描述', tagsLabel: '话题', tagsHelp: '可选，发布时追加为 #话题', supportsTags: true },
  wechat_video: { titleLabel: '标题', bodyLabel: '描述', tagsLabel: '话题', tagsHelp: '可选，发布时追加为 #话题', supportsTags: true },
  youtube: { titleLabel: '标题', bodyLabel: '说明', tagsLabel: '标签', tagsHelp: '自动提取，可修改；发布时逐个加入视频标签', supportsTags: true },
  tiktok: { titleLabel: '标题', bodyLabel: '说明文字', tagsLabel: '话题标签', tagsHelp: '自动提取，可修改；发布时逐个确认平台话题', supportsTags: true },
  facebook: { titleLabel: '帖子标题', bodyLabel: '帖子正文', tagsLabel: '标签', tagsHelp: '自动提取，可修改；发布时追加为 #标签', supportsTags: true },
  instagram: { titleLabel: '标题', bodyLabel: '说明文字', tagsLabel: '标签', tagsHelp: '自动提取，可修改；发布时追加为 #标签', supportsTags: true },

  // Bilibili's current controlled adapter uses the video composer only.
  // Tags/categories/declarations are deliberately left to the creator page
  // rather than guessed from a generic tag set.
  bilibili: { titleLabel: '稿件标题', bodyLabel: '视频简介', tagsLabel: '', tagsHelp: '', supportsTags: false },
  wechat_mp: { titleLabel: '文章标题', bodyLabel: '文章正文', tagsLabel: '', tagsHelp: '', supportsTags: false },
  csdn: { titleLabel: '文章标题', bodyLabel: 'Markdown 正文', tagsLabel: '文章标签', tagsHelp: '自动提取，可修改；逗号或换行分隔', supportsTags: true },
};

function loadDefaults(): PersistedDefaults {
  try {
    const decoded: unknown = JSON.parse(globalThis.localStorage?.getItem(publishDefaultsStorageKey) || '{}');
    if (!decoded || typeof decoded !== 'object' || Array.isArray(decoded)) return {};
    const value = decoded as PersistedDefaults;
    const settingsWithoutOldDeclarations = (source: Record<string, string> | undefined) => Object.fromEntries(
      Object.entries(source ?? {}).map(([key, settingsJSON]) => [
        key,
        withoutRememberedOptionalDeclarations(typeof settingsJSON === 'string' ? settingsJSON : '{}'),
      ]),
    );
    return {
      // ponytail: legacy "video_image" and unknown values migrate to video.
      family: value.family === 'article' ? 'article' : value.family === 'graph_text' ? 'graph_text' : 'video',
      // Never restore a previous publish target: stale selection can submit an
      // already-attempted platform when the user intended a different account.
      assetIds: [],
      settingsByPlatform: value.settingsByPlatform && typeof value.settingsByPlatform === 'object' ? settingsWithoutOldDeclarations(value.settingsByPlatform) : {},
      settingsByTarget: value.settingsByTarget && typeof value.settingsByTarget === 'object' ? settingsWithoutOldDeclarations(value.settingsByTarget) : {},
      templateByPlatform: value.templateByPlatform && typeof value.templateByPlatform === 'object' ? value.templateByPlatform : {},
    };
  } catch {
    return {};
  }
}

function saveDefaults(value: PersistedDefaults) {
  try {
    globalThis.localStorage?.setItem(publishDefaultsStorageKey, JSON.stringify(value));
  } catch {
    // Local defaults are optional. SQLite remains the publish truth.
  }
}

function resolvePlatformSettings(schema: PlatformSettingsSchema | undefined, source: string, contentType: ContentType): ResolvedPlatformSettings {
  if (!schema) return { values: null, normalizedJSON: '', summary: '', error: '' };
  try {
    const values = parsePlatformSettingsDraft(schema, source || '{}');
    try {
      const normalizedJSON = serializePlatformSettings(schema, values, contentType);
      return { schema, values, normalizedJSON, summary: summarizePlatformSettings(schema, normalizedJSON, contentType), error: '' };
    } catch (reason) {
      return {
        schema,
        values,
        normalizedJSON: serializePlatformSettingsDraft(schema, values, contentType),
        summary: '',
        error: reason instanceof Error ? reason.message : '平台参数不可用',
      };
    }
  } catch (reason) {
    return { schema, values: null, normalizedJSON: '', summary: '', error: reason instanceof Error ? reason.message : '平台参数不可用' };
  }
}

function platformLabel(code: string) {
  const labels: Record<string, string> = {
    xhs: '小红书', douyin: '抖音', kuaishou: '快手', wechat_video: '视频号',
    wechat_mp: '微信公众号', csdn: 'CSDN', tiktok: 'TikTok', youtube: 'YouTube',
    x: 'X', facebook: 'Facebook', instagram: 'Instagram', bilibili: '哔哩哔哩',
  };
  return labels[code] || code;
}

function publishCapabilityFor(manifest: EgoPublishManifest | undefined, contentType: ContentType) {
	return manifest?.capabilities.find((capability) => capability.content_type === contentType);
}

function supportsPublishContentType(manifest: EgoPublishManifest | undefined, contentType: ContentType) {
	return Boolean(publishCapabilityFor(manifest, contentType));
}

function isAutomatedPrepare(manifest: EgoPublishManifest | undefined, contentType: ContentType) {
	const capability = publishCapabilityFor(manifest, contentType);
	return capability?.prepare_mode === 'automated' && capability.prepare_adapter_supported;
}

/**
 * 该形态是否已经通过真实创作者后台验收、允许自动点击最终提交。
 *
 * 未验收时后端会以 EGO_PUBLISH_SUBMIT_UNVERIFIED 拒绝——这是有意的
 * fail-closed 设计。但按钮能点、点了必然失败、原因还看不到，用户会以为
 * 功能坏了。所以这里在提交前就说清楚。
 */
function submitAcceptance(manifest: EgoPublishManifest | undefined, contentType: ContentType): 'verified' | 'unverified' | 'unsupported' {
	const capability = publishCapabilityFor(manifest, contentType);
	if (!capability) return 'unsupported';
	if (!capability.submit_adapter_supported) return 'unsupported';
	return capability.submit_verified ? 'verified' : 'unverified';
}

function publishCapabilityLabel(manifest: EgoPublishManifest | undefined, contentType: ContentType) {
	const capability = publishCapabilityFor(manifest, contentType);
	if (!capability) return '当前形态不可用';
	return capability.prepare_mode === 'automated' && capability.prepare_adapter_supported ? '自动预填 · 提交前确认' : '暂不支持自动发布';
}

function fileName(path: string) {
  return path.split(/[\\/]/).pop() || path;
}

function plannedAtLabel(value: string | undefined) {
  if (!value) return '立即填写';
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? '排期无效' : parsed.toLocaleString();
}

function studioHandoffTimeLabel(handoff: StudioScheduleHandoff) {
  return `${handoff.original_local_time.replace('T', ' ')} · ${handoff.timezone}`;
}

function nextNativePublishAt(now = Date.now()) {
  const value = new Date(now + nativeScheduleOffsetMs);
  value.setSeconds(0, 0);
  return value;
}

function tagsInputFromJSON(raw: string) {
	try {
		const decoded: unknown = JSON.parse(raw || '[]');
		return Array.isArray(decoded) ? decoded.filter((tag): tag is string => typeof tag === 'string').join(', ') : '';
	} catch {
		return '';
	}
}

function tagsJSONFromInput(raw: string) {
	const tags = raw.split(/[\n,，]+/).map((tag) => tag.trim()).filter(Boolean);
	return JSON.stringify([...new Set(tags)]);
}

function emptyOverride(content: Pick<ContentItem, 'title' | 'caption' | 'body' | 'tags_json'>): PlatformOverride {
  const body = content.body || content.caption;
  const savedTags = tagsInputFromJSON(content.tags_json);
  return { title: content.title, body, tags: savedTags || suggestPublishTags(content.title, body).join(', ') };
}

function contentFieldsForPlatform(platformCode: string): PlatformContentFields {
  return platformContentFields[platformCode] ?? {
    titleLabel: '标题',
    bodyLabel: '正文 / 描述',
    tagsLabel: '标签',
    tagsHelp: '可选，逗号或换行分隔',
    supportsTags: true,
  };
}

function contentTagsForPlatform(platformCode: string, raw: string) {
  return contentFieldsForPlatform(platformCode).supportsTags ? raw : '';
}

function settingsMemoryKey(platformCode: string, selectedAssets: readonly AccountAsset[]) {
  const accountIDs = [...new Set(selectedAssets
    .filter((asset) => asset.platform_code === platformCode)
    .map((asset) => asset.account_id))]
    .sort((left, right) => left - right);
  return `${platformCode}:${accountIDs.join(',')}`;
}

function variantIDFromToolResult(data: Record<string, unknown> | undefined) {
  const variant = data?.variant;
  if (!variant || typeof variant !== 'object' || Array.isArray(variant)) return 0;
  const id = Number((variant as Record<string, unknown>).id);
  return Number.isInteger(id) && id > 0 ? id : 0;
}

function hasPreparedPublishTarget(execution: PublishPlanExecution | null | undefined) {
  return Boolean(execution?.targets.some((target) => target.status === 'awaiting_review'
    && (target.step === 'ready_to_submit' || target.step === 'review_or_submit')));
}

function PlatformSettingsPanel({
  platformCode,
  contentType,
  settings,
  templates,
  selectedTemplateID,
  saving,
  allowTemplateSave,
  onTemplate,
  onChange,
  onSaveDefault,
}: {
  platformCode: string;
  contentType: ContentType;
  settings: ResolvedPlatformSettings;
  templates: PlatformTemplate[];
  selectedTemplateID: number;
  saving: boolean;
  allowTemplateSave: boolean;
  onTemplate: (id: number) => void;
  onChange: (key: string, value: string | boolean) => void;
  onSaveDefault: () => void;
}) {
  if (!settings.schema || !settings.values) return null;
  const evidence = settings.schema.verification === 'console_verified'
    ? `创作后台实机核对${settings.schema.verified_at ? ` · ${settings.schema.verified_at}` : ''}`
    : settings.schema.verification === 'contract_verified'
      ? `字段契约已核对${settings.schema.verified_at ? ` · ${settings.schema.verified_at}` : ''}，执行时会在当前页面二次确认`
      : '测试夹具，待实机核对';
  const visibleFields = settings.schema.fields.filter((field) => !field.content_types?.length || field.content_types.includes(contentType));
  const remembersOptionalFields = visibleFields.some((field) => field.type !== 'manual' && !field.required && field.remember);
  const fieldState = (field: PlatformSettingsSchema['fields'][number]) => field.required
    ? <span className="ml-1 text-red-500">必填</span>
    : field.type === 'manual'
      ? <span className="ml-1 font-normal text-brand-warning">平台页面确认</span>
      : field.remember
        ? <span className="ml-1 font-normal text-gray-400">选填 · 记住上次</span>
        : <span className="ml-1 font-normal text-gray-400">选填</span>;
  return (
    <div className="space-y-3 rounded-lg border border-brand-border bg-black/[0.02] p-3" data-testid={`publish-platform-settings-${platformCode}`}>
      {allowTemplateSave ? <div className="flex flex-wrap items-end gap-2">
          <label className="min-w-48 flex-1 text-xs font-semibold text-gray-700">常用选项模板
            <select data-testid={`publish-platform-template-${platformCode}`} value={selectedTemplateID} onChange={(event) => onTemplate(Number(event.target.value))} className="mt-1.5 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm font-normal">
              <option value={0}>使用上次选择</option>
              {templates.map((template) => <option key={template.id} value={template.id}>{template.name}{template.is_default ? '（默认）' : ''}</option>)}
            </select>
          </label>
          <button type="button" disabled={saving || Boolean(settings.error)} onClick={onSaveDefault} className="ui-btn shrink-0 disabled:opacity-40"><Save className="h-3.5 w-3.5" />{saving ? '保存中…' : '保存为默认模板'}</button>
        </div> : null}
      <div className="grid gap-3 sm:grid-cols-2">
        {visibleFields.map((field) => field.type === 'manual' ? (
          <div key={field.key} className="rounded-lg border border-brand-warning/30 bg-brand-warning/10/60 px-3 py-2 text-xs text-gray-700">
            <strong>{field.label}</strong>{fieldState(field)}
            {field.description ? <p className="mt-1 font-normal text-gray-500">{field.description}</p> : null}
          </div>
        ) : field.type === 'enum' ? (
          <label key={field.key} className="text-xs font-semibold text-gray-700">{field.label}{fieldState(field)}
            <select data-testid={`publish-settings-${platformCode}-${field.key}`} value={String(settings.values?.[field.key] ?? field.default)} onChange={(event) => onChange(field.key, event.target.value)} className="mt-1.5 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm font-normal">
              {field.options?.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
            </select>
            {field.description ? <span className="mt-1 block font-normal text-gray-400">{field.description}</span> : null}
          </label>
        ) : field.type === 'boolean' ? (
          <label key={field.key} className="self-end rounded-lg border border-brand-border px-3 py-2.5 text-sm text-gray-700">
            <span className="flex items-center gap-2"><input data-testid={`publish-settings-${platformCode}-${field.key}`} type="checkbox" checked={Boolean(settings.values?.[field.key])} onChange={(event) => onChange(field.key, event.target.checked)} />{field.label}{fieldState(field)}</span>
            {field.description ? <span className="mt-1 block text-[11px] text-gray-400">{field.description}</span> : null}
          </label>
        ) : (
          <label key={field.key} className="text-xs font-semibold text-gray-700">{field.label}{fieldState(field)}
            <input required={field.required} aria-required={field.required} data-testid={`publish-settings-${platformCode}-${field.key}`} type="text" maxLength={128} value={String(settings.values?.[field.key] ?? field.default ?? '')} onChange={(event) => onChange(field.key, event.target.value)} placeholder={platformSettingInputHint(field).placeholder || (field.required ? '必填' : '可留空')} className="mt-1.5 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm font-normal" />
            {field.description ? <span className="mt-1 block font-normal text-gray-400">{field.description}</span> : null}
            {platformSettingInputHint(field).help ? <span data-testid={`publish-settings-${platformCode}-${field.key}-match-help`} className="mt-1 block font-normal text-gray-400">{platformSettingInputHint(field).help}</span> : null}
          </label>
        ))}
      </div>
      <p className="text-xs text-gray-500">{settings.error || settings.summary}{remembersOptionalFields ? ' · 选填项按账号记住上次选择' : ''}</p>
      <p data-testid={`publish-platform-schema-evidence-${platformCode}`} className="text-[11px] text-gray-400">参数依据：{evidence} · 规则版本 {settings.schema.version}</p>
    </div>
  );
}

export function PublishView({ embedded = false, initialContentID = 0, initialAccountID = 0 }: { embedded?: boolean; initialContentID?: number; initialAccountID?: number }) {
  const { toast, setActiveTab } = useUIStore();
  const persisted = useMemo(loadDefaults, []);
  const { data: contents } = useSWR('api/contents', api.getContents);
  const { data: studioScheduleHandoffs = [] } = useSWR('api/studio/schedule-handoffs', api.getStudioScheduleHandoffs);
  const { data: assets, error: assetsError } = useSWR('api/account-assets', api.getAccountAssets, { refreshInterval: 5_000, refreshWhenHidden: false });
  const { data: accounts, mutate: mutateAccounts, error: accountsError } = useSWR('api/accounts', api.getAccounts, { refreshInterval: 5_000, refreshWhenHidden: false });
  const { data: egoSpaces, mutate: mutateEgoSpaces, error: spacesError } = useSWR('api/ego/spaces', api.getEgoSpaces, { refreshInterval: 5_000, refreshWhenHidden: false });
  const { data: accountTags } = useSWR('api/account-tags', api.getAccountTags);
  const { data: platforms } = useSWR('api/platforms', api.getPlatforms);
  const { data: egoPublishManifests } = useSWR('api/ego/publish-manifests', api.getEgoPublishManifests);
  const { data: templates, mutate: mutateTemplates } = useSWR('api/templates', api.getTemplates);
  const [step, setStep] = useState<PublishStep>('accounts');
  const [family, setFamily] = useState<PublishFamily>(persisted.family ?? 'video');
  const [assetIDs, setAssetIDs] = useState<number[]>(persisted.assetIds ?? []);
  const [publishTagIDs, setPublishTagIDs] = useState<number[]>([]);
  const [contentID, setContentID] = useState(initialContentID);
  const [handoffId, setHandoffId] = useState(0);
  const [platformOverrides, setPlatformOverrides] = useState<Record<string, PlatformOverride>>({});
  const [activePlatformCode, setActivePlatformCode] = useState('');
  const [platformSettingsJSON, setPlatformSettingsJSON] = useState<Record<string, string>>({
    ...(persisted.settingsByPlatform ?? {}),
    ...(persisted.settingsByTarget ?? {}),
  });
  const [templateByPlatform, setTemplateByPlatform] = useState<Record<string, number>>(persisted.templateByPlatform ?? {});
  const [savingTemplate, setSavingTemplate] = useState('');
  const [running, setRunning] = useState(false);
  const [frozenPlan, setFrozenPlan] = useState<PublishPlan | null>(null);
  const [planExecution, setPlanExecution] = useState<PublishPlanExecution | null>(null);
  const previousSelectedAssetKey = useRef<string | null>(null);
  // 首次渲染时保留外部预选的内容/账号；family 变化时才重置，避免挂载即清空交接预选。
  const familyEffectMounted = useRef(false);
  const initialAccountApplied = useRef(false);

  const clearFrozenPlan = () => {
    setFrozenPlan(null);
    setPlanExecution(null);
  };

  // Normal platforms freeze against their browser session. Meta freezes the
  // explicit Page / Instagram target selected inside that owning session.
	const publishableAssets = useMemo(() => assets?.filter(isControlledPrepareAccountAsset) ?? [], [assets]);
	const accountsByID = useMemo(() => new Map((accounts ?? []).map((account) => [account.id, account])), [accounts]);
	const publishManifestByPlatform = useMemo(
		() => new Map((egoPublishManifests ?? []).map((manifest) => [manifest.platform_code, manifest])),
		[egoPublishManifests],
	);
	const firstReleaseManifests = useMemo(
		() => (egoPublishManifests ?? []).filter((manifest) => manifest.first_release),
		[egoPublishManifests],
	);
	const familyCodes = useMemo(() => new Set(firstReleaseManifests
		.filter((manifest) => supportsPublishContentType(manifest, family))
		.map((manifest) => manifest.platform_code)), [family, firstReleaseManifests]);
	const familyAssets = useMemo(() => publishableAssets.filter((asset) => familyCodes.has(asset.platform_code)), [familyCodes, publishableAssets]);
	const controlledFamilyAssets = useMemo(() => accounts === undefined ? [] : familyAssets.filter((asset) => (
		accountsByID.get(asset.account_id)?.enabled !== false
	)), [accounts, accountsByID, familyAssets]);
  const nonMetaControlledFamilyAssets = useMemo(
    () => controlledFamilyAssets.filter((asset) => asset.platform_code !== 'facebook' && asset.platform_code !== 'instagram'),
    [controlledFamilyAssets],
  );
  const metaPublishGroups = useMemo(
    () => familyCodes.has('facebook') || familyCodes.has('instagram')
      ? buildMetaPublishGroups(accounts ?? [], assets ?? [])
      : [],
    [accounts, assets, familyCodes],
  );
  const availableIdentityIDs = useMemo(() => new Set((egoSpaces ?? [])
    .filter(isEgoSpaceAvailableForExplicitPublish)
    .map((space) => space.identity_id)), [egoSpaces]);
  const egoSpaceByIdentity = useMemo(
    () => new Map((egoSpaces ?? []).map((space) => [space.identity_id, space])),
    [egoSpaces],
  );
  const readyAccountIDs = useMemo(() => new Set((accounts ?? [])
    .filter((account) => account.enabled && account.loginState === 'logged_in' && availableIdentityIDs.has(account.identityId))
    .map((account) => account.id)), [accounts, availableIdentityIDs]);
  const selectableMetaGroups = useMemo(() => metaPublishGroups.filter((group) => {
    const targets = [...group.targets.facebook.assets, ...group.targets.instagram.assets];
    return group.targets.facebook.assets.length > 0
      && group.targets.instagram.assets.length > 0
      && targets.every((asset) => readyAccountIDs.has(asset.account_id));
  }), [metaPublishGroups, readyAccountIDs]);
  const selectableFamilyAssets = useMemo(() => [
    ...nonMetaControlledFamilyAssets.filter((asset) => readyAccountIDs.has(asset.account_id)),
    ...selectableMetaGroups.flatMap((group) => [...group.targets.facebook.assets, ...group.targets.instagram.assets]),
  ], [nonMetaControlledFamilyAssets, readyAccountIDs, selectableMetaGroups]);
  const selectedAssets = useMemo(() => selectableFamilyAssets.filter((asset) => assetIDs.includes(asset.id)), [assetIDs, selectableFamilyAssets]);
  const selectedAssetKey = selectedAssets.map((asset) => asset.id).sort((left, right) => left - right).join(',');
  const selectedPlatformCodes = useMemo(() => [...new Set(selectedAssets.map((asset) => asset.platform_code))], [selectedAssets]);
  const settingsMemoryKeys = useMemo(() => Object.fromEntries(selectedPlatformCodes.map((code) => [code, settingsMemoryKey(code, selectedAssets)])), [selectedAssets, selectedPlatformCodes]);
  const activeCode = activePlatformCode && selectedPlatformCodes.includes(activePlatformCode) ? activePlatformCode : (selectedPlatformCodes[0] ?? '');
  const familyContents = useMemo(() => (contents ?? []).filter((item) => item.type === family), [contents, family]);
	const compatibleContents = useMemo(() => familyContents.filter((item) => selectedAssets.length > 0 && selectedAssets.every((asset) => supportsPublishContentType(publishManifestByPlatform.get(asset.platform_code), item.type))), [familyContents, publishManifestByPlatform, selectedAssets]);
  const readyStudioHandoffs = useMemo(() => studioScheduleHandoffs.filter((handoff) => handoff.status === 'ready'
    && handoff.media_mode === family
    && compatibleContents.some((content) => content.id === handoff.content_id)), [compatibleContents, family, studioScheduleHandoffs]);
  const selectedStudioHandoff = useMemo(() => readyStudioHandoffs.find((handoff) => handoff.id === handoffId) ?? null, [handoffId, readyStudioHandoffs]);
  const studioHandoffSelectionInvalid = handoffId > 0 && !selectedStudioHandoff;
  const selectedContent = useMemo(() => compatibleContents.find((content) => content.id === contentID), [compatibleContents, contentID]);
  const contentType: ContentType = selectedStudioHandoff?.media_mode ?? selectedContent?.type ?? family;
  const usesNativeSchedule = Boolean(selectedStudioHandoff)
    || contentType !== 'article'
    || (selectedPlatformCodes.length > 0 && selectedPlatformCodes.every((code) => code === 'wechat_mp'));
  const plannedPublishAtPreview = selectedStudioHandoff?.planned_at_utc ?? (usesNativeSchedule ? nextNativePublishAt().toISOString() : '');
  const studioHandoffScheduleVerified = Boolean(selectedStudioHandoff) && selectedAssets.every((asset) => (
    publishCapabilityFor(publishManifestByPlatform.get(asset.platform_code), contentType)?.native_schedule_verified === true
  ));
  const { data: attachedMedia, error: attachedMediaError } = useSWR(contentID ? `api/contents/${contentID}/media` : null, () => api.getContentMedia(contentID));
  const settingsByPlatform = useMemo<Record<string, ResolvedPlatformSettings>>(() => Object.fromEntries(selectedPlatformCodes.map((platformCode) => {
    const platform = platforms?.find((item) => item.code === platformCode);
    const selectedTemplate = templates?.find((item) => item.id === templateByPlatform[platformCode] && item.platform_code === platformCode && item.enabled);
    const schema = platform?.settingsSchema;
    return [platformCode, resolvePlatformSettings(schema, platformSettingsJSON[settingsMemoryKeys[platformCode]] || platformSettingsJSON[platformCode] || selectedTemplate?.settings_json || '{}', contentType)];
  })), [contentType, platformSettingsJSON, platforms, selectedPlatformCodes, settingsMemoryKeys, templateByPlatform, templates]);
  const settingsError = Object.values(settingsByPlatform).find((item) => item.error)?.error ?? '';
  const requiredMediaReady = contentType === 'article'
    || (contentType === 'video' ? Boolean(attachedMedia?.some((item) => item.kind === 'video')) : Boolean(attachedMedia?.some((item) => item.kind === 'image')));
  const mediaLoading = Boolean(selectedContent) && attachedMedia === undefined && !attachedMediaError;
  useEffect(() => {
    saveDefaults({ family, settingsByPlatform: persisted.settingsByPlatform, settingsByTarget: platformSettingsJSON, templateByPlatform });
  }, [assetIDs, family, persisted.settingsByPlatform, platformSettingsJSON, templateByPlatform]);

  useEffect(() => {
    // A manual login recheck revalidates SWR's account list. Do not clear a
    // user's selected account during that transient loading state.
    if (!assets || accounts === undefined) return;
    const selectableIDs = new Set(selectableFamilyAssets.map((asset) => asset.id));
    const normalIDs = new Set(nonMetaControlledFamilyAssets.map((asset) => asset.id));
    setAssetIDs((current) => {
      const next = current.filter((id) => selectableIDs.has(id) && normalIDs.has(id));
      for (const group of selectableMetaGroups) {
        const ids = [...group.targets.facebook.assets, ...group.targets.instagram.assets].map((asset) => asset.id);
        if (ids.every((id) => current.includes(id))) next.push(...ids);
      }
      return [...new Set(next)];
    });
  }, [accounts, assets, nonMetaControlledFamilyAssets, selectableFamilyAssets, selectableMetaGroups]);

  useEffect(() => {
    // 手动交接：等可选资产首次加载后按来源账号预选一次目标；账号不可选时不覆盖既有选择。
    if (initialAccountApplied.current || !initialAccountID || !selectableFamilyAssets.length) return;
    initialAccountApplied.current = true;
    const matching = selectableFamilyAssets.filter((asset) => asset.account_id === initialAccountID).map((asset) => asset.id);
    if (matching.length) setAssetIDs(matching);
  }, [initialAccountID, selectableFamilyAssets]);

  useEffect(() => {
    const previous = previousSelectedAssetKey.current;
    previousSelectedAssetKey.current = selectedAssetKey;
    if (previous === null || previous === selectedAssetKey || (!frozenPlan && !planExecution)) return;
    setFrozenPlan(null);
    setPlanExecution(null);
    setStep('accounts');
    toast.show('账号选择已变化，请重新确认后再填入平台', 'info');
  }, [frozenPlan, planExecution, selectedAssetKey, toast]);

  useEffect(() => {
    // 挂载时不动：contentID 已由 useState(initialContentID) 预置（手动交接预选），
    // 只有用户主动切换 family 时才重置内容与平台设置。
    if (!familyEffectMounted.current) {
      familyEffectMounted.current = true;
      return;
    }
    setContentID(0);
    setPlatformOverrides({});
    setActivePlatformCode('');
    setFrozenPlan(null);
    setPlanExecution(null);
  }, [family]);

  useEffect(() => {
    if (initialContentID && compatibleContents.some((content) => content.id === initialContentID) && contentID === 0) setContentID(initialContentID);
  }, [compatibleContents, contentID, initialContentID]);

	useEffect(() => {
		if (!assets || !contents || !contentID || !selectedAssets.length || compatibleContents.some((content) => content.id === contentID)) return;
		setContentID(0);
		setPlatformOverrides({});
		setActivePlatformCode('');
		setFrozenPlan(null);
		setPlanExecution(null);
	}, [assets, compatibleContents, contentID, contents, selectedAssets.length]);

  useEffect(() => {
    if (!selectedContent) {
      setPlatformOverrides({});
      return;
    }
    setPlatformOverrides((current) => {
      const next = { ...current };
      for (const code of selectedPlatformCodes) {
        if (!next[code]) {
          const original = emptyOverride(selectedContent);
          next[code] = { ...original, tags: contentTagsForPlatform(code, original.tags) };
        }
      }
      return next;
    });
  }, [selectedContent, selectedPlatformCodes]);

  useEffect(() => {
    if (activePlatformCode && selectedPlatformCodes.includes(activePlatformCode)) return;
    setActivePlatformCode(selectedPlatformCodes[0] ?? '');
  }, [activePlatformCode, selectedPlatformCodes]);

  useEffect(() => {
    for (const code of selectedPlatformCodes) {
      const schema = platforms?.find((item) => item.code === code)?.settingsSchema;
      const memoryKey = settingsMemoryKeys[code];
      if (!schema || platformSettingsJSON[memoryKey]) continue;
      const selected = templates?.find((item) => item.id === templateByPlatform[code] && item.platform_code === code && item.enabled)
        ?? templates?.find((item) => item.platform_code === code && item.enabled && item.is_default);
      const normalized = serializePlatformSettingsDraft(
        schema,
        parsePlatformSettingsDraft(schema, withoutRememberedOptionalDeclarations(selected?.settings_json || '{}')),
        contentType,
      );
      setPlatformSettingsJSON((current) => current[memoryKey] ? current : { ...current, [memoryKey]: normalized });
      if (selected && !templateByPlatform[code]) setTemplateByPlatform((current) => ({ ...current, [code]: selected.id }));
    }
  }, [platformSettingsJSON, platforms, selectedPlatformCodes, settingsMemoryKeys, templateByPlatform, templates]);

  const toggleAsset = (asset: AccountAsset) => {
	if (!supportsPublishContentType(publishManifestByPlatform.get(asset.platform_code), family) || !readyAccountIDs.has(asset.account_id)) return;
    setAssetIDs((current) => current.includes(asset.id) ? current.filter((id) => id !== asset.id) : [...current, asset.id]);
    clearFrozenPlan();
  };

  const toggleMetaGroup = (group: (typeof metaPublishGroups)[number]) => {
    const ids = [...group.targets.facebook.assets, ...group.targets.instagram.assets].map((asset) => asset.id);
    if (!ids.length || !ids.every((id) => selectableFamilyAssets.some((asset) => asset.id === id))) return;
    setAssetIDs((current) => ids.every((id) => current.includes(id))
      ? current.filter((id) => !ids.includes(id))
      : [...new Set([...current, ...ids])]);
    clearFrozenPlan();
  };

  const selectAllFamilyAccounts = () => {
    setAssetIDs(selectableFamilyAssets.map((asset) => asset.id));
    clearFrozenPlan();
  };

  const selectAccountsByTags = async () => {
    if (!publishTagIDs.length) {
      toast.show('请先选择账号标签', 'error');
      return;
    }
    try {
      const result = await api.resolveAccountTags(publishTagIDs);
      const matchingAccounts = new Set(result.accounts.map((account) => account.id));
      const matchingAssets = selectableFamilyAssets.filter((asset) => matchingAccounts.has(asset.account_id));
      setAssetIDs(matchingAssets.map((asset) => asset.id));
      clearFrozenPlan();
      toast.show(`当前分类已选中 ${matchingAssets.length} 个标签匹配账号`, 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '按标签选择账号失败', 'error');
    }
  };

  const chooseFamily = (next: PublishFamily) => {
    setFamily(next);
    setStep('accounts');
    setContentID(0);
    setHandoffId(0);
    setPlatformOverrides({});
    setActivePlatformCode('');
    clearFrozenPlan();
  };

  const chooseContent = (id: number) => {
    setContentID(id);
    setHandoffId(0);
    setPlatformOverrides({});
    clearFrozenPlan();
  };

  const chooseStudioHandoff = (handoff: StudioScheduleHandoff) => {
    setContentID(handoff.content_id);
    setHandoffId(handoff.id);
    setPlatformOverrides({});
    clearFrozenPlan();
  };

  const updateOverride = (platformCode: string, patch: Partial<PlatformOverride>) => {
    if (selectedStudioHandoff) return;
    setPlatformOverrides((current) => ({
      ...current,
      [platformCode]: { ...(current[platformCode] ?? emptyOverride(selectedContent ?? { title: '', caption: '', body: '', tags_json: '[]' })), ...patch },
    }));
    clearFrozenPlan();
  };

  const updatePlatformSetting = (platformCode: string, key: string, value: string | boolean) => {
    const settings = settingsByPlatform[platformCode];
    if (!settings?.schema || !settings.values) return;
    const memoryKey = settingsMemoryKeys[platformCode] || platformCode;
    setPlatformSettingsJSON((current) => ({ ...current, [memoryKey]: serializePlatformSettingsDraft(settings.schema!, { ...settings.values, [key]: value }) }));
    clearFrozenPlan();
  };

  const selectTemplate = (platformCode: string, id: number) => {
    const schema = platforms?.find((item) => item.code === platformCode)?.settingsSchema;
    const template = templates?.find((item) => item.id === id && item.platform_code === platformCode && item.enabled);
    setTemplateByPlatform((current) => ({ ...current, [platformCode]: id }));
    if (schema) {
      const memoryKey = settingsMemoryKeys[platformCode] || platformCode;
      setPlatformSettingsJSON((current) => ({ ...current, [memoryKey]: serializePlatformSettingsDraft(schema, parsePlatformSettingsDraft(schema, template?.settings_json || current[memoryKey] || current[platformCode] || '{}')) }));
    }
    clearFrozenPlan();
  };

  const saveDefaultTemplate = async (platformCode: string) => {
    if (selectedStudioHandoff) return;
    const settings = settingsByPlatform[platformCode];
    if (!settings?.schema || settings.error) return;
    setSavingTemplate(platformCode);
    try {
      const existing = templates?.find((item) => item.platform_code === platformCode && item.is_default);
      const bodyValue = {
        platform_code: platformCode,
        name: existing?.name || '一键发布默认',
        description: existing?.description || '一键发布常用选项',
        settings_json: settings.normalizedJSON,
        enabled: true,
        is_default: true,
      };
      const saved = existing ? await api.updateTemplate(existing.id, bodyValue) : await api.createTemplate(bodyValue);
      setTemplateByPlatform((current) => ({ ...current, [platformCode]: saved.id }));
      await mutateTemplates();
      toast.show(`${platformLabel(platformCode)}常用选项已保存为默认模板`, 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '保存模板失败', 'error');
    } finally {
      setSavingTemplate('');
    }
  };

  const validateContent = (): { message: string; platformCode?: string; settings?: boolean } => {
    if (!selectedContent) return { message: '请选择已保存的内容' };
    if (selectedStudioHandoff) {
      if (attachedMediaError) return { message: '无法读取内容关联素材，请先到“内容库”检查' };
      if (settingsError) {
        const platformCode = selectedPlatformCodes.find((code) => settingsByPlatform[code]?.error);
        return { message: settingsError, platformCode, settings: true };
      }
      return { message: '' };
    }
    if (!selectedContent.title.trim()) return { message: '内容标题不能为空' };
    if (!(selectedContent.body || selectedContent.caption).trim()) return { message: '内容正文不能为空' };
    const mixedArticleSchedule = contentType === 'article'
      && selectedPlatformCodes.includes('wechat_mp')
      && selectedPlatformCodes.some((code) => code !== 'wechat_mp');
    if (mixedArticleSchedule) return { message: '公众号定时发表与其他文章平台需分开发布' };
    const plannedAtError = selectedStudioHandoff ? '' : usesNativeSchedule ? validatePlannedPublishAt(nextNativePublishAt().toISOString()) : '';
    if (plannedAtError) return { message: plannedAtError };
		const unsupportedTarget = selectedAssets.find((asset) => !supportsPublishContentType(publishManifestByPlatform.get(asset.platform_code), contentType));
		if (unsupportedTarget) return { message: `${platformLabel(unsupportedTarget.platform_code)}不支持当前内容类型的填写` };
    if (attachedMediaError) return { message: '无法读取内容关联素材，请先到“内容库”检查' };
    if (contentType === 'video' && !requiredMediaReady) return { message: '这条视频内容尚未关联视频，请先到“内容库”关联素材' };
    if (contentType === 'graph_text' && !requiredMediaReady) return { message: '这条图文内容尚未关联图片，请先到“内容库”关联素材' };
    for (const code of selectedPlatformCodes) {
      const override = platformOverrides[code];
      if (!override) return { message: `${platformLabel(code)}：内容尚未准备好`, platformCode: code };
      const platformTitle = override.title;
      const platformBody = override.body;
      const platformTags = override.tags.split(/[\n,，]+/).map((tag) => tag.trim()).filter(Boolean);
      const error = validatePlatformContent(contentRuleFor(code, contentType), platformTitle, platformBody, platformTags);
      if (error) return { message: `${platformLabel(code)}：${error}`, platformCode: code };
    }
    if (settingsError) {
      const platformCode = selectedPlatformCodes.find((code) => settingsByPlatform[code]?.error);
      return { message: settingsError, platformCode, settings: true };
    }
    return { message: '' };
  };

  const saveContentAndFreeze = async () => {
    if (frozenPlan?.status === 'frozen') {
      setStep('review');
      return;
    }
    if (studioHandoffSelectionInvalid) {
      toast.show('Studio 交接已失效；请返回 Studio 检查排期后重新选择。', 'error');
      return;
    }
    const validation = validateContent();
    if (validation.message) {
      if (validation.platformCode) {
        setActivePlatformCode(validation.platformCode);
      }
      toast.show(validation.message, 'error');
      return;
    }
    setRunning(true);
    try {
      if (selectedStudioHandoff) {
        if (!studioHandoffScheduleVerified) {
          toast.show('PENDING VERIFICATION · 当前平台原生排期尚未验收；未打开任何 Space。', 'error');
          return;
        }
        const plan = await api.freezePublishPlanHandoff({
          handoffId: selectedStudioHandoff.id,
		concurrency: 1,
          targets: selectedAssets.map((asset) => ({
            account_id: asset.account_id,
            ...(asset.asset_type !== 'browser_account' ? { asset_id: asset.id } : {}),
            settings_json: settingsByPlatform[asset.platform_code]?.normalizedJSON || '{}',
          })),
        });
        setFrozenPlan(plan);
        setPlanExecution(null);
        setStep('review');
        toast.show('Studio 交接已冻结；下一步按平台原生排期准备。', 'success');
        return;
      }
      const selected = selectedContent!;
      // CSDN remains immediate. WeChat MP articles and visual posts use the
      // reviewed native 24-hour schedule, filled only by this explicit action.
      const publishAt = usesNativeSchedule ? nextNativePublishAt() : null;
      const saved = await api.updateContent(selected.id, {
        type: selected.type,
        title: selected.title,
        caption: selected.caption,
        body: selected.body,
        tags_json: selected.tags_json,
        status: selected.status,
        planned_at: publishAt?.toISOString() || '',
      });
      const media = await api.getContentMedia(saved.id);

      let mediaIds: number[] = [];
      let coverMediaId = 0;
      if (contentType === 'video') {
        const video = media.find((item) => item.kind === 'video');
        if (!video) throw new Error('视频发布需要选择一个本地视频');
        mediaIds = [video.id];
        coverMediaId = media.find((item) => item.kind === 'image')?.id ?? 0;
      } else {
        mediaIds = media.filter((item) => item.kind === 'image').slice(0, 9).map((item) => item.id);
        if (contentType === 'graph_text' && !mediaIds.length) throw new Error('图文发布需要至少一张本地图片');
      }

      const variantIDs: Record<string, number> = {};
      for (const code of selectedPlatformCodes) {
        const override = platformOverrides[code];
        if (!override) throw new Error(`${platformLabel(code)}内容尚未准备好`);
        const response = await api.callTool('content.variant.upsert', {
          content_id: saved.id,
          platform: code,
          title: override.title,
					caption: '',
          body: override.body,
						tags_json: tagsJSONFromInput(contentTagsForPlatform(code, override.tags)),
        });
        if (!response.result.ok) throw new Error(response.result.error || `${platformLabel(code)}内容版本保存失败`);
        const variantID = variantIDFromToolResult(response.result.data);
        if (!variantID) throw new Error(`${platformLabel(code)}内容版本未返回有效 ID`);
        variantIDs[code] = variantID;
      }

      const plan = await api.createPublishPlan({
        contentId: saved.id,
        mediaMode: contentType,
        mediaIds,
        ...(coverMediaId ? { coverMediaId } : {}),
		concurrency: 1,
        targets: selectedAssets.map((asset) => ({
          account_id: asset.account_id,
          ...(asset.asset_type !== 'browser_account' ? { asset_id: asset.id } : {}),
          ...(variantIDs[asset.platform_code] ? { variant_id: variantIDs[asset.platform_code] } : {}),
          settings_json: settingsByPlatform[asset.platform_code]?.normalizedJSON || '{}',
        })),
      });
      const expectedPublishAt = saved.planned_at || '';
      const frozenPublishTimeMatches = (plan.snapshot.content.planned_at || '') === expectedPublishAt
        && plan.snapshot.targets.every((target) => (target.publish_at || '') === expectedPublishAt);
      if (!frozenPublishTimeMatches) {
        throw new Error('冻结计划的发布时间与当前内容不一致，请重新进入发布页后再试');
      }
      setFrozenPlan(plan);
      setPlanExecution(null);
      setStep('review');
      toast.show('冻结清单已生成；下一步将按平台逐个串行预填', 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '准备发布内容失败', 'error');
    } finally {
      setRunning(false);
    }
  };

  const prepareFrozenPlan = async () => {
    if (!frozenPlan) return;
    setRunning(true);
    try {
      const prepareExecution = await prepareFrozenPublishPlan<PublishPlanExecution>(api, frozenPlan);
      const readyForSubmit = hasPreparedPublishTarget(prepareExecution);
      setPlanExecution(prepareExecution);
      setFrozenPlan(await api.getPublishPlan(frozenPlan.id));
      if (!readyForSubmit) {
        const loginPlatforms = [...new Set(prepareExecution.targets
          .filter((target) => target.errorCode === 'LOGIN_REQUIRED')
          .map((target) => platformLabel(target.platformCode)))];
        if (loginPlatforms.length) {
          toast.show(`需要登录：${loginPlatforms.join('、')}；已通知且未执行最终发布`, 'error');
        } else {
          toast.show('平台自动准备失败，已保存原因并通知；未执行最终发布', 'error');
        }
        return;
      }
      toast.show('平台表单已逐个预填；确认冻结批次后将逐个平台提交一次并自动核验', 'info');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '平台表单预填失败；未执行最终发布', 'error');
    } finally {
      setRunning(false);
    }
  };

  const resumeSpaceForPrepare = async (account: Account) => {
    setRunning(true);
    try {
      await api.resumeEgoSpace(account.id);
      await Promise.all([mutateAccounts(), mutateEgoSpaces()]);
      toast.show('浏览器已交还给自动准备；内容仍需预填后由你确认提交', 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '无法交还浏览器控制权', 'error');
    } finally {
      setRunning(false);
    }
  };

  const submitPreparedPlan = async () => {
    if (!frozenPlan || !hasPreparedPublishTarget(planExecution)) return;
    setRunning(true);
    try {
      const execution = await submitPreparedPublishPlan<PublishPlanExecution>(api, frozenPlan);
      setPlanExecution(execution);
      setFrozenPlan(await api.getPublishPlan(frozenPlan.id));
      await mutateAccounts();
      const platformVerificationRequired = execution.targets.some(
        (target) => target.errorCode === 'LOGIN_REQUIRED',
      );
      if (platformVerificationRequired) {
        const verificationPlatforms = [...new Set(execution.targets
          .filter((target) => target.errorCode === 'LOGIN_REQUIRED')
          .map((target) => platformLabel(target.platformCode)))].join('、');
        toast.show(`需要登录：${verificationPlatforms}；请从账号页完成验证后再发起新的发布`, 'error');
      } else if (['completed', 'published', 'published_verified', 'scheduled_verified'].includes(execution.status)) {
        toast.show('创作者后台已确认发布', 'success');
      } else if (
        execution.status === 'submitted_pending_verification'
        || execution.status === 'awaiting_verification'
        || execution.status === 'awaiting_review'
      ) {
        toast.show('已提交一次，正在等待创作者后台核验', 'info');
      } else if (execution.status === 'result_unknown') {
        toast.show('已点击一次，但结果证据不足；已禁止自动重试', 'error');
      } else {
        toast.show('未能安全完成发布；没有证据时不会重复点击', 'error');
      }
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '真实发布失败；请在发布记录查看原因', 'error');
    } finally {
      setRunning(false);
    }
  };

  const verifyFrozenPlanManager = async (target?: { accountId: number; platformCode: string }) => {
    if (!frozenPlan) return;
    setRunning(true);
    try {
      const verification = await api.verifyPublishPlanManager(frozenPlan.id, target);
      const refreshed = await api.getPublishPlan(frozenPlan.id);
      setFrozenPlan(refreshed);
      if (verification.publish_record) {
        setPlanExecution((current) => current ? {
          ...current,
          status: refreshed.status,
          targets: current.targets.map((target) => ({
            ...target,
            ...(target.accountId === verification.inspection.accountId && target.platformCode === verification.inspection.platformCode ? {
              status: 'published_verified', step: 'creator_backend_verified', errorCode: undefined,
            } : {}),
          })),
        } : current);
        toast.show('创作者后台已确认发布', 'success');
      } else {
        toast.show('后台暂未找到标题和已发布状态完全匹配的作品；没有再次提交', 'info');
      }
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '后台核验失败；没有再次提交', 'error');
    } finally {
      setRunning(false);
    }
  };

  const canContinueAccounts = selectedAssets.length > 0;
  const canRetryUnsubmitted = Boolean(planExecution) && Boolean(frozenPlan?.targetStates?.some((target) =>
    target.errorCode !== 'PLATFORM_VERIFICATION_REQUIRED'
      && !target.submitAttempted
      && (target.status === 'failed' || target.status === 'cancelled')));
  const queuedTargets = frozenPlan?.targetStates?.filter((target) => target.status === 'queued') ?? [];
  const queueBlocked = Boolean(frozenPlan?.targetStates?.some((target) =>
    !['queued', 'published', 'skipped', 'failed', 'cancelled'].includes(target.status)));
  const canPrepareQueued = queuedTargets.length > 0 && !queueBlocked;
  const preparedForSubmit = hasPreparedPublishTarget(planExecution);
  const manualTakeoverActive = Boolean(planExecution?.targets.some((target) => target.status === 'manual_takeover'));
  const verificationTargets = planExecution?.targets.filter(
    (target) => target.errorCode === 'LOGIN_REQUIRED',
  ) ?? [];
  const platformVerificationRequired = verificationTargets.length > 0;
  const verificationPlatforms = [...new Set(verificationTargets.map((target) => platformLabel(target.platformCode)))].join('、');
	const managerVerificationTargets = (frozenPlan?.snapshot.targets ?? []).filter((target) => {
		const state = frozenPlan?.targetStates?.find((item) => item.accountId === target.account_id && item.platformCode === target.platform_code);
		return Boolean(state?.submitAttempted && (state.status === 'submitted_pending_verification' || state.status === 'result_unknown'))
			&& ['douyin', 'csdn', 'tiktok', 'youtube', 'kuaishou', 'wechat_video', 'facebook'].includes(target.platform_code);
	});
	const prepareLabel = running ? '正在预填…'
		: canRetryUnsubmitted ? '重试未提交账号'
		: canPrepareQueued ? `继续串行预填（${queuedTargets.length} 个历史排队目标）`
		: '预填并打开平台';
	const submitLabel = running ? '正在提交…'
		: preparedForSubmit ? '确认并提交一次'
		: manualTakeoverActive ? '历史人工状态（仅兼容旧记录）'
		: platformVerificationRequired ? `需要登录：${verificationPlatforms}`
		: ['completed', 'published', 'published_verified', 'scheduled_verified'].includes(planExecution?.status || '') ? '创作者后台已确认发布'
		: planExecution?.status === 'submitted_pending_verification' || planExecution?.status === 'awaiting_verification' || planExecution?.status === 'awaiting_review' ? '已提交，等待核验'
		: planExecution?.status === 'result_unknown' ? '结果待核验，禁止重试'
		: '发布未完成';
	const stepMeta: ReadonlyArray<{ id: PublishStep; label: string; testID: string; note: string }> = [
		{ id: 'accounts', label: '选账号', testID: 'publish-step-accounts', note: selectedAssets.length ? `已选 ${selectedAssets.length} 个目标` : '选择发布目标' },
		{ id: 'content', label: '填内容', testID: 'publish-step-content', note: selectedContent ? '内容已选，逐平台核对' : '选择并核对内容' },
		{ id: 'review', label: '发布', testID: 'publish-step-review', note: frozenPlan ? '计划已冻结，等待操作' : '冻结后才可执行' },
	];
	const targetCount = frozenPlan?.snapshot.targets.length ?? selectedAssets.length;

  return (
    <section className="mx-auto w-full max-w-[88rem] space-y-5">
      {!embedded ? <div className="border-b border-brand-border pb-4"><h3 className="ui-page-title text-lg">一键发布</h3><p className="ui-page-sub">选账号 → 冻结内容 → EgoLite 打开对应创作页 → 仅在独立确认后尝试一次提交。</p></div> : null}
      <div className="overflow-x-auto border-y border-brand-border" aria-label="发布步骤">
        <div className="grid min-w-[38rem] grid-cols-3">
          {stepMeta.map((item, index) => {
            const current = step === item.id;
            const disabled = item.id === 'content' ? !canContinueAccounts : item.id === 'review' ? !frozenPlan : false;
            return <button key={item.id} type="button" data-testid={item.testID} aria-pressed={current} disabled={disabled} onClick={() => setStep(item.id)} className={`relative flex min-h-18 items-center gap-3 border-r border-brand-border px-4 py-3 text-left transition-colors last:border-r-0 disabled:cursor-not-allowed disabled:opacity-45 ${current ? 'bg-brand-accent/[0.06] text-brand-fg' : 'text-gray-500 hover:bg-black/[0.02]'}`}>
              <span className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-[11px] font-bold ${current ? 'border-brand-accent bg-brand-accent text-brand-on-accent' : 'border-brand-border text-gray-500'}`}>{index + 1}</span>
              <span className="min-w-0"><strong className="block text-xs">{item.label}</strong><span className="mt-0.5 block truncate text-[11px] font-normal text-gray-500">{item.note}</span></span>
              {index < stepMeta.length - 1 ? <ArrowRight aria-hidden="true" className="absolute -right-2 z-10 hidden h-4 w-4 bg-brand-bg text-gray-400 xl:block" /> : null}
            </button>;
          })}
        </div>
      </div>
      <div className="min-w-0 space-y-4">

      {step === 'accounts' ? (
        <section className="ui-section overflow-hidden">
          <div className="ui-section-header flex-wrap py-3">
            <div><h4 className="text-sm font-bold text-gray-900">选择账号</h4><p className="mt-1 text-xs text-gray-500">视频、图文和文章分别准备；只显示可用于当前形态的账号。</p></div>
            <div className="ui-utility-toolbar" aria-label="选择内容形态">
            <button type="button" data-testid="publish-family-video" aria-pressed={family === 'video'} onClick={() => chooseFamily('video')} className={`flex items-center gap-2 rounded-md px-3 py-2 text-xs font-semibold ${family === 'video' ? 'bg-brand-accent/10 text-brand-accent' : 'text-gray-600'}`}><Film className="h-4 w-4" />视频</button>
            <button type="button" data-testid="publish-family-graph-text" aria-pressed={family === 'graph_text'} onClick={() => chooseFamily('graph_text')} className={`flex items-center gap-2 rounded-md px-3 py-2 text-xs font-semibold ${family === 'graph_text' ? 'bg-brand-accent/10 text-brand-accent' : 'text-gray-600'}`}><Images className="h-4 w-4" />图文</button>
            <button type="button" data-testid="publish-family-article" aria-pressed={family === 'article'} onClick={() => chooseFamily('article')} className={`flex items-center gap-2 rounded-md px-3 py-2 text-xs font-semibold ${family === 'article' ? 'bg-brand-accent/10 text-brand-accent' : 'text-gray-600'}`}><FileText className="h-4 w-4" />文章</button>
          </div>
          </div>
          {accountsError || assetsError || spacesError ? (
            <p role="alert" data-testid="publish-status-load-error" className="mx-4 mt-3 rounded-lg bg-red-50 px-3 py-2 text-xs font-semibold text-red-700">
              账号、资产或浏览器状态加载失败；下面的账号列表可能不完整，请刷新后重试。
            </p>
          ) : null}
          <fieldset>
            <legend className="sr-only">选择要填充的账号</legend>
            <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3"><span className="text-xs font-bold text-gray-700">发布账号 <span className="ml-1 font-normal text-gray-400">同一 Space 按计划串行执行</span></span><button type="button" data-testid="publish-select-platform-group" disabled={accounts === undefined || !selectableFamilyAssets.length} onClick={selectAllFamilyAccounts} className="ui-btn text-xs disabled:opacity-40">全选可用账号</button></div>
            {accountTags?.length ? <details className="border-y border-brand-border bg-black/[0.02] px-4 py-2.5" data-testid="publish-account-tag-picker"><summary className="cursor-pointer text-xs font-semibold text-gray-600">按标签批量选择</summary><div className="mt-3 flex flex-wrap items-center gap-2">{accountTags.map((tag) => { const selected = publishTagIDs.includes(tag.id); return <button key={tag.id} type="button" data-testid={`publish-account-tag-${tag.id}`} aria-pressed={selected} onClick={() => setPublishTagIDs((current) => current.includes(tag.id) ? current.filter((id) => id !== tag.id) : [...current, tag.id])} className={`rounded-full border px-2 py-1 text-xs font-semibold ${selected ? 'ring-1 ring-current' : 'opacity-70'}`} style={{ borderColor: tag.color, color: tag.color }}>{tag.name} · {tag.accountCount}</button>; })}<button type="button" data-testid="publish-select-by-tags" disabled={!publishTagIDs.length} onClick={() => void selectAccountsByTags()} className="ui-btn ml-auto text-xs disabled:opacity-40">选中匹配账号</button></div></details> : null}
            <div className="divide-y divide-brand-border">
              {nonMetaControlledFamilyAssets.map((asset) => {
                const account = accountsByID.get(asset.account_id);
                const name = account?.name || asset.name || `账号 #${asset.account_id}`;
                const ready = readyAccountIDs.has(asset.account_id);
                const space = account ? egoSpaceByIdentity.get(account.identityId) : undefined;
                const canResume = Boolean(space && canResumeEgoSpaceForExplicitPrepare(space));
                const unavailableReason = canResume
                  ? '浏览器正在由你操作，请先交还给自动准备'
                  : '❗ Ego Space 未就绪，暂不可发布';
                const manifest = publishManifestByPlatform.get(asset.platform_code);
                const automatedPrepare = isAutomatedPrepare(manifest, family);
                const accountBody = <><AccountAvatar src={account?.avatar} platform={asset.platform_code} size="md" alt={`${name} 的账号头像`} testId={`publish-account-avatar-${asset.account_id}`} /><span className="min-w-0 flex-1"><strong className="block truncate">{name}</strong><span className="block text-[10px] text-gray-500">{platformLabel(asset.platform_code)}</span><span data-testid={`publish-capability-${asset.id}`} className={`mt-1 block text-[10px] font-semibold ${automatedPrepare ? 'text-brand-muted' : 'text-brand-warning'}`}>{publishCapabilityLabel(manifest, family)}</span><span className={`mt-1 block text-[10px] font-semibold ${ready ? 'text-emerald-600' : 'text-red-600'}`}>{ready ? '✓ Space 已就绪，可发布' : unavailableReason}</span></span></>;
                if (ready) return <label key={asset.id} className={`ui-row flex cursor-pointer items-center gap-3 px-4 py-3 text-xs text-gray-700 ${assetIDs.includes(asset.id) ? 'bg-brand-accent/[0.05]' : ''}`}>
                  <input type="checkbox" data-testid={`publish-target-${asset.id}`} checked={assetIDs.includes(asset.id)} onChange={() => toggleAsset(asset)} />
                  {accountBody}
                </label>;
                return <div key={asset.id} className="ui-row flex items-center gap-3 bg-red-50/70 px-4 py-3 text-xs text-gray-700"><input type="checkbox" data-testid={`publish-target-${asset.id}`} checked={false} disabled={!ready} aria-label={`${name}：${unavailableReason}`} />{accountBody}{canResume && account ? <button type="button" data-testid={`publish-return-space-${asset.account_id}`} disabled={running} onClick={() => void resumeSpaceForPrepare(account)} className="ui-btn shrink-0 text-xs disabled:opacity-40">交还自动准备</button> : null}</div>;
              })}
			  {metaPublishGroups.map((group) => {
				const ids = [...group.targets.facebook.assets, ...group.targets.instagram.assets].map((asset) => asset.id);
				const configured = group.targets.facebook.assets.length > 0 && group.targets.instagram.assets.length > 0;
				const ready = configured && ids.every((id) => selectableFamilyAssets.some((asset) => asset.id === id));
				const selected = ready && ids.every((id) => assetIDs.includes(id));
				const missing = group.targets.facebook.missingReason || group.targets.instagram.missingReason;
				const identity = group.targets.facebook.account ?? group.targets.instagram.account ?? group.accounts[0];
				const metaCapabilities = (['facebook', 'instagram'] as MetaPublishPlatformCode[])
					.map((platformCode) => publishCapabilityFor(publishManifestByPlatform.get(platformCode), family));
				const automatedMetaCount = metaCapabilities.filter((capability) => capability?.prepare_mode === 'automated').length;
				const metaCapabilityLabel = automatedMetaCount === metaCapabilities.length
					? '自动预填 · 提交前确认'
					: automatedMetaCount > 0 ? '部分自动预填 · 其余暂不支持' : '暂不支持自动发布';
				const unavailableReason = !ready
                  ? '❗ Ego Space 未就绪，暂不可发布'
                  : missing || '❗ 当前 Meta 账号不可选择';
                return <label key={group.key} data-testid={`publish-meta-group-${group.key}`} title={!ready ? unavailableReason : undefined} className={`ui-row flex items-center gap-3 px-4 py-3 text-xs text-gray-700 ${ready ? `cursor-pointer ${selected ? 'bg-brand-accent/[0.05]' : ''}` : 'bg-red-50/70'}`}>
                  <input type="checkbox" checked={selected} disabled={!ready} aria-label={`${group.name}：${unavailableReason}`} onChange={() => toggleMetaGroup(group)} />
                  <span className="flex items-center gap-1">
                    <AccountAvatar src={identity?.avatar} platform="meta" size="md" alt={`${group.name} 的 Meta 登录头像`} testId={`publish-meta-avatar-${group.key}`} />
                    <span data-testid={`publish-meta-platform-badges-${group.key}`} className="flex flex-col gap-0.5" aria-label="Facebook 和 Instagram">
                      <PlatformIcon platform="facebook" className="h-3.5 w-3.5" />
                      <PlatformIcon platform="instagram" className="h-3.5 w-3.5" />
                    </span>
                  </span>
                  <span className="min-w-0 flex-1">
                    <strong className="block truncate">{group.name}</strong>
                    <span className="mt-1 flex gap-1">
                      {(['facebook', 'instagram'] as MetaPublishPlatformCode[]).map((platformCode) => <span key={platformCode} data-testid={`publish-meta-platform-${group.key}-${platformCode}`} className="rounded-full border border-brand-border px-1.5 py-0.5 text-[9px] font-semibold">{platformCode === 'facebook' ? 'Facebook' : 'Instagram'}</span>)}
                    </span>
					<span data-testid={`publish-capability-meta-${group.key}`} className={`mt-1 block text-[10px] font-semibold ${automatedMetaCount ? 'text-brand-muted' : 'text-brand-warning'}`}>{metaCapabilityLabel}</span>
                    <span className="mt-1 block text-[10px] font-semibold text-red-600">{unavailableReason}</span>
                  </span>
                </label>;
              })}
              {!nonMetaControlledFamilyAssets.length && !metaPublishGroups.length ? <p className="p-6 text-center text-xs text-gray-400">{accounts === undefined ? '正在读取账号…' : '没有可发布的这一类账号。'}</p> : null}
            </div>
          </fieldset>
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-brand-border bg-black/[0.02] px-4 py-3 text-xs text-gray-600"><span><CircleUserRound className="mr-1 inline h-4 w-4" />已选 {selectedAssets.length} 个账号</span><button type="button" data-testid="publish-next-content" disabled={!canContinueAccounts} onClick={() => setStep('content')} className="ui-btn ui-btn-primary disabled:opacity-40">下一步：选择内容<ArrowRight className="h-3.5 w-3.5" /></button></div>
        </section>
      ) : null}

      {step === 'content' ? (
        <div className="space-y-4">
          <section className="ui-section overflow-hidden">
            <div className="ui-section-header flex-wrap py-3"><div><h4 className="text-sm font-bold text-gray-900">选择内容</h4><p className="mt-1 text-xs text-gray-500">内容先从本地库选择，再按每个平台分别核对。</p></div><button type="button" onClick={() => setStep('accounts')} className="ui-btn"><ArrowLeft className="h-3.5 w-3.5" />返回账号</button></div>
            <div className="grid lg:grid-cols-[minmax(0,1.25fr)_minmax(15rem,0.75fr)]">
              <fieldset data-testid="publish-content-list" className="min-w-0 border-b border-brand-border p-4 lg:border-b-0 lg:border-r">
                <legend className="mb-3 text-xs font-semibold text-gray-700">已保存内容</legend>
                {readyStudioHandoffs.length ? <div className="mb-3 rounded-lg border border-brand-accent/30 bg-brand-accent/[0.05] p-3" data-testid="publish-studio-handoffs"><p className="text-xs font-semibold text-gray-800">Studio 排期交接</p><p className="mt-1 text-[11px] text-gray-600">选择后时间只读；调整排期请返回 Studio。</p><div className="mt-2 grid gap-2">{readyStudioHandoffs.map((handoff) => <button key={handoff.id} type="button" data-testid={`publish-studio-handoff-${handoff.id}`} aria-pressed={selectedStudioHandoff?.id === handoff.id} onClick={() => chooseStudioHandoff(handoff)} className={`rounded border px-3 py-2 text-left text-xs ${selectedStudioHandoff?.id === handoff.id ? 'border-brand-accent bg-brand-accent/10 text-brand-accent' : 'border-brand-border bg-white text-gray-700 hover:border-brand-accent/50'}`}><strong className="block">内容 #{handoff.content_id}</strong><span className="mt-1 block">{studioHandoffTimeLabel(handoff)} · {handoff.status}</span></button>)}</div></div> : null}
                <div className="grid gap-2 sm:grid-cols-2">
                {familyContents.map((item) => {
					const compatible = selectedAssets.length > 0 && selectedAssets.every((asset) => supportsPublishContentType(publishManifestByPlatform.get(asset.platform_code), item.type));
                  const selected = contentID === item.id;
                  return <button
                    key={item.id}
                    type="button"
                    data-testid={`publish-content-${item.id}`}
                    aria-pressed={selected}
                    disabled={!compatible}
                    onClick={() => chooseContent(item.id)}
                    className={`flex w-full items-stretch gap-3 overflow-hidden rounded-lg border p-2 text-left text-xs transition-[background,border-color,box-shadow] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-accent disabled:cursor-not-allowed disabled:opacity-45 ${selected ? 'border-brand-accent bg-brand-accent/10 ring-1 ring-brand-accent/40' : 'border-brand-border hover:border-brand-accent/40 hover:bg-black/[0.02]'}`}
                  >
                    <ContentCover content={item} className="h-20 w-28 shrink-0 rounded-md" />
                    <span className="min-w-0 flex-1 py-1">
                      <span className="mb-1 inline-block rounded border border-brand-border px-1.5 py-0.5 text-[10px] font-semibold text-gray-500">{item.type === 'article' ? '文章' : item.type === 'graph_text' ? '图文' : '视频'}</span>
                      <strong className="block truncate text-sm text-gray-900">{item.title || `内容 #${item.id}`}</strong>
                      <span className="mt-1 block line-clamp-2 text-[11px] leading-relaxed text-gray-500">{item.body || item.caption || '暂无正文或描述'}</span>
                      {!compatible ? <span className="mt-1 block text-[11px] text-brand-warning">当前账号组合不支持</span> : null}
                    </span>
                  </button>;
                })}
                </div>
                {!compatibleContents.length ? <div className="mt-3 border border-dashed border-brand-border p-5 text-center text-xs text-gray-500">这一分类还没有可用内容。<button type="button" onClick={() => setActiveTab('contents')} className="ml-2 font-semibold text-brand-accent hover:underline">去管理内容</button></div> : null}
              </fieldset>
              <div className="space-y-3 bg-black/[0.015] p-4">
                <p className="text-xs font-semibold text-gray-700">本次内容</p>
                {selectedContent ? <div data-testid="publish-content-summary" className="space-y-2 text-xs text-gray-600"><div className="flex items-start gap-3"><ContentCover content={selectedContent} className="h-16 w-16 shrink-0 rounded-md" /><div className="min-w-0"><strong className="block truncate text-sm text-gray-900">{selectedContent.title || `内容 #${selectedContent.id}`}</strong><span className="mt-1 inline-block border border-brand-border px-1.5 py-0.5 text-[10px]">{contentType === 'article' ? '文章' : contentType === 'graph_text' ? '图文' : '视频'}</span></div></div><p className="line-clamp-3 whitespace-pre-wrap leading-relaxed">{selectedContent.body || selectedContent.caption || '暂无正文或描述'}</p><dl className="space-y-1 border-t border-brand-border pt-2 text-[11px]"><div><dt className="inline text-gray-400">关联素材：</dt><dd className="inline">{attachedMediaError ? '读取失败' : attachedMedia === undefined ? '读取中…' : attachedMedia.length ? attachedMedia.map((item) => fileName(item.file_path)).join('、') : '无'}</dd></div><div data-testid="publish-content-planned-at"><dt className="inline text-gray-400">发布时间：</dt><dd className="inline">{selectedStudioHandoff ? `${studioHandoffTimeLabel(selectedStudioHandoff)}（调整排期请返回 Studio）` : usesNativeSchedule ? `${plannedAtLabel(plannedPublishAtPreview)}（点击“下一步：确认”时按当前时间 +24 小时重新计算）` : '不定时，立即发布'}</dd></div></dl>{selectedStudioHandoff && !studioHandoffScheduleVerified ? <p data-testid="publish-studio-handoff-pending" className="border border-brand-warning/30 bg-brand-warning/10 px-2 py-1.5 text-[11px] font-semibold text-brand-warning">PENDING VERIFICATION · 当前原生排期能力尚未验收，冻结不会打开 Space。</p> : null}<p data-testid="publish-silent-upload" className="border-t border-brand-border pt-2 text-[11px] text-gray-500">上传方式：后台从冻结本机路径静默上传；不打开平台文件选择器。</p></div> : <p className="text-xs leading-relaxed text-gray-500">选择一条内容后，会在此确认素材、排期与本次上传方式。</p>}
                {studioHandoffSelectionInvalid ? <p data-testid="publish-studio-handoff-invalid" className="border border-red-300 bg-red-50 px-2 py-1.5 text-[11px] font-semibold text-red-800">Studio 交接已失效；请返回 Studio 检查排期后重新选择。不会改写原内容或重新计算排期。</p> : null}
                {selectedContent && mediaLoading ? <div data-testid="publish-media-loading" className="border-l-2 border-brand-accent bg-brand-surface px-3 py-2 text-xs text-gray-600">正在读取关联素材…</div> : null}
                {selectedContent && attachedMediaError ? <div data-testid="publish-media-error" className="border-l-2 border-red-500 bg-red-50 px-3 py-2 text-xs text-red-800">无法读取关联素材，请稍后重试或到“内容库”检查。</div> : null}
                {selectedContent && !mediaLoading && !attachedMediaError && !requiredMediaReady ? <div className="border-l-2 border-brand-warning/30 bg-brand-warning/10 px-3 py-2 text-xs text-brand-warning">{contentType === 'video' ? '这条内容还没有视频。' : '这条内容还没有图片。'}<button type="button" onClick={() => setActiveTab('contents')} className="ml-2 font-semibold underline">去关联素材</button></div> : null}
              </div>
            </div>
          </section>

          {selectedContent && selectedPlatformCodes.length ? <div className="overflow-x-auto border-y border-brand-border" role="tablist" aria-label="切换发布平台">
            <div className="flex min-w-max gap-1 px-1 py-1.5">
              {selectedPlatformCodes.map((code) => {
                const platformAccounts = selectedAssets.filter((asset) => asset.platform_code === code)
                  .map((asset) => accountsByID.get(asset.account_id))
                  .filter((account): account is NonNullable<typeof account> => Boolean(account));
                const selected = activeCode === code;
                return <button
                  key={code}
                  type="button"
                  role="tab"
                  data-testid={`publish-platform-tab-${code}`}
                  aria-selected={selected}
                  onClick={() => setActivePlatformCode(code)}
                  className={`flex min-w-40 items-center gap-2 border px-3 py-2 text-left transition-colors ${selected ? 'border-brand-accent bg-brand-accent/[0.06] text-brand-accent' : 'border-transparent text-gray-600 hover:bg-black/[0.03]'}`}
                >
                  <span className="flex -space-x-1">
                    {platformAccounts.map((account) =>
                      <AccountAvatar
                        key={account.id}
                        src={account.avatar}
                        platform={account.platformCode}
                        size="sm"
                        alt={`${account.name} 的账号头像`}
                        className="ring-2 ring-brand-surface"
                        testId={`publish-platform-tab-avatar-${account.id}`}
                      />
                    )}
                  </span>
                  <span className="min-w-0"><strong className="block truncate text-xs">{platformLabel(code)}</strong><span className="block truncate text-[10px] opacity-70">{platformAccounts.map((account) => account.name).join('、')}</span></span>
                </button>;
              })}
            </div>
          </div> : null}
          {selectedContent && activeCode ? (() => {
            const code = activeCode;
            const original = emptyOverride(selectedContent);
            const override = platformOverrides[code] ?? { ...original, tags: contentTagsForPlatform(code, original.tags) };
            const fields = contentFieldsForPlatform(code);
            const rule = contentRuleFor(code, contentType);
            const platformTemplates = templates?.filter((item) => item.platform_code === code && item.enabled) ?? [];
            const platformAccounts = selectedAssets.filter((asset) => asset.platform_code === code)
              .map((asset) => accountsByID.get(asset.account_id))
              .filter((account): account is NonNullable<typeof account> => Boolean(account));
            const videoAttached = Boolean(attachedMedia?.some((item) => item.kind === 'video'));
            const imageCount = attachedMedia?.filter((item) => item.kind === 'image').length ?? 0;
            const coverAttached = contentType === 'video' && imageCount > 0;
            const tagsRequired = Boolean(rule.minTags);
            const requirementLabel = (required: boolean | undefined) => required === true
              ? <span className="ml-1 text-red-500">必填</span>
              : required === false
                ? <span className="ml-1 font-normal text-gray-400">选填</span>
                : <span className="ml-1 font-normal text-brand-warning">待平台确认</span>;
            const ruleEvidence = rule.source === 'official'
              ? '官方规则'
              : rule.source === 'console_verified'
                ? '创作后台实机核对'
                : rule.source === 'contract_verified'
                  ? '字段契约已核对，发布页仍会二次确认'
                  : '官方要求待验证';
            const studioHandoffCopy = selectedStudioHandoff ? (
              <div data-testid={`publish-studio-handoff-copy-${code}`} className="mx-4 my-4 rounded-lg border border-brand-accent/30 bg-brand-accent/[0.05] px-3 py-2.5 text-xs text-gray-700">
                <strong className="block text-gray-900">平台文案已从 Studio 冻结</strong>
                <p className="mt-1 leading-relaxed">标题、正文和标签以 Studio 已审核内容为准，发布页不可按平台修改；如需调整，请返回 Studio 修改并创建新的交接。</p>
              </div>
            ) : null;
            const editablePlatformOverrides = !selectedStudioHandoff ? (
              <div data-testid={`publish-platform-overrides-${code}`} className="grid gap-3 px-4 py-4 md:grid-cols-2">
                <label className="text-xs font-semibold text-gray-700">{fields.titleLabel}{requirementLabel(rule.titleRequired)} <span className="font-normal text-gray-400">{countText(override.title)}{rule.titleMax ? ` / ${rule.titleMax}` : ''}</span>
                  <input required={rule.titleRequired === true} aria-required={rule.titleRequired === true} data-testid={`publish-platform-title-${code}`} value={override.title} onChange={(event) => updateOverride(code, { title: event.target.value })} className="mt-1.5 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm font-normal" />
                </label>
                {fields.supportsTags ? <label className="text-xs font-semibold text-gray-700">{fields.tagsLabel} {tagsRequired ? <span className="text-red-500">必填</span> : null}<span className="ml-1 font-normal text-gray-400">{fields.tagsHelp}</span>
                  <input required={tagsRequired} aria-required={tagsRequired} data-testid={`publish-platform-tags-${code}`} value={override.tags} onChange={(event) => updateOverride(code, { tags: event.target.value })} className="mt-1.5 block w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm font-normal" />
                  <button type="button" data-testid={`publish-suggest-tags-${code}`} onClick={(event) => { event.preventDefault(); updateOverride(code, { tags: suggestPublishTags(override.title, override.body).join(', ') }); }} className="mt-1.5 text-[11px] font-semibold text-brand-accent hover:underline">重新智能提取</button>
                </label> : null}
                <label className="text-xs font-semibold text-gray-700 md:col-span-2">{fields.bodyLabel}{requirementLabel(rule.bodyRequired)}
                  <textarea required={rule.bodyRequired === true} aria-required={rule.bodyRequired === true} data-testid={`publish-platform-body-${code}`} value={override.body} onChange={(event) => updateOverride(code, { body: event.target.value })} className="mt-1.5 min-h-32 w-full rounded-lg border border-brand-border bg-transparent px-3 py-2 text-sm font-normal" />
                </label>
              </div>
            ) : null;
            return <section className="ui-section overflow-hidden" data-testid={`publish-platform-content-${code}`} role="tabpanel">
              <div className="flex flex-wrap items-start justify-between gap-2 border-b border-brand-border px-4 py-3">
                <div><h5 className="text-sm font-bold text-gray-900">{platformLabel(code)}发布参数</h5><p className="mt-1 text-xs text-gray-500">对应账号：{platformAccounts.map((account) => account.name).join('、')}</p></div>
                <span className="border border-brand-border px-2 py-1 text-[11px] text-gray-500">{selectedStudioHandoff ? 'Studio 交接内容只读' : '独立保存，不与其他平台混用'}</span>
              </div>
              <p data-testid={`publish-content-rule-evidence-${code}`} className="px-4 pt-3 text-[11px] text-gray-500">字段依据：{ruleEvidence} · {rule.note}</p>
              <div className="mx-4 mt-3 grid divide-y divide-brand-border border-y border-brand-border text-xs sm:grid-cols-3 sm:divide-x sm:divide-y-0">
                <div data-testid="publish-content-title-required" className="px-3 py-2.5"><strong>内容标题</strong><span className="ml-1 text-red-500">必填</span><p className="mt-1 text-gray-500">{selectedContent.title.trim() ? '已填写' : '缺少标题'}</p></div>
                <div data-testid="publish-content-body-required" className="px-3 py-2.5"><strong>内容正文</strong><span className="ml-1 text-red-500">必填</span><p className="mt-1 text-gray-500">{(selectedContent.body || selectedContent.caption).trim() ? '已填写' : '缺少正文'}</p></div>
                <div data-testid="publish-content-media-required" className="px-3 py-2.5"><strong>内容素材</strong><span className="ml-1 text-red-500">必填</span><p className="mt-1 text-gray-500">{contentType === 'article' ? '文章正文' : requiredMediaReady ? '已关联' : '缺少素材'}</p></div>
              </div>
              <div className="mx-4 grid divide-y divide-brand-border border-b border-brand-border text-xs sm:grid-cols-3 sm:divide-x sm:divide-y-0" data-testid={`publish-required-summary-${code}`}>
                <div className="px-3 py-2.5"><strong>发布素材</strong><span className="ml-1 text-red-500">必填</span><p className="mt-1 text-gray-500">{contentType === 'video' ? (videoAttached ? '已选择视频' : '缺少视频') : contentType === 'graph_text' ? (imageCount ? `已选择 ${imageCount} 张图片` : '缺少图片') : '文章正文'}</p></div>
                <div className="px-3 py-2.5"><strong>封面</strong><span className="ml-1 font-normal text-gray-400">{contentType === 'video' ? '选填' : '随内容'}</span><p className="mt-1 text-gray-500">{contentType === 'video' ? (coverAttached ? '已选择本地封面' : '未选择，将使用平台首帧能力并在页面复核') : imageCount ? '使用内容图片' : '由平台正文生成'}</p></div>
                <div className="px-3 py-2.5"><strong>发布时间</strong><span className="ml-1 font-normal text-gray-400">本次内容</span><p className="mt-1 text-gray-500">{selectedStudioHandoff ? `${studioHandoffTimeLabel(selectedStudioHandoff)}（调整排期请返回 Studio）` : usesNativeSchedule ? `${plannedAtLabel(plannedPublishAtPreview)}（确认时重新计算）` : '不定时，立即发布'}</p></div>
              </div>
              {studioHandoffCopy}
              {editablePlatformOverrides}
              {settingsByPlatform[code]?.schema ? <div className="border-t border-brand-border px-4 py-4"><h6 className="mb-2 text-xs font-bold text-gray-700">平台发布选项</h6><PlatformSettingsPanel platformCode={code} contentType={contentType} settings={settingsByPlatform[code]} templates={platformTemplates} selectedTemplateID={templateByPlatform[code] ?? 0} saving={savingTemplate === code} allowTemplateSave={!selectedStudioHandoff} onTemplate={(id) => selectTemplate(code, id)} onChange={(key, value) => updatePlatformSetting(code, key, value)} onSaveDefault={() => void saveDefaultTemplate(code)} /></div> : null}
            </section>;
          })() : null}
          <div className="flex flex-wrap items-center justify-between gap-3 border-t border-brand-border bg-black/[0.02] px-4 py-3"><button type="button" onClick={() => setStep('accounts')} className="ui-btn"><ArrowLeft className="h-3.5 w-3.5" />上一步</button><div className="flex items-center gap-3"><span className="hidden text-[11px] text-gray-500 sm:inline">冻结后可在下一步审核，尚不会打开或提交平台。</span><button type="button" data-testid="publish-next-review" disabled={running || !selectedContent || mediaLoading || Boolean(attachedMediaError) || !requiredMediaReady || !selectedAssets.length || Boolean(settingsError)} onClick={() => void saveContentAndFreeze()} className="ui-btn ui-btn-primary disabled:opacity-40">{running ? '正在准备…' : mediaLoading ? '正在读取素材…' : attachedMediaError ? '素材读取失败' : '下一步：确认'}<ArrowRight className="h-3.5 w-3.5" /></button></div></div>
        </div>
      ) : null}

      {step === 'review' && frozenPlan ? (
        <section className="ui-section overflow-hidden">
          <div className="ui-section-header flex-wrap py-3"><div><h4 className="text-sm font-bold text-gray-900">审核发布计划</h4><p className="mt-1 text-xs text-gray-500">计划已冻结；预填和最终提交仍是两个独立操作。</p></div><button type="button" onClick={() => setStep('content')} className="ui-btn"><ArrowLeft className="h-3.5 w-3.5" />返回修改</button></div>
          <div className="grid gap-4 p-4 lg:grid-cols-[minmax(0,0.72fr)_minmax(16rem,1.28fr)]" data-testid="publish-review-content">
            {selectedContent ? <div data-testid="publish-review-cover"><ContentCover content={selectedContent} className="h-28 w-28 shrink-0 rounded-lg" /></div> : null}
            <div className="min-w-0 flex-1">
              <strong className="block truncate text-sm text-gray-900" data-testid="publish-review-title">{frozenPlan.snapshot.content.title}</strong>
              <p className="mt-1 text-[11px] text-gray-500">{targetCount} 个目标将按同一 Space 串行准备；本页不会替你点击最终发布。</p>
              <div className="mt-3 divide-y divide-brand-border border-y border-brand-border">
                {frozenPlan.snapshot.targets.map((target) => {
                  const asset = selectedAssets.find((item) => item.account_id === target.account_id && item.platform_code === target.platform_code);
                  const account = accountsByID.get(target.account_id);
                  const accountName = account?.name || asset?.name || platformLabel(target.platform_code);
                  return <div key={`${target.account_id}-${target.platform_code}`} data-testid={`publish-review-account-${target.account_id}-${target.platform_code}`} className="flex min-w-0 items-center gap-2 px-2.5 py-2 text-xs text-gray-700">
                    <AccountAvatar src={account?.avatar || asset?.avatar} platform={target.platform_code} size="sm" alt={`${accountName} 的账号头像`} />
                    <span className="min-w-0"><strong className="block max-w-40 truncate">{accountName}</strong><span className="block text-[10px] text-gray-500">{platformLabel(target.platform_code)}</span><span data-testid={`publish-review-capability-${target.account_id}-${target.platform_code}`} className={`block text-[10px] font-semibold ${isAutomatedPrepare(publishManifestByPlatform.get(target.platform_code), contentType) ? 'text-brand-muted' : 'text-brand-warning'}`}>{publishCapabilityLabel(publishManifestByPlatform.get(target.platform_code), contentType)}</span></span>
                  </div>;
                })}
              </div>
            </div>
          </div>
          <div className="border-t border-brand-border bg-black/[0.02] p-4">
            {queuedTargets.length > 0 ? <p className="mb-3 text-xs text-gray-500">{queuedTargets.length} 个历史排队目标将在下一次预填中逐个串行处理。</p> : null}
            {!planExecution || canRetryUnsubmitted || canPrepareQueued ? <><p className="mb-2 text-[11px] text-gray-500">阶段 1 · 系统逐个平台预填，不会执行最终发布。</p><button data-testid="publish-plan-prepare" type="button" disabled={running} onClick={() => void prepareFrozenPlan()} className="ui-btn ui-btn-primary w-full justify-center py-3 disabled:opacity-40"><Check className="h-4 w-4" />{prepareLabel}</button></> : <><p className="mb-2 text-[11px] text-gray-500">阶段 2 · 一次确认冻结批次后，系统逐个平台提交并自动核验；登录异常才需要你处理。</p>
              {(() => {
                // 逐平台报告验收状态：用户点提交之前就该知道谁会成功。
                const planTargets = planExecution?.targets ?? [];
                const unverified = planTargets.filter((target) => submitAcceptance(publishManifestByPlatform.get(target.platformCode), contentType) === 'unverified');
                if (unverified.length === 0) return null;
                return (
                  <p data-testid="publish-submit-unverified-notice" className="mb-2 rounded-md border border-brand-warning/30 bg-brand-warning/10 px-2 py-1.5 text-[11px] leading-4 text-brand-warning">
                    以下平台尚未通过真实创作者后台验收，点提交后会被拒绝（EGO_PUBLISH_SUBMIT_UNVERIFIED）：
                    {Array.from(new Set(unverified.map((target) => platformLabel(target.platformCode)))).join('、')}
                    。这是有意的保护，产品不会替未验收的平台点发布。
                  </p>
                );
              })()}<button data-testid="publish-plan-submit" type="button" disabled={running || !preparedForSubmit} onClick={() => void submitPreparedPlan()} className="ui-btn ui-btn-primary w-full justify-center py-3 disabled:opacity-40"><Check className="h-4 w-4" />{submitLabel}</button></>}
            {managerVerificationTargets.length ? <div className="mt-3 space-y-2 border-t border-brand-border pt-3">{managerVerificationTargets.map((target) => <button key={`${target.account_id}-${target.platform_code}`} data-testid={`publish-plan-verify-manager-${target.account_id}-${target.platform_code}`} type="button" disabled={running} onClick={() => void verifyFrozenPlanManager({ accountId: target.account_id, platformCode: target.platform_code })} className="ui-btn w-full justify-center disabled:opacity-40">{running ? '正在核验后台…' : `核验 ${platformLabel(target.platform_code)} 后台（不会再次发布）`}</button>)}</div> : null}
          </div>
        </section>
      ) : null}
      </div>
    </section>
  );
}
