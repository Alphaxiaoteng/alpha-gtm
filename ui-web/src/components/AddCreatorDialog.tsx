import React, { useEffect, useRef, useState } from 'react';
import { LoaderCircle, Plus, X } from 'lucide-react';
import { api } from '../api';

export type CreatorPlatform = 'douyin' | 'xhs';

type ParseResult = { platform: CreatorPlatform; userID: string } | null;

const PLATFORM_OPTIONS: Array<{ id: CreatorPlatform; label: string; hint: string }> = [
  { id: 'douyin', label: '抖音', hint: '粘贴主页链接或 sec_user_id' },
  { id: 'xhs', label: '小红书', hint: '粘贴主页链接或 user_id' },
];

/**
 * Parses a creator identifier from either a bare ID or a platform profile URL.
 * A pasted URL wins over the selected platform so users cannot silently create a
 * Douyin entry from a Xiaohongshu link.
 */
export function parseCreatorInput(value: string, fallback: CreatorPlatform): ParseResult {
  const input = value.trim();
  if (!input) return null;
  if (!/^https?:\/\//i.test(input)) {
    return /^[A-Za-z0-9_.-]{1,256}$/.test(input) ? { platform: fallback, userID: input } : null;
  }
  try {
    const url = new URL(input);
    const hostname = url.hostname.toLocaleLowerCase('en-US');
    if (hostname === 'xiaohongshu.com' || hostname.endsWith('.xiaohongshu.com')) {
      const userID = url.pathname.match(/\/user\/profile\/([^/?#]+)/)?.[1];
      return userID ? { platform: 'xhs', userID } : null;
    }
    if (hostname === 'douyin.com' || hostname.endsWith('.douyin.com')) {
      const userID = url.pathname.match(/\/user\/([^/?#]+)/)?.[1];
      return userID ? { platform: 'douyin', userID } : null;
    }
  } catch {
    return null;
  }
  return null;
}

/** Detects the platform implied by a pasted profile URL, if any. */
function platformFromURL(value: string): CreatorPlatform | null {
  const trimmed = value.trim();
  if (!/^https?:\/\//i.test(trimmed)) return null;
  try {
    const hostname = new URL(trimmed).hostname.toLocaleLowerCase('en-US');
    if (hostname === 'xiaohongshu.com' || hostname.endsWith('.xiaohongshu.com')) return 'xhs';
    if (hostname === 'douyin.com' || hostname.endsWith('.douyin.com')) return 'douyin';
  } catch {
    return null;
  }
  return null;
}

type Props = {
  open: boolean;
  onClose: () => void;
  /** Called after a successful write so the caller can refresh local data. */
  onAdded: () => unknown | Promise<unknown>;
  onError: (message: string) => void;
  onSuccess: (message: string) => void;
};

/**
 * Centered dialog for adding one Trend Radar creator. Kept separate from the
 * dashboard so the add flow has a single implementation instead of the two
 * duplicated inline forms it used to have.
 */
export default function AddCreatorDialog({ open, onClose, onAdded, onError, onSuccess }: Props) {
  const [platform, setPlatform] = useState<CreatorPlatform>('douyin');
  const [name, setName] = useState('');
  const [identifier, setIdentifier] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const nameInput = useRef<HTMLInputElement>(null);

  // Reset every time the dialog opens so a previous failure never leaks in.
  useEffect(() => {
    if (!open) return;
    setPlatform('douyin');
    setName('');
    setIdentifier('');
    setError('');
    setSaving(false);
    const timer = window.setTimeout(() => nameInput.current?.focus(), 30);
    return () => window.clearTimeout(timer);
  }, [open]);

  useEffect(() => {
    if (!open) return undefined;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !saving) onClose();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [open, saving, onClose]);

  if (!open) return null;

  const parsed = parseCreatorInput(identifier, platform);
  const canSubmit = Boolean(parsed) && !saving;

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!parsed) {
      setError('请输入有效的抖音或小红书主页链接 / ID');
      return;
    }
    setSaving(true);
    setError('');
    try {
      await api.addTrendRadarCreator({
        platform: parsed.platform,
        user_id: parsed.userID,
        name: name.trim(),
        note: '趋势雷达添加',
      });
      await onAdded();
      onSuccess('已加入本地监控名单；尚未发起网络请求');
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '添加博主失败');
      onError(cause instanceof Error ? cause.message : '添加博主失败');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-xs animate-fade-in"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !saving) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="add-creator-title"
        data-testid="add-creator-dialog"
        className="w-full max-w-md overflow-hidden rounded-xl border border-brand-border bg-brand-surface shadow-2xl"
      >
        <header className="flex items-center justify-between border-b border-brand-border px-5 py-4">
          <div>
            <h3 id="add-creator-title" className="text-sm font-bold text-brand-fg">添加监控博主</h3>
            <p className="mt-0.5 text-[11px] text-brand-muted">只写入本地名单，不会联网</p>
          </div>
          <button type="button" aria-label="关闭添加博主弹窗" onClick={onClose} disabled={saving} className="p-1 text-brand-muted transition hover:text-brand-fg disabled:opacity-40">
            <X className="h-4 w-4" />
          </button>
        </header>

        <form onSubmit={submit}>
          <div className="space-y-4 px-5 py-5">
            <label className="block">
              <span className="mb-1.5 block text-xs font-semibold text-brand-fg">博主名称</span>
              <input
                ref={nameInput}
                data-testid="add-creator-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                maxLength={80}
                placeholder="例如：AI 效率研究所"
                className="w-full rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-sm outline-none focus:border-brand-accent"
              />
              <span className="mt-1 block text-[10px] text-brand-muted">可留空，之后按平台资料自动补全</span>
            </label>

            <div>
              <span className="mb-1.5 block text-xs font-semibold text-brand-fg">平台</span>
              <div className="grid grid-cols-2 gap-2" role="radiogroup" aria-label="平台">
                {PLATFORM_OPTIONS.map((option) => {
                  const active = platform === option.id;
                  return (
                    <button
                      key={option.id}
                      type="button"
                      role="radio"
                      aria-checked={active}
                      data-testid={`add-creator-platform-${option.id}`}
                      onClick={() => setPlatform(option.id)}
                      className={`rounded-lg border px-3 py-2 text-sm font-semibold transition ${
                        active
                          ? 'border-brand-accent bg-brand-accent/10 text-brand-accent'
                          : 'border-brand-border text-brand-fg-subtle hover:border-brand-accent/40'
                      }`}
                    >
                      {option.label}
                    </button>
                  );
                })}
              </div>
            </div>

            <label className="block">
              <span className="mb-1.5 block text-xs font-semibold text-brand-fg">主页链接或 ID</span>
              <input
                data-testid="add-creator-identifier"
                value={identifier}
                onChange={(event) => {
                  const next = event.target.value;
                  setIdentifier(next);
                  // 粘贴链接时自动切换平台，避免链接与所选平台不一致
                  const detected = platformFromURL(next);
                  if (detected) setPlatform(detected);
                }}
                placeholder={PLATFORM_OPTIONS.find((option) => option.id === platform)?.hint}
                className="w-full rounded-lg border border-brand-border bg-brand-surface px-3 py-2 text-sm outline-none focus:border-brand-accent"
              />
              {identifier.trim() && !parsed ? (
                <span className="mt-1 block text-[10px] text-rose-600">无法识别，请粘贴主页链接或纯 ID</span>
              ) : null}
            </label>

            {error ? (
              <p data-testid="add-creator-error" className="rounded-lg bg-rose-50 px-3 py-2 text-xs text-rose-700">{error}</p>
            ) : null}
          </div>

          <footer className="flex items-center justify-end gap-2 border-t border-brand-border px-5 py-4">
            <button type="button" className="ui-btn" onClick={onClose} disabled={saving}>取消</button>
            <button type="submit" data-testid="add-creator-submit" className="ui-btn ui-btn-primary" disabled={!canSubmit}>
              {saving ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
              加入名单
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}
