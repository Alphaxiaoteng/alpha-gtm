import React, { useEffect, useMemo, useState } from 'react';
import useSWR from 'swr';
import { api, type BatchRun, type BatchItem } from '../api';
import { useUIStore } from '../store';
import { PlatformIcon } from './PlatformIcon';
import { isPublishableAccountAsset } from '../publishAssets';
import {
  Layers,
  RefreshCw,
  Clock,
  CheckCircle,
  XCircle,
  AlertTriangle,
  Play
} from 'lucide-react';

const runStatusLabel = (run: BatchRun) => {
  if (run.dryRun) {
    if (run.status === 'pending') return '待演练';
    if (run.status === 'running') return '演练中';
    if (run.status === 'success' || run.status === 'completed') return '演练通过';
    if (run.status === 'failed') return '演练失败';
    if (run.status === 'completed_with_errors') return '演练有错误';
  }
  if (run.status === 'running') return '进行中';
  if (run.status === 'completed') return '已完成';
  if (run.status === 'completed_with_errors') return '完成但有错误';
  return run.status;
};

const itemStatusLabel = (status: string, dryRun: boolean) => {
  if (!dryRun) return status;
  if (status === 'pending') return '待演练';
  if (status === 'running') return '演练中';
  if (status === 'success') return '通过';
  if (status === 'failed') return '未通过';
  if (status === 'skipped') return '已跳过';
  return status;
};

export const BatchView: React.FC = () => {
  const { toast } = useUIStore();
  const [selectedBatchId, setSelectedBatchId] = useState<number | null>(null);
	const [showCreate, setShowCreate] = useState(false);
	const [contentID, setContentID] = useState(0);
	const [platformCode, setPlatformCode] = useState('');
	const [selectedAssetIDs, setSelectedAssetIDs] = useState<number[]>([]);
	const [concurrency, setConcurrency] = useState(1);
	const [creating, setCreating] = useState(false);

  // Fetch once; users refresh status explicitly instead of background polling.
  const { data: runs, mutate: mutateRuns } = useSWR('api/batch/runs', api.getBatchRuns);

  // Fetch batch items for selected run
  const { data: items, error: itemsError, mutate: mutateItems } = useSWR(
    selectedBatchId ? `api/batch/items?batchId=${selectedBatchId}` : null,
    () => api.getBatchItems(selectedBatchId!)
  );

  // Fetch accounts list to match names
  const { data: accounts } = useSWR('api/accounts', api.getAccounts);
	const { data: contents } = useSWR('api/contents', api.getContents);
	const { data: assets } = useSWR('api/account-assets', api.getAccountAssets);
	const publishableAssets = useMemo(() => assets?.filter(isPublishableAccountAsset) ?? [], [assets]);
	const platformCodes = useMemo(
		() => Array.from(new Set(publishableAssets.map((asset) => asset.platform_code))),
		[publishableAssets],
	);
	const selectedAssets = useMemo(() => publishableAssets.filter((asset) => asset.platform_code === platformCode), [publishableAssets, platformCode]);
	const chosenAssets = selectedAssets.filter((asset) => selectedAssetIDs.includes(asset.id));

	useEffect(() => {
		if (contentID === 0 && contents?.[0]) setContentID(contents[0].id);
		if (!platformCodes.includes(platformCode)) setPlatformCode(platformCodes[0] ?? '');
		}, [contentID, contents, platformCode, platformCodes]);

	useEffect(() => {
		setSelectedAssetIDs(selectedAssets.map((asset) => asset.id));
	}, [selectedAssets]);

  const handleRefresh = async () => {
    toast.show('正在刷新任务数据...', 'info');
    await mutateRuns();
    if (selectedBatchId) {
      await mutateItems();
    }
    toast.show('数据已更新', 'success');
  };

	const createDryRunBatch = async () => {
		if (!contentID || !platformCode || chosenAssets.length === 0) {
			toast.show('请选择内容和至少一个同平台账号资产', 'error');
			return;
		}
		setCreating(true);
		try {
			const run = await api.createDryRunBatch({
				contentId: contentID,
				assetIds: chosenAssets.map((asset) => asset.id),
				concurrency,
				dryRun: true,
			});
			setSelectedBatchId(run.id);
			await mutateRuns();
			toast.show(`已创建 #${run.id} 演练批次（${chosenAssets.length} 项）`, 'success');
			setShowCreate(false);
		} catch (err) {
			toast.show(err instanceof Error ? `创建批次失败: ${err.message}` : '创建批次失败', 'error');
		} finally {
			setCreating(false);
		}
	};

  const getAccountName = (accountId: number) => {
    const acc = accounts?.find(a => a.id === accountId);
    return acc ? acc.name : `账号 #${accountId}`;
  };

  const formatTime = (timeStr: string) => {
    if (!timeStr || timeStr.startsWith('0001-01-01')) return '—';
    try {
      const d = new Date(timeStr);
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    } catch {
      return timeStr;
    }
  };

  const selectedRun = runs?.find(r => r.id === selectedBatchId);

  return (
    <div className="space-y-6">
      {/* Action Header */}
      <div className="flex items-center justify-between">
        <div>
	          <h3 className="text-base font-bold text-gray-900">批量演练与作业列表</h3>
	          <p className="text-xs text-gray-550">查看批量任务的进程、结果与日志；新建任务仅执行演练。</p>
        </div>
        <div className="flex gap-2">
          <button onClick={() => setShowCreate((value) => !value)} className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold rounded-lg bg-brand-accent text-brand-on-accent transition">{showCreate ? '收起创建' : '创建演练批次'}</button>
          <button onClick={handleRefresh} className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold rounded-lg border border-brand-border bg-brand-surface hover:bg-black/5 text-gray-700 transition"><RefreshCw className="w-3.5 h-3.5" />刷新数据</button>
        </div>
      </div>

		{showCreate ? <div className="ui-card grid gap-3 p-4 md:grid-cols-4 md:items-end">
			<label className="text-xs font-semibold text-gray-700">内容<select value={contentID} onChange={(event) => setContentID(Number(event.target.value))} className="mt-1 block w-full rounded border border-brand-border bg-transparent px-2 py-1.5 text-xs">{contents?.map((content) => <option key={content.id} value={content.id}>{content.title || `内容 #${content.id}`}</option>)}</select></label>
			<label className="text-xs font-semibold text-gray-700">平台 / 账号资产<select value={platformCode} onChange={(event) => setPlatformCode(event.target.value)} className="mt-1 block w-full rounded border border-brand-border bg-transparent px-2 py-1.5 text-xs">{platformCodes.map((code) => <option key={code} value={code}>{code} · {publishableAssets.filter((asset) => asset.platform_code === code).length} 个账号</option>)}</select></label>
			<label className="text-xs font-semibold text-gray-700">并发<select value={concurrency} onChange={(event) => setConcurrency(Number(event.target.value))} className="mt-1 block w-full rounded border border-brand-border bg-transparent px-2 py-1.5 text-xs"><option value={1}>1（顺序）</option><option value={2}>2</option><option value={3}>3</option><option value={5}>5</option></select></label>
			<div><button type="button" disabled={creating || !contentID || chosenAssets.length === 0} onClick={createDryRunBatch} className="w-full rounded-lg bg-brand-accent px-3 py-2 text-xs font-semibold text-brand-on-accent disabled:opacity-45">{creating ? '创建中…' : `创建 ${chosenAssets.length} 项演练`}</button><p className="mt-1 text-[10px] text-gray-500">只创建 dry-run；不启动浏览器、不提交平台。</p></div>
			<div className="md:col-span-4 rounded-lg border border-brand-border p-2.5"><div className="mb-2 flex items-center justify-between text-xs"><span className="font-semibold text-gray-700">目标账号（同平台多选）</span><button type="button" onClick={() => setSelectedAssetIDs(chosenAssets.length === selectedAssets.length ? [] : selectedAssets.map((asset) => asset.id))} className="text-brand-accent">{chosenAssets.length === selectedAssets.length ? '全不选' : '全选'}</button></div><div className="flex flex-wrap gap-2">{selectedAssets.map((asset) => <label key={asset.id} className="flex items-center gap-1.5 rounded border border-brand-border px-2 py-1 text-xs text-gray-700"><input type="checkbox" checked={selectedAssetIDs.includes(asset.id)} onChange={() => setSelectedAssetIDs((ids) => ids.includes(asset.id) ? ids.filter((id) => id !== asset.id) : [...ids, asset.id])} />{asset.name || `账号 #${asset.account_id}`}</label>)}</div></div>
		</div> : null}

      {/* Grid: Left Runs list, Right Items list */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Left Side: Batch Runs List */}
        <div className="ui-card flex flex-col h-[520px] overflow-hidden">
          <div className="p-4 border-b border-brand-border flex items-center justify-between">
            <h4 className="text-xs font-bold text-gray-900">批次任务历史</h4>
            <span className="px-2 py-0.5 text-[9px] font-bold bg-black/5 rounded-full text-gray-600">
              {runs?.length ?? 0} 次
            </span>
          </div>

          <div className="flex-1 overflow-y-auto divide-y divide-brand-border">
            {!runs || runs.length === 0 ? (
              <div className="h-full flex flex-col items-center justify-center p-8 text-center text-gray-500">
                <Layers className="w-8 h-8 opacity-20 mb-2" />
                <span className="text-xs">暂无批量任务执行记录</span>
              </div>
            ) : (
              runs.map((run: BatchRun) => {
                const isSelected = selectedBatchId === run.id;
                
                let statusColor = 'text-gray-400 border-gray-500/20 bg-gray-500/10';
                if (run.status === 'running') statusColor = 'text-brand-accent border-brand-accent/20 bg-brand-accent/10';
                if (run.status === 'success' || run.status === 'completed') statusColor = 'text-emerald-400 border-emerald-500/20 bg-emerald-500/10';
                if (run.status === 'failed' || run.status === 'completed_with_errors') statusColor = 'text-red-400 border-red-500/20 bg-red-500/10';

                return (
                  <div
                    key={run.id}
                    onClick={() => setSelectedBatchId(run.id)}
                    className={`p-4 flex flex-col gap-2 cursor-pointer transition ${
                      isSelected 
                        ? 'bg-brand-accent/10 border-l-4 border-brand-accent' 
                        : 'hover:bg-white/5'
                    }`}
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-bold text-gray-900">批次 #{run.id}</span>
                      <span className={`px-2 py-0.5 rounded-full border text-[9px] font-bold uppercase ${statusColor}`}>
                        {runStatusLabel(run)}
                      </span>
                    </div>

                    <div className="text-[10px] text-gray-500 space-y-1">
                      <div className="flex justify-between">
                        <span>操作类型</span>
	                        <span className="font-semibold text-gray-700">{run.operationType === 'publish' ? (run.dryRun ? '批量演练' : '批量发布') : run.operationType}</span>
                      </div>
                      <div className="flex justify-between items-center">
                        <span>目标平台</span>
                        <span className="font-semibold text-gray-700 flex items-center gap-1">
                          <PlatformIcon platform={run.platformCode} className="w-3.5 h-3.5" />
                          {run.platformCode === 'xhs' ? '小红书' : run.platformCode}
                        </span>
                      </div>
                      <div className="flex justify-between">
                        <span>并发线程</span>
                        <span className="font-semibold text-gray-700">{run.concurrency} 个并发</span>
                      </div>
                    </div>

                    {/* Progress details */}
                    <div className="grid grid-cols-4 gap-1.5 pt-1.5 border-t border-brand-border/40 text-[9.5px] text-center font-bold">
                      <div className="bg-black/10 rounded py-0.5" title="待执行">
                        <span className="text-gray-400">{run.pendingCount + run.runningCount} 待</span>
                      </div>
                      <div className="bg-emerald-500/10 rounded py-0.5" title={run.dryRun ? '演练通过' : '成功'}>
                        <span className="text-emerald-400">{run.successCount} {run.dryRun ? '通过' : '成功'}</span>
                      </div>
                      <div className="bg-red-500/10 rounded py-0.5" title="失败">
                        <span className="text-red-400">{run.failedCount} 败</span>
                      </div>
                      <div className="bg-gray-500/15 rounded py-0.5" title="总计">
                        <span className="text-gray-300">{run.totalItems} 总</span>
                      </div>
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>

        {/* Right Side: Batch Items List */}
        <div className="lg:col-span-2 ui-card flex flex-col h-[520px] overflow-hidden">
          {selectedBatchId ? (
            <>
              {/* Header */}
              <div className="p-4 border-b border-brand-border flex items-center justify-between">
                <div>
                  <h4 className="text-xs font-bold text-gray-900">批次 #{selectedBatchId} 的任务细则</h4>
                  <p className="text-[10px] text-gray-550">共 {items?.length ?? 0} 个{selectedRun?.dryRun ? '演练' : '发布'}子任务</p>
                </div>

                <div className="flex items-center gap-1.5 text-[10px] text-gray-500">
                  <Play className="w-3 h-3 text-brand-accent" />
                  <span>{selectedRun?.dryRun ? '演练状态' : '批次状态'}: <strong className="text-gray-950">{selectedRun ? runStatusLabel(selectedRun) : '—'}</strong></span>
                </div>
              </div>

              {/* Table List */}
              <div className="flex-1 overflow-auto">
                {itemsError ? (
                  <div className="h-full flex flex-col items-center justify-center p-8 text-center text-red-400">
                    <AlertTriangle className="w-8 h-8 mb-2 opacity-50" />
                    <span className="text-xs">加载队列详情失败</span>
                  </div>
                ) : !items ? (
                  <div className="h-full flex items-center justify-center p-8 text-gray-500">
                    <span className="text-xs animate-pulse">正在载入队列明细...</span>
                  </div>
                ) : items.length === 0 ? (
                  <div className="h-full flex flex-col items-center justify-center p-8 text-gray-500">
                    <Layers className="w-8 h-8 opacity-20 mb-2" />
                    <span className="text-xs">该批次中暂无执行项</span>
                  </div>
                ) : (
                  <table className="w-full text-left text-xs border-collapse">
                    <thead>
                      <tr className="bg-black/5 text-gray-650 border-b border-brand-border text-[10px] font-bold uppercase tracking-wider">
                        <th className="p-3 pl-4">任务项 ID</th>
                        <th className="p-3">关联账号</th>
                        <th className="p-3">{selectedRun?.dryRun ? '演练状态' : '发布状态'}</th>
                        <th className="p-3">开始时间</th>
                        <th className="p-3">完成时间</th>
                        <th className="p-3 pl-2">错误报告</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-brand-border/40">
                      {items.map((item: BatchItem) => {
                        let itemStatusColor = 'text-gray-500';
                        let ItemIcon = Clock;
                        if (item.status === 'running') {
                          itemStatusColor = 'text-brand-accent';
                          ItemIcon = RefreshCw;
                        } else if (item.status === 'success') {
                          itemStatusColor = 'text-emerald-650';
                          ItemIcon = CheckCircle;
                        } else if (item.status === 'failed') {
                          itemStatusColor = 'text-red-500';
                          ItemIcon = XCircle;
                        }

                        return (
                          <tr key={item.id} className="hover:bg-black/5 text-[11.5px]">
                            <td className="p-3 pl-4 font-mono text-gray-500">#{item.id}</td>
                            <td className="p-3 font-semibold text-gray-900">
                              {getAccountName(item.accountId)}
                            </td>
                            <td className="p-3">
                              <span className={`flex items-center gap-1 font-semibold ${itemStatusColor}`}>
                                <ItemIcon className={`w-3.5 h-3.5 ${item.status === 'running' ? 'animate-spin' : ''}`} />
                                {itemStatusLabel(item.status, selectedRun?.dryRun ?? false)}
                              </span>
                            </td>
                            <td className="p-3 text-gray-500 font-mono">{formatTime(item.startedAt)}</td>
                            <td className="p-3 text-gray-500 font-mono">{formatTime(item.finishedAt)}</td>
                            <td className="p-3 text-red-500 font-medium max-w-xs truncate" title={item.errorMessage}>
                              {item.errorMessage || <span className="text-gray-400">—</span>}
                            </td>
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                )}
              </div>
            </>
          ) : (
            <div className="h-full flex flex-col items-center justify-center p-8 text-center text-gray-550">
              <Layers className="w-12 h-12 opacity-10 mb-4" />
              <h5 className="text-xs font-bold text-gray-900 mb-1">选择左侧的批次任务</h5>
              <p className="text-[10px] text-gray-400 max-w-xs">点击左侧的任务历史，即可调阅每一个子项的详细执行流程与对应的自动化错误堆栈日志</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
