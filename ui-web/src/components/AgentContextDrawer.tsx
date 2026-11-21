import { Activity, ExternalLink, FileText, Globe2, ShieldCheck, X } from 'lucide-react';
import { api } from '../api';

export type AgentContextKind =
  | 'skill'
  | 'registry_tool'
  | 'egolite'
  | 'content'
  | 'knowledge'
  | 'publish_plan'
  | 'approval'
  | 'evidence'
  | 'subagent'
  | 'system';

export interface AgentContextSelection {
  id: string;
  kind: AgentContextKind;
  title: string;
  summary: string;
  status?: string;
  occurredAt?: string;
  fields?: Array<{ label: string; value: string }>;
}

export const AGENT_CONTEXT_OPEN_EVENT = 'alpha-nexus:agent-context-open';

export function openAgentContext(detail: AgentContextSelection): void {
  window.dispatchEvent(new CustomEvent<AgentContextSelection>(AGENT_CONTEXT_OPEN_EVENT, { detail }));
}

function ContextIcon({ kind }: { kind: AgentContextKind }) {
  if (kind === 'egolite') return <Globe2 className="h-4 w-4" />;
  if (kind === 'approval') return <ShieldCheck className="h-4 w-4" />;
  if (kind === 'content' || kind === 'knowledge' || kind === 'evidence') return <FileText className="h-4 w-4" />;
  return <Activity className="h-4 w-4" />;
}

export function AgentContextDrawer({ selection, onClose }: { selection: AgentContextSelection | null; onClose: () => void }) {
  if (!selection) return null;
  const isEgoLite = selection.kind === 'egolite';
  return (
    <aside
      data-testid="agent-context-drawer"
      aria-label="Agent 活动详情"
      className="fixed inset-y-8 right-0 z-[65] flex w-[min(390px,calc(100vw-2rem))] flex-col border-l border-brand-border bg-brand-surface shadow-[-12px_0_30px_rgba(15,23,42,0.08)] xl:relative xl:inset-auto xl:z-10 xl:h-full xl:w-[360px] xl:shrink-0 xl:shadow-none"
    >
      <header className="flex min-h-14 items-center gap-3 border-b border-brand-border px-4 py-3">
        <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-black/[0.045] text-gray-700">
          <ContextIcon kind={selection.kind} />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="truncate text-sm font-semibold text-gray-900">{selection.title}</h2>
          <p className="mt-0.5 truncate text-[10px] uppercase tracking-wide text-gray-400">{selection.kind.replace('_', ' ')}</p>
        </div>
        <button type="button" className="ui-icon-button" onClick={onClose} aria-label="关闭详情">
          <X className="h-4 w-4" />
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-5">
        <div className="flex items-center justify-between gap-3 border-b border-brand-border pb-3 text-[11px]">
          <span className="text-gray-500">状态</span>
          <span className="font-semibold text-gray-800">{selection.status || '已记录'}</span>
        </div>
        <p className="mt-4 whitespace-pre-wrap text-xs leading-6 text-gray-700">{selection.summary}</p>
        {selection.fields?.length ? (
          <dl className="mt-5 divide-y divide-brand-border border-y border-brand-border">
            {selection.fields.map((field) => (
              <div key={`${field.label}:${field.value}`} className="grid grid-cols-[92px_minmax(0,1fr)] gap-3 py-3 text-[11px]">
                <dt className="text-gray-400">{field.label}</dt>
                <dd className="break-words text-gray-700">{field.value}</dd>
              </div>
            ))}
          </dl>
        ) : null}
        {selection.occurredAt ? <p className="mt-4 text-[10px] text-gray-400">记录时间：{selection.occurredAt}</p> : null}
      </div>

      {isEgoLite ? (
        <footer className="border-t border-brand-border p-4">
          <button
            type="button"
            className="ui-btn w-full justify-center"
            onClick={() => void api.focusEgoLite()}
          >
            <ExternalLink className="h-3.5 w-3.5" />在 EgoLite 中打开
          </button>
        </footer>
      ) : null}
    </aside>
  );
}
