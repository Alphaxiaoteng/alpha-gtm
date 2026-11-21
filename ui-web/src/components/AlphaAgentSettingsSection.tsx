import { useEffect, useState } from 'react';
import useSWR from 'swr';
import { Bot, CheckCircle2, Loader2, RefreshCw } from 'lucide-react';
import { api, type AgentHubProviderID, type CreationModelRoleID } from '../api';
import { preferredAgentProvider, setPreferredAgentProvider, useUIStore } from '../store';
import { LLMConfigPanel } from './SettingsView';
import { AlphaPixelMark } from './AlphaPixelMark';

const AGENT_PROVIDER_LOGOS: Partial<Record<AgentHubProviderID, { alt: string; src: string }>> = {
  alpha: { alt: 'Alpha Agent', src: '/brand/alpha-intelligence-logo.png' },
  codex: { alt: 'Codex', src: '/brand/codex-logo.png' },
  opencode: { alt: 'OpenCode', src: '/brand/opencode-logo.png' },
  claude: { alt: 'Claude Code', src: '/agent-providers/claude-code.ico' },
  kimi: { alt: 'Kimi Code', src: '/agent-providers/kimi-code.png' },
  codebuddy: { alt: 'CodeBuddy Code', src: '/agent-providers/codebuddy.svg' },
  antigravity: { alt: 'Antigravity CLI', src: '/brand/antigravity-logo.png' },
};

const CREATION_MODEL_ROLES: Array<{ id: CreationModelRoleID; label: string; note: string; fallback: string }> = [
  { id: 'conversation', label: '创作对话模型', note: '处理工作台中的明确 Agent 指令。', fallback: '未配置时无法调用外部模型。' },
  { id: 'copywriter', label: '文案生成模型', note: '生成选题、大纲、文案、脚本与分镜。', fallback: '回退到创作对话模型。' },
  { id: 'reviewer', label: '质量审校模型', note: '检查事实边界、结构、平台适配和夸张承诺。', fallback: '回退到文案生成模型，并明确标记。' },
];

function AgentProviderMark({ providerID, label }: { providerID: AgentHubProviderID; label: string }) {
  const logo = AGENT_PROVIDER_LOGOS[providerID];
  return (
    <span
      data-testid={`alpha-agent-provider-${providerID}-mark`}
      role="img"
      aria-label={label}
      className="flex h-9 w-9 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-brand-border bg-brand-bg"
    >
      {providerID === 'alpha' ? (
        <AlphaPixelMark className="h-full w-full" decorative />
      ) : logo ? (
        <img src={logo.src} alt={logo.alt} className={providerID === 'alpha' ? 'h-full w-full object-cover object-left' : 'h-full w-full object-contain p-1'} />
      ) : providerID === 'custom' ? (
        <span aria-label={`${label} 标识`} className="text-[9px] font-black tracking-tight text-brand-accent">LLM</span>
      ) : (
        <Bot aria-label={`${label} 标识`} className="h-4 w-4 text-brand-accent" />
      )}
    </span>
  );
}

export function AlphaAgentSettingsSection() {
  const toast = useUIStore((state) => state.toast);
  const { data, error, isLoading, mutate } = useSWR(
    'api/agent/providers',
    () => api.agentHubProviders(),
    { refreshInterval: (latest) => latest?.probes.some((item) => item.state === 'probing') ? 1_000 : 0 },
  );
  const { data: creationRoles, mutate: mutateCreationRoles } = useSWR('api/creation/model-roles', api.getCreationModelRoles);
  const [currentProvider, setCurrentProvider] = useState<AgentHubProviderID>(() => preferredAgentProvider() || 'alpha');
  const [selectedModels, setSelectedModels] = useState<Partial<Record<AgentHubProviderID, string>>>({});
  const [validatingModelKey, setValidatingModelKey] = useState('');
  const [candidateQuery, setCandidateQuery] = useState('');

  useEffect(() => {
    const refreshModels = () => void mutate();
    window.addEventListener('alpha-nexus:llm-models-synced', refreshModels);
    return () => window.removeEventListener('alpha-nexus:llm-models-synced', refreshModels);
  }, [mutate]);

  async function probe(providerID: AgentHubProviderID) {
    try {
      await api.agentHubProviderProbe(providerID);
      await mutate();
      toast.show('Agent 运行时检测已启动。', 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : 'Agent 检测失败', 'error');
    }
  }

  // Real-call validation for one CLI candidate model. The backend flips
  // validating_model until the nonce call settles; poll until it clears so
  // the chip reflects the final verdict.
  async function validateCandidateModel(providerID: AgentHubProviderID, modelID: string) {
    const key = `${providerID}:${modelID}`;
    if (validatingModelKey) return;
    setValidatingModelKey(key);
    try {
      await api.agentHubProviderModelValidate(providerID, modelID);
      for (let attempt = 0; attempt < 75; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, 2000));
        const latest = await api.agentHubProviders();
        await mutate(latest, { revalidate: false });
        const status = latest.probes.find((item) => item.provider_id === providerID);
        if (!status?.validating_model) {
          if (status?.models?.includes(modelID)) {
            await api.agentHubProviderSelection(providerID, { model_id: modelID });
            window.dispatchEvent(new CustomEvent('alpha-nexus:agent-model-verified', {
              detail: { providerID, modelID },
            }));
            toast.show(`${modelID} 已通过真实调用验证。`, 'success');
          } else {
            toast.show(status?.model_validation_error || `${modelID} 未通过验证。`, 'error');
          }
          return;
        }
      }
      toast.show('验证仍在进行，可稍后刷新查看结果。', 'info');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '模型验证失败', 'error');
    } finally {
      setValidatingModelKey('');
    }
  }

  async function chooseProvider(providerID: AgentHubProviderID) {
    setPreferredAgentProvider(providerID);
    setCurrentProvider(providerID);
    toast.show(`Alpha Agent 已切换到 ${providerID}。`, 'success');
    await mutate();
  }

  async function chooseModel(providerID: AgentHubProviderID, modelID: string) {
    if (!modelID) return;
    try {
      await api.agentHubProviderSelection(providerID, { model_id: modelID });
      setSelectedModels((current) => ({ ...current, [providerID]: modelID }));
      window.dispatchEvent(new CustomEvent('alpha-nexus:agent-model-verified', {
        detail: { providerID, modelID },
      }));
      toast.show(`已切换到 ${modelID}。`, 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '模型切换失败', 'error');
    }
  }

  async function chooseCreationRole(role: CreationModelRoleID, selected: string) {
    const [providerID, ...modelParts] = selected.split('::');
    const modelID = modelParts.join('::');
    if (!providerID || !modelID) return;
    try {
      await api.putCreationModelRole(role, { provider_id: providerID, model_id: modelID });
      await mutateCreationRoles();
      toast.show('创作模型角色已保存。', 'success');
    } catch (reason) {
      toast.show(reason instanceof Error ? reason.message : '创作模型角色保存失败', 'error');
    }
  }

  return (
    <div className="space-y-5" data-testid="alpha-agent-settings-page">
      <section className="ui-card overflow-hidden" data-testid="alpha-agent-settings">
        <header className="flex items-start justify-between gap-3 p-5">
          <div className="flex items-start gap-3">
            <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-brand-surface-raised text-brand-fg">
              <Bot className="h-5 w-5" />
            </span>
            <div>
              <h4 className="text-sm font-bold text-gray-900">Alpha Agent</h4>
              <p className="mt-1 text-[10px] leading-4 text-gray-500">
                默认使用隔离的 Pi Agent 运行时；仅在发送任务时启动，响应完成或取消后立即退出。
              </p>
            </div>
          </div>
          <button
            type="button"
            aria-label="刷新 Agent 状态"
            className="ui-btn shrink-0"
            onClick={() => void mutate()}
          >
            <RefreshCw className="h-3.5 w-3.5" />刷新
          </button>
        </header>

        {isLoading && !data ? (
          <p className="flex items-center gap-2 border-t border-brand-border px-5 py-4 text-xs text-gray-500">
            <Loader2 className="h-4 w-4 animate-spin" />正在读取本机 Agent 状态
          </p>
        ) : null}
        {error ? <p role="status" className="border-t border-red-200 bg-red-50 px-5 py-3 text-xs text-red-700">Agent 状态读取失败。</p> : null}

        <div data-testid="alpha-agent-llm-config" className="border-t border-brand-border bg-brand-bg/30 p-5">
          <div className="mb-3">
            <h5 className="text-xs font-bold text-gray-900">统一 LLM 配置</h5>
            <p className="mt-1 text-[10px] leading-4 text-gray-500">先在这里填写并真实验证模型；成功后可在下方运行时中直接切换使用。</p>
          </div>
          <LLMConfigPanel initiallyExpanded />
        </div>

        {data ? (
          <div className="border-t border-brand-border">
            <div className="px-5 py-3">
              <h5 className="text-xs font-bold text-gray-900">Agent 运行时与模型</h5>
              <p className="mt-1 text-[10px] leading-4 text-gray-500">Alpha Agent 默认使用官方 Codex app-server；每次运行独立启动并在完成后退出。</p>
              {/* DSH 会同步几十个候选；没有筛选时 grok 这类排在末尾的模型看不到。 */}
              <input
                data-testid="alpha-agent-candidate-filter"
                value={candidateQuery}
                onChange={(event) => setCandidateQuery(event.target.value)}
                placeholder="筛选候选模型，例如 grok"
                autoComplete="off"
                className="mt-2 w-full max-w-xs rounded-lg border border-brand-border bg-white px-2.5 py-1.5 text-[10px] text-gray-700"
              />
            </div>
            <div className="divide-y divide-brand-border">
            {data.providers.map((provider) => {
              const status = data.probes.find((item) => item.provider_id === provider.id);
              const models = status?.models || [];
              const ready = status?.ready === true && models.length > 0;
              const active = currentProvider === provider.id;
              const label = provider.id === 'alpha' ? 'Alpha Agent' : provider.label;
              return (
                <article key={provider.id} data-testid={`alpha-agent-provider-${provider.id}`} className="grid gap-3 px-5 py-4 md:grid-cols-[minmax(190px,0.8fr)_minmax(180px,1fr)_auto] md:items-center">
                  <div className="flex min-w-0 items-center gap-3">
                    <AgentProviderMark providerID={provider.id} label={label} />
                    <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <strong className="truncate text-xs text-gray-900">{label}</strong>
                      {active ? <span className="rounded-full bg-brand-accent/12 px-2 py-0.5 text-[9px] font-bold text-brand-fg">当前</span> : null}
                    </div>
                    <p className="mt-1 truncate text-[10px] text-gray-500">
                      {ready ? `${models.length} 个已验证模型` : status?.reason || '等待本机运行时检测'}
                    </p>
                    </div>
                  </div>

                  <select
                    aria-label={`${label} 模型`}
                    disabled={!ready}
                    value={selectedModels[provider.id] || ''}
                    onChange={(event) => void chooseModel(provider.id, event.target.value)}
                    className="min-h-9 w-full rounded-lg border border-brand-border bg-white px-3 text-[10px] text-gray-700 disabled:bg-gray-50 disabled:text-gray-400"
                  >
                    <option value="" disabled>{ready ? '选择已验证模型' : '暂无已验证模型'}</option>
                    {models.map((model) => <option key={model} value={model}>{model}</option>)}
                  </select>

                  <div className="flex items-center justify-end gap-2">
                    <button type="button" className="ui-btn" onClick={() => void probe(provider.id)}>检测</button>
                    <button
                      type="button"
                      disabled={!ready || active}
                      className="ui-btn ui-btn-primary disabled:opacity-45"
                      onClick={() => void chooseProvider(provider.id)}
                    >
                      {active ? <><CheckCircle2 className="h-3.5 w-3.5" />使用中</> : '设为当前'}
                    </button>
                  </div>
                  {(() => {
                    const query = candidateQuery.trim().toLowerCase();
                    const pending = (status?.model_candidates || [])
                      .filter((model) => !models.includes(model))
                      .filter((model) => !query || model.toLowerCase().includes(query));
                    if (!pending.length) return null;
                    return (
                    <div className="flex flex-wrap items-center gap-1.5 border-t border-brand-border pt-2.5 md:col-span-3">
                      <span className="text-[10px] font-semibold text-gray-400">候选待验证 {pending.length}</span>
                      {pending.slice(0, 10).map((model) => {
                        const key = `${provider.id}:${model}`;
                        return (
                          <button
                            key={model}
                            type="button"
                            data-testid={`alpha-agent-validate-${provider.id}-${model}`}
                            disabled={Boolean(validatingModelKey)}
                            onClick={() => void validateCandidateModel(provider.id, model)}
                            className="rounded-full border border-brand-border bg-white px-2.5 py-1 text-[10px] font-semibold text-gray-600 transition-colors hover:border-brand-accent/50 hover:text-brand-accent disabled:cursor-not-allowed disabled:opacity-45"
                          >
                            {validatingModelKey === key ? '验证中…' : `验证 ${model}`}
                          </button>
                        );
                      })}
                      {pending.length > 10 ? (
                        <span className="text-[9px] text-gray-400">仅显示前 10 个，用上面的筛选框缩小范围</span>
                      ) : null}
                    </div>
                    );
                  })()}
                </article>
              );
            })}
            </div>
          </div>
        ) : null}

        <div className="border-t border-brand-border p-5" data-testid="creation-model-roles">
          <div className="mb-3">
            <h5 className="text-xs font-bold text-gray-900">创作模型用途</h5>
            <p className="mt-1 text-[10px] leading-4 text-gray-500">文案、审校与对话可以使用不同的已验证模型。Kimi API、Gemini API 请先在统一 LLM 配置中验证后再选择。</p>
          </div>
          <div className="divide-y divide-brand-border rounded-xl border border-brand-border">
            {CREATION_MODEL_ROLES.map((role) => {
              const selected = creationRoles?.roles.find((item) => item.role === role.id);
              const value = selected ? `${selected.provider_id}::${selected.model_id}` : '';
              const models = creationRoles?.verified_models ?? [];
              return <label key={role.id} className="grid gap-2 px-3 py-3 md:grid-cols-[minmax(180px,0.8fr)_minmax(220px,1fr)] md:items-center">
                <span>
                  <strong className="block text-[11px] text-gray-800">{role.label}</strong>
                  <span className="mt-1 block text-[10px] leading-4 text-gray-500">{role.note} {role.fallback}</span>
                </span>
                <select
                  aria-label={role.label}
                  value={value}
                  disabled={models.length === 0}
                  onChange={(event) => void chooseCreationRole(role.id, event.target.value)}
                  className="min-h-9 w-full rounded-lg border border-brand-border bg-white px-3 text-[10px] text-gray-700 disabled:bg-gray-50 disabled:text-gray-400"
                >
                  <option value="" disabled>{models.length ? '选择已验证模型' : '暂无可用于创作的已验证模型'}</option>
                  {models.map((model) => <option key={`${model.provider_id}:${model.model_id}`} value={`${model.provider_id}::${model.model_id}`}>{model.provider_id} · {model.model_id}</option>)}
                </select>
              </label>;
            })}
          </div>
        </div>
      </section>
    </div>
  );
}
