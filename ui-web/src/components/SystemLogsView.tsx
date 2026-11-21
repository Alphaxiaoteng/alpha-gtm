import React, { useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  Activity,
  Database,
  FileCode,
  FolderOpen,
  LoaderCircle,
  Play,
  RefreshCw,
  Search,
  Server,
  Terminal,
} from 'lucide-react';
import {
  api,
  type DataAPIEndpoint,
  type TikHubAPICall,
} from '../api';
import { PlatformIcon } from './PlatformIcon';

const platformNames: Record<string, string> = {
  douyin: '抖音',
  xhs: '小红书',
  wechat_video: '视频号',
  kuaishou: '快手',
  wechat_mp: '公众号',
  youtube: 'YouTube',
  x: 'X',
};

function formatTime(value?: string) {
  if (!value) return '尚无记录';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '时间未知' : date.toLocaleString('zh-CN', { hour12: false });
}

function parseStoredParameters(json?: string): Record<string, unknown> {
  if (!json) return {};
  try {
    const parsed = JSON.parse(json);
    return parsed && typeof parsed === 'object' ? (parsed as Record<string, unknown>) : {};
  } catch {
    return { raw: json };
  }
}

const MiniStat: React.FC<{ label: string; value: React.ReactNode }> = ({ label, value }) => (
  <div className="rounded-lg border border-brand-border bg-white px-3 py-2.5 shadow-2xs">
    <div className="text-[10px] font-medium text-gray-500">{label}</div>
    <div className="mt-0.5 text-base font-bold text-gray-900">{value}</div>
  </div>
);

const CallLogRow: React.FC<{
  call: TikHubAPICall;
  endpoint?: DataAPIEndpoint;
  raw?: string;
  loadingRaw: boolean;
  onLoadRaw: () => void;
}> = ({ call, endpoint, raw, loadingRaw, onLoadRaw }) => {
  const parameters = parseStoredParameters(call.parameters_json);
  const statusLabel =
    call.status === 'succeeded'
      ? '成功'
      : call.status === 'running'
        ? '进行中'
        : call.status === 'storage_failed'
          ? '保存失败'
          : '失败';
  const statusStyle =
    call.status === 'succeeded'
      ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
      : call.status === 'running'
        ? 'border-brand-warning/30 bg-brand-warning/10 text-brand-warning'
        : 'border-rose-200 bg-rose-50 text-rose-700';

  return (
    <details
      className="group px-4 py-3 border-b border-brand-border/60 hover:bg-black/[0.015]"
      onToggle={(event) => {
        if (event.currentTarget.open && call.raw_path && !raw) onLoadRaw();
      }}
    >
      <summary className="flex cursor-pointer list-none items-start gap-3">
        <span className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-black/[0.035]">
          <PlatformIcon platform={call.platform_code} className="h-4 w-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs font-bold text-gray-950">{endpoint?.name || call.endpoint_id}</span>
            <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold ${statusStyle}`}>
              {statusLabel}
            </span>
            <span className="rounded-full bg-black/[0.04] px-2 py-0.5 text-[10px] text-gray-600">
              {platformNames[call.platform_code] || call.platform_code}
            </span>
          </div>
          <p className="mt-1 truncate text-xs text-gray-500">
            #{call.id} · {formatTime(call.started_at)} · {call.duration_ms} ms · {call.item_count} 条 · {(call.response_bytes / 1024).toFixed(1)} KB
          </p>
        </div>
        <span className="shrink-0 text-xs font-semibold text-gray-500">
          {endpoint?.cost_hint || '价格未验证'}
        </span>
      </summary>
      <div className="mt-3 grid gap-3 pl-11">
        <dl className="grid gap-2 text-xs sm:grid-cols-2">
          <div>
            <dt className="text-[10px] text-gray-400">调用端点</dt>
            <dd className="font-mono text-gray-800">{call.endpoint_id}</dd>
          </div>
          <div>
            <dt className="text-[10px] text-gray-400">调用状态</dt>
            <dd className="font-mono text-gray-800">{call.status}</dd>
          </div>
          <div className="sm:col-span-2">
            <dt className="text-[10px] text-gray-400">本地存储文件</dt>
            <dd className="truncate font-mono text-[11px] text-gray-600">{call.raw_path || '未保存原始文件'}</dd>
          </div>
          {call.provider_message && (
            <div className="sm:col-span-2">
              <dt className="text-[10px] text-gray-400">服务商消息</dt>
              <dd className="rounded bg-rose-50 p-2 font-mono text-rose-800">{call.provider_message}</dd>
            </div>
          )}
        </dl>

        <div>
          <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-gray-400">调用参数</div>
          <pre className="max-h-48 overflow-auto rounded bg-gray-900 p-2 text-[10px] text-gray-100 font-mono">
            {JSON.stringify(parameters, null, 2)}
          </pre>
        </div>

        {call.raw_path && (
          <div>
            <div className="mb-1 flex items-center justify-between text-[10px] font-semibold uppercase tracking-wider text-gray-400">
              <span>本地原始响应</span>
              {loadingRaw && <span className="text-gray-500">正在读取...</span>}
            </div>
            {raw ? (
              <pre className="max-h-60 overflow-auto rounded bg-gray-900 p-2 text-[10px] text-emerald-400 font-mono">
                {raw}
              </pre>
            ) : (
              <button
                type="button"
                onClick={onLoadRaw}
                className="text-xs text-brand-accent hover:underline"
              >
                展开查看完整响应 JSON
              </button>
            )}
          </div>
        )}
      </div>
    </details>
  );
};

export const SystemLogsView: React.FC = () => {
  const [activeSubTab, setActiveSubTab] = useState<'calls' | 'jobs' | 'events' | 'diagnostics'>('calls');
  const [filterPlatform, setFilterPlatform] = useState('all');
  const [filterQuery, setFilterQuery] = useState('');
  const [rawCalls, setRawCalls] = useState<Record<number, string>>({});
  const [loadingRawCall, setLoadingRawCall] = useState(0);
  const [creatingSnapshot, setCreatingSnapshot] = useState(false);
  const [snapshotResult, setSnapshotResult] = useState<string | null>(null);

  // 1. 数据采集与抓取 API 调用历史
  const { data: provider } = useSWR('api/data-api/config', api.getDataAPIConfig);
  const {
    data: callHistory,
    isLoading: callHistoryLoading,
    mutate: mutateCallHistory,
  } = useSWR('api/data-api/calls', api.getDataAPICalls, {
    revalidateOnFocus: false,
  });

  // 2. 后台任务运行与发布 Job 列表
  const {
    data: jobs,
    isLoading: jobsLoading,
    mutate: mutateJobs,
  } = useSWR('api/jobs/list', api.getJobs, {
    revalidateOnFocus: false,
  });

  // 3. 全局 Job 执行事件流
  const {
    data: allJobEvents,
    isLoading: eventsLoading,
    mutate: mutateEvents,
  } = useSWR('api/jobs/events/all', api.getAllJobEvents, {
    revalidateOnFocus: false,
  });

  // 4. 系统诊断日志目录
  const {
    data: diagLogDir,
    mutate: mutateDiagLogDir,
  } = useSWR('api/diagnostics/log-directory', api.getDiagnosticLogDirectory, {
    revalidateOnFocus: false,
  });

  const endpointDefinitions = useMemo(() => {
    const definitions = new Map<string, DataAPIEndpoint>();
    for (const platform of provider?.endpoint_catalog ?? []) {
      for (const capability of platform.capabilities) {
        for (const endpoint of capability.endpoints ?? []) {
          if (endpoint.id) definitions.set(endpoint.id, endpoint);
        }
      }
    }
    return definitions;
  }, [provider?.endpoint_catalog]);

  const loadRawCall = async (call: TikHubAPICall) => {
    if (rawCalls[call.id]) return;
    setLoadingRawCall(call.id);
    try {
      const raw = await api.getDataAPICallResponse(call.id);
      setRawCalls((current) => ({ ...current, [call.id]: JSON.stringify(raw, null, 2) }));
    } catch {
      // ignore
    } finally {
      setLoadingRawCall(0);
    }
  };

  const filteredCalls = useMemo(() => {
    const list = callHistory?.calls ?? [];
    const query = filterQuery.trim().toLowerCase();
    return list.filter((call) => {
      if (filterPlatform !== 'all' && call.platform_code !== filterPlatform) return false;
      if (!query) return true;
      const endpoint = endpointDefinitions.get(call.endpoint_id);
      return (
        call.endpoint_id.toLowerCase().includes(query) ||
        call.platform_code.toLowerCase().includes(query) ||
        (endpoint?.name && endpoint.name.toLowerCase().includes(query)) ||
        call.parameters_json.toLowerCase().includes(query)
      );
    });
  }, [callHistory?.calls, filterPlatform, filterQuery, endpointDefinitions]);

  const filteredEvents = useMemo(() => {
    const list = allJobEvents ?? [];
    const query = filterQuery.trim().toLowerCase();
    return list.filter((event) => {
      if (!query) return true;
      return (
        String(event.job_id).includes(query) ||
        (event.event_type && event.event_type.toLowerCase().includes(query)) ||
        (event.step && event.step.toLowerCase().includes(query)) ||
        (event.message && event.message.toLowerCase().includes(query)) ||
        (event.level && event.level.toLowerCase().includes(query))
      );
    });
  }, [allJobEvents, filterQuery]);

  const filteredJobs = useMemo(() => {
    const list = jobs ?? [];
    const query = filterQuery.trim().toLowerCase();
    return list.filter((job) => {
      if (!query) return true;
      return (
        String(job.id).includes(query) ||
        (job.platform_code && job.platform_code.toLowerCase().includes(query)) ||
        (job.status && job.status.toLowerCase().includes(query)) ||
        (job.current_step && job.current_step.toLowerCase().includes(query))
      );
    });
  }, [jobs, filterQuery]);

  const handleCreateSnapshot = async () => {
    setCreatingSnapshot(true);
    setSnapshotResult(null);
    try {
      const result = await api.createDiagnosticSnapshot();
      setSnapshotResult(
        `快照已生成：文件路径 ${result.path} (失败任务 ${result.counts.publish_jobs_failed} 个，审计事件 ${result.counts.audit_events_total} 条)`,
      );
    } catch (err) {
      setSnapshotResult(`生成失败: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setCreatingSnapshot(false);
    }
  };

  const openLogDirectory = async () => {
    if (!diagLogDir?.path) return;
    if (window.electronAPI?.openPathInFinder) {
      await window.electronAPI.openPathInFinder(diagLogDir.path);
    } else {
      window.alert(`日志物理目录：${diagLogDir.path}`);
    }
  };

  return (
    <div className="mx-auto w-full max-w-[1440px] space-y-4 pb-12 px-3 sm:px-6" data-testid="system-logs-view">
      {/* 头部标题与操作 */}
      <div className="flex flex-wrap items-center justify-between gap-4 border-b border-brand-border pb-3">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-gray-950 flex items-center gap-2">
            <Server className="h-5 w-5 text-brand-accent" />
            全项目系统日志与运行审计
          </h1>
          <p className="mt-1 text-xs text-gray-500">
            涵盖数据采集 API 审计、后��发布与队列 Job 调度、全链路执行事件流及本地诊断快照。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            className="ui-btn"
            onClick={() => {
              mutateCallHistory();
              mutateJobs();
              mutateEvents();
              mutateDiagLogDir();
            }}
          >
            <RefreshCw className="h-3.5 w-3.5" />
            刷新全部
          </button>
        </div>
      </div>

      {/* 统计横条 */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <MiniStat label="抓取与采集记录" value={callHistory?.count ?? 0} />
        <MiniStat label="后台 Job 总数" value={jobs?.length ?? 0} />
        <MiniStat label="系统审计事件" value={allJobEvents?.length ?? 0} />
        <MiniStat label="日志物理目录" value={diagLogDir?.path ? '已就绪' : '检查中'} />
      </div>

      {/* 选项卡 */}
      <div className="flex border-b border-brand-border">
        <button
          type="button"
          onClick={() => setActiveSubTab('calls')}
          className={`flex items-center gap-1.5 border-b-2 px-4 py-2 text-xs font-semibold transition ${
            activeSubTab === 'calls'
              ? 'border-brand-accent text-brand-accent'
              : 'border-transparent text-gray-500 hover:text-gray-900'
          }`}
        >
          <Database className="h-3.5 w-3.5" />
          采集与 API 审计 ({callHistory?.count ?? 0})
        </button>
        <button
          type="button"
          onClick={() => setActiveSubTab('jobs')}
          className={`flex items-center gap-1.5 border-b-2 px-4 py-2 text-xs font-semibold transition ${
            activeSubTab === 'jobs'
              ? 'border-brand-accent text-brand-accent'
              : 'border-transparent text-gray-500 hover:text-gray-900'
          }`}
        >
          <Play className="h-3.5 w-3.5" />
          后台 Job 任务 ({jobs?.length ?? 0})
        </button>
        <button
          type="button"
          onClick={() => setActiveSubTab('events')}
          className={`flex items-center gap-1.5 border-b-2 px-4 py-2 text-xs font-semibold transition ${
            activeSubTab === 'events'
              ? 'border-brand-accent text-brand-accent'
              : 'border-transparent text-gray-500 hover:text-gray-900'
          }`}
        >
          <Activity className="h-3.5 w-3.5" />
          执行事件流 ({allJobEvents?.length ?? 0})
        </button>
        <button
          type="button"
          onClick={() => setActiveSubTab('diagnostics')}
          className={`flex items-center gap-1.5 border-b-2 px-4 py-2 text-xs font-semibold transition ${
            activeSubTab === 'diagnostics'
              ? 'border-brand-accent text-brand-accent'
              : 'border-transparent text-gray-500 hover:text-gray-900'
          }`}
        >
          <Terminal className="h-3.5 w-3.5" />
          本地诊断快照
        </button>
      </div>

      {/* 过滤条（针对列表视图） */}
      {activeSubTab !== 'diagnostics' && (
        <div className="flex flex-wrap items-center gap-3">
          <div className="relative min-w-48 flex-1">
            <Search className="absolute left-3 top-2.5 h-3.5 w-3.5 text-gray-400" />
            <input
              type="text"
              value={filterQuery}
              onChange={(e) => setFilterQuery(e.target.value)}
              placeholder="按关键词、事件类型、状态或 ID 过滤..."
              className="w-full rounded-lg border border-brand-border bg-white pl-9 pr-3 py-1.5 text-xs outline-none focus:border-brand-accent"
            />
          </div>
          {activeSubTab === 'calls' && (
            <select
              value={filterPlatform}
              onChange={(e) => setFilterPlatform(e.target.value)}
              className="rounded-lg border border-brand-border bg-white px-3 py-1.5 text-xs"
            >
              <option value="all">全部平台</option>
              {Object.entries(platformNames).map(([id, name]) => (
                <option key={id} value={id}>
                  {name}
                </option>
              ))}
            </select>
          )}
        </div>
      )}

      {/* 视图 1：数据采集 API 调用审计 */}
      {activeSubTab === 'calls' && (
        <section className="ui-card overflow-hidden">
          {callHistoryLoading && (
            <div className="flex items-center justify-center p-12 text-xs text-gray-400">
              <LoaderCircle className="h-4 w-4 animate-spin mr-2" />
              正在加载调用审计记录...
            </div>
          )}
          {!callHistoryLoading && filteredCalls.length === 0 && (
            <div className="p-12 text-center text-xs text-gray-400">
              暂无匹配的数据采集记录
            </div>
          )}
          {!callHistoryLoading && filteredCalls.length > 0 && (
            <div className="divide-y divide-brand-border/60">
              {filteredCalls.map((call) => (
                <CallLogRow
                  key={call.id}
                  call={call}
                  endpoint={endpointDefinitions.get(call.endpoint_id)}
                  raw={rawCalls[call.id]}
                  loadingRaw={loadingRawCall === call.id}
                  onLoadRaw={() => void loadRawCall(call)}
                />
              ))}
            </div>
          )}
        </section>
      )}

      {/* 视图 2：后台 Job 任务 */}
      {activeSubTab === 'jobs' && (
        <section className="ui-card overflow-hidden">
          {jobsLoading && (
            <div className="flex items-center justify-center p-12 text-xs text-gray-400">
              <LoaderCircle className="h-4 w-4 animate-spin mr-2" />
              正在读取 Job 列表...
            </div>
          )}
          {!jobsLoading && filteredJobs.length === 0 && (
            <div className="p-12 text-center text-xs text-gray-400">
              当前没有后台 Job 记录
            </div>
          )}
          {!jobsLoading && filteredJobs.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs">
                <thead className="border-b border-brand-border bg-black/[0.02] text-gray-500">
                  <tr>
                    <th className="px-4 py-2.5 font-medium">Job ID</th>
                    <th className="px-4 py-2.5 font-medium">平台</th>
                    <th className="px-4 py-2.5 font-medium">状态</th>
                    <th className="px-4 py-2.5 font-medium">当前步骤</th>
                    <th className="px-4 py-2.5 font-medium">重试次数</th>
                    <th className="px-4 py-2.5 font-medium">错误码</th>
                    <th className="px-4 py-2.5 font-medium">创建时间</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-brand-border/60">
                  {filteredJobs.map((job) => (
                    <tr key={job.id} className="hover:bg-black/[0.015]">
                      <td className="px-4 py-2.5 font-mono font-bold text-gray-900">#{job.id}</td>
                      <td className="px-4 py-2.5">
                        <span className="inline-flex items-center gap-1.5">
                          <PlatformIcon platform={job.platform_code} className="h-3.5 w-3.5" />
                          {platformNames[job.platform_code] || job.platform_code}
                        </span>
                      </td>
                      <td className="px-4 py-2.5">
                        <span
                          className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ${
                            job.status === 'succeeded' || job.status === 'published'
                              ? 'bg-emerald-50 text-emerald-700'
                              : job.status === 'running' || job.status === 'in_progress'
                                ? 'bg-brand-warning/10 text-brand-warning'
                                : job.status === 'failed'
                                  ? 'bg-rose-50 text-rose-700'
                                  : 'bg-gray-100 text-gray-600'
                          }`}
                        >
                          {job.status}
                        </span>
                      </td>
                      <td className="px-4 py-2.5 font-mono text-[11px] text-gray-600">{job.current_step || '-'}</td>
                      <td className="px-4 py-2.5 text-gray-500">
                        {job.retry_count} / {job.max_retries}
                      </td>
                      <td className="px-4 py-2.5 font-mono text-rose-700">{job.error_code || '-'}</td>
                      <td className="px-4 py-2.5 text-gray-500">{formatTime(job.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      )}

      {/* 视图 3：全链路 Job 执行事件流 */}
      {activeSubTab === 'events' && (
        <section className="ui-card overflow-hidden">
          {eventsLoading && (
            <div className="flex items-center justify-center p-12 text-xs text-gray-400">
              <LoaderCircle className="h-4 w-4 animate-spin mr-2" />
              正在读取事件流...
            </div>
          )}
          {!eventsLoading && filteredEvents.length === 0 && (
            <div className="p-12 text-center text-xs text-gray-400">
              暂无匹配的审计事件
            </div>
          )}
          {!eventsLoading && filteredEvents.length > 0 && (
            <div className="divide-y divide-brand-border/60 font-mono text-xs">
              {filteredEvents.map((event) => (
                <div key={event.id} className="p-3 hover:bg-black/[0.015]">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span
                        className={`inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-bold ${
                          event.level === 'error'
                            ? 'bg-rose-100 text-rose-800'
                            : event.level === 'warn'
                              ? 'bg-brand-warning/10 text-brand-warning'
                              : 'bg-brand-surface-raised text-brand-fg'
                        }`}
                      >
                        {event.level?.toUpperCase()}
                      </span>
                      <span className="font-bold text-gray-900">Job #{event.job_id}</span>
                      <span className="text-gray-400">·</span>
                      <span className="text-brand-muted">{event.event_type}</span>
                      {event.step && <span className="text-gray-500">[{event.step}]</span>}
                    </div>
                    <span className="text-[10px] text-gray-400">{formatTime(event.created_at)}</span>
                  </div>
                  {event.message && (
                    <p className="mt-1.5 text-gray-700 text-[11px] leading-relaxed break-all">
                      {event.message}
                    </p>
                  )}
                  {event.error_code && (
                    <div className="mt-1 text-[11px] text-rose-600">
                      ErrorCode: {event.error_code} ({event.error_class || 'General'})
                    </div>
                  )}
                  <div className="mt-1 flex gap-3 text-[10px] text-gray-400">
                    <span>耗时: {event.duration_ms} ms</span>
                    {event.trace_id && <span>Trace: {event.trace_id}</span>}
                    {event.task_id && <span>Task: {event.task_id}</span>}
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      )}

      {/* 视图 4：本地诊断快照 */}
      {activeSubTab === 'diagnostics' && (
        <div className="space-y-4">
          <section className="ui-card p-5">
            <h2 className="text-sm font-bold text-gray-950 flex items-center gap-2">
              <FileCode className="h-4 w-4 text-brand-accent" />
              生成系统诊断快照
            </h2>
            <p className="mt-1 text-xs text-gray-500 leading-relaxed">
              导出当前失败任务、环境配置、数据库完整性状态与错误事件，快照脱敏保存至本地 `diagnostics/` 目录中，便于离线排查与复盘。
            </p>
            <div className="mt-4 flex flex-wrap items-center gap-3">
              <button
                type="button"
                className="ui-btn ui-btn-primary"
                onClick={handleCreateSnapshot}
                disabled={creatingSnapshot}
              >
                {creatingSnapshot ? (
                  <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Server className="h-3.5 w-3.5" />
                )}
                生成诊断快照
              </button>
              {diagLogDir?.path && (
                <button
                  type="button"
                  className="ui-btn"
                  onClick={openLogDirectory}
                >
                  <FolderOpen className="h-3.5 w-3.5" />
                  打开本地日志目录
                </button>
              )}
            </div>
            {snapshotResult && (
              <div className="mt-3 rounded-lg border border-brand-border bg-black/[0.02] p-3 text-xs font-mono text-gray-800">
                {snapshotResult}
              </div>
            )}
          </section>
        </div>
      )}
    </div>
  );
};
