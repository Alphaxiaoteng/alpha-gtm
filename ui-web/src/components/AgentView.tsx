import React, { useCallback, useEffect, useRef, useState } from 'react';
import useSWR from 'swr';
import {
  Loader2,
  Send,
  Trash2,
  ChevronDown,
  Plus,
  Paperclip,
  ImagePlus,
  FolderOpen,
  MessageSquarePlus,
  X,
  FolderUp,
  Sparkles,
} from 'lucide-react';
import {
  api,
  type CodingSession,
  type CodingSessionSummary,
  type CodingMsg,
} from '../api';
import { useUIStore } from '../store';

export interface ReasoningOption {
  id: string;
  label: string;
  sub?: string;
}

export function getModelReasoningOptions(modelId?: string): ReasoningOption[] {
  const lower = (modelId || '').toLowerCase();

  // Anthropic Claude 3.7 Sonnet official budget options
  if (lower.includes('claude-3-7') || lower.includes('claude-3.7')) {
    return [
      { id: 'auto', label: 'Auto 智能思考', sub: '根据复杂度动态匹配 Token 预算' },
      { id: 'none', label: 'None 关闭思考', sub: 'budget_tokens: 0' },
      { id: 'low', label: 'Low 低强度 (1k tokens)', sub: 'budget_tokens: 1024' },
      { id: 'medium', label: 'Medium 中强度 (4k tokens)', sub: 'budget_tokens: 4096' },
      { id: 'high', label: 'High 高强度 (16k tokens)', sub: 'budget_tokens: 16384' },
    ];
  }

  // OpenAI / DeepSeek / Volcengine standard reasoning_effort options
  return [
    { id: 'auto', label: 'Auto 智能思考', sub: '根据输入复杂度自动判断' },
    { id: 'none', label: 'None 关闭思考', sub: 'reasoning_effort: none' },
    { id: 'low', label: 'Low 低强度思考', sub: 'reasoning_effort: low' },
    { id: 'medium', label: 'Medium 中强度思考', sub: 'reasoning_effort: medium' },
    { id: 'high', label: 'High 高强度思考', sub: 'reasoning_effort: high' },
  ];
}

const PRESET_MODELS = [
  { id: 'deepseek-v4.1-flash', name: 'DeepSeek V4.1 Flash' },
  { id: 'deepseek-v4-pro', name: 'DeepSeek V4 Pro' },
  { id: 'gemini-3.8-flash', name: 'Gemini 3.8 Flash' },
  { id: 'claude-sonnet-4-6-thinking', name: 'Claude Sonnet 4.6 (Thinking)' },
  { id: 'qwen3.8-flash', name: 'Qwen 3.8 Flash' },
];

const TOOL_MEDIA_ACCEPT = 'image/*,.mp4,.mov,.webm,.mkv,.mp3,.wav,.m4a';
const CHAT_ATTACHMENT_ACCEPT = 'image/*,.txt,.md,.json,.csv,.mp4,.mov';
const toolMediaFilePattern = /\.(?:mp4|mov|webm|mkv|jpe?g|png|gif|webp|mp3|wav|m4a)$/i;
const DEFAULT_AGENT_MODEL = 'deepseek-v4.1-flash';

function isToolMediaFile(name: string): boolean {
  return toolMediaFilePattern.test(name.trim());
}

/** 模型 id → 侧栏/工具栏短标签。 */
function shortModelLabel(raw?: string): string {
  if (!raw) return 'deepseek-v4.1-flash';
  const id = raw.includes('/') ? raw.split('/').pop()! : raw;
  const clean = id.replace(/^proxy[_/]?/i, '').replace(/-free$/i, '');
  if (clean.length <= 22) return clean;
  return clean.slice(0, 20) + '…';
}

// Older sessions can contain the proxy's raw transport envelope. Keep
// history readable while the backend now writes the same actionable message.
function formatAgentMessage(raw: string): string {
  const value = raw.trim();
	// 旧版会话曾把“本地模型网关未连接”持久化为助手文本。当前产品只
	// 支持豆包 API；明确标注为历史配置失效，避免误导为当前请求已执行。
	if (/本地模型网关未连接|本地代理后重试/.test(value)) {
		return '旧会话的模型配置已失效，模型没有执行。请新建会话并使用已配置的豆包 API。';
	}
  try {
    const parsed = JSON.parse(value) as { error?: { message?: unknown } };
    const message = parsed.error?.message;
    if (typeof message === 'string' && /connection refused|connection reset|no such host|network is unreachable|timed out/i.test(message)) {
      return '豆包 API 暂时不可达，模型没有执行。请检查网络与本机 API 配置后重试。';
    }
  } catch {
    // Normal assistant text is not JSON.
  }
  return raw;
}

/**
 * 多会话对话：左会话列表 + 中间单栏聊天 + 底部署名栏。
 * 风格跟随全局 skin（默认 Hermes Agent 黑金官方气质）。
 */
export const AgentView: React.FC = () => {
  const { toast, setActiveTab, agentDraft, clearAgentDraft } = useUIStore();
  const showToast = toast.show;
  const [sessions, setSessions] = useState<CodingSessionSummary[]>([]);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [active, setActive] = useState<CodingSession | null>(null);
  const [effort, setEffort] = useState('auto');
  const [mode, setMode] = useState<'chat' | 'tools'>('chat');
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const [messageQueue, setMessageQueue] = useState<{ text: string; files: string[] }[]>([]);
  const [pendingFiles, setPendingFiles] = useState<{ path: string; name: string }[]>([]);
  const [menu, setMenu] = useState<'effort' | 'model' | null>(null);
  const [worksOpen, setWorksOpen] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const worksRef = useRef<HTMLInputElement>(null);
  const composerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const bootstrappedRef = useRef(false);

  const { data: aiConfig, mutate: mutateAIConfig } = useSWR('api/ai/config', api.getAIConfig);
  const { data: status, mutate: mutateStatus } = useSWR('api/coding/status', api.codingStatus, {
    revalidateOnFocus: true,
  });

  const enterToolsMode = () => {
    if (pendingFiles.some((file) => !isToolMediaFile(file.name))) {
      toast.show('工具协同附件仅支持图片、视频或音频素材；请移除文本附件后再切换。', 'error');
      return;
    }
    setMode('tools');
  };

  const refreshSessions = useCallback(async () => {
    try {
      const res = await api.codingSessions();
      setSessions(res.sessions || []);
      return res.sessions || [];
    } catch (e) {
      showToast(String(e), 'error');
      return [];
    }
  }, [showToast]);

  const loadSession = useCallback(
    async (id: string) => {
      try {
        const s = await api.codingSessionGet(id);
        setActive(s);
        setActiveId(s.id);
        if (s.effort) setEffort(s.effort);
      } catch (e) {
        showToast(String(e), 'error');
      }
    },
    [showToast],
  );

  useEffect(() => {
    if (!status || bootstrappedRef.current) return;
    bootstrappedRef.current = true;
    void (async () => {
      const list = await refreshSessions();
      const defEffort = status.default_effort || 'medium';
      if (list.length > 0) {
        await loadSession(list[0].id);
      } else {
        const s = await api.codingSessionCreate({
          engine: 'doubao_api',
          model: DEFAULT_AGENT_MODEL,
          effort: defEffort,
        });
        await refreshSessions();
        await loadSession(s.id);
      }
    })();
  }, [loadSession, refreshSessions, status]);

  // A Studio handoff only prepares a local Tool Agent prompt. The user must
  // still explicitly send it before any model or tool is invoked.
  useEffect(() => {
    if (!agentDraft || !activeId) return;
    setInput(agentDraft.message);
    if (agentDraft.mode === 'tools') {
      setMode('tools');
    } else {
      setMode('chat');
    }
    clearAgentDraft();
    inputRef.current?.focus();
  }, [activeId, agentDraft, clearAgentDraft]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [active?.messages, sending]);

  useEffect(() => {
    const onDoc = (e: MouseEvent) => {
      if (!composerRef.current?.contains(e.target as Node)) setMenu(null);
    };
    document.addEventListener('mousedown', onDoc);
    return () => document.removeEventListener('mousedown', onDoc);
  }, []);

  const newSession = async () => {
    try {
      const s = await api.codingSessionCreate({
        engine: 'doubao_api',
        model: DEFAULT_AGENT_MODEL,
        effort,
      });
      await refreshSessions();
      await loadSession(s.id);
      setPendingFiles([]);
      setInput('');
      inputRef.current?.focus();
    } catch (e) {
      toast.show(String(e), 'error');
    }
  };

  const deleteSession = async (id: string, e?: React.MouseEvent) => {
    e?.stopPropagation();
    try {
      await api.codingSessionDelete(id);
      const list = await refreshSessions();
      if (activeId === id) {
        if (list[0]) await loadSession(list[0].id);
        else {
          setActive(null);
          setActiveId(null);
          await newSession();
        }
      }
    } catch (err) {
      toast.show(String(err), 'error');
    }
  };

  const onPickFiles = async (files: FileList | null) => {
    if (!files?.length || !activeId) return;
    for (const f of Array.from(files)) {
      if (mode === 'tools' && !isToolMediaFile(f.name)) {
        toast.show('工具协同附件仅支持图片、视频或音频素材。', 'error');
        continue;
      }
      try {
        const up = await api.codingUpload(activeId, f);
        setPendingFiles((p) => [...p, { path: up.path, name: up.name || f.name }]);
      } catch (err) {
        toast.show(`上传失败: ${err}`, 'error');
      }
    }
  };

  const onWorksUpload = async (files: FileList | null) => {
    if (!files?.length) return;
    try {
      const res = await api.codingWorksUpload(files, '_inbox');
      toast.show(
        res.indexed_media > 0
          ? `已放入作品区 ${res.count} 个；Studio 已索引 ${res.indexed_media} 个媒体`
          : `已放入作品区 ${res.count} 个`,
        'success',
      );
      setWorksOpen(false);
    } catch (e) {
      toast.show(String(e), 'error');
    }
  };

  const currentModel = aiConfig?.agent_model || 'deepseek-v4.1-flash';
  const reasoningOptions = getModelReasoningOptions(currentModel);
  const activeEffortObj = reasoningOptions.find((e) => e.id === effort) || reasoningOptions[0];
  const effortLabel = activeEffortObj.label;

  const executeChatTurn = useCallback(
    async (text: string, files: string[]) => {
      if (!activeId) return;
      setSending(true);
      try {
        const res = await api.codingChat({
          session_id: activeId,
          engine: 'doubao_api',
          message: text || '请查看附件',
          model: currentModel,
          effort,
          thinking: effort !== 'none',
          files,
          mode,
        });
        await loadSession(activeId);
        await refreshSessions();
        if (!res.ok) toast.show(res.error || '发送失败', 'error');
      } catch (e) {
        toast.show(String(e), 'error');
        await loadSession(activeId);
      } finally {
        setSending(false);
        inputRef.current?.focus();
      }
    },
    [activeId, currentModel, effort, loadSession, mode, refreshSessions, toast],
  );

  // Auto-process message queue when current turn finishes
  useEffect(() => {
    if (!sending && messageQueue.length > 0 && activeId) {
      const nextMsg = messageQueue[0];
      setMessageQueue((prev) => prev.slice(1));
      void executeChatTurn(nextMsg.text, nextMsg.files);
    }
  }, [sending, messageQueue, activeId, executeChatTurn]);

  const send = async () => {
    const text = input.trim();
    if ((!text && pendingFiles.length === 0) || !activeId) return;
    setInput('');
    const files = pendingFiles.map((f) => f.path);
    setPendingFiles([]);

    const isQueued = sending;
    const optimistic: CodingMsg = {
      id: `tmp-${Date.now()}`,
      role: 'user',
      text: isQueued ? `${text} (排队待追加处理)` : text || '（附件）',
      files,
      created_at: new Date().toISOString(),
    };

    setActive((a) => (a ? { ...a, messages: [...(a.messages || []), optimistic] } : a));

    if (isQueued) {
      setMessageQueue((prev) => [...prev, { text, files }]);
      toast.show('助手处理中，追加消息已存入排队队列（上一轮完成后自动发送）', 'info');
      return;
    }

    await executeChatTurn(text, files);
  };

  const msgs = active?.messages || [];

  return (
    <div
      className="agent-studio claude-desk flex h-full min-h-0 w-full overflow-hidden"
      data-testid="agent-view"
    >
      {/* 左：会话列表 — 禁止负 margin，避免被主侧栏裁切首字 */}
      <aside className="claude-desk-aside w-[268px] shrink-0 flex flex-col border-r border-brand-border min-w-0 overflow-hidden">
        <div className="px-4 pt-5 pb-3 shrink-0">
          <button
            type="button"
            onClick={() => void newSession()}
            className="w-full inline-flex items-center justify-center gap-2 py-2.5 text-[13px] font-medium rounded-[var(--ui-radius-btn)] border border-brand-border bg-brand-surface text-[var(--brand-fg)] hover:bg-[var(--brand-soft)] transition shadow-[var(--ui-shadow)]"
          >
            <MessageSquarePlus className="w-4 h-4 text-[var(--brand-accent)]" />
            新建会话
          </button>
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto overflow-x-hidden px-3 pb-3 space-y-1">
          {sessions.map((s) => {
            const on = s.id === activeId;
            const modelTag = shortModelLabel(s.model);
            return (
              <button
                key={s.id}
                type="button"
                onClick={() => void loadSession(s.id)}
                className={[
                  'group w-full text-left px-3 py-2.5 rounded-[var(--ui-radius-btn)] transition flex items-start gap-2 min-w-0',
                  on
                    ? 'bg-[var(--ui-nav-active-bg)] text-[var(--brand-fg)]'
                    : 'text-[var(--brand-fg-subtle)] hover:bg-black/[0.03]',
                ].join(' ')}
              >
                <span className="flex-1 min-w-0 overflow-hidden">
                  <span
                    className={[
                      'block truncate leading-snug text-[13px]',
                      on ? 'font-semibold' : 'font-medium',
                    ].join(' ')}
                    title={s.title || '新会话'}
                  >
                    {s.title || '新会话'}
                  </span>
                  {modelTag ? (
                    <span
                      className="block text-[11px] text-[var(--brand-muted)] font-normal mt-0.5 truncate tracking-normal"
                      title={s.model}
                    >
                      {modelTag}
                    </span>
                  ) : null}
                </span>
                <span
                  role="button"
                  tabIndex={0}
                  onClick={(ev) => void deleteSession(s.id, ev)}
                  onKeyDown={(ev) => {
                    if (ev.key === 'Enter' || ev.key === ' ') void deleteSession(s.id, ev as unknown as React.MouseEvent);
                  }}
                  className="opacity-0 group-hover:opacity-100 p-1 text-[var(--brand-muted)] hover:text-red-500 shrink-0 rounded-[var(--ui-radius)]"
                  aria-label="删除会话"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </span>
              </button>
            );
          })}
        </div>

        <div className="px-4 py-3 border-t border-brand-border space-y-1 shrink-0">
          <button
            type="button"
            onClick={() => setWorksOpen((v) => !v)}
            className="w-full flex items-center gap-2 px-2 py-2 rounded-[var(--ui-radius-btn)] text-[12px] text-[var(--brand-muted)] hover:bg-black/[0.03]"
            data-testid="agent-works-toggle"
          >
            <FolderOpen className="w-3.5 h-3.5 shrink-0" />
            <span className="truncate">作品素材</span>
          </button>
          {worksOpen ? (
            <div className="px-1 pb-1 space-y-2">
              <p className="text-[10px] text-gray-400 leading-relaxed px-1">
				发布作品保存在本地 works；视频和文章在“自媒体”中继续编辑。
              </p>
              <button
                type="button"
				onClick={() => setActiveTab('social-media')}
                className="w-full rounded-[var(--ui-radius-btn)] border border-brand-border px-2 py-1.5 text-[11px] text-[var(--brand-fg-subtle)] hover:bg-black/[0.03]"
				data-testid="agent-open-content"
              >
				打开本地内容
              </button>
              <button
                type="button"
                onClick={() => worksRef.current?.click()}
                className="agent-primary-action w-full inline-flex items-center justify-center gap-1.5 py-2 text-[12px] font-medium rounded-[var(--ui-radius-btn)] text-white"
                style={{ background: 'var(--brand-accent)' }}
              >
                <FolderUp className="w-3.5 h-3.5" />
                上传
              </button>
              <input
                ref={worksRef}
                type="file"
                multiple
                className="hidden"
                onChange={(e) => void onWorksUpload(e.target.files)}
              />
            </div>
          ) : null}
        </div>
      </aside>

      {/* 中：单栏对话（Claude 居中阅读宽） */}
      <div className="flex-1 flex flex-col min-w-0 min-h-0 bg-[var(--brand-bg)]">
        <header className="shrink-0 h-12 px-6 flex items-center justify-between border-b border-brand-border/60">
          <h3 className="type-title text-[14px] font-semibold text-gray-900 truncate max-w-[60%]">
            {active?.title || '新会话'}
          </h3>
          <button
            type="button"
            onClick={() => void mutateStatus()}
            className="text-gray-400 hover:text-gray-700 p-1.5 rounded-lg hover:bg-black/[0.04]"
            title="刷新模型"
          >
            <Sparkles className="w-4 h-4" />
          </button>
        </header>

        <div className="flex-1 overflow-y-auto">
          <div className="max-w-[720px] mx-auto px-5 py-8 space-y-6 min-h-full flex flex-col">
            {msgs.length === 0 && !sending ? (
              <div className="flex-1 flex flex-col items-center justify-center text-center py-16">
                <p className="type-title text-[22px] font-semibold text-gray-900 tracking-tight">
                  今天想做什么？
                </p>
                <p className="text-[13.5px] text-gray-500 mt-2 max-w-md leading-relaxed">
                  多轮对话 · 可续聊。仅使用固定直连的豆包 API。
                </p>
                <div className="flex flex-wrap justify-center gap-2 mt-8">
                  {[
                    { text: '检查账号状态', mode: 'tools' as const },
                    { text: '整理作品素材', mode: 'tools' as const },
                    { text: '写一条配文', mode: 'chat' as const },
                  ].map((s) => (
                    <button
                      key={s.text}
                      type="button"
                      onClick={() => {
                        setInput(s.text);
                        if (s.mode === 'tools') enterToolsMode();
                        else setMode('chat');
                        inputRef.current?.focus();
                      }}
                      className="px-4 py-2 text-[12.5px] font-medium rounded-full border border-brand-border bg-brand-surface text-gray-700 hover:bg-[var(--brand-soft)] transition"
                    >
                      {s.text}
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              <>
                {msgs.map((m) => (
                  <div key={m.id} className="flex gap-3">
                    {m.role !== 'user' ? (
                      <div
                        className="w-7 h-7 rounded-full shrink-0 mt-0.5 flex items-center justify-center text-[11px] font-semibold"
                        style={{
                          background: 'var(--brand-soft)',
                          color: 'var(--brand-accent)',
                        }}
                      >
                        α
                      </div>
                    ) : (
                      <div className="w-7 shrink-0" />
                    )}
                    <div
                      className={[
                        'flex-1 min-w-0',
                        m.role === 'user' ? 'agent-message-user' : 'agent-message-assistant',
                      ].join(' ')}
                    >
                      <div
                        className={[
                          'text-[11px] font-medium mb-1',
                          m.role === 'user' ? 'text-gray-400' : 'text-gray-500',
                        ].join(' ')}
                      >
                        {m.role === 'user'
                          ? '你'
                          : m.engine === 'llm_tools'
                            ? ['本地工具协同', m.duration].filter(Boolean).join(' · ')
                            : [shortModelLabel(m.model), m.duration].filter(Boolean).join(' · ') ||
                              '助手'}
                      </div>
                      <div
                        className={[
                          'text-[14.5px] leading-[1.65] whitespace-pre-wrap break-words',
                          m.role === 'user'
                            ? 'text-gray-900'
                            : m.error
                              ? 'text-red-700'
                              : 'text-gray-800',
                        ].join(' ')}
                      >
                        {formatAgentMessage(m.text)}
                      </div>
                      {m.files?.length ? (
                        <div className="mt-2 flex flex-wrap gap-1.5">
                          {m.files.map((f) => (
                            <span
                              key={f}
                              className="inline-flex items-center gap-1 text-[11px] px-2 py-1 rounded-lg border border-brand-border bg-brand-surface text-gray-600"
                            >
                              <Paperclip className="w-3 h-3" />
                              {f.split('/').pop()}
                            </span>
                          ))}
                        </div>
                      ) : null}
                    </div>
                  </div>
                ))}
                {sending ? (
                  <div className="flex gap-3 items-center text-[13px] text-gray-400 pl-10">
                    <Loader2 className="w-3.5 h-3.5 animate-spin text-[var(--brand-accent)]" />
                    思考中…
                  </div>
                ) : null}
                <div ref={bottomRef} />
              </>
            )}
          </div>
        </div>

        {/* 底部署名 — Claude 式悬浮输入 */}
        <div className="shrink-0 px-4 pb-5 pt-2" ref={composerRef}>
          <div className="max-w-[720px] mx-auto">
            {messageQueue.length > 0 ? (
              <div className="flex items-center justify-between px-3 py-1.5 mb-2 rounded-xl bg-brand-surface-raised border border-brand-border text-brand-muted text-xs font-medium animate-pulse">
                <span>消息排队中：已有 {messageQueue.length} 条追加需求等待上一轮完成后自动发送</span>
                <button
                  type="button"
                  onClick={() => setMessageQueue([])}
                  className="hover:text-brand-fg underline text-[11px]"
                >
                  清空队列
                </button>
              </div>
            ) : null}
            {pendingFiles.length > 0 ? (
              <div className="flex flex-wrap gap-1.5 mb-2 px-1">
                {pendingFiles.map((f) => (
                  <span
                    key={f.path}
                    className="inline-flex items-center gap-1 text-[11px] px-2.5 py-1 rounded-full border border-brand-border bg-brand-surface"
                  >
                    <ImagePlus className="w-3 h-3" />
                    {f.name}
                    <button
                      type="button"
                      onClick={() => setPendingFiles((p) => p.filter((x) => x.path !== f.path))}
                      className="text-gray-400 hover:text-gray-700"
                    >
                      <X className="w-3 h-3" />
                    </button>
                  </span>
                ))}
              </div>
            ) : null}

            <div className="agent-composer rounded-2xl border border-brand-border bg-brand-surface shadow-[var(--ui-shadow)] focus-within:border-[color-mix(in_srgb,var(--brand-accent)_45%,var(--brand-border))] transition">
              <textarea
                ref={inputRef}
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault();
                    void send();
                  }
                }}
                rows={2}
                disabled={sending}
                placeholder="给助手发消息…"
                className="w-full resize-none bg-transparent px-4 pt-3.5 pb-1 text-[14.5px] text-gray-900 placeholder:text-gray-400 focus:outline-none disabled:opacity-60"
                data-testid="agent-chat-input"
              />
              <div className="flex items-center justify-between gap-2 px-2.5 pb-2.5">
                <div className="flex items-center gap-1 flex-wrap min-w-0">
                  <button
                    type="button"
                    title={activeId ? '附件' : '会话初始化中'}
                    disabled={!activeId || sending}
                    onClick={() => fileRef.current?.click()}
                    className="p-2 rounded-xl text-gray-500 hover:bg-black/[0.04] disabled:opacity-35"
                  >
                    <Plus className="w-4 h-4" />
                  </button>
                  <input
                    ref={fileRef}
                    type="file"
                    accept={mode === 'tools' ? TOOL_MEDIA_ACCEPT : CHAT_ATTACHMENT_ACCEPT}
                    multiple
                    disabled={!activeId || sending}
                    className="hidden"
                    data-testid="agent-attachment-input"
                    onChange={(e) => void onPickFiles(e.target.files)}
                  />

                  {/* Model Selector Pill */}
                  <Pill
                    open={menu === 'model'}
                    onToggle={() => setMenu(menu === 'model' ? null : 'model')}
                    label={shortModelLabel(currentModel)}
                    dataTestId="agent-model-selector"
                  >
                    {PRESET_MODELS.map((m) => (
                      <MenuRow
                        key={m.id}
                        active={currentModel === m.id}
                        title={m.name}
                        sub={m.id}
                        onClick={async () => {
                          if (aiConfig) {
                            await api.updateAIConfig({
                              ...aiConfig,
                              agent_model: m.id,
                              coding_model: m.id,
                              plan_model: m.id,
                            });
                            await mutateAIConfig();
                          }
                          setMenu(null);
                        }}
                      />
                    ))}
                  </Pill>

                  {/* Official Reasoning Effort Pill */}
                  <Pill
                    open={menu === 'effort'}
                    onToggle={() => setMenu(menu === 'effort' ? null : 'effort')}
                    label={effortLabel}
                    dataTestId="agent-effort-selector"
                  >
                    {reasoningOptions.map((e) => (
                      <MenuRow
                        key={e.id}
                        active={effort === e.id}
                        title={e.label}
                        sub={e.sub}
                        onClick={() => {
                          setEffort(e.id);
                          setMenu(null);
                        }}
                      />
                    ))}
                  </Pill>
                </div>

                <button
                  type="button"
                  disabled={!activeId || sending || (!input.trim() && !pendingFiles.length)}
                  onClick={() => void send()}
                  className="agent-send p-2 rounded-xl text-white disabled:opacity-35 shrink-0"
                  style={{ background: 'var(--brand-accent)' }}
                  data-testid="agent-chat-send"
                >
                  {sending ? (
                    <Loader2 className="w-4 h-4 animate-spin" />
                  ) : (
                    <Send className="w-4 h-4" />
                  )}
                </button>
              </div>
            </div>
            <p className="text-center text-[10px] text-gray-400 mt-2">
              Enter 发送 · Shift+Enter 换行
            </p>
          </div>
        </div>
      </div>
    </div>
  );
};

function Pill({
  label,
  open,
  onToggle,
  children,
  dataTestId,
}: {
  label: string;
  open: boolean;
  onToggle: () => void;
  children: React.ReactNode;
  dataTestId?: string;
}) {
  return (
    <div className="relative">
      <button
        type="button"
        data-testid={dataTestId}
        onClick={onToggle}
        className={[
          'inline-flex items-center gap-0.5 px-2.5 py-1.5 rounded-xl text-[11.5px] font-medium max-w-[12rem] text-gray-600 hover:bg-black/[0.04]',
          open ? 'bg-black/[0.05] text-gray-900' : '',
        ].join(' ')}
      >
        <span className="truncate">{label}</span>
        <ChevronDown className={['w-3 h-3 opacity-50 shrink-0', open ? 'rotate-180' : ''].join(' ')} />
      </button>
      {open ? (
        <div className="absolute bottom-[calc(100%+6px)] left-0 z-30 min-w-[15rem] max-h-64 overflow-y-auto rounded-2xl border border-brand-border bg-brand-surface shadow-xl p-1.5">
          {children}
        </div>
      ) : null}
    </div>
  );
}

function MenuRow({
  title,
  sub,
  active,
  onClick,
}: {
  title: string;
  sub?: string;
  active?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={[
        'w-full text-left px-2.5 py-2 rounded-xl text-[12.5px]',
        active
          ? 'bg-[var(--ui-nav-active-bg)] font-semibold text-gray-900'
          : 'hover:bg-black/[0.03] text-gray-700',
      ].join(' ')}
    >
      <div className="truncate">{title}</div>
      {sub ? <div className="text-[9px] text-gray-400 font-mono truncate mt-0.5">{sub}</div> : null}
    </button>
  );
}

export default AgentView;
