import useSWR from 'swr';
import { Eye, Heart, MessageCircle, Share2, Star, UserRound, FileText, TrendingUp } from 'lucide-react';
import { api, type MetricSummary } from '../api';

/**
 * 账号数据。表格优先：同一批数字用表格横排对比，比一堆卡片更容易看出
 * 哪个平台在涨、哪个停在原地。
 *
 * 数字全部来自本地已采集的快照，不推断、不估算；没有采集过的账号显示 0
 * 而不是灰色占位，避免把「没数据」误读成「数据是 0」。
 */

const totals = [
  { key: 'views', label: '浏览', icon: Eye },
  { key: 'likes', label: '点赞', icon: Heart },
  { key: 'comments', label: '评论', icon: MessageCircle },
  { key: 'shares', label: '分享', icon: Share2 },
  { key: 'favorites', label: '收藏', icon: Star },
  { key: 'followers', label: '关注', icon: UserRound },
  { key: 'posts', label: '作品', icon: FileText },
  { key: 'interactions', label: '互动总量', icon: TrendingUp },
] as const;

function compact(n: number | undefined): string {
  const value = n ?? 0;
  if (value >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`;
  if (value >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
  return value.toLocaleString('zh-CN');
}

function percent(value: number | undefined): string {
  if (!value) return '—';
  return `${(value * 100).toFixed(2)}%`;
}

export function MetricsView() {
  const { data, error, isLoading } = useSWR<MetricSummary>('api/metrics/summary', api.getMetricSummary);
  const platforms = data?.platforms ?? [];
  const hasData = (data?.samples ?? 0) > 0;

  return (
    <section className="mx-auto max-w-6xl space-y-5" data-testid="metrics-view">
      <div>
        <h3 className="text-base font-bold text-gray-900">账号数据</h3>
        <p className="mt-1 text-xs text-gray-500">
          仅汇总本地已采集的快照；每个账号只取最新一天，不会把同一账号在多天里重复相加。
        </p>
      </div>

      {error ? (
        <p className="rounded-lg border border-red-200 p-3 text-sm text-red-600">指标加载失败</p>
      ) : null}

      {/* 汇总条：一眼看总量 */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 lg:grid-cols-8">
        {totals.map(({ key, label, icon: Icon }) => (
          <div key={key} className="rounded-xl border border-brand-border bg-brand-surface p-3">
            <div className="flex items-center gap-1.5 text-brand-muted">
              <Icon className="h-3.5 w-3.5" />
              <span className="text-[11px]">{label}</span>
            </div>
            <p className="mt-1.5 text-lg font-bold tabular-nums text-gray-900">{compact(data?.[key])}</p>
          </div>
        ))}
      </div>

      {/* 平台明细表：多维度横排 */}
      <div className="overflow-hidden rounded-xl border border-brand-border bg-brand-surface">
        <div className="flex items-center justify-between border-b border-brand-border px-4 py-3">
          <div>
            <h4 className="text-sm font-bold text-gray-900">平台明细</h4>
            <p className="mt-0.5 text-[11px] text-gray-500">
              {hasData ? `${data?.samples} 个账号有快照 · 互动率 = 互动总量 / 浏览` : '还没有采集到快照'}
            </p>
          </div>
          {data?.engagement_rate ? (
            <span className="rounded-md bg-brand-accent/10 px-2 py-1 text-[11px] font-semibold text-brand-accent">
              整体互动率 {percent(data.engagement_rate)}
            </span>
          ) : null}
        </div>

        {isLoading ? (
          <p className="p-8 text-center text-sm text-gray-400">正在读取本地快照…</p>
        ) : platforms.length === 0 ? (
          <p className="p-8 text-center text-sm text-gray-400">
            尚无本地采集指标。在账号数据页触发一次采集后会出现在这里。
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[860px] border-collapse text-sm">
              <thead>
                <tr className="border-b border-brand-border bg-brand-bg/60 text-[11px] text-brand-muted">
                  <th className="px-4 py-2.5 text-left font-medium">平台</th>
                  <th className="px-3 py-2.5 text-right font-medium">账号数</th>
                  <th className="px-3 py-2.5 text-right font-medium">关注</th>
                  <th className="px-3 py-2.5 text-right font-medium">作品</th>
                  <th className="px-3 py-2.5 text-right font-medium">浏览</th>
                  <th className="px-3 py-2.5 text-right font-medium">点赞</th>
                  <th className="px-3 py-2.5 text-right font-medium">评论</th>
                  <th className="px-3 py-2.5 text-right font-medium">分享</th>
                  <th className="px-3 py-2.5 text-right font-medium">收藏</th>
                  <th className="px-3 py-2.5 text-right font-medium">互动率</th>
                </tr>
              </thead>
              <tbody>
                {platforms.map((row: MetricSummary['platforms'][number]) => (
                  <tr
                    key={row.platform_code || 'unknown'}
                    className="border-b border-brand-border/60 last:border-0 hover:bg-brand-bg/40"
                  >
                    <td className="px-4 py-2.5 font-semibold text-gray-900">
                      {row.platform_code || '未标注平台'}
                    </td>
                    <td className="px-3 py-2.5 text-right tabular-nums text-gray-600">{row.samples}</td>
                    <td className="px-3 py-2.5 text-right tabular-nums text-gray-600">{compact(row.followers)}</td>
                    <td className="px-3 py-2.5 text-right tabular-nums text-gray-600">{compact(row.posts)}</td>
                    <td className="px-3 py-2.5 text-right tabular-nums font-semibold text-gray-900">{compact(row.views)}</td>
                    <td className="px-3 py-2.5 text-right tabular-nums text-gray-600">{compact(row.likes)}</td>
                    <td className="px-3 py-2.5 text-right tabular-nums text-gray-600">{compact(row.comments)}</td>
                    <td className="px-3 py-2.5 text-right tabular-nums text-gray-600">{compact(row.shares)}</td>
                    <td className="px-3 py-2.5 text-right tabular-nums text-gray-600">{compact(row.favorites)}</td>
                    <td className="px-3 py-2.5 text-right tabular-nums text-gray-900">{percent(row.engagement_rate)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </section>
  );
}
