import React, { useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  BookOpen,
  Brain,
  FileText,
  KeyRound,
  Layers,
  LoaderCircle,
  Plus,
  Search,
  Sparkles,
} from 'lucide-react';
import {
  api,
  type KnowledgeSearchResult,
} from '../api';
import { useUIStore } from '../store';

const EmptyState: React.FC<{
  icon: React.ElementType;
  title: string;
  message: string;
  actionLabel?: string;
  onAction?: () => void;
}> = ({ icon: Icon, title, message, actionLabel, onAction }) => (
  <div className="flex min-h-48 flex-col items-center justify-center p-8 text-center">
    <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-black/[0.035] text-gray-400">
      <Icon className="h-5 w-5" />
    </div>
    <h3 className="mt-3 text-xs font-bold text-gray-900">{title}</h3>
    <p className="mt-1 max-w-sm text-[10px] leading-relaxed text-gray-500">{message}</p>
    {actionLabel && onAction && (
      <button type="button" className="ui-btn ui-btn-primary mt-4" onClick={onAction}>
        <Plus className="h-3.5 w-3.5" />
        {actionLabel}
      </button>
    )}
  </div>
);

export type CognitiveMemoryTier = 'core' | 'episodic' | 'semantic' | 'procedural' | 'vault';

interface CognitiveMemoryEntry {
  id: string;
  tier: CognitiveMemoryTier;
  title: string;
  content: string;
  tags: string[];
  importance: number;
  source: string;
  updated_at: string;
  source_id?: number;
}

const TIER_META: Record<CognitiveMemoryTier, { label: string; desc: string; color: string; icon: React.ElementType }> = {
  core: {
    label: 'Core (长期核心约束)',
    desc: '始终最高优先级的业务基调、用户强偏好与安全硬红线',
    color: 'border-rose-200 bg-rose-50 text-rose-800',
    icon: Sparkles,
  },
  episodic: {
    label: 'Episodic (阶段执行实绩)',
    desc: '本次与近期会话中的事实记录、异常诊断、修复决策与复盘',
    color: 'border-brand-warning/30 bg-brand-warning/10 text-brand-warning',
    icon: Layers,
  },
  semantic: {
    label: 'Semantic (实体与领域知识)',
    desc: '行业术语、竞品特征、知识库沉淀资料及主体关系映射',
    color: 'border-brand-border bg-brand-surface-raised text-brand-fg',
    icon: BookOpen,
  },
  procedural: {
    label: 'Procedural (SOP与流程策略)',
    desc: '可复用的打法、标准化起稿流程、发布策略及审查检查单',
    color: 'border-brand-border bg-brand-surface-raised text-brand-fg',
    icon: FileText,
  },
  vault: {
    label: 'Vault (永恒凭据与��可淡化)',
    desc: '用户明确要求永不淡化、长期归档保存的关键经验与凭证规范',
    color: 'border-emerald-200 bg-emerald-50 text-emerald-800',
    icon: KeyRound,
  },
};

const LOCAL_STORAGE_KEY = 'alpha-nexus-cognitive-memories-v1';

export const CognitiveMemoryView: React.FC = () => {
  const { setAgentDraft, openAgentDrawer, toast } = useUIStore();
  const [selectedTier, setSelectedTier] = useState<CognitiveMemoryTier | 'all'>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [searchingRemote, setSearchingRemote] = useState(false);
  const [remoteSearchResults, setRemoteSearchResults] = useState<KnowledgeSearchResult[]>([]);

  // 记忆录入表单
  const [isAdding, setIsAdding] = useState(false);
  const [newTier, setNewTier] = useState<CognitiveMemoryTier>('core');
  const [newTitle, setNewTitle] = useState('');
  const [newContent, setNewContent] = useState('');
  const [newTags, setNewTags] = useState('');
  const [newImportance, setNewImportance] = useState(0.8);
  const [newSource, setNewSource] = useState('user-input');

  // 本地持久化自定义记忆
  const [memories, setMemories] = useState<CognitiveMemoryEntry[]>(() => {
    try {
      const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
      if (stored) {
        const parsed = JSON.parse(stored);
        if (Array.isArray(parsed) && parsed.length > 0) return parsed;
      }
    } catch {
      // ignore
    }
    // 不预置演示记忆：空就是空，用户能看到真实状态。
    return [];
  });

  const saveMemories = (next: CognitiveMemoryEntry[]) => {
    setMemories(next);
    try {
      localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(next));
    } catch {
      // ignore
    }
  };

  // 获取后端知识库信息（映射为 Semantic 层记忆底座）
  const { data: knowledgeBases = [] } = useSWR('api/knowledge/bases', api.getKnowledgeBases, {
    revalidateOnFocus: false,
  });

  const handleAddMemory = (e: React.FormEvent) => {
    e.preventDefault();
    if (!newTitle.trim() || !newContent.trim()) return;

    const entry: CognitiveMemoryEntry = {
      id: `mem-${Date.now()}`,
      tier: newTier,
      title: newTitle.trim(),
      content: newContent.trim(),
      tags: newTags
        .split(/[,，\s]+/)
        .map((t) => t.trim())
        .filter(Boolean),
      importance: Number(newImportance),
      source: newSource.trim() || 'user-input',
      updated_at: new Date().toISOString(),
    };

    saveMemories([entry, ...memories]);
    setNewTitle('');
    setNewContent('');
    setNewTags('');
    setIsAdding(false);
  };

  const handleDeleteMemory = (id: string) => {
    if (!window.confirm('确认归档/遗忘该条记忆？')) return;
    saveMemories(memories.filter((m) => m.id !== id));
  };

  // 跨层混合检索：本地内存过滤 + 后端语义知识库 FTS 检索
  const handleSearch = async () => {
    const q = searchQuery.trim();
    if (!q) {
      setRemoteSearchResults([]);
      return;
    }
    setSearchingRemote(true);
    try {
      const res = await api.searchKnowledge({ query: q, top_k: 8 });
      setRemoteSearchResults(res.results ?? []);
    } catch (reason) {
      setRemoteSearchResults([]);
      toast.show(reason instanceof Error ? `知识库检索失败：${reason.message}` : '知识库检索失败，只显示本地记忆', 'error');
    } finally {
      setSearchingRemote(false);
    }
  };

  const handoffMemoryToAgent = (entry: CognitiveMemoryEntry) => {
    setAgentDraft({
      mode: 'tools',
      message: `请结合以下【${TIER_META[entry.tier].label}】长期记忆执行任务：\n\n【${entry.title}】\n${entry.content}\n\n标签：${entry.tags.join(', ')} | 来源：${entry.source}`,
    });
    openAgentDrawer();
  };

  const filteredMemories = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    return memories.filter((mem) => {
      if (selectedTier !== 'all' && mem.tier !== selectedTier) return false;
      if (!q) return true;
      return (
        mem.title.toLowerCase().includes(q) ||
        mem.content.toLowerCase().includes(q) ||
        mem.tags.some((t) => t.toLowerCase().includes(q)) ||
        mem.source.toLowerCase().includes(q)
      );
    });
  }, [memories, selectedTier, searchQuery]);

  return (
    <div className="mx-auto w-full max-w-[1440px] space-y-5 pb-12 px-3 sm:px-6" data-testid="cognitive-memory-view">
      {/* 头部标题与操作 */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-brand-border pb-4">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-gray-950 flex items-center gap-2">
            <Brain className="h-5 w-5 text-brand-accent" />
            认知记忆系统 (Cognitive Memory)
          </h1>
          <p className="mt-1 text-xs text-gray-500">
            支持 5 层记忆分级架构（Core / Episodic / Semantic / Procedural / Vault）��已接入 {knowledgeBases.length} 个知识库底座。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            className="ui-btn ui-btn-primary"
            onClick={() => setIsAdding((v) => !v)}
          >
            <Plus className="h-3.5 w-3.5" />
            沉��新记忆
          </button>
        </div>
      </div>

      {/* 5层分级卡片速览 */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-3">
        {(Object.keys(TIER_META) as CognitiveMemoryTier[]).map((tierKey) => {
          const meta = TIER_META[tierKey];
          const Icon = meta.icon;
          const count = memories.filter((m) => m.tier === tierKey).length;
          const isSelected = selectedTier === tierKey;
          return (
            <button
              key={tierKey}
              type="button"
              onClick={() => setSelectedTier(isSelected ? 'all' : tierKey)}
              className={`rounded-xl border p-3.5 text-left transition-all ${
                isSelected
                  ? 'border-brand-accent bg-brand-accent/5 ring-2 ring-brand-accent/20'
                  : 'border-brand-border bg-white hover:border-gray-400'
              }`}
            >
              <div className="flex items-center justify-between">
                <span className={`inline-flex rounded-lg p-1.5 ${meta.color}`}>
                  <Icon className="h-4 w-4" />
                </span>
                <span className="text-sm font-black text-gray-900">{count}</span>
              </div>
              <h3 className="mt-2 text-xs font-bold text-gray-950">{meta.label.split(' ')[0]}</h3>
              <p className="mt-1 text-[10px] text-gray-500 line-clamp-2 leading-relaxed">{meta.desc}</p>
            </button>
          );
        })}
      </div>

      {/* 新增记忆录入面板 */}
      {isAdding && (
        <form onSubmit={handleAddMemory} className="ui-card p-5 space-y-4 border-brand-accent/30 bg-black/[0.01]">
          <div className="flex items-center justify-between border-b border-brand-border pb-3">
            <h2 className="text-sm font-bold text-gray-950 flex items-center gap-2">
              <Sparkles className="h-4 w-4 text-brand-accent" />
              写入结构化长期记忆
            </h2>
            <button
              type="button"
              className="text-xs text-gray-500 hover:text-gray-900"
              onClick={() => setIsAdding(false)}
            >
              取消
            </button>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            <div>
              <label className="block text-[10px] font-semibold text-gray-600 mb-1">记忆分层</label>
              <select
                value={newTier}
                onChange={(e) => setNewTier(e.target.value as CognitiveMemoryTier)}
                className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs"
              >
                {(Object.keys(TIER_META) as CognitiveMemoryTier[]).map((t) => (
                  <option key={t} value={t}>
                    {TIER_META[t].label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-[10px] font-semibold text-gray-600 mb-1">重要度 (0.0 ~ 1.0)</label>
              <input
                type="number"
                step="0.05"
                min="0"
                max="1"
                value={newImportance}
                onChange={(e) => setNewImportance(Number(e.target.value))}
                className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs"
              />
            </div>
            <div>
              <label className="block text-[10px] font-semibold text-gray-600 mb-1">来源标��� (Source)</label>
              <input
                type="text"
                value={newSource}
                onChange={(e) => setNewSource(e.target.value)}
                placeholder="例如：agents-team / review / user"
                className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs"
              />
            </div>
          </div>
          <div>
            <label className="block text-[10px] font-semibold text-gray-600 mb-1">记忆主题 / 摘要</label>
            <input
              type="text"
              required
              value={newTitle}
              onChange={(e) => setNewTitle(e.target.value)}
              placeholder="简要概括该记忆的核心命题..."
              className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs"
            />
          </div>
          <div>
            <label className="block text-[10px] font-semibold text-gray-600 mb-1">可复用事实与详细规范</label>
            <textarea
              required
              rows={3}
              value={newContent}
              onChange={(e) => setNewContent(e.target.value)}
              placeholder="沉淀具体结论、禁忌或策略；不写空话..."
              className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs font-mono"
            />
          </div>
          <div>
            <label className="block text-[10px] font-semibold text-gray-600 mb-1">标签 (逗号分隔)</label>
            <input
              type="text"
              value={newTags}
              onChange={(e) => setNewTags(e.target.value)}
              placeholder="例如：novel, characterGraph, publish-rule"
              className="w-full rounded-lg border border-brand-border bg-white px-3 py-2 text-xs"
            />
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <button type="submit" className="ui-btn ui-btn-primary">
              确认沉淀到记忆库
            </button>
          </div>
        </form>
      )}

      {/* 检索栏与分层筛选 */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="relative min-w-56 flex-1">
          <Search className="absolute left-3 top-2.5 h-3.5 w-3.5 text-gray-400" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && void handleSearch()}
            placeholder="全文回忆检索 (支持匹配标题、正文、标签及后端知识库)..."
            className="w-full rounded-lg border border-brand-border bg-white pl-9 pr-20 py-2 text-xs outline-none focus:border-brand-accent"
          />
          <button
            type="button"
            onClick={() => void handleSearch()}
            disabled={searchingRemote}
            className="absolute right-1.5 top-1.5 rounded bg-gray-100 hover:bg-gray-200 px-2.5 py-1 text-[11px] font-medium text-gray-700"
          >
            {searchingRemote ? <LoaderCircle className="h-3 w-3 animate-spin" /> : '回忆'}
          </button>
        </div>
        <div className="flex items-center gap-1.5 overflow-x-auto text-xs">
          <button
            type="button"
            onClick={() => setSelectedTier('all')}
            className={`rounded-lg px-3 py-1.5 font-medium transition ${
              selectedTier === 'all'
                ? 'bg-gray-900 text-white'
                : 'border border-brand-border bg-white text-gray-600 hover:bg-gray-50'
            }`}
          >
            全部 ({memories.length})
          </button>
          {(Object.keys(TIER_META) as CognitiveMemoryTier[]).map((t) => (
            <button
              key={t}
              type="button"
              onClick={() => setSelectedTier(t)}
              className={`rounded-lg px-3 py-1.5 font-medium transition ${
                selectedTier === t
                  ? 'bg-gray-900 text-white'
                  : 'border border-brand-border bg-white text-gray-600 hover:bg-gray-50'
              }`}
            >
              {t}
            </button>
          ))}
        </div>
      </div>

      {/* 远端知识库语义召回区（如果检索触发并有结果） */}
      {remoteSearchResults.length > 0 && (
        <section className="ui-card p-4 space-y-3 bg-brand-surface-raised border-brand-border">
          <div className="flex items-center justify-between">
            <h2 className="text-xs font-bold text-brand-fg flex items-center gap-1.5">
              <BookOpen className="h-4 w-4 text-brand-muted" />
              关联语义知识库命中 (Semantic Vault: {remoteSearchResults.length} 条)
            </h2>
            <button
              type="button"
              className="text-[10px] text-gray-500 hover:text-gray-900"
              onClick={() => setRemoteSearchResults([])}
            >
              收起
            </button>
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {remoteSearchResults.map((res) => (
              <div key={res.chunk_id} className="rounded-lg border border-brand-border bg-brand-surface p-3">
                <div className="flex items-center justify-between text-[10px] text-gray-500 mb-1">
                  <span className="font-semibold text-brand-fg">{res.source_name}</span>
                  <span>相关度 {res.score?.toFixed(2)}</span>
                </div>
                <p className="line-clamp-3 text-xs text-gray-700 leading-relaxed font-mono">
                  {res.content}
                </p>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* 记忆列表展示 */}
      {filteredMemories.length === 0 ? (
        <section className="ui-card">
          <EmptyState
            icon={Brain}
            title="暂无对应分层记忆"
            message="点击“沉淀新记忆”或切换分层标签查看已沉淀的认知知识。"
          />
        </section>
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {filteredMemories.map((mem) => {
            const meta = TIER_META[mem.tier];
            const Icon = meta.icon;
            return (
              <article
                key={mem.id}
                className="ui-card flex flex-col justify-between p-4 hover:shadow-sm transition-shadow"
              >
                <div>
                  <div className="flex items-start justify-between gap-2">
                    <span className={`inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[10px] font-bold ${meta.color}`}>
                      <Icon className="h-3 w-3" />
                      {meta.label.split(' ')[0]}
                    </span>
                    <span className="text-[10px] text-gray-400">
                      重要度 {(mem.importance * 100).toFixed(0)}%
                    </span>
                  </div>
                  <h3 className="mt-2 text-sm font-bold text-gray-950 leading-snug">
                    {mem.title}
                  </h3>
                  <p className="mt-2 text-xs text-gray-700 leading-relaxed whitespace-pre-wrap font-sans">
                    {mem.content}
                  </p>
                </div>

                <div className="mt-4 border-t border-brand-border/60 pt-3">
                  <div className="flex flex-wrap items-center gap-1 mb-2">
                    {mem.tags.map((t) => (
                      <span
                        key={t}
                        className="rounded bg-black/[0.035] px-1.5 py-0.5 text-[9px] font-medium text-gray-600"
                      >
                        #{t}
                      </span>
                    ))}
                  </div>
                  <div className="flex items-center justify-between text-[10px] text-gray-400">
                    <span>来源：{mem.source}</span>
                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        onClick={() => handoffMemoryToAgent(mem)}
                        className="font-semibold text-brand-accent hover:underline"
                      >
                        交给 Agent
                      </button>
                      <span>·</span>
                      <button
                        type="button"
                        onClick={() => handleDeleteMemory(mem.id)}
                        className="text-gray-400 hover:text-rose-600"
                      >
                        遗忘/归档
                      </button>
                    </div>
                  </div>
                </div>
              </article>
            );
          })}
        </div>
      )}
    </div>
  );
};
