import React, { useState } from 'react';
import useSWR from 'swr';
import { HardDrive, Loader2, RefreshCw, Trash2 } from 'lucide-react';
import { api, type StorageCategory } from '../api';
import { useUIStore } from '../store';

/**
 * 设置页「存储」区。
 *
 * 数据目录会长期累积陈旧 DB 快照、旧二进制和诊断日志，但界面上此前完全看不出来。
 * 这里按类别展示实测占用，并只对应用明确知道如何安全删除的类别提供清理按钮。
 */

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const exponent = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  const value = bytes / 1024 ** exponent;
  return `${value >= 100 || exponent === 0 ? Math.round(value) : value.toFixed(1)} ${units[exponent]}`;
}

export const StorageSettingsSection: React.FC = () => {
  const { toast } = useUIStore();
  const { data, error, isLoading, mutate } = useSWR<{ data_dir: string; total_bytes: number; categories: StorageCategory[] }>(
    'api/storage/usage',
    api.getStorageUsage,
  );
  const [cleaning, setCleaning] = useState('');
  const [pendingConfirm, setPendingConfirm] = useState('');

  const runCleanup = async (category: StorageCategory) => {
    if (category.requires_confirm && pendingConfirm !== category.key) {
      setPendingConfirm(category.key);
      return;
    }
    setCleaning(category.key);
    setPendingConfirm('');
    try {
      const result = await api.cleanupStorage(category.key);
      toast.show(`${category.label}：${result.detail} 释放 ${formatBytes(result.bytes)}`, 'success');
      await mutate();
    } catch (cleanupError) {
      toast.show(`${category.label} 清理失败：${(cleanupError as Error).message}`, 'error');
    } finally {
      setCleaning('');
    }
  };

  const categories = data?.categories ?? [];
  const total = data?.total_bytes ?? 0;

  return (
    <div data-testid="settings-storage" className="bg-brand-surface border border-brand-border rounded-xl p-5 space-y-4 shadow-xs">
      <div className="flex items-center justify-between gap-2 border-b border-brand-border/60 pb-3">
        <div className="flex items-center gap-2">
          <HardDrive className="w-4 h-4 text-brand-accent" />
          <h4 className="text-xs font-bold text-gray-900">存储占用</h4>
        </div>
        <div className="flex items-center gap-2">
          <span data-testid="settings-storage-total" className="font-mono text-[11px] text-gray-600">
            合计 {formatBytes(total)}
          </span>
          <button
            type="button"
            onClick={() => void mutate()}
            disabled={isLoading}
            aria-label="刷新存储占用"
            className="ui-btn p-1.5 disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${isLoading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      <p className="text-[11px] leading-5 text-gray-550">
        数据目录只会增长。这里列出每一类实际占用；标有清理的类别可以安全删除，活动数据库与正在写入的文件不会被触碰。
      </p>

      {error ? (
        <p className="text-[11px] text-rose-600">读取失败：{(error as Error).message}</p>
      ) : categories.length === 0 ? (
        <p className="text-[11px] text-gray-500">{isLoading ? '正在统计…' : '暂无数据。'}</p>
      ) : (
        <ul className="divide-y divide-brand-border/60">
          {categories.map((category) => (
            <li key={category.key} data-testid={`settings-storage-row-${category.key}`} className="flex items-center gap-3 py-2.5">
              <div className="min-w-0 flex-1">
                <p className="text-[11px] font-semibold text-gray-900">{category.label}</p>
                <p className="mt-0.5 truncate text-[10px] text-gray-500" title={category.description}>
                  {category.description}
                </p>
              </div>
              <span className="shrink-0 font-mono text-[10px] text-gray-500">{category.files} 个文件</span>
              <span className="w-16 shrink-0 text-right font-mono text-[11px] text-gray-800">
                {formatBytes(category.bytes)}
              </span>
              {category.reclaimable ? (
                <button
                  type="button"
                  data-testid={`settings-storage-clean-${category.key}`}
                  onClick={() => void runCleanup(category)}
                  disabled={cleaning === category.key || category.bytes === 0}
                  className={`shrink-0 rounded-md border px-2 py-1 text-[10px] font-bold transition disabled:opacity-40 ${
                    pendingConfirm === category.key
                      ? 'border-rose-300 bg-rose-50 text-rose-700'
                      : 'border-brand-border text-gray-600 hover:border-brand-accent/50 hover:text-gray-900'
                  }`}
                >
                  {cleaning === category.key ? (
                    <Loader2 className="h-3 w-3 animate-spin" />
                  ) : pendingConfirm === category.key ? (
                    '确认删除'
                  ) : (
                    <Trash2 className="h-3 w-3" />
                  )}
                </button>
              ) : (
                <span className="w-8 shrink-0" />
              )}
            </li>
          ))}
        </ul>
      )}

      {data?.data_dir ? (
        <p className="truncate font-mono text-[10px] text-gray-400" title={data.data_dir}>
          {data.data_dir}
        </p>
      ) : null}
    </div>
  );
};
