import { useCallback, useEffect, useMemo, useState } from 'react';
import { api, type RadarCreatorComparison, type RadarCreatorComparisonEntry } from '../../api';
import {
  assignColors,
  isSeriesInsufficient,
  projectPoints,
  seriesPoints,
  windowDays,
  type CreatorCompareSeriesKind,
} from '../../radarCreatorCompare';

type CompareWindowDays = 7 | 30 | 90;

const WINDOW_OPTIONS: CompareWindowDays[] = [7, 30, 90];
// 默认勾选的博主数：后端已按真实观测条数排序，前几个就是当前最有得比的对象。
const DEFAULT_SELECTED = 5;
// 图例一次最多渲染多少位博主：真实库里博主可能上百，全渲染会把面板刷成一片标签墙。
const LEGEND_LIMIT = 24;

const CHART = {
  width: 1180,
  height: 220,
  padding: { top: 16, right: 24, bottom: 30, left: 24 },
};

interface CompareChartProps {
  title: string;
  hint: string;
  kind: CreatorCompareSeriesKind;
  creators: RadarCreatorComparisonEntry[];
  colors: Record<string, string>;
  from: string;
  to: string;
}

function CompareChart({ title, hint, kind, creators, colors, from, to }: CompareChartProps) {
  const series = creators.map((creator) => ({
    creator,
    points: seriesPoints(creator, kind),
  }));
  const allPoints = series.flatMap((entry) => entry.points);
  const maxValue = Math.max(0, ...allPoints.map((point) => point.value));
  const hasData = allPoints.length > 0;
  const axisDays = windowDays(from, to);
  const axisDates = axisDays.length > 1 ? [axisDays[0], axisDays[axisDays.length - 1]] : axisDays;

  return (
    <article className="ui-section p-4" data-testid={`creator-compare-chart-${kind}`}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h3 className="text-sm font-semibold text-gray-900">{title}</h3>
          <p className="mt-0.5 text-[10px] text-gray-500">{hint}</p>
        </div>
        <span className="text-[10px] font-semibold text-gray-500">
          {from || '—'} 至 {to || '—'}
        </span>
      </div>
      {hasData ? (
        <div className="mt-3 overflow-x-auto pb-1">
          <svg
            className="h-56 w-full min-w-[860px] overflow-visible text-gray-900"
            viewBox={`0 0 ${CHART.width} ${CHART.height}`}
            role="img"
            aria-label={`${title}：仅显示真实采集到的点，缺日不补零`}
          >
            <title>{`${title}：仅显示真实采集到的点，缺日不补零`}</title>
            <line
              x1={CHART.padding.left}
              y1={CHART.height - CHART.padding.bottom}
              x2={CHART.width - CHART.padding.right}
              y2={CHART.height - CHART.padding.bottom}
              stroke="currentColor"
              strokeOpacity="0.12"
            />
            {axisDates.map((date, index) => (
              <text
                key={`${date}-${index}`}
                x={index === 0 ? CHART.padding.left : CHART.width - CHART.padding.right}
                y={CHART.height - 10}
                textAnchor={index === 0 ? 'start' : 'end'}
                className="fill-gray-400 text-[10px]"
              >
                {date.slice(5).replace('-', '/')}
              </text>
            ))}
            {series.map(({ creator, points }) => {
              const projected = projectPoints(points, {
                from,
                width: CHART.width,
                height: CHART.height,
                padding: CHART.padding,
                maxValue,
              });
              if (projected.length === 0) return null;
              const color = colors[creator.key] ?? '#111111';
              return (
                <g key={creator.key} role="group" aria-label={`${creator.author} ${title}`}>
                  <title>
                    {`${creator.author}：${projected
                      .map((point) => `${point.date.slice(5)} ${point.value}`)
                      .join(' · ')}`}
                  </title>
                  {projected.length > 1 ? (
                    <polyline
                      fill="none"
                      stroke={color}
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth="2"
                      points={projected.map((point) => `${point.x},${point.y}`).join(' ')}
                    />
                  ) : null}
                  {projected.map((point) => (
                    <circle
                      key={`${creator.key}-${point.date}`}
                      cx={point.x}
                      cy={point.y}
                      r={projected.length > 1 ? 3.5 : 4.5}
                      fill={projected.length > 1 ? color : '#ffffff'}
                      stroke={color}
                      strokeWidth="2"
                    />
                  ))}
                </g>
              );
            })}
          </svg>
        </div>
      ) : (
        <p
          className="mt-3 rounded-lg border border-dashed border-brand-border px-4 py-8 text-center text-xs text-gray-500"
          data-testid={`creator-compare-empty-${kind}`}
        >
          {kind === 'followers'
            ? '还没有读到任何粉丝快照，粉丝走势画不出来。'
            : '选中的博主还没有真实观测数据，互动走势画不出来。'}
        </p>
      )}
    </article>
  );
}

export function CreatorComparePanel() {
  const [days, setDays] = useState<CompareWindowDays>(30);
  const [data, setData] = useState<RadarCreatorComparison | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [selectionTouched, setSelectionTouched] = useState(false);
  const [legendQuery, setLegendQuery] = useState('');

  const load = useCallback(async (windowDays: CompareWindowDays) => {
    setLoading(true);
    setError('');
    try {
      const response = await api.getRadarCreatorComparison(windowDays);
      setData(response);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : '读取博主对比数据失败');
      setData(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(days);
  }, [days, load]);

  const creators = useMemo(() => data?.creators ?? [], [data]);

  // 默认选中数据最全的前几个；用户动过勾选之后不再覆盖他的选择。
  useEffect(() => {
    if (selectionTouched) return;
    setSelected(creators.slice(0, DEFAULT_SELECTED).map((creator) => creator.key));
  }, [creators, selectionTouched]);

  const colors = useMemo(() => assignColors(creators), [creators]);
  // 图例：有关键词就按名字过滤（已选中的永远保留），否则只取数据最多的前 LEGEND_LIMIT 位。
  const legendCreators = useMemo(() => {
    const query = legendQuery.trim().toLowerCase();
    if (query) {
      return creators
        .filter((creator) => creator.author.toLowerCase().includes(query) || selected.includes(creator.key))
        .slice(0, LEGEND_LIMIT);
    }
    return creators.slice(0, LEGEND_LIMIT);
  }, [creators, legendQuery, selected]);
  const selectedCreators = useMemo(
    () => creators.filter((creator) => selected.includes(creator.key)),
    [creators, selected],
  );
  const compareWindow = data?.window;
  const from = compareWindow?.from ?? '';
  const to = compareWindow?.to ?? '';

  const toggle = (key: string) => {
    setSelectionTouched(true);
    setSelected((current) =>
      current.includes(key) ? current.filter((entry) => entry !== key) : [...current, key],
    );
  };

  if (loading && !data) {
    return (
      <div className="ui-section p-6 text-center text-xs text-gray-500" data-testid="creator-compare-loading">
        正在读取真实观测数据…
      </div>
    );
  }

  if (error) {
    return (
      <div className="ui-section p-6 text-center text-xs" data-testid="creator-compare-error">
        <p className="text-gray-700">读取博主对比数据失败：{error}</p>
        <button
          type="button"
          className="mt-3 rounded-md border border-brand-border px-3 py-1.5 text-xs font-semibold text-gray-700 hover:bg-black/[0.04]"
          onClick={() => void load(days)}
        >
          重试
        </button>
      </div>
    );
  }

  if (!data || data.empty || creators.length === 0) {
    return (
      <div className="space-y-4">
        <WindowPicker days={days} onChange={setDays} />
        <div className="ui-section p-8 text-center" data-testid="creator-compare-empty">
          <h3 className="text-sm font-semibold text-gray-900">暂无博主对比数据</h3>
          <p className="mt-1 text-xs text-gray-500">
            窗口内还没有任何博主的真实观测或粉丝快照。先在「博主列表」抓取或归档博主，数据落库后这里会自动出现。
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4" data-testid="creator-compare-panel">
      <WindowPicker days={days} onChange={setDays} />
      <article className="ui-section p-4">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <h3 className="text-sm font-semibold text-gray-900">博主对比</h3>
            <p className="mt-0.5 text-[10px] text-gray-500">
              只用真实采集到的点：粉丝点来自归档的博主资料快照，互动点来自雷达观测；缺日不补零、不插值。
            </p>
          </div>
          <span className="text-[10px] font-semibold text-gray-500">共 {creators.length} 位博主</span>
        </div>
        {creators.length > LEGEND_LIMIT ? (
          <input
            type="search"
            value={legendQuery}
            onChange={(event) => setLegendQuery(event.target.value)}
            placeholder={`共 ${creators.length} 位博主，输入名字筛选`}
            className="mt-3 w-full max-w-sm rounded-md border border-brand-border bg-brand-input-bg px-3 py-1.5 text-xs"
            data-testid="creator-compare-search"
          />
        ) : null}
        <ul className="mt-3 flex flex-wrap gap-2" data-testid="creator-compare-legend">
          {legendCreators.map((creator) => {
            const active = selected.includes(creator.key);
            const followersShort = isSeriesInsufficient(creator, 'followers');
            const seriesShort = isSeriesInsufficient(creator, 'interactions');
            return (
              <li key={creator.key}>
                <label
                  className={`flex cursor-pointer items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs ${
                    active ? 'border-gray-900 bg-black/[0.03] text-gray-900' : 'border-brand-border text-gray-500'
                  }`}
                >
                  <input
                    type="checkbox"
                    className="h-3.5 w-3.5"
                    checked={active}
                    onChange={() => toggle(creator.key)}
                  />
                  <span
                    className="h-2.5 w-2.5 rounded-full"
                    style={{ backgroundColor: colors[creator.key] ?? '#111111' }}
                    aria-hidden="true"
                  />
                  <span className="font-semibold">{creator.author}</span>
                  <span className="text-[10px] text-gray-500">
                    观测 {creator.observations} · 作品 {creator.works}
                  </span>
                  {followersShort ? (
                    <span
                      className="rounded-sm bg-amber-50 px-1.5 py-0.5 text-[10px] text-amber-700"
                      data-testid={`creator-compare-followers-short-${creator.key}`}
                    >
                      粉丝数据点不足（{creator.follower_points} 个真实点）
                    </span>
                  ) : null}
                  {seriesShort ? (
                    <span
                      className="rounded-sm bg-amber-50 px-1.5 py-0.5 text-[10px] text-amber-700"
                      data-testid={`creator-compare-series-short-${creator.key}`}
                    >
                      互动数据点不足
                    </span>
                  ) : null}
                </label>
              </li>
            );
          })}
        </ul>
        {legendCreators.length < creators.length ? (
          <p className="mt-2 text-[10px] text-gray-500" data-testid="creator-compare-legend-truncated">
            只列出数据最多的 {legendCreators.length} 位；其余博主可用上方搜索框按名字找到。
          </p>
        ) : null}
        {selectedCreators.length === 0 ? (
          <p className="mt-3 text-xs text-gray-500" data-testid="creator-compare-none-selected">
            至少勾选一位博主才能看图。
          </p>
        ) : null}
      </article>

      <CompareChart
        title="粉丝走势"
        hint="每个点是一次真实读取到的粉丝数；只有一个点的博主不连线，如实标注数据点不足。"
        kind="followers"
        creators={selectedCreators}
        colors={colors}
        from={from}
        to={to}
      />
      <CompareChart
        title="互动走势（点赞+收藏+分享+评论 / 日）"
        hint="每个点是当天所有作品观测的互动合计；没有观测的日期不画成 0。"
        kind="interactions"
        creators={selectedCreators}
        colors={colors}
        from={from}
        to={to}
      />
    </div>
  );
}

function WindowPicker({ days, onChange }: { days: CompareWindowDays; onChange: (value: CompareWindowDays) => void }) {
  return (
    <div className="flex items-center gap-2" data-testid="creator-compare-window">
      <span className="text-xs text-gray-500">时间窗</span>
      {WINDOW_OPTIONS.map((option) => (
        <button
          key={option}
          type="button"
          className={`h-8 rounded-md px-3 text-xs font-semibold transition-colors ${
            days === option ? 'bg-gray-950 text-white' : 'text-gray-600 hover:bg-black/[0.04] hover:text-gray-900'
          }`}
          onClick={() => onChange(option)}
        >
          {option} 天
        </button>
      ))}
    </div>
  );
}
