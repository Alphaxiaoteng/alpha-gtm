import { useMemo, useState, type FormEvent } from 'react';
import useSWR from 'swr';
import { Blocks, Bot, Code2, FileUp, Plus, ShieldCheck, X } from 'lucide-react';
import { api, type SkillCreateInput, type SkillLibraryItem } from '../api';
import { useUIStore } from '../store';
import { LocalLoadingState } from './LocalLoadingState';

type EditorMode = 'create' | 'import' | null;

const EMPTY_SKILL: SkillCreateInput = {
  id: '',
  name: '',
  description: '',
  instructions: '',
  automation: false,
};

function statusLabel(skill: SkillLibraryItem): string {
  if (skill.status === 'review_required') return '待开发审核';
  if (skill.status === 'reviewed') return '已审核上线';
  return '本地可用';
}

function sourceLabel(skill: SkillLibraryItem): string {
  return skill.source === 'system' ? 'Alpha-Nexus' : '仅本机';
}

export function SkillLibraryView({ embedded = false }: { embedded?: boolean }) {
  const { data, error, isLoading, mutate } = useSWR('/api/skills', api.skillsList);
  const [mode, setMode] = useState<EditorMode>(null);
  const [draft, setDraft] = useState<SkillCreateInput>(EMPTY_SKILL);
  const [markdown, setMarkdown] = useState('');
  const [busyID, setBusyID] = useState('');
  const [formError, setFormError] = useState('');
  const { openAgentDrawer, setAgentDraft } = useUIStore();
  const skills = data?.skills || [];
  const enabledCount = useMemo(() => skills.filter((skill) => skill.enabled).length, [skills]);

  const closeEditor = () => {
    setMode(null);
    setFormError('');
    setDraft(EMPTY_SKILL);
    setMarkdown('');
  };

  const createSkill = async (event: FormEvent) => {
    event.preventDefault();
    setFormError('');
    try {
      await api.skillCreate(draft);
      await mutate();
      closeEditor();
    } catch (cause) {
      setFormError(cause instanceof Error ? cause.message : 'Skill 创建失败');
    }
  };

  const importSkill = async (event: FormEvent) => {
    event.preventDefault();
    setFormError('');
    try {
      await api.skillImport(markdown);
      await mutate();
      closeEditor();
    } catch (cause) {
      setFormError(cause instanceof Error ? cause.message : 'SKILL.md 导入失败');
    }
  };

  const setEnabled = async (skill: SkillLibraryItem, enabled: boolean) => {
    setBusyID(skill.id);
    setFormError('');
    try {
      await api.skillSetEnabled(skill.id, enabled);
      await mutate();
    } catch (cause) {
      setFormError(cause instanceof Error ? cause.message : 'Skill 状态更新失败');
    } finally {
      setBusyID('');
    }
  };

  const createWithAgent = () => {
    setAgentDraft({
      mode: 'chat',
      message: '/skill-creator 请通过对话帮我创建一个新的 Alpha-Nexus Skill。先确认触发场景、固定输入和最小必要步骤；涉及浏览器、Playwright、账号或发布时标记为待开发审核。',
    });
    openAgentDrawer();
  };

  return (
    <section data-testid="skill-library" className={embedded ? 'w-full space-y-6' : 'mx-auto w-full max-w-6xl space-y-6 p-1'}>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="flex items-center gap-2">
            <Blocks className="h-5 w-5 text-brand-accent" />
            <h1 className="text-xl font-bold text-gray-900">Skill 库</h1>
          </div>
          <p className="mt-2 max-w-2xl text-xs leading-6 text-gray-500">
            内置 Skill 随应用启动自动更新，自定义 Skill 仅保存在本机且永不覆盖；启用后可在 Agent 中输入
            <span className="mx-1 font-mono text-brand-accent">/skill-id</span>
            或
            <span className="mx-1 font-mono text-brand-accent">@skill-id</span>
            调用。
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button data-testid="skill-create-with-agent" type="button" onClick={createWithAgent} className="inline-flex items-center gap-2 border border-brand-border bg-brand-surface px-3 py-2 text-xs font-semibold shadow-sm hover:bg-black/[0.04]">
            <Bot className="h-4 w-4 text-brand-accent" />用 Agent 创建
          </button>
          <button data-testid="skill-import" type="button" onClick={() => setMode('import')} className="inline-flex items-center gap-2 border border-brand-border bg-brand-surface px-3 py-2 text-xs font-semibold shadow-sm hover:bg-black/[0.04]">
            <FileUp className="h-4 w-4" />导入 SKILL.md
          </button>
          <button data-testid="skill-create" type="button" onClick={() => setMode('create')} className="inline-flex items-center gap-2 bg-brand-accent px-3 py-2 text-xs font-semibold text-brand-on-accent shadow-sm hover:brightness-95">
            <Plus className="h-4 w-4" />新建 Skill
          </button>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="border border-brand-border bg-brand-surface p-4">
          <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">全部</p>
          <p className="mt-1 text-2xl font-bold text-gray-900">{skills.length}</p>
        </div>
        <div className="border border-brand-border bg-brand-surface p-4">
          <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">已启用</p>
          <p className="mt-1 text-2xl font-bold text-emerald-600">{enabledCount}</p>
        </div>
        <div className="border border-brand-border bg-brand-surface p-4">
          <p className="text-[10px] font-semibold uppercase tracking-[0.16em] text-gray-400">待审核自动化</p>
          <p className="mt-1 text-2xl font-bold text-brand-warning">{skills.filter((skill) => skill.status === 'review_required').length}</p>
        </div>
      </div>

      {formError ? <div role="status" className="border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">{formError}</div> : null}
      {isLoading ? (
        <LocalLoadingState
          label="正在读取 Skill 库"
          detail="加载这台设备上已审核和启用的能力"
          className="border border-brand-border bg-brand-surface"
        />
      ) : null}
      {error ? <div className="py-14 text-center text-xs text-red-600">Skill 库暂不可用</div> : null}

      <div className="grid gap-3 md:grid-cols-2">
        {skills.map((skill) => {
          const reviewRequired = skill.status === 'review_required';
          return (
            <article key={skill.id} className="flex min-h-44 flex-col border border-brand-border bg-brand-surface p-4 shadow-sm">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="truncate text-sm font-bold text-gray-900">{skill.name}</h2>
                    <span className={`px-2 py-0.5 text-[9px] font-semibold ${reviewRequired ? 'bg-brand-warning/10 text-brand-warning' : 'bg-emerald-50 text-emerald-700'}`}>
                      {statusLabel(skill)}
                    </span>
                  </div>
                  <p className="mt-1 font-mono text-[10px] text-brand-accent">/{skill.id}</p>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={skill.enabled}
                  aria-label={`${skill.enabled ? '停用' : '启用'} ${skill.name}`}
                  disabled={busyID === skill.id || reviewRequired}
                  onClick={() => void setEnabled(skill, !skill.enabled)}
                  className={`relative h-6 w-11 shrink-0 transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${skill.enabled ? 'bg-emerald-500' : 'bg-gray-300'}`}
                >
                  <span className={`absolute top-1 h-4 w-4 bg-white shadow transition-transform ${skill.enabled ? 'left-6' : 'left-1'}`} />
                </button>
              </div>
              <p className="mt-4 flex-1 text-xs leading-6 text-gray-600">{skill.description}</p>
              <div className="mt-4 flex flex-wrap items-center gap-3 border-t border-brand-border pt-3 text-[10px] text-gray-400">
                <span className="inline-flex items-center gap-1"><ShieldCheck className="h-3.5 w-3.5" />{sourceLabel(skill)}</span>
                <span className="inline-flex items-center gap-1"><Code2 className="h-3.5 w-3.5" />{skill.runtime === 'registry' ? 'Registry 固定能力' : '指令 Skill'}</span>
                {skill.has_scripts ? <span>固定脚本需审核</span> : null}
              </div>
            </article>
          );
        })}
      </div>

      {mode ? (
        <div className="fixed inset-0 z-[80] flex items-center justify-center bg-black/30 p-6 backdrop-blur-sm">
          <form onSubmit={mode === 'create' ? createSkill : importSkill} className="w-full max-w-xl border border-brand-border bg-brand-surface p-5 shadow-2xl">
            <div className="flex items-center justify-between gap-3">
              <h2 className="text-sm font-bold text-gray-900">{mode === 'create' ? '新建本地 Skill' : '导入 SKILL.md'}</h2>
              <button type="button" onClick={closeEditor} className="p-1.5 hover:bg-black/5" aria-label="关闭"><X className="h-4 w-4" /></button>
            </div>
            {mode === 'create' ? (
              <div className="mt-4 space-y-3">
                <input required pattern="[a-z0-9][a-z0-9-]{1,63}" value={draft.id} onChange={(event) => setDraft({ ...draft, id: event.target.value })} placeholder="skill-id，例如 xhs-title-check" className="w-full border border-brand-border bg-brand-bg px-3 py-2 text-xs outline-none focus:ring-2 focus:ring-brand-accent/30" />
                <input required maxLength={80} value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} placeholder="显示名称" className="w-full border border-brand-border bg-brand-bg px-3 py-2 text-xs outline-none focus:ring-2 focus:ring-brand-accent/30" />
                <input required maxLength={400} value={draft.description} onChange={(event) => setDraft({ ...draft, description: event.target.value })} placeholder="什么时候应该使用这个 Skill" className="w-full border border-brand-border bg-brand-bg px-3 py-2 text-xs outline-none focus:ring-2 focus:ring-brand-accent/30" />
                <textarea required rows={8} value={draft.instructions} onChange={(event) => setDraft({ ...draft, instructions: event.target.value })} placeholder="只写完成任务必需的步骤、输入校验和安全边界。" className="w-full resize-y border border-brand-border bg-brand-bg px-3 py-2 text-xs leading-6 outline-none focus:ring-2 focus:ring-brand-accent/30" />
                <label className="flex items-start gap-2 text-xs text-gray-600">
                  <input type="checkbox" checked={draft.automation || false} onChange={(event) => setDraft({ ...draft, automation: event.target.checked })} className="mt-0.5" />
                  <span>包含浏览器、Playwright 或固定脚本自动化（创建后进入待审核，不能直接启用）</span>
                </label>
              </div>
            ) : (
              <div className="mt-4 space-y-3">
                <label className="block border border-dashed border-brand-border bg-brand-bg px-3 py-3 text-xs text-gray-500">
                  <span className="font-semibold text-gray-700">选择本机 SKILL.md</span>
                  <input
                    type="file"
                    accept=".md,text/markdown,text/plain"
                    onChange={(event) => {
                      const file = event.target.files?.[0];
                      if (file) void file.text().then(setMarkdown);
                    }}
                    className="mt-2 block w-full text-[11px]"
                  />
                </label>
                <textarea required rows={12} value={markdown} onChange={(event) => setMarkdown(event.target.value)} placeholder={'---\\nname: my-skill\\ndescription: This skill should be used when...\\n---\\n\\n# Instructions'} className="w-full resize-y border border-brand-border bg-brand-bg px-3 py-2 font-mono text-xs leading-6 outline-none focus:ring-2 focus:ring-brand-accent/30" />
              </div>
            )}
            <div className="mt-4 flex items-center justify-end gap-2">
              <button type="button" onClick={closeEditor} className="border border-brand-border px-4 py-2 text-xs font-semibold hover:bg-black/[0.04]">取消</button>
              <button type="submit" className="bg-brand-accent px-4 py-2 text-xs font-semibold text-brand-on-accent hover:brightness-95">{mode === 'create' ? '保存到本机' : '导入到库'}</button>
            </div>
          </form>
        </div>
      ) : null}
    </section>
  );
}
