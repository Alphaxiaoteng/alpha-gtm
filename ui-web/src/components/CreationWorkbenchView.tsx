import { useEffect, useMemo, useRef, useState } from 'react';
import useSWR from 'swr';
import { Check, ChevronDown, FileText, FolderOpen, Plus, Send, Settings2, Sparkles, Square, X } from 'lucide-react';
import { api, streamAgentHubEvents, type AgentHubConversation, type AgentHubEvent, type AgentHubMessage, type AgentHubRun, type CreationWorkflowStage, type CreationWorkflowView, type KnowledgeScope, type KnowledgeScopeOption, type SkillLibraryItem, type TrendRadarSignal } from '../api';
import { useUIStore } from '../store';

const stages: Array<{ id: CreationWorkflowStage; label: string }> = [
  { id: 'topic', label: '选题' },
  { id: 'copy', label: '文案' },
  { id: 'script', label: '脚本' },
  { id: 'review', label: '审校' },
];

function stageIndex(stage: CreationWorkflowStage) {
  return stages.findIndex((item) => item.id === stage);
}

function stageLabel(stage: CreationWorkflowStage) {
  return stages.find((item) => item.id === stage)?.label || stage;
}

function stagePlaceholder(stage: CreationWorkflowStage) {
  if (stage === 'topic') return '告诉 Agent 你想做什么；它会先问一个最关键的问题。';
  if (stage === 'copy') return '告诉 Agent 你想怎么改文案，或直接说“继续”。';
  if (stage === 'script') return '告诉 Agent 要怎么拍、怎么讲；脚本阶段会按内容 Skill 组织。';
  return '告诉 Agent 需要检查什么；确认后再保存本地草稿。';
}

function scopeKey(scope: KnowledgeScope) {
  return `${scope.base_id}:${scope.source_prefix || ''}`;
}

function scopeLabel(scope: KnowledgeScope, options: KnowledgeScopeOption[]) {
  return options.find((item) => scopeKey(item) === scopeKey(scope))?.label || `资料 ${scope.base_id}`;
}

function trendFolderKey(signal: TrendRadarSignal) {
  return signal.platform || '未分类';
}

function trendFolderSignals(signals: TrendRadarSignal[]) {
  const folders = new Map<string, TrendRadarSignal[]>();
  for (const signal of signals) {
    const key = trendFolderKey(signal);
    folders.set(key, [...(folders.get(key) || []), signal]);
  }
  return [...folders.entries()].map(([label, items]) => ({ label, items }));
}

function messageTime(value: string) {
  const timestamp = Date.parse(value);
  return Number.isFinite(timestamp) ? new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '';
}

function runErrorMessage(code?: string) {
  const messages: Record<string, string> = {
    AGENT_MODEL_NOT_VERIFIED: '当前模型还没有通过真实调用验证，请先到设置中完成验证。',
    AGENT_PROVIDER_UNAVAILABLE: '当前 Agent 暂不可用，请到设置中检查运行状态。',
    AGENT_PROVIDER_ALPHA_FAILED: 'Agent 本次运行失败，可以检查设置后重试。',
    AGENT_EVENT_STREAM_INTERRUPTED: '回答连接中断，本轮没有推进阶段。',
  };
  return messages[code || ''] || '本轮没有完成，阶段保持不变，可以重试。';
}

function conversationMessages(conversation: AgentHubConversation | null) {
  return conversation?.messages || [];
}

function latestAssistant(messages: AgentHubMessage[]) {
  return [...messages].reverse().find((message) => message.role === 'assistant');
}

function dispatchSharedConversation(conversationID: string) {
  if (typeof window === 'undefined' || !conversationID) return;
  window.dispatchEvent(new CustomEvent('alpha-nexus:agent-conversation-selected', { detail: { conversationID } }));
}

export function CreationWorkbenchView() {
  const { toast, setActiveTab } = useUIStore();
  const { data: bootstrap, mutate: refreshBootstrap } = useSWR('creation-agent-bootstrap', () => api.agentHubBootstrap());
  const { data: scopeOptions = [] } = useSWR('api/knowledge/scopes', api.getKnowledgeScopes);
  const { data: trendSummary } = useSWR('api/trend-radar/summary', api.getTrendRadarSummary);
  const { data: skillsData } = useSWR('api/skills', api.skillsList);
  const [workflows, setWorkflows] = useState<CreationWorkflowView[]>([]);
  const [workflow, setWorkflow] = useState<CreationWorkflowView | null>(null);
  const [conversation, setConversation] = useState<AgentHubConversation | null>(null);
  const [stage, setStage] = useState<CreationWorkflowStage>('topic');
  const [title, setTitle] = useState('');
  const [platform, setPlatform] = useState('');
  const [input, setInput] = useState('');
  const [selectedScopes, setSelectedScopes] = useState<KnowledgeScope[]>([]);
  const [selectedTrend, setSelectedTrend] = useState<TrendRadarSignal | null>(null);
  const [selectedTrendFolder, setSelectedTrendFolder] = useState('');
  const [openContextMenu, setOpenContextMenu] = useState<'knowledge' | 'trend' | 'skill' | null>(null);
  const [openTrendFolder, setOpenTrendFolder] = useState('');
  const [editedMessage, setEditedMessage] = useState('');
  const [streamingText, setStreamingText] = useState('');
  const [activeRun, setActiveRun] = useState<AgentHubRun | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [pulse, setPulse] = useState(false);
  const [saving, setSaving] = useState(false);
  const [knowledgeTurns, setKnowledgeTurns] = useState<Awaited<ReturnType<typeof api.agentHubKnowledgeTurns>>['turns']>([]);
  const sourceRef = useRef<AbortController | null>(null);

  const messages = useMemo(() => conversationMessages(conversation), [conversation]);
  const currentAssistant = latestAssistant(messages);
  const currentAssistantText = editedMessage || currentAssistant?.content || '';
  const selectedModel = bootstrap?.selection.model_id || '';
  const selectedProbe = bootstrap?.probes.find((probe) => probe.provider_id === bootstrap.selection.provider_id);
  const modelReady = Boolean(selectedModel && selectedProbe?.models?.includes(selectedModel));
  const trendSignals = useMemo(() => [...(trendSummary?.signals || [])].slice(0, 8), [trendSummary?.signals]);
  const trendFolders = useMemo(() => trendFolderSignals(trendSignals), [trendSignals]);
  const skills = useMemo(() => (skillsData?.skills || []).filter((skill) => skill.enabled), [skillsData?.skills]);
  const skillToken = input.match(/(?:^|\s)\/([a-z0-9-]*)$/);
  const skillSuggestions = skillToken ? skills.filter((skill) => skill.id.startsWith(skillToken[1])).slice(0, 6) : [];
  const trendContext = useMemo(() => {
    if (selectedTrend) {
      return `${selectedTrend.platform} · ${selectedTrend.title}${selectedTrend.url ? ` · ${selectedTrend.url}` : ''}`;
    }
    if (selectedTrendFolder) {
      const folder = trendFolders.find((item) => item.label === selectedTrendFolder);
      const titles = folder?.items.slice(0, 6).map((item) => item.title).filter(Boolean) || [];
      return [`趋势文件夹：${selectedTrendFolder}`, ...titles.map((title) => `- ${title}`)].join('\n');
    }
    return '';
  }, [selectedTrend, selectedTrendFolder, trendFolders]);
  const acceptedArtifacts = workflow?.workflow.accepted_artifacts || {};
  const canAdvance = Boolean(currentAssistantText.trim()) && stageIndex(stage) < stages.length - 1 && !activeRun;
  const hasConversation = Boolean(workflow && conversation);
  const selectedTrendLabel = selectedTrend
    ? `@趋势/${selectedTrendFolder} · ${selectedTrend.title}`
    : selectedTrendFolder
      ? `@趋势/${selectedTrendFolder}`
      : '';

  const loadWorkflow = async (view: CreationWorkflowView) => {
    setWorkflow(view);
    setConversation(view.conversation);
    setStage(view.workflow.stage);
    setTitle(view.workflow.title);
    setPlatform(view.workflow.platform || '');
    setSelectedScopes(view.workflow.knowledge_scopes || []);
    setSelectedTrend(null);
    setSelectedTrendFolder('');
    setOpenContextMenu(null);
    setOpenTrendFolder('');
    setEditedMessage('');
    try {
      const turns = await api.agentHubKnowledgeTurns(view.conversation.id);
      setKnowledgeTurns(turns.turns || []);
    } catch {
      setKnowledgeTurns([]);
    }
	  try { window.localStorage.setItem('alpha-nexus:creation-workflow-id', view.workflow.id); } catch { /* best effort */ }
	  void api.agentHubProviderSelection(view.conversation.provider_id, {
	    conversation_id: view.conversation.id,
	    model_id: view.conversation.model_id || '',
	  }).catch(() => undefined);
	  dispatchSharedConversation(view.conversation.id);
  };

  useEffect(() => {
    let cancelled = false;
    void api.getCreationWorkflows().then(async (items) => {
      if (cancelled) return;
      setWorkflows(items);
      let savedID = '';
      try { savedID = window.localStorage.getItem('alpha-nexus:creation-workflow-id') || ''; } catch { /* best effort */ }
      const target = items.find((item) => item.workflow.id === savedID) || items[0];
      if (target) await loadWorkflow(target);
    }).catch(() => {
      if (!cancelled) setError('创作会话列表读取失败');
    });
    return () => { cancelled = true; };
  // The initial list is the only automatic restore; all later selection is explicit.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => () => sourceRef.current?.abort(), []);

  const createWorkflow = async () => {
    const created = await api.createCreationWorkflow({
      title: title.trim() || '创作访谈',
      platform: platform.trim(),
      stage,
      knowledge_scopes: selectedScopes,
    });
    setWorkflows((current) => [created, ...current.filter((item) => item.workflow.id !== created.workflow.id)]);
    await loadWorkflow(created);
    await refreshBootstrap();
    return created;
  };

  const refreshConversation = async (workflowID: string) => {
    const next = await api.getCreationWorkflow(workflowID);
    setWorkflow(next);
	setConversation(next.conversation);
	setStage(next.workflow.stage);
	setEditedMessage('');
	setTitle(next.workflow.title);
    setKnowledgeTurns((await api.agentHubKnowledgeTurns(next.conversation.id)).turns || []);
    setWorkflows((current) => current.map((item) => item.workflow.id === workflowID ? next : item));
    dispatchSharedConversation(next.conversation.id);
  };

  const finishRun = async (run: AgentHubRun, status: 'completed' | 'failed' | 'cancelled' | 'interrupted', code?: string) => {
    setActiveRun(null);
    setStreamingText('');
    if (status === 'completed' && workflow) {
      await refreshConversation(workflow.workflow.id).catch(() => undefined);
      setNotice('Agent 已回复；确认后再推进下一步。');
    }
    if (status !== 'completed') setError(runErrorMessage(code || run.error_code));
  };

  const streamRun = (run: AgentHubRun, workflowID: string) => {
    sourceRef.current?.abort();
    const controller = new AbortController();
    sourceRef.current = controller;
    let settled = false;
    const onEvent = (event: AgentHubEvent) => {
      if (event.type === 'message.delta') {
        const text = typeof event.payload.text === 'string' ? event.payload.text : '';
        setStreamingText((current) => current + text);
      }
      if (event.type === 'run.failed') {
        setError(runErrorMessage(typeof event.payload.code === 'string' ? event.payload.code : 'AGENT_RUN_FAILED'));
      }
      if (event.type === 'run.completed' || event.type === 'run.failed' || event.type === 'run.cancelled' || event.type === 'run.interrupted') {
        settled = true;
        controller.abort();
        sourceRef.current = null;
        const status = event.type === 'run.completed' ? 'completed' : event.type === 'run.cancelled' ? 'cancelled' : event.type === 'run.interrupted' ? 'interrupted' : 'failed';
        void finishRun(run, status, typeof event.payload.code === 'string' ? event.payload.code : undefined);
        return true;
      }
      return false;
    };
    void streamAgentHubEvents(run.id, onEvent, controller.signal).catch(() => {
      if (settled || controller.signal.aborted) return;
      void finishRun(run, 'interrupted', 'AGENT_EVENT_STREAM_INTERRUPTED');
    });
    void workflowID;
  };

  const send = async () => {
    const message = input.trim();
    if (!message || activeRun) return;
    setError('');
    setNotice('');
    if (!modelReady) {
      setError('请先在设置中选择并验证当前 Alpha Agent 模型。');
      return;
    }
    setInput('');
    try {
      const current = workflow || await createWorkflow();
      const optimistic: AgentHubMessage = { id: `local_${Date.now()}`, role: 'user', content: message, context: current.conversation.context, created_at: new Date().toISOString() };
      setConversation((value) => value ? { ...value, messages: [...(value.messages || []), optimistic] } : value);
      const response = await api.runCreationWorkflow(current.workflow.id, {
        message,
        stage: current.workflow.stage,
        knowledge_scopes: selectedScopes,
        trend_context: trendContext,
      });
      setActiveRun(response.run);
      streamRun(response.run, current.workflow.id);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : 'Agent 会话启动失败');
    }
  };

  const cancel = async () => {
    if (!activeRun) return;
    try {
      await api.agentHubRunCancel(activeRun.id);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '停止失败');
    }
  };

  const adoptAndAdvance = async () => {
    if (!workflow || !canAdvance) return;
    try {
      const next = await api.advanceCreationWorkflow(workflow.workflow.id, { from_stage: stage, artifact: currentAssistantText.trim() });
      setPulse(true);
      window.setTimeout(() => setPulse(false), 420);
      await loadWorkflow(next);
      setNotice(`已采用${stageLabel(stage)}，进入${stageLabel(next.workflow.stage)}。`);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '阶段推进失败');
    }
  };

  const jumpBack = async (target: CreationWorkflowStage) => {
    if (!workflow || stageIndex(target) >= stageIndex(stage) || activeRun) return;
    try {
      const next = await api.updateCreationWorkflow(workflow.workflow.id, { stage: target });
      await loadWorkflow(next);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '阶段切换失败');
    }
  };

  const saveDraft = async () => {
    if (!workflow || stage !== 'review' || saving) return;
    const review = currentAssistantText.trim();
    const script = acceptedArtifacts.script || '';
    if (!script) {
      setError('请先采用脚本，再进行审校入库。');
      return;
    }
    setSaving(true);
    try {
      await api.commitCreationDraft({
        type: 'video',
        title: title.trim() || '未命名创作',
        caption: acceptedArtifacts.copy || '',
        body: [acceptedArtifacts.topic ? `## 选题\n\n${acceptedArtifacts.topic}` : '', acceptedArtifacts.copy ? `## 文案\n\n${acceptedArtifacts.copy}` : '', `## 脚本\n\n${script}`, review ? `## 审校\n\n${review}` : ''].filter(Boolean).join('\n\n'),
        tags_json: JSON.stringify(['创作工作台']),
        review_result: review,
        manual_review_confirmed: false,
      });
      toast.show('已保存为本地内容草稿', 'success');
      setNotice('已保存本地草稿；未触发发布。');
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '保存本地草稿失败');
    } finally {
      setSaving(false);
    }
  };

  const startNew = () => {
    sourceRef.current?.abort();
    setWorkflow(null);
    setConversation(null);
    setStage('topic');
    setTitle('');
    setPlatform('');
    setInput('');
    setSelectedScopes([]);
    setSelectedTrend(null);
    setSelectedTrendFolder('');
    setOpenContextMenu(null);
    setOpenTrendFolder('');
    setEditedMessage('');
    setKnowledgeTurns([]);
    setError('');
    setNotice('');
    try { window.localStorage.removeItem('alpha-nexus:creation-workflow-id'); } catch { /* best effort */ }
  };

  const insertSkill = (skill: SkillLibraryItem) => {
    setInput((current) => /\/([a-z0-9-]*)$/.test(current)
      ? current.replace(/\/([a-z0-9-]*)$/, `/${skill.id} `)
      : `${current}${current && !current.endsWith(' ') ? ' ' : ''}/${skill.id} `);
    setOpenContextMenu(null);
  };

  const selectTrendFolder = (label: string) => {
    setSelectedTrendFolder(label);
    setSelectedTrend(null);
    setOpenTrendFolder('');
    setOpenContextMenu(null);
  };

  const selectTrend = (signal: TrendRadarSignal) => {
    setSelectedTrendFolder(trendFolderKey(signal));
    setSelectedTrend(signal);
    setOpenTrendFolder('');
    setOpenContextMenu(null);
  };

  const selectWorkflow = (id: string) => {
    const item = workflows.find((candidate) => candidate.workflow.id === id);
    if (item) void loadWorkflow(item);
  };

  return <section data-testid="creation-workbench" className="flex h-full min-h-0 flex-col bg-brand-bg">
    <header className="shrink-0 border-b border-brand-border bg-brand-surface px-5 py-3">
      <div className="flex flex-wrap items-center gap-3">
        <strong className="text-sm text-gray-900">Alpha Agent</strong>
        <select aria-label="会话切换" value={workflow?.workflow.id || ''} onChange={(event) => selectWorkflow(event.target.value)} className="min-w-44 max-w-xs border-0 bg-transparent text-xs text-gray-600 outline-none">
          <option value="">新会话</option>
          {workflows.map((item) => <option key={item.workflow.id} value={item.workflow.id}>{item.workflow.title}</option>)}
        </select>
        <button type="button" aria-label="新建会话" onClick={startNew} className="ui-btn h-8 px-2 text-xs"><Plus className="h-3.5 w-3.5" />新建</button>
        <div className="ml-auto flex items-center gap-1.5" data-testid="creation-sop-progress" aria-label="创作阶段">
          {stages.map((item, index) => {
            const active = item.id === stage;
            const completed = index < stageIndex(stage);
            return <button key={item.id} type="button" onClick={() => void jumpBack(item.id)} disabled={index > stageIndex(stage) || activeRun !== null} aria-current={active ? 'step' : undefined} className={`group flex items-center gap-1 text-[11px] ${active ? 'font-semibold text-brand-accent' : completed ? 'text-gray-700' : 'text-gray-400'} disabled:cursor-default`}>
              <span className={`flex h-5 w-5 items-center justify-center rounded-full border text-[10px] transition-all duration-300 ${pulse && active ? 'scale-125' : ''} ${active ? 'border-brand-accent bg-brand-accent text-brand-on-accent' : completed ? 'border-gray-400 bg-gray-100' : 'border-gray-300'}`}>{completed ? <Check className="h-3 w-3" /> : index + 1}</span>
              <span className="hidden sm:inline">{item.label}</span>{index < stages.length - 1 ? <span className="mx-0.5 text-gray-300">/</span> : null}
            </button>;
          })}
        </div>
      </div>
    </header>

    <main className="min-h-0 flex-1 overflow-y-auto px-5 py-5">
      <div className="mx-auto flex max-w-3xl flex-col gap-4" data-testid="creation-agent-conversation">
        {!modelReady ? <div className="border border-brand-border bg-brand-surface px-5 py-6" data-testid="creation-model-gate"><div className="flex items-start gap-3"><Settings2 className="mt-0.5 h-4 w-4 text-brand-accent" /><div><h2 className="text-sm font-semibold text-gray-900">先连接一个已验证模型</h2><p className="mt-1 text-xs leading-5 text-gray-500">创作访谈会直接使用设置里的当前 Alpha Agent 模型。完成真实调用验证后，才会开始会话。</p><button type="button" onClick={() => setActiveTab('settings')} className="ui-btn ui-btn-primary mt-4 h-8 px-3 text-xs">去设置验证</button></div></div></div> : null}
        {hasConversation && messages.length === 0 && !streamingText ? <div className="py-20 text-center"><div className="mx-auto flex h-10 w-10 items-center justify-center rounded-full bg-brand-accent/10 text-brand-accent"><Sparkles className="h-5 w-5" /></div><p className="mt-4 text-sm text-gray-700">从一个明确目标开始。</p><p className="mt-1 text-xs text-gray-400">Agent 会围绕当前阶段，一次只问一个关键问题。</p></div> : null}
        {!hasConversation ? <div className="py-20 text-center"><div className="mx-auto flex h-10 w-10 items-center justify-center rounded-full bg-brand-accent/10 text-brand-accent"><Sparkles className="h-5 w-5" /></div><p className="mt-4 text-sm text-gray-700">从一个明确目标开始。</p><p className="mt-1 text-xs text-gray-400">例如：我想做一条关于新手露营避坑的视频。</p></div> : null}
        {messages.map((message) => {
          const assistant = message.role === 'assistant';
          const isLatest = assistant && message.id === currentAssistant?.id;
          const turn = message.run_id ? knowledgeTurns.find((item) => item.run_id === message.run_id) : undefined;
          return <article key={message.id} className={`flex ${assistant ? 'justify-start' : 'justify-end'}`}><div className={`max-w-[88%] ${assistant ? 'border border-brand-border bg-brand-surface' : 'bg-brand-accent text-brand-on-accent'} px-4 py-3`}><div className={`flex items-center gap-2 text-[10px] ${assistant ? 'text-gray-500' : 'text-white/70'}`}><span>{assistant ? 'Alpha Agent' : '你'}</span><span>{messageTime(message.created_at)}</span></div>{isLatest && assistant ? <textarea aria-label="当前 Agent 草稿" value={editedMessage || message.content} onChange={(event) => setEditedMessage(event.target.value)} className="mt-2 min-h-28 w-full resize-y border-0 bg-transparent p-0 text-xs leading-6 text-gray-800 outline-none" /> : <p className={`mt-2 whitespace-pre-wrap text-xs leading-6 ${assistant ? 'text-gray-800' : 'text-white'}`}>{message.content}</p>}{turn?.citations?.length ? <details className="mt-3 border-t border-brand-border pt-2"><summary className="cursor-pointer text-[10px] text-gray-500">来源 · {turn.citations.length}</summary><div className="mt-2 space-y-1 text-[10px] text-gray-500">{turn.citations.map((citation) => <p key={citation.chunk_id}>[KB:{citation.chunk_id}] {citation.base_name} / {citation.source_name}</p>)}</div></details> : null}{isLatest && assistant && stage !== 'review' ? <button type="button" disabled={!canAdvance} onClick={() => void adoptAndAdvance()} className="ui-btn ui-btn-primary mt-4 h-8 px-3 text-xs disabled:opacity-40"><Check className="h-3.5 w-3.5" />采用并继续</button> : null}</div></article>;
        })}
        {streamingText ? <article className="flex justify-start"><div className="max-w-[88%] border border-brand-border bg-brand-surface px-4 py-3"><div className="text-[10px] text-gray-500">Alpha Agent · 生成中</div><p className="mt-2 whitespace-pre-wrap text-xs leading-6 text-gray-800">{streamingText}</p></div></article> : null}
        {notice ? <p className="text-center text-[11px] text-emerald-700">{notice}</p> : null}
        {error ? <div className="flex items-center justify-between gap-3 border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700"><span>{error}</span><button type="button" onClick={() => setError('')} aria-label="关闭错误"><X className="h-3.5 w-3.5" /></button></div> : null}
      </div>
    </main>

    <footer className="shrink-0 border-t border-brand-border bg-brand-surface px-5 py-3">
      <div className="mx-auto max-w-3xl">
        <div className="relative mb-2 flex flex-wrap items-center gap-1.5">
          <button type="button" data-testid="creation-knowledge-folder-button" onClick={() => setOpenContextMenu((current) => current === 'knowledge' ? null : 'knowledge')} className="ui-btn h-7 px-2 text-[11px]" aria-expanded={openContextMenu === 'knowledge'}><FolderOpen className="h-3 w-3" />@资料<ChevronDown className="h-3 w-3" /></button>
          {openContextMenu === 'knowledge' ? <div data-testid="creation-knowledge-folder-menu" className="absolute bottom-9 left-0 z-20 max-h-64 w-80 overflow-y-auto border border-brand-border bg-brand-surface p-2 shadow-lg"><p className="px-2 pb-1 text-[10px] text-gray-400">选择资料文件夹</p>{scopeOptions.length ? scopeOptions.map((option) => { const selected = selectedScopes.some((scope) => scopeKey(scope) === scopeKey(option)); return <button key={scopeKey(option)} type="button" aria-pressed={selected} onClick={() => setSelectedScopes((current) => selected ? current.filter((scope) => scopeKey(scope) !== scopeKey(option)) : [...current, { base_id: option.base_id, source_prefix: option.source_prefix }])} className={`flex w-full items-center justify-between gap-2 px-2 py-2 text-left text-xs ${selected ? 'bg-brand-accent/10 text-brand-accent' : 'text-gray-700 hover:bg-black/[0.03]'}`}><span className="flex min-w-0 items-center gap-2"><FolderOpen className="h-3.5 w-3.5 shrink-0" /><span className="truncate">{option.label}</span></span>{selected ? <Check className="h-3.5 w-3.5 shrink-0" /> : null}</button>; }) : <p className="p-3 text-xs text-gray-400">暂无可附加资料文件夹</p>}</div> : null}
          <button type="button" data-testid="creation-trend-folder-button" onClick={() => { setOpenContextMenu((current) => current === 'trend' ? null : 'trend'); setOpenTrendFolder(''); }} className="ui-btn h-7 px-2 text-[11px]" aria-expanded={openContextMenu === 'trend'}><FolderOpen className="h-3 w-3" />@趋势<ChevronDown className="h-3 w-3" /></button>
          {openContextMenu === 'trend' ? <div data-testid="creation-trend-folder-menu" className="absolute bottom-9 left-20 z-20 max-h-72 w-96 overflow-y-auto border border-brand-border bg-brand-surface p-2 shadow-lg"><p className="px-2 pb-1 text-[10px] text-gray-400">选择趋势文件夹或具体趋势</p>{openTrendFolder ? <><button type="button" onClick={() => selectTrendFolder(openTrendFolder)} className="flex w-full items-center gap-2 px-2 py-2 text-left text-xs font-semibold text-brand-accent hover:bg-brand-accent/10"><FolderOpen className="h-3.5 w-3.5" />使用 @{`趋势/${openTrendFolder}`} 文件夹</button><button type="button" onClick={() => setOpenTrendFolder('')} className="px-2 py-1 text-[10px] text-gray-400 hover:text-gray-700">返回趋势文件夹</button>{trendFolders.find((folder) => folder.label === openTrendFolder)?.items.map((signal) => <button key={`${signal.platform}:${signal.video_id || signal.title}`} type="button" onClick={() => selectTrend(signal)} className="block w-full truncate px-2 py-2 text-left text-xs text-gray-700 hover:bg-black/[0.03]">@趋势 · {signal.title}</button>)}</> : trendFolders.length ? trendFolders.map((folder) => <button key={folder.label} type="button" onClick={() => setOpenTrendFolder(folder.label)} className="flex w-full items-center justify-between gap-2 px-2 py-2 text-left text-xs text-gray-700 hover:bg-black/[0.03]"><span className="flex min-w-0 items-center gap-2"><FolderOpen className="h-3.5 w-3.5 shrink-0" /><span className="truncate">{folder.label}</span></span><span className="text-[10px] text-gray-400">{folder.items.length} 条</span></button>) : <p className="p-3 text-xs text-gray-400">暂无已保存趋势文件夹</p>}</div> : null}
          {selectedScopes.map((scope) => <span key={scopeKey(scope)} className="inline-flex max-w-56 items-center gap-1 truncate rounded-full bg-brand-accent/10 px-2 py-0.5 text-[10px] text-brand-accent">@资料/{scopeLabel(scope, scopeOptions)}<button type="button" aria-label={`移除${scopeLabel(scope, scopeOptions)}`} onClick={() => setSelectedScopes((current) => current.filter((item) => scopeKey(item) !== scopeKey(scope)))}><X className="h-3 w-3" /></button></span>)}
          {selectedTrendLabel ? <span className="inline-flex max-w-72 items-center gap-1 truncate rounded-full bg-gray-100 px-2 py-0.5 text-[10px] text-gray-600">{selectedTrendLabel}<button type="button" aria-label="移除趋势" onClick={() => { setSelectedTrend(null); setSelectedTrendFolder(''); }}><X className="h-3 w-3" /></button></span> : null}
          {stage === 'review' ? <button type="button" disabled={saving || !workflow} onClick={() => void saveDraft()} className="ui-btn h-7 px-2 text-[11px] disabled:opacity-40"><FileText className="h-3 w-3" />{saving ? '保存中…' : '保存本地草稿'}</button> : null}
          {activeRun ? <button type="button" onClick={() => void cancel()} className="ui-btn h-7 px-2 text-[11px] text-red-700"><Square className="h-3 w-3" />停止</button> : null}
          <span className="ml-auto text-[10px] text-gray-400">{stageLabel(stage)}</span>
        </div>
        <form onSubmit={(event) => { event.preventDefault(); void send(); }} className="relative flex items-end gap-2 border border-brand-border bg-brand-bg px-3 py-2 focus-within:border-brand-accent">
          {skillSuggestions.length > 0 ? <div data-testid="creation-skill-suggestions" className="absolute bottom-full left-0 right-0 z-20 border border-brand-border bg-brand-surface p-1 shadow-lg">{skillSuggestions.map((skill) => <button key={skill.id} type="button" onClick={() => insertSkill(skill)} className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left hover:bg-black/[0.04]"><span className="min-w-0"><span className="block truncate text-xs font-semibold text-gray-800">{skill.name}</span><span className="block truncate font-mono text-[10px] text-brand-accent">/{skill.id}</span></span><span className="shrink-0 text-[9px] text-gray-400">调用 Skill</span></button>)}</div> : null}
          <textarea aria-label="创作访谈输入" value={input} onChange={(event) => setInput(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); void send(); } }} rows={2} className="min-h-12 flex-1 resize-none border-0 bg-transparent p-0 text-sm leading-6 outline-none" placeholder={stagePlaceholder(stage)} disabled={Boolean(activeRun)} />
          <button type="submit" aria-label="发送" disabled={!input.trim() || Boolean(activeRun) || !modelReady} className="flex h-9 w-9 items-center justify-center bg-brand-accent text-brand-on-accent disabled:cursor-not-allowed disabled:opacity-40"><Send className="h-4 w-4" /></button>
        </form>
      </div>
    </footer>
  </section>;
}
