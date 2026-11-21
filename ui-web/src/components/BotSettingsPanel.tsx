import { useEffect, useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  CheckCircle2,
  ChevronRight,
  ImagePlus,
  Loader2,
  MessageCircle,
  Pencil,
  Plus,
  Server,
  Sparkles,
  Trash2,
  Users,
  X,
} from 'lucide-react';
import { api, type AgentBot } from '../api';
import { useUIStore } from '../store';
import { revalidateBotCaches } from '../lib/botCache';
import { PixelAvatar, PIXEL_AVATARS, PIXEL_AVATAR_ROLES, PLATFORM_AVATARS, pixelAvatarRole, pixelAvatarSrc, platformAvatarID } from './PixelAvatars';

type BotDraft = {
  name: string;
  instructions: string;
  model: string;
  avatar: string;
  kickoff: string;
  groupName: string;
  muted: boolean;
  pinned: boolean;
  isPrimary: boolean;
};

const EMPTY_DRAFT: BotDraft = {
  name: '',
  instructions: '',
  model: '',
  avatar: '',
  kickoff: '',
  groupName: '',
  muted: false,
  pinned: false,
  isPrimary: false,
};

const AVATAR_COLORS = ['#4f46e5', '#0ea5e9', '#059669', '#d97706', '#db2777', '#7c3aed'];

/**
 * Bundled pixel-art avatars, one per responsibility (not per user-chosen name),
 * with four female and four male variants. Rendered as inline SVG, so nothing
 * is fetched at runtime.
 */
const BOT_AVATAR_PRESETS = PIXEL_AVATAR_ROLES.map((role) => ({
  id: role,
  src: pixelAvatarSrc(role),
  label: PIXEL_AVATARS[role].label,
}));

/** Stable per-bot accent so a bot without `color` still looks intentional. */
function avatarColor(seed: string): string {
  let hash = 0;
  for (let i = 0; i < seed.length; i += 1) hash = (hash * 31 + seed.charCodeAt(i)) % 100_000;
  return AVATAR_COLORS[hash % AVATAR_COLORS.length];
}

/** The one-liner doubles as the visible role badge, so the form stays at three inputs. */
function roleOf(instructions: string): string {
  return instructions.trim().split('\n')[0].trim().slice(0, 40);
}

/**
 * Seeds the avatar prompt. English because image models follow it far more
 * reliably. Everything else (model, aspect ratio, size) is decided server-side:
 * the UI deliberately exposes no image parameter controls.
 */
function buildAvatarPrompt(name: string, role: string): string {
  if (!name) return '';
  return `Cute chibi anime avatar for an AI assistant named ${name}, ${role || 'a helpful local assistant'}. ` +
    'Big expressive eyes, soft pastel palette, rounded minimal shapes, plain light background, centered head and shoulders, no text.';
}

/** Bot 端点的错误码 -> 中文；未知码原样返回，便于定位。 */
const BOT_ERROR_MESSAGES: Record<string, string> = {
  AGENT_BOT_NAME_REQUIRED: 'Bot 名字不能为空。',
  AGENT_BOT_INVALID: 'Bot 字段不合法（检查名字、头像或指令长度）。',
  AGENT_BOT_NOT_FOUND: '这个 Bot 已被删除。',
  AGENT_MODEL_INVALID: '模型格式必须是 provider/model。',
  AGENT_BOT_LOCAL_ONLY: 'Bot 接口只接受本机同源请求。',
  AGENT_BOT_METHOD_NOT_ALLOWED: '不支持的操作。',
};

function humanError(reason: unknown): string {
  const raw = reason instanceof Error ? reason.message : '';
  const code = raw.split(':', 1)[0].trim();
  return BOT_ERROR_MESSAGES[code] || raw || '未知错误';
}

function BotAvatar({ bot }: { bot: AgentBot }) {
  const initial = (bot.name || '?').trim().slice(0, 1).toUpperCase() || '?';
  const role = pixelAvatarRole(bot.avatar);
  if (role) {
    return <PixelAvatar role={role} size={36} className="shrink-0 rounded-[3px]" />;
  }
  if (platformAvatarID(bot.avatar)) {
    return (
      <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[3px] border border-brand-border bg-white">
        <img src={bot.avatar} alt="" aria-hidden="true" className="h-6 w-6 object-contain" />
      </span>
    );
  }
  if (bot.avatar) {
    return (
      <img
        src={bot.avatar}
        alt=""
        aria-hidden="true"
        className="h-9 w-9 shrink-0 rounded-[3px] object-cover [image-rendering:pixelated]"
      />
    );
  }
  return (
    <span
      aria-hidden="true"
      className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[3px] text-xs font-bold text-white"
      style={{ background: bot.color || avatarColor(bot.id || bot.name) }}
    >
      {initial}
    </span>
  );
}

function CardStatus({ tone, label }: { tone: 'success' | 'danger' | 'muted'; label: string }) {
  return <span className={`ui-status ui-status-${tone}`}>{label}</span>;
}

export function BotSettingsPanel() {
  const toast = useUIStore((state) => state.toast);
  const { data, error, isLoading } = useSWR('api/agent-bots', api.listAgentBots, {
    revalidateOnFocus: false,
  });
  const bots = useMemo(() => data ?? [], [data]);
  // Only verified models are offered: picking anything else would fail at the
  // first run. The list is the union of every provider's verified models.
  const { data: providerState } = useSWR('api/agent/providers', api.agentHubProviders, {
    revalidateOnFocus: false,
  });
  const modelOptions = useMemo(() => {
    const options: Array<{ value: string; label: string }> = [];
    for (const probe of providerState?.probes ?? []) {
      for (const model of probe.models ?? []) {
        options.push({ value: model, label: `${probe.provider_id} · ${model}` });
      }
    }
    return options;
  }, [providerState]);


  const [formOpen, setFormOpen] = useState(false);
  const [editingId, setEditingId] = useState('');
  const [draft, setDraft] = useState<BotDraft>(EMPTY_DRAFT);
  const [saving, setSaving] = useState(false);
  const [busyBotId, setBusyBotId] = useState('');

  const [avatarPrompt, setAvatarPrompt] = useState('');
  const [promptTouched, setPromptTouched] = useState(false);
  const [generatingAvatar, setGeneratingAvatar] = useState(false);
  const [reference, setReference] = useState<{ name: string; url?: string } | null>(null);
  const [uploadingReference, setUploadingReference] = useState(false);

  // Keep the prompt in sync with the name/persona until the user edits it.
  useEffect(() => {
    if (promptTouched) return;
    setAvatarPrompt(buildAvatarPrompt(draft.name.trim(), roleOf(draft.instructions)));
  }, [draft.name, draft.instructions, promptTouched]);

  function resetAvatarFields() {
    setAvatarPrompt('');
    setPromptTouched(false);
    setReference(null);
  }

  function openCreate() {
    setEditingId('');
    setDraft(EMPTY_DRAFT);
    resetAvatarFields();
    setFormOpen(true);
  }

  function openEdit(bot: AgentBot) {
    setEditingId(bot.id);
    setDraft({
      name: bot.name || '',
      instructions: bot.instructions || bot.role || '',
      model: bot.model || '',
      avatar: bot.avatar || '',
      kickoff: bot.kickoff || '',
      groupName: bot.group_name || '',
      muted: Boolean(bot.muted),
      pinned: Boolean(bot.pinned),
      isPrimary: Boolean(bot.is_primary),
    });
    resetAvatarFields();
    setFormOpen(true);
  }

  function closeForm() {
    setFormOpen(false);
    setEditingId('');
    setDraft(EMPTY_DRAFT);
    resetAvatarFields();
  }

  async function pickReference(file: File | undefined) {
    if (!file) return;
    setUploadingReference(true);
    try {
      // Reuses the existing asset endpoint; no extra upload channel.
      const asset = await api.uploadAIImageAsset(file);
      setReference({ name: asset.name, url: asset.url });
      toast.show('参考图已就绪', 'success');
    } catch (reason) {
      toast.show(`参考图上传失败：${humanError(reason)}`, 'error');
    } finally {
      setUploadingReference(false);
    }
  }

  async function generateAvatar() {
    const prompt = avatarPrompt.trim();
    if (!prompt) {
      toast.show('先填一个名称，提示词会自动补好', 'error');
      return;
    }
    setGeneratingAvatar(true);
    try {
      // Only the prompt and an optional reference image: every other knob is
      // decided by the backend.
      const result = await api.generateAIImage({
        prompt,
        ...(reference ? { reference_image: reference.name } : {}),
      });
      if (!result?.url) throw new Error('生图服务没有返回图片地址');
      setDraft((current) => ({ ...current, avatar: result.url }));
      toast.show('头像已生成，可以随时再换一张', 'success');
    } catch (reason) {
      // Nothing is written on failure: the previous avatar stays untouched.
      toast.show(`头像生成失败：${humanError(reason)}`, 'error');
    } finally {
      setGeneratingAvatar(false);
    }
  }

  async function submitBot() {
    const name = draft.name.trim();
    if (!name) {
      toast.show('Bot 需要一个名字', 'error');
      return;
    }
    const isEdit = Boolean(editingId);
    setSaving(true);
    try {
      const payload = {
        name,
        role: roleOf(draft.instructions),
        instructions: draft.instructions.trim(),
        model: draft.model.trim(),
        avatar: draft.avatar,
        kickoff: draft.kickoff.trim(),
        group_name: draft.groupName.trim(),
        muted: draft.muted,
        pinned: draft.pinned,
        is_primary: draft.isPrimary,
      };
      if (isEdit) await api.updateAgentBot(editingId, payload);
      else await api.createAgentBot(payload);
      await revalidateBotCaches();
      closeForm();
      toast.show(isEdit ? 'Bot 已更新' : 'Bot 已创建', 'success');
    } catch (reason) {
      toast.show(`保存失败：${humanError(reason)}`, 'error');
    } finally {
      setSaving(false);
    }
  }

  async function removeBot(bot: AgentBot) {
    const confirmed = window.confirm(`删除 Bot「${bot.name}」？已绑定的对话会解除关联，不会被删除。`);
    if (!confirmed) return;
    setBusyBotId(bot.id);
    try {
      await api.deleteAgentBot(bot.id);
      await revalidateBotCaches();
      if (editingId === bot.id) closeForm();
      toast.show('Bot 已删除', 'success');
    } catch (reason) {
      toast.show(`删除失败：${humanError(reason)}`, 'error');
    } finally {
      setBusyBotId('');
    }
  }

  return (
    <div className="space-y-4" data-testid="settings-connect">
      <header>
        <h4 className="text-xs font-bold text-gray-900">连接</h4>
        <p className="mt-1 text-[10px] leading-4 text-gray-500">三件事：助手是谁、模型从哪来、消息从哪进。</p>
      </header>

      <div className="grid gap-4 lg:grid-cols-3">
        {/* ① Bots */}
        <article data-testid="connect-card-bots" className="ui-card flex min-h-[15rem] flex-col gap-3 p-4">
          <div className="flex items-start justify-between gap-2">
            <div className="flex items-center gap-2">
              <Users className="h-4 w-4 text-brand-muted" />
              <h5 className="text-xs font-bold text-gray-900">Bots</h5>
            </div>
            <CardStatus
              tone={bots.length ? 'success' : 'muted'}
              label={bots.length ? `${bots.length} 个` : '空'}
            />
          </div>
          <p className="text-[10px] leading-4 text-gray-500">给助手一个身份，它会记住自己是谁。</p>

          {isLoading && !data ? (
            <p className="flex items-center gap-2 text-[10px] text-gray-500">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />正在读取 Bot
            </p>
          ) : null}
          {error ? (
            <p role="status" className="text-[10px] text-brand-danger">读取 Bot 失败：{humanError(error)}</p>
          ) : null}

          {bots.length ? (
            <ul className="divide-y divide-brand-border" data-testid="connect-bot-list">
              {bots.map((bot) => (
                <li key={bot.id} data-testid="connect-bot-row" className="flex items-center gap-2 py-2">
                  <BotAvatar bot={bot} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <strong className="truncate text-[11px] text-gray-900">{bot.name}</strong>
                      <CheckCircle2 className="h-3 w-3 shrink-0 text-brand-success" aria-label="已保存" />
                    </div>
                    <p className="mt-0.5 truncate text-[10px] text-gray-500">
                      {bot.role || bot.model || '还没有一句话人格'}
                    </p>
                  </div>
                  <button
                    type="button"
                    aria-label={`编辑 ${bot.name}`}
                    data-testid="connect-bot-edit"
                    className="ui-icon-button"
                    onClick={() => openEdit(bot)}
                  >
                    <Pencil className="h-3.5 w-3.5" />
                  </button>
                  <button
                    type="button"
                    aria-label={`删除 ${bot.name}`}
                    data-testid="connect-bot-delete"
                    className="ui-icon-button"
                    disabled={busyBotId === bot.id}
                    onClick={() => void removeBot(bot)}
                  >
                    {busyBotId === bot.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                  </button>
                </li>
              ))}
            </ul>
          ) : null}

          {!bots.length && !isLoading && !error ? (
            <div className="flex flex-1 flex-col items-start justify-center gap-2" data-testid="connect-bot-empty">
              <p className="text-[11px] text-gray-500">还没有 Bot，建一个试试。</p>
            </div>
          ) : null}

          <div className="mt-auto">
            {formOpen ? (
              <div className="space-y-2 border-t border-brand-border pt-3" data-testid="connect-bot-form">
                <label className="block">
                  <span className="text-[10px] font-semibold text-gray-600">名称</span>
                  <input
                    data-testid="connect-bot-form-name"
                    value={draft.name}
                    maxLength={80}
                    onChange={(event) => setDraft((current) => ({ ...current, name: event.target.value }))}
                    placeholder="比如：选题助手"
                    className="mt-1 w-full rounded-lg border border-brand-border bg-white px-2.5 py-1.5 text-[11px] text-gray-800"
                  />
                </label>
                <label className="block">
                  <span className="text-[10px] font-semibold text-gray-600">一句话人格</span>
                  <textarea
                    data-testid="connect-bot-form-instructions"
                    value={draft.instructions}
                    maxLength={4000}
                    onChange={(event) => setDraft((current) => ({ ...current, instructions: event.target.value }))}
                    rows={2}
                    placeholder="只说一件事：它擅长什么、说话什么调子。"
                    className="mt-1 w-full resize-none rounded-lg border border-brand-border bg-white px-2.5 py-1.5 text-[11px] text-gray-800"
                  />
                </label>
                <label className="block">
                  <span className="text-[10px] font-semibold text-gray-600">Agent 模型（可选）</span>
                  <select
                    data-testid="connect-bot-form-model"
                    value={draft.model}
                    onChange={(event) => setDraft((current) => ({ ...current, model: event.target.value }))}
                    className="mt-1 w-full rounded-lg border border-brand-border bg-white px-2.5 py-1.5 font-mono text-[10px] text-gray-800"
                  >
                    <option value="">跟随当前 Agent 模型</option>
                    {modelOptions.map((option) => (
                      <option key={option.value} value={option.value}>{option.label}</option>
                    ))}
                  </select>
                  {Boolean(draft.model) && !modelOptions.some((option) => option.value === draft.model) ? (
                    <span data-testid="connect-bot-model-unverified" className="mt-1 block text-[9px] leading-3 text-brand-warning">
                      当前值不在已验证列表，保存后对话可能失败
                    </span>
                  ) : null}
                </label>
                <label className="block">
                  <span className="text-[10px] font-semibold text-gray-600">开场语（可选）</span>
                  <input
                    data-testid="connect-bot-form-kickoff"
                    value={draft.kickoff}
                    maxLength={2000}
                    onChange={(event) => setDraft((current) => ({ ...current, kickoff: event.target.value }))}
                    placeholder="第一次打开它时显示的招呼语"
                    className="mt-1 w-full rounded-lg border border-brand-border bg-white px-2.5 py-1.5 text-[11px] text-gray-800"
                  />
                </label>
                <label className="block">
                  <span className="text-[10px] font-semibold text-gray-600">团队 / 分组（可选）</span>
                  <input
                    data-testid="connect-bot-form-group"
                    value={draft.groupName}
                    maxLength={40}
                    onChange={(event) => setDraft((current) => ({ ...current, groupName: event.target.value }))}
                    placeholder="比如：开发团队。留空归到「其他」"
                    className="mt-1 w-full rounded-lg border border-brand-border bg-white px-2.5 py-1.5 text-[11px] text-gray-800"
                  />
                </label>
                <div className="flex flex-wrap items-center gap-3 rounded-lg border border-brand-border px-2.5 py-1.5">
                  <label className="flex items-center gap-1.5 text-[10px] font-semibold text-gray-600">
                    <input
                      data-testid="connect-bot-form-pinned"
                      type="checkbox"
                      checked={draft.pinned}
                      onChange={(event) => setDraft((current) => ({ ...current, pinned: event.target.checked }))}
                      className="h-3 w-3"
                    />
                    置顶
                  </label>
                  <label className="flex items-center gap-1.5 text-[10px] font-semibold text-gray-600">
                    <input
                      data-testid="connect-bot-form-primary"
                      type="checkbox"
                      checked={draft.isPrimary}
                      onChange={(event) => setDraft((current) => ({ ...current, isPrimary: event.target.checked }))}
                      className="h-3 w-3"
                    />
                    设为主 Bot
                  </label>
                  <label className="flex items-center gap-1.5 text-[10px] font-semibold text-gray-600">
                    <input
                      data-testid="connect-bot-form-muted"
                      type="checkbox"
                      checked={draft.muted}
                      onChange={(event) => setDraft((current) => ({ ...current, muted: event.target.checked }))}
                      className="h-3 w-3"
                    />
                    免打扰
                  </label>
                  <span className="text-[9px] text-gray-400">主 Bot 全局只有一个</span>
                </div>

                {/* 内置二次元头像：点一下即用，不需要配置模型或 key */}
                <div className="space-y-1.5" data-testid="connect-bot-avatar-presets">
                  <div className="flex items-center gap-2">
                    <span className="text-[10px] font-semibold text-gray-600">平台频道</span>
                    <span className="text-[10px] text-gray-400">直接使用平台标识</span>
                  </div>
                  <div className="grid grid-cols-6 gap-1">
                    {PLATFORM_AVATARS.map((preset) => {
                      const selected = draft.avatar === preset.src;
                      return (
                        <button
                          key={preset.id}
                          type="button"
                          data-testid={`connect-bot-avatar-platform-${preset.id}`}
                          aria-pressed={selected}
                          aria-label={preset.label}
                          title={preset.label}
                          onClick={() => setDraft((current) => ({ ...current, avatar: preset.src }))}
                          className={`flex h-8 items-center justify-center overflow-hidden rounded-[3px] border-2 bg-white transition ${
                            selected ? 'border-brand-accent' : 'border-transparent hover:border-brand-border'
                          }`}
                        >
                          <img src={preset.src} alt="" className="h-6 w-6 object-contain" />
                        </button>
                      );
                    })}
                  </div>
                  <div className="flex items-center gap-2 pt-1">
                    <span className="text-[10px] font-semibold text-gray-600">按职责选形象</span>
                    <span className="text-[10px] text-gray-400">像素风 · 4 女 4 男</span>
                  </div>
                  <div className="grid grid-cols-8 gap-1">
                    {BOT_AVATAR_PRESETS.map((preset) => {
                      const selected = draft.avatar === preset.src;
                      return (
                        <button
                          key={preset.src}
                          type="button"
                          data-testid={`connect-bot-avatar-preset-${preset.id}`}
                          aria-pressed={selected}
                          aria-label={preset.label}
                          title={preset.label}
                          onClick={() => setDraft((current) => ({ ...current, avatar: preset.src }))}
                          className={`overflow-hidden rounded-[3px] border-2 transition ${
                            selected ? 'border-brand-accent' : 'border-transparent hover:border-brand-border'
                          }`}
                        >
                          <PixelAvatar role={preset.id} size={28} className="block" />
                        </button>
                      );
                    })}
                  </div>
                  {draft.avatar ? (
                    <button
                      type="button"
                      data-testid="connect-bot-avatar-clear"
                      className="text-[10px] font-semibold text-gray-400 hover:text-brand-accent"
                      onClick={() => setDraft((current) => ({ ...current, avatar: '' }))}
                    >
                      清除头像
                    </button>
                  ) : null}
                </div>

                {/* 生图：只有提示词 + 可选参考图，参数全部由后端决定 */}
                <div className="space-y-2 rounded-lg border border-brand-border bg-brand-surface-raised p-2.5" data-testid="connect-bot-avatar-block">
                  <div className="flex items-center gap-2">
                    <BotAvatar bot={{ id: editingId || draft.name, name: draft.name || '?', avatar: draft.avatar }} />
                    <span className="text-[10px] text-gray-500">头像</span>
                  </div>
                  <label className="block">
                    <span className="text-[10px] font-semibold text-gray-600">头像提示词</span>
                    <textarea
                      data-testid="connect-bot-avatar-prompt"
                      value={avatarPrompt}
                      maxLength={2000}
                      rows={3}
                      onChange={(event) => {
                        setPromptTouched(true);
                        setAvatarPrompt(event.target.value);
                      }}
                      placeholder="填好名称和人格后会自动生成，可自行修改"
                      className="mt-1 w-full resize-none rounded-lg border border-brand-border bg-white px-2.5 py-1.5 text-[10px] leading-4 text-gray-700"
                    />
                  </label>
                  <div className="flex flex-wrap items-center gap-2">
                    <label className="ui-btn cursor-pointer">
                      {uploadingReference ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ImagePlus className="h-3.5 w-3.5" />}
                      {uploadingReference ? '上传中…' : reference ? '换参考图' : '参考图（可选）'}
                      <input
                        data-testid="connect-bot-avatar-reference"
                        type="file"
                        accept="image/*"
                        className="hidden"
                        onChange={(event) => {
                          void pickReference(event.target.files?.[0]);
                          event.target.value = '';
                        }}
                      />
                    </label>
                    {reference ? (
                      <span className="inline-flex items-center gap-1 text-[10px] text-gray-600">
                        {reference.url ? (
                          <img src={reference.url} alt="" aria-hidden="true" className="h-6 w-6 rounded border border-brand-border object-cover" />
                        ) : null}
                        <span className="max-w-[7rem] truncate font-mono">{reference.name}</span>
                        <button
                          type="button"
                          aria-label="移除参考图"
                          data-testid="connect-bot-avatar-reference-clear"
                          className="ui-icon-button h-5 w-5"
                          onClick={() => setReference(null)}
                        >
                          <X className="h-3 w-3" />
                        </button>
                      </span>
                    ) : null}
                    <button
                      type="button"
                      data-testid="connect-bot-form-avatar-generate"
                      className="ui-btn ml-auto"
                      disabled={generatingAvatar || uploadingReference}
                      onClick={() => void generateAvatar()}
                    >
                      {generatingAvatar ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Sparkles className="h-3.5 w-3.5" />}
                      {generatingAvatar ? '生成中…' : '生成头像'}
                    </button>
                  </div>
                </div>

                <div className="flex items-center gap-2 pt-1">
                  <button
                    type="button"
                    data-testid="connect-bot-form-save"
                    className="ui-btn ui-btn-primary"
                    disabled={saving || generatingAvatar}
                    onClick={() => void submitBot()}
                  >
                    {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                    {saving ? '保存中…' : editingId ? '保存' : '创建'}
                  </button>
                  <button type="button" className="ui-btn" onClick={closeForm} disabled={saving}>
                    取消
                  </button>
                </div>
              </div>
            ) : null}

            {!formOpen ? (
              <button
                type="button"
                data-testid="connect-bot-create"
                className="ui-btn ui-btn-primary w-full justify-center"
                onClick={openCreate}
              >
                <Plus className="h-3.5 w-3.5" />新建 Bot
              </button>
            ) : null}
          </div>
        </article>

        {/* ② Model provider */}
        <ProviderCard />

        {/* ③ Channels — placeholder on purpose, no fake switch and no fake state */}
        <article data-testid="connect-card-channel" className="ui-card flex min-h-[15rem] flex-col gap-3 p-4">
          <div className="flex items-start justify-between gap-2">
            <div className="flex items-center gap-2">
              <MessageCircle className="h-4 w-4 text-brand-muted" />
              <h5 className="text-xs font-bold text-gray-900">渠道</h5>
            </div>
            <CardStatus tone="muted" label="未启用" />
          </div>
          <p className="text-[10px] leading-4 text-gray-500">微信 · 即将支持</p>
          <div className="flex flex-1 items-center">
            <div className="w-full rounded-lg border border-dashed border-brand-border px-3 py-4 text-center" data-testid="connect-channel-placeholder">
              <p className="text-[11px] font-semibold text-gray-600">微信</p>
              <p className="mt-1 text-[10px] text-gray-500">下一轮启用</p>
            </div>
          </div>
          <p className="text-[10px] leading-4 text-gray-400">本轮只登记身份和模型，渠道接入单独验收。</p>
        </article>
      </div>
    </div>
  );
}

/**
 * DSH catalog mirror. The backend reads ~/.dsh/cordis.patch.yml 1:1, so this
 * block only reports what the local dsh runtime already offers — it never
 * writes DSH's file and never asks for a key.
 */
function DshSyncBlock() {
  const { data: catalog } = useSWR('api/agent/dsh-catalog', api.getDshCatalog, { revalidateOnFocus: false });
  if (!catalog) return null;
  const providerCount = catalog.providers.length;
  const modelCount = catalog.model_ids.length;
  return (
    <div data-testid="connect-dsh-sync" className="rounded-lg border border-brand-border bg-brand-surface-raised px-2.5 py-2">
      {providerCount ? (
        // One quiet line by default: the counts are a fact about the local dsh
        // file, not something to read every visit.
        <details className="group" data-testid="connect-dsh-details">
          <summary className="flex cursor-pointer list-none items-center justify-between gap-2 text-[10px] font-semibold text-gray-700 [&::-webkit-details-marker]:hidden">
            <span>DSH 配置 1:1 已同步 ✓</span>
            <ChevronRight className="h-3 w-3 shrink-0 text-gray-400 transition-transform group-open:rotate-90" />
          </summary>
          <div className="mt-1.5 space-y-0.5 border-t border-brand-border pt-1.5">
            <p className="text-[10px] text-gray-500">
              {providerCount} 个供应商 · {modelCount} 个模型
            </p>
            <p className="truncate font-mono text-[9px] text-gray-400" title={catalog.source_path}>
              默认 {catalog.default_provider}/{catalog.default_model}
            </p>
          </div>
        </details>
      ) : (
        <>
          <div className="flex items-center justify-between gap-2">
            <span className="text-[10px] font-semibold text-gray-700">DSH 配置</span>
            <span className="shrink-0 text-[9px] text-gray-400">未检测到</span>
          </div>
          <p className="mt-1 text-[10px] text-gray-400">未找到 ~/.dsh/cordis.patch.yml</p>
        </>
      )}
    </div>
  );
}

type VerifyState = '' | 'ok' | 'error';

function ProviderCard() {
  const toast = useUIStore((state) => state.toast);
  const { data: config, mutate: mutateConfig } = useSWR('api/ai-image/config', api.getAIImageConfig, {
    revalidateOnFocus: false,
  });
  const [baseURL, setBaseURL] = useState('');
  // The key lives in component state only: never in the store, never in storage.
  const [apiKey, setAPIKey] = useState('');
  const [verifying, setVerifying] = useState(false);
  const [verifyState, setVerifyState] = useState<VerifyState>('');
  const [verifyMessage, setVerifyMessage] = useState('');

  const configured = config?.credential_configured === true;
  const tone = verifyState === 'ok' ? 'success' : verifyState === 'error' ? 'danger' : 'muted';
  const label = verifyState === 'ok' ? '已验证' : verifyState === 'error' ? '验证失败' : configured ? '待验证' : '未配置';

  async function verify() {
    if (verifying) return;
    setVerifying(true);
    setVerifyState('');
    setVerifyMessage('');
    const key = apiKey.trim();
    const custom = Boolean(baseURL.trim());
    const provider = custom ? 'custom' : config?.provider || 'agnes';
    try {
      const test = await api.testAIConnection({
        category: 'image',
        provider,
        base_url: baseURL.trim(),
        // Empty key means "reuse the stored credential" (backend sentinel).
        api_key: key || (configured ? '****' : ''),
      });
      if (!test.ok) throw new Error(test.error || test.message || '模型没有响应');
      if (key) await api.saveAgnesAPIKey(key);
      try {
        await api.saveAIImageProviderConfig({ provider, base_url: baseURL.trim() });
        await mutateConfig();
      } catch (saveReason) {
        setVerifyState('error');
        setVerifyMessage(`连接可用，但保存失败：${humanError(saveReason)}`);
        toast.show(`连接可用，但保存失败：${humanError(saveReason)}`, 'error');
        return;
      }
      setAPIKey('');
      setVerifyState('ok');
      toast.show(test.message || '模型供应商已验证并保存', 'success');
    } catch (reason) {
      setVerifyState('error');
      setVerifyMessage(humanError(reason));
      toast.show(`验证失败：${humanError(reason)}`, 'error');
    } finally {
      setVerifying(false);
    }
  }

  return (
    <article data-testid="connect-card-provider" className="ui-card flex min-h-[15rem] flex-col gap-3 p-4">
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2">
          <Server className="h-4 w-4 text-brand-muted" />
          <h5 className="text-xs font-bold text-gray-900">模型供应商</h5>
        </div>
        <CardStatus tone={tone} label={label} />
      </div>
      <p className="text-[10px] leading-4 text-gray-500">换一个模型，不用换整个工具。</p>

      <DshSyncBlock />

      <div className="space-y-2">
        <label className="block">
          <span className="text-[10px] font-semibold text-gray-600">服务地址</span>
          <input
            data-testid="connect-provider-base-url"
            value={baseURL}
            onChange={(event) => setBaseURL(event.target.value)}
            placeholder={config?.base_url || 'https://api.zexapi.com'}
            autoComplete="off"
            className="mt-1 w-full rounded-lg border border-brand-border bg-white px-2.5 py-1.5 font-mono text-[10px] text-gray-800"
          />
        </label>
        <label className="block">
          <span className="text-[10px] font-semibold text-gray-600">API Key</span>
          <input
            data-testid="connect-provider-key"
            type="password"
            value={apiKey}
            onChange={(event) => setAPIKey(event.target.value)}
            placeholder={configured ? '已保存，留空则沿用' : '粘贴 API Key'}
            autoComplete="new-password"
            className="mt-1 w-full rounded-lg border border-brand-border bg-white px-2.5 py-1.5 font-mono text-[10px] text-gray-800"
          />
        </label>
      </div>

      <div className="mt-auto space-y-2">
        <button
          type="button"
          data-testid="connect-provider-verify"
          className="ui-btn ui-btn-primary w-full justify-center"
          disabled={verifying}
          onClick={() => void verify()}
        >
          {verifying ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
          {verifying ? '验证中…' : '验证'}
        </button>
        {verifyMessage ? (
          <p role="status" data-testid="connect-provider-message" className="text-[10px] leading-4 text-brand-danger">
            {verifyMessage}
          </p>
        ) : null}
        <p className="text-[9px] leading-3 text-gray-400">模型与出图参数由服务端自动选择。</p>
      </div>
    </article>
  );
}
