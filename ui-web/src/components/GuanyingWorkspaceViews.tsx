import React, { useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  AlertTriangle,
  ArrowRight,
  Bot,
  Clock,
  Copy,
  ExternalLink,
  FileCheck2,
  Film,
  Filter,
  MessageSquarePlus,
  RefreshCw,
  Search,
  Send,
  ShieldCheck,
  Sparkles,
  Table2,
  Zap,
} from 'lucide-react';
import { api } from '../api';
import { useUIStore } from '../store';
import { openAgentContext } from './AgentContextDrawer';

export interface GuanyingProduct {
  id: string;
  code: string;
  oe_or_wva: string;
  name: string;
  brand: string;
  category: string;
  vehicle_series: string;
  outer_arc_mm: number;
  inner_arc_mm: number;
  width_mm: number;
  thickness_str: string;
  thickness_mm: number;
  drum_or_taper: string;
  holes: number;
  rivet_spec: string;
  applicable_models: string;
  swap_warning: string;
  formula_code: string;
  price_fob_usd: string;
  price_cny: string;
  moq: string;
  catalog_page: string;
  image_url: string;
}

export interface GuanyingFormula {
  code: string;
  name: string;
  certification: string;
  friction_range: string;
  wear_400c: string;
  weight_loss_400c: string;
  noise_limit: string;
  service_life: string;
  best_for: string;
}

export interface GuanyingLead {
  id: string;
  company: string;
  country: string;
  channel: string;
  timezone: string;
  buyer_type: string;
  confidence: number;
  is_terminal: boolean;
  inquiry_text: string;
  matched_code: string;
  matched_product: string;
  status: string;
  filter_reason: string;
  created_at: string;
}

export interface GuanyingQuote {
  id: string;
  lead_id?: string;
  customer_query: string;
  product_id: string;
  product_code: string;
  product_name: string;
  dimensions: string;
  formula_code: string;
  unit_price_usd: string;
  unit_price_cny: string;
  moq: string;
  swap_advice: string;
  bilingual_text: string;
  created_at: string;
}

export interface GuanyingSEOPage {
  id: string;
  slug: string;
  lang: string;
  title: string;
  meta_description: string;
  target_keywords: string;
  target_market: string;
  series_focus: string;
  hero_heading: string;
  hero_subheading: string;
  highlights_json: string;
  product_codes: string;
  schema_jsonld: string;
  inquiry_count: number;
  created_at: string;
}

export interface GuanyingOverview {
  products: GuanyingProduct[];
  formulas: GuanyingFormula[];
  leads: GuanyingLead[];
  quotes: GuanyingQuote[];
  seo_pages: GuanyingSEOPage[];
  stats: {
    product_count: number;
    terminal_lead_count: number;
    filtered_trader_count: number;
    quote_count: number;
    seo_page_count: number;
    content_count: number;
    knowledge_doc_count: number;
  };
}

async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  });
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(text || `HTTP ${res.status}`);
  }
  return res.json() as Promise<T>;
}

export interface GuanyingVideoItem {
  id: string;
  filename: string;
  url: string;
  title: string;
  platform: string;
  account: string;
  source_url: string;
  duration_sec: number;
  tags: string[];
  suggested_caption: string;
}

const SCENARIO_PLAYBOOKS = [
  {
    id: 'hand-measure',
    badge: '场景一 · 无 OE 编码识片',
    title: '门店手量尺寸 30 秒报价',
    pain: '客户发卡尺实测外弧 240 × 宽 220mm（20孔），无原厂编码',
    action: '±5mm 公差瞬时锁定汉德 35T (3501D10-120) 并出双语报价',
    query: '',
    outer: '240',
    width: '220',
    holes: '20',
  },
  {
    id: 'axle-swap',
    badge: '场景二 · 换桥防退货',
    title: '后市场改装/换桥纠错',
    pain: '买家口述东风 EQ153，海外维修厂常改桥造成错孔错铆钉',
    action: '强制触发换桥对比与 14孔/铆钉核对红线提醒，杜绝整柜退货',
    query: 'EQ153',
    outer: '',
    width: '',
    holes: '',
  },
  {
    id: 'mining-heat',
    badge: '场景三 · 矿山高温工况',
    title: '非洲/拉美重载长坡配方升级',
    pain: 'WVA 29087 盘刹重载长坡易热衰，用低价竞品 2 个月磨穿',
    action: '自动挂载 GY-NAO-38 配方（400℃磨损仅 0.040mm，寿命 3 倍）',
    query: '29087',
    outer: '',
    width: '',
    holes: '',
  },
  {
    id: 'howo-compare',
    badge: '场景四 · 重汽高频核孔',
    title: 'HOWO 14 孔 vs 8 孔防错发',
    pain: '重汽出口询盘常混报 HOWO 后桥 14 孔与 8 孔规格',
    action: '并列校对 GYS-126 等匹配型号与 FOB 单价，一线 30 秒发车',
    query: 'HOWO',
    outer: '',
    width: '220',
    holes: '',
  },
];

export const GuanyingDatabaseView: React.FC = () => {
  const toast = useUIStore((s) => s.toast);
  const setActiveTab = useUIStore((s) => s.setActiveTab);
  const setAgentDraft = useUIStore((s) => s.setAgentDraft);

  const { data: overview, mutate } = useSWR('api/guanying/overview', () =>
    fetchJSON<GuanyingOverview>('/api/guanying/overview'),
  );
  const { data: videosData } = useSWR<GuanyingVideoItem[]>('guanying-videos/manifest', async () => {
    try {
      const res = await fetch('/guanying-videos/manifest.json');
      if (!res.ok) return [];
      return (await res.json()) as GuanyingVideoItem[];
    } catch {
      return [];
    }
  });
  const videos = videosData ?? [];

  const [subTab, setSubTab] = useState<'catalog' | 'leads' | 'videos'>('catalog');
  const [query, setQuery] = useState('');
  const [outerArc, setOuterArc] = useState('');
  const [widthMM, setWidthMM] = useState('');
  const [holes, setHoles] = useState('');
  const [seriesFilter, setSeriesFilter] = useState('全部');
  const [leadFilter, setLeadFilter] = useState<'all' | 'terminal' | 'trader'>('all');

  const [matching, setMatching] = useState(false);
  const [activeQuote, setActiveQuote] = useState<GuanyingQuote | null>(null);
  const [activeProduct, setActiveProduct] = useState<GuanyingProduct | null>(null);
  const [previewImg, setPreviewImg] = useState<string | null>(null);

  const products = overview?.products ?? [];
  const leads = overview?.leads ?? [];
  const formulas = overview?.formulas ?? [];

  const seriesOptions = useMemo(() => {
    const s = new Set<string>(['全部']);
    products.forEach((p) => {
      if (p.vehicle_series) s.add(p.vehicle_series);
    });
    return Array.from(s);
  }, [products]);

  const filteredProducts = useMemo(() => {
    const q = query.trim().toLowerCase();
    const o = Number(outerArc) || 0;
    const w = Number(widthMM) || 0;
    const h = Number(holes) || 0;

    return products.filter((p) => {
      if (seriesFilter !== '全部' && p.vehicle_series !== seriesFilter) return false;
      if (o > 0 && Math.abs(p.outer_arc_mm - o) > 5) return false;
      if (w > 0 && Math.abs(p.width_mm - w) > 5) return false;
      if (h > 0 && p.holes !== h) return false;
      if (!q) return true;
      const hay = `${p.code} ${p.oe_or_wva} ${p.name} ${p.vehicle_series} ${p.applicable_models} ${p.brand}`.toLowerCase();
      return hay.includes(q);
    });
  }, [products, query, outerArc, widthMM, holes, seriesFilter]);

  const filteredLeads = useMemo(() => {
    return leads.filter((l) => {
      if (leadFilter === 'terminal') return l.is_terminal;
      if (leadFilter === 'trader') return !l.is_terminal;
      return true;
    });
  }, [leads, leadFilter]);

  const selectedProduct = activeProduct ?? filteredProducts[0] ?? products[0] ?? null;

  async function generateQuote(opts: {
    productID?: string;
    leadID?: string;
    queryText?: string;
    outerArcMM?: number;
    widthMM?: number;
    holesCount?: number;
    save?: boolean;
  }) {
    setMatching(true);
    try {
      const resp = await fetchJSON<{ quote?: GuanyingQuote; matches: Array<{ product: GuanyingProduct }> }>(
        '/api/guanying/match-quote',
        {
          method: 'POST',
          body: JSON.stringify({
            query: opts.queryText !== undefined ? opts.queryText : query,
            outer_arc_mm: opts.outerArcMM !== undefined ? opts.outerArcMM : Number(outerArc) || 0,
            width_mm: opts.widthMM !== undefined ? opts.widthMM : Number(widthMM) || 0,
            holes: opts.holesCount !== undefined ? opts.holesCount : Number(holes) || 0,
            product_id: opts.productID || '',
            lead_id: opts.leadID || '',
            save_quote: Boolean(opts.save),
          }),
        },
      );
      if (resp.matches?.[0]?.product) {
        setActiveProduct(resp.matches[0].product);
      }
      if (resp.quote) {
        setActiveQuote(resp.quote);
        if (opts.save) {
          toast.show(`已生成并保存场景报价单：${resp.quote.product_code}`, 'success');
          await mutate();
        }
      } else {
        toast.show('未匹配到符合容差条件的型号，请放宽尺寸条件。', 'info');
      }
    } catch (err) {
      toast.show(err instanceof Error ? err.message : '匹配报价失败', 'error');
    } finally {
      setMatching(false);
    }
  }

  function askAgentWithContext(p: GuanyingProduct) {
    setAgentDraft({
      mode: 'chat',
      message: `请作为冠英一线技术报价专家，帮我针对以下官方型号核对后市场换桥风险并润色一封发给海外车队的报价邮件：\n型号：${p.code} (${p.oe_or_wva})\n名称：${p.name}\n尺寸：外弧 ${p.outer_arc_mm} × 宽 ${p.width_mm} × 厚 ${p.thickness_str} mm，${p.holes} 孔，铆钉 ${p.rivet_spec}\n适配：${p.applicable_models}\n配方：${p.formula_code}｜FOB参考：${p.price_fob_usd}`,
    });
    setActiveTab('bot');
  }

  return (
    <div className="mx-auto max-w-[1360px] space-y-6 pb-10">
      {/* Header */}
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-brand-accent">
            <Zap className="h-3.5 w-3.5" />
            <span>浙江冠英 GTM 核心业务场景工作台 · 智能识片报价 · 跨时差防漏单</span>
          </div>
          <h1 className="mt-1 text-2xl font-bold tracking-tight text-brand-fg">
            识片报价与防漏单场景
          </h1>
          <p className="mt-1 text-xs text-brand-muted">
            聚焦门店手量识片、后市场换桥纠错、矿山高温配方升级与跨时差防漏单 4 大成交场景，30 秒直出中英双语报价单。
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-1.5 rounded-xl border border-brand-border bg-brand-surface p-1">
          <button
            type="button"
            onClick={() => setSubTab('catalog')}
            className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-semibold transition ${
              subTab === 'catalog' ? 'bg-brand-accent text-brand-on-accent shadow-xs' : 'text-brand-muted hover:text-brand-fg'
            }`}
          >
            <Zap className="h-3.5 w-3.5" />
            现场识片与 30 秒报价
          </button>
          <button
            type="button"
            onClick={() => setSubTab('leads')}
            className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-semibold transition ${
              subTab === 'leads' ? 'bg-brand-accent text-brand-on-accent shadow-xs' : 'text-brand-muted hover:text-brand-fg'
            }`}
          >
            <ShieldCheck className="h-3.5 w-3.5" />
            跨时差询盘防漏单 ({overview?.stats.terminal_lead_count ?? 0})
          </button>
          <button
            type="button"
            onClick={() => setSubTab('videos')}
            className={`inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-semibold transition ${
              subTab === 'videos' ? 'bg-brand-accent text-brand-on-accent shadow-xs' : 'text-brand-muted hover:text-brand-fg'
            }`}
          >
            <Film className="h-3.5 w-3.5" />
            老板娘实拍短视频 ({videos.length})
          </button>
        </div>
      </div>

      {subTab === 'catalog' ? (
        <>
          {/* Scenario Playbook Cards */}
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            {SCENARIO_PLAYBOOKS.map((sc) => (
              <button
                key={sc.id}
                type="button"
                onClick={() => {
                  setQuery(sc.query);
                  setOuterArc(sc.outer);
                  setWidthMM(sc.width);
                  setHoles(sc.holes);
                  setSeriesFilter('全部');
                  setActiveProduct(null);
                  void generateQuote({
                    queryText: sc.query,
                    outerArcMM: Number(sc.outer) || 0,
                    widthMM: Number(sc.width) || 0,
                    holesCount: Number(sc.holes) || 0,
                    save: true,
                  });
                }}
                className="ui-card group flex flex-col justify-between space-y-2.5 p-4 text-left transition hover:border-brand-accent hover:bg-brand-surface-raised"
              >
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <span className="rounded-full bg-brand-accent/12 px-2 py-0.5 text-[10px] font-bold text-brand-accent">
                      {sc.badge}
                    </span>
                    <ArrowRight className="h-3.5 w-3.5 text-brand-muted transition group-hover:translate-x-0.5 group-hover:text-brand-accent" />
                  </div>
                  <h3 className="text-xs font-bold text-brand-fg">{sc.title}</h3>
                  <p className="text-[11px] leading-relaxed text-brand-muted">{sc.pain}</p>
                </div>
                <p className="border-t border-brand-border/70 pt-2 text-[11px] font-medium text-emerald-600 dark:text-emerald-400">
                  {sc.action}
                </p>
              </button>
            ))}
          </div>
          {/* Search & Dimension Bar */}
          <div className="ui-card space-y-4 p-5">
            <div className="grid gap-3 md:grid-cols-[2fr_repeat(3,minmax(100px,0.55fr))_auto]">
              <div className="relative">
                <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-brand-muted" />
                <input
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  placeholder="输入车型 / 车桥 / WVA / 编码（如 HOWO、29087、307AF、EQ153、汉德）"
                  className="h-10 w-full rounded-lg border border-brand-border bg-brand-bg pl-9 pr-3 text-xs text-brand-fg focus:border-brand-accent focus:outline-none"
                />
              </div>
              <input
                type="number"
                value={outerArc}
                onChange={(e) => setOuterArc(e.target.value)}
                placeholder="外弧 A (mm)"
                className="h-10 rounded-lg border border-brand-border bg-brand-bg px-3 text-xs text-brand-fg focus:border-brand-accent focus:outline-none"
              />
              <input
                type="number"
                value={widthMM}
                onChange={(e) => setWidthMM(e.target.value)}
                placeholder="宽度 C (mm)"
                className="h-10 rounded-lg border border-brand-border bg-brand-bg px-3 text-xs text-brand-fg focus:border-brand-accent focus:outline-none"
              />
              <input
                type="number"
                value={holes}
                onChange={(e) => setHoles(e.target.value)}
                placeholder="孔数 (#)"
                className="h-10 rounded-lg border border-brand-border bg-brand-bg px-3 text-xs text-brand-fg focus:border-brand-accent focus:outline-none"
              />
              <button
                type="button"
                disabled={matching}
                onClick={() => void generateQuote({ save: true })}
                className="inline-flex h-10 items-center justify-center gap-1.5 rounded-lg bg-brand-accent px-4 text-xs font-semibold text-brand-on-accent transition hover:brightness-110 disabled:opacity-60"
              >
                <Sparkles className="h-3.5 w-3.5" />
                {matching ? '匹配中…' : '秒出选型报价'}
              </button>
            </div>

            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-[11px] font-medium text-brand-muted">快速演练参数：</span>
                {SCENARIO_PLAYBOOKS.map((sc) => (
                  <button
                    key={sc.id}
                    type="button"
                    onClick={() => {
                      setQuery(sc.query);
                      setOuterArc(sc.outer);
                      setWidthMM(sc.width);
                      setHoles(sc.holes);
                      setSeriesFilter('全部');
                    }}
                    className="rounded-full border border-brand-border bg-brand-surface-raised px-2.5 py-1 text-[11px] font-medium text-brand-fg transition hover:border-brand-accent hover:text-brand-accent"
                  >
                    {sc.title}
                  </button>
                ))}
                {(query || outerArc || widthMM || holes || seriesFilter !== '全部') && (
                  <button
                    type="button"
                    onClick={() => {
                      setQuery('');
                      setOuterArc('');
                      setWidthMM('');
                      setHoles('');
                      setSeriesFilter('全部');
                    }}
                    className="px-2 py-1 text-[11px] text-brand-muted underline hover:text-brand-fg"
                  >
                    重置筛选
                  </button>
                )}
              </div>

              <div className="flex items-center gap-1.5">
                <Filter className="h-3.5 w-3.5 text-brand-muted" />
                <select
                  value={seriesFilter}
                  onChange={(e) => setSeriesFilter(e.target.value)}
                  className="h-8 rounded-lg border border-brand-border bg-brand-bg px-2.5 text-[11px] text-brand-fg"
                >
                  {seriesOptions.map((s) => (
                    <option key={s} value={s}>{s}</option>
                  ))}
                </select>
              </div>
            </div>
          </div>

          {/* Main 2-column Workspace: Left Product Cards, Right Instant Quote & Bench Evidence */}
          <div className="grid gap-6 lg:grid-cols-[1.25fr_1fr]">
            <div className="space-y-3">
              <div className="flex items-center justify-between px-1">
                <span className="text-xs font-semibold text-brand-fg">
                  匹配型号结果（{filteredProducts.length} / {products.length}）
                </span>
                <span className="text-[11px] text-brand-muted">点击卡片生成该型号双语报价单</span>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                {filteredProducts.slice(0, 16).map((p) => {
                  const isSelected = selectedProduct?.id === p.id;
                  return (
                    <article
                      key={p.id}
                      onClick={() => {
                        setActiveProduct(p);
                        void generateQuote({ productID: p.id, queryText: p.code, save: false });
                      }}
                      className={`ui-card cursor-pointer space-y-2.5 p-4 transition ${
                        isSelected
                          ? 'ring-2 ring-brand-accent bg-brand-surface-raised'
                          : 'hover:border-brand-accent/50'
                      }`}
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div className="min-w-0">
                          <div className="flex items-center gap-1.5">
                            <span className="rounded bg-brand-accent/12 px-1.5 py-0.5 font-mono text-[10px] font-bold text-brand-accent">
                              {p.code}
                            </span>
                            <span className="rounded bg-brand-fg/[0.06] px-1.5 py-0.5 text-[10px] font-medium text-brand-muted">
                              {p.category}
                            </span>
                          </div>
                          <h3 className="mt-1.5 truncate text-sm font-bold text-brand-fg">{p.name}</h3>
                          <p className="truncate text-[11px] text-brand-muted">{p.oe_or_wva}</p>
                        </div>
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation();
                            setPreviewImg(p.image_url);
                          }}
                          title="查看官方 PDF 原图"
                          className="shrink-0 overflow-hidden rounded-md border border-brand-border bg-white p-0.5 transition hover:scale-105"
                        >
                          <img src={p.image_url} alt={p.code} className="h-11 w-14 object-cover" />
                        </button>
                      </div>

                      <div className="grid grid-cols-3 gap-1.5 rounded-lg bg-brand-bg/70 px-2.5 py-2 text-[11px]">
                        <div>
                          <span className="block text-[9px] text-brand-muted">外弧×宽×厚</span>
                          <span className="font-mono font-semibold text-brand-fg">
                            {p.outer_arc_mm}×{p.width_mm}×{p.thickness_str}
                          </span>
                        </div>
                        <div>
                          <span className="block text-[9px] text-brand-muted">孔数 / 铆钉</span>
                          <span className="font-semibold text-brand-fg">
                            {p.holes > 0 ? `${p.holes}孔 · ${p.rivet_spec}` : p.rivet_spec}
                          </span>
                        </div>
                        <div>
                          <span className="block text-[9px] text-brand-muted">参考 FOB</span>
                          <span className="font-semibold text-emerald-600">{p.price_fob_usd}</span>
                        </div>
                      </div>

                      <p className="line-clamp-1 text-[11px] text-brand-muted">
                        适配：{p.applicable_models}
                      </p>
                      <div className="flex items-center justify-between text-[10px] text-brand-muted">
                        <span>{p.brand} · {p.catalog_page}</span>
                        <span className="font-medium text-brand-accent">点击出报价 →</span>
                      </div>
                    </article>
                  );
                })}
              </div>
              {filteredProducts.length > 16 && (
                <p className="text-center text-[11px] text-brand-muted">
                  当前展示前 16 条匹配结果，可通过上方车型/编码/尺寸进一步缩小范围。
                </p>
              )}
            </div>

            {/* Right Panel: Selected Spec + Swap Warning + 30s Quote Output */}
            <div className="space-y-4">
              {selectedProduct ? (
                <div className="ui-card sticky top-4 space-y-4 p-5">
                  <div className="flex items-start justify-between gap-3 border-b border-brand-border pb-3">
                    <div>
                      <span className="text-[10px] font-bold uppercase tracking-wider text-brand-accent">
                        一线销售当场选型 & 闪电报价单
                      </span>
                      <h2 className="mt-1 text-base font-bold text-brand-fg">
                        {selectedProduct.code} · {selectedProduct.name}
                      </h2>
                      <p className="text-xs text-brand-muted">{selectedProduct.applicable_models}</p>
                    </div>
                    <button
                      type="button"
                      onClick={() => setPreviewImg(selectedProduct.image_url)}
                      className="shrink-0 rounded-lg border border-brand-border px-2.5 py-1.5 text-[11px] font-semibold text-brand-fg hover:bg-brand-fg/[0.04]"
                    >
                      查看 PDF 原页
                    </button>
                  </div>

                  {/* Aftermarket Swap Warning */}
                  <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 p-3 text-xs">
                    <p className="font-bold text-amber-700 dark:text-amber-300">后市场换桥 / 编码核对提醒</p>
                    <p className="mt-1 leading-relaxed text-brand-fg/90">{selectedProduct.swap_warning}</p>
                  </div>

                  {/* Bench Test Formula */}
                  {(() => {
                    const formula = formulas.find((f) => f.code === selectedProduct.formula_code) ?? formulas[0];
                    if (!formula) return null;
                    return (
                      <div className="rounded-xl border border-brand-border bg-brand-bg/70 p-3.5 text-xs space-y-1.5">
                        <div className="flex items-center justify-between">
                          <span className="font-bold text-brand-fg">{formula.code} · {formula.name}</span>
                          <span className="rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] font-semibold text-emerald-600">
                            IATF16949 / 无石棉
                          </span>
                        </div>
                        <p className="text-[11px] text-brand-muted">台架实测：{formula.wear_400c} · {formula.friction_range}</p>
                      </div>
                    );
                  })()}

                  {/* Bilingual Quote Box */}
                  <div className="space-y-2">
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-semibold text-brand-fg">中英双语标准报价单</span>
                      <span className="text-[11px] text-brand-muted">
                        FOB {selectedProduct.price_fob_usd} · MOQ {selectedProduct.moq}
                      </span>
                    </div>
                    <textarea
                      readOnly
                      rows={11}
                      value={
                        activeQuote && activeQuote.product_id === selectedProduct.id
                          ? activeQuote.bilingual_text
                          : `【浙江冠英 Kooying / GOOLE 闪电选型报价单】\n1. 型号：${selectedProduct.code} (${selectedProduct.oe_or_wva})\n2. 几何参数：外弧 ${selectedProduct.outer_arc_mm} × 宽 ${selectedProduct.width_mm} × 厚 ${selectedProduct.thickness_str} mm，${selectedProduct.holes}孔 (${selectedProduct.rivet_spec})\n3. 适配车桥/车型：${selectedProduct.applicable_models}\n4. 换桥防错核对：${selectedProduct.swap_warning}\n5. 推荐配方：${selectedProduct.formula_code}（400℃ 厚度磨损 0.040mm，摩擦系数 0.35-0.38）\n6. 参考价格：FOB ${selectedProduct.price_fob_usd} / 国内 ${selectedProduct.price_cny}（MOQ ${selectedProduct.moq}）`
                      }
                      className="w-full rounded-xl border border-brand-border bg-brand-bg p-3 font-mono text-[11px] leading-relaxed text-brand-fg"
                    />
                  </div>

                  <div className="flex flex-wrap items-center gap-2">
                    <button
                      type="button"
                      onClick={() => void generateQuote({ productID: selectedProduct.id, queryText: selectedProduct.code, save: true })}
                      className="inline-flex flex-1 items-center justify-center gap-1.5 rounded-lg bg-brand-accent px-3 py-2 text-xs font-semibold text-brand-on-accent hover:brightness-110"
                    >
                      <FileCheck2 className="h-3.5 w-3.5" />保存报价入库
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        const text = activeQuote?.bilingual_text || `${selectedProduct.code} (${selectedProduct.oe_or_wva}) FOB ${selectedProduct.price_fob_usd}`;
                        void navigator.clipboard?.writeText(text);
                        toast.show('报价话术已复制，可直接发 WhatsApp / 邮件', 'success');
                      }}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-brand-border px-3 py-2 text-xs font-semibold text-brand-fg hover:bg-brand-fg/[0.04]"
                    >
                      <Copy className="h-3.5 w-3.5" />复制双语话术
                    </button>
                    <button
                      type="button"
                      onClick={() => askAgentWithContext(selectedProduct)}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-brand-border px-3 py-2 text-xs font-semibold text-brand-fg hover:bg-brand-fg/[0.04]"
                    >
                      <MessageSquarePlus className="h-3.5 w-3.5" />交给 Bot 团队
                    </button>
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </>
      ) : subTab === 'leads' ? (
        /* Overseas Inquiry Anti-Miss Sub-tab */
        <div className="space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              {([
                { id: 'all', label: `全部线索 (${leads.length})` },
                { id: 'terminal', label: `高价值终端买家 (${overview?.stats.terminal_lead_count ?? 0})` },
                { id: 'trader', label: `已过滤贸易商 (${overview?.stats.filtered_trader_count ?? 0})` },
              ] as const).map((item) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => setLeadFilter(item.id)}
                  className={`rounded-lg px-3 py-1.5 text-xs font-semibold transition ${
                    leadFilter === item.id
                      ? 'bg-brand-accent text-brand-on-accent'
                      : 'border border-brand-border bg-brand-surface text-brand-muted hover:text-brand-fg'
                  }`}
                >
                  {item.label}
                </button>
              ))}
            </div>
            <button
              type="button"
              onClick={() => setActiveTab('studio')}
              className="inline-flex items-center gap-1.5 rounded-lg border border-brand-border bg-brand-surface px-3 py-1.5 text-xs font-semibold text-brand-fg hover:border-brand-accent"
            >
              去官网 SEO 与 PDF 留资钩子测试 <ArrowRight className="h-3.5 w-3.5" />
            </button>
          </div>

          <div className="grid gap-3">
            {filteredLeads.map((lead) => (
              <article key={lead.id} className="ui-card flex flex-wrap items-start justify-between gap-4 p-5">
                <div className="min-w-0 flex-1 space-y-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <span
                      className={`rounded-full px-2.5 py-0.5 text-[10px] font-bold ${
                        lead.is_terminal
                          ? 'bg-emerald-500/15 text-emerald-600'
                          : 'bg-zinc-500/15 text-brand-muted'
                      }`}
                    >
                      {lead.buyer_type} · 置信度 {lead.confidence}%
                    </span>
                    <span className="rounded bg-brand-fg/[0.06] px-2 py-0.5 text-[10px] font-medium text-brand-fg">
                      {lead.channel}
                    </span>
                    <span className="text-[11px] text-brand-muted">{lead.country} · {lead.timezone}</span>
                  </div>
                  <h3 className="text-sm font-bold text-brand-fg">{lead.company}</h3>
                  <p className="rounded-lg bg-brand-bg/70 p-2.5 text-xs leading-relaxed text-brand-fg/90">
                    “{lead.inquiry_text}”
                  </p>
                  <p className="text-[11px] text-brand-muted">
                    <strong className="text-brand-fg">AI 甄别依据：</strong>{lead.filter_reason}
                  </p>
                </div>

                <div className="flex w-full shrink-0 flex-col items-end justify-between gap-3 sm:w-64">
                  <div className="w-full rounded-xl border border-brand-border bg-brand-bg/60 p-3 text-right">
                    <span className="block text-[10px] text-brand-muted">AI 自动匹配冠英型号</span>
                    <span className="mt-0.5 block font-mono text-xs font-bold text-brand-accent">{lead.matched_code}</span>
                    <span className="mt-0.5 block truncate text-[11px] text-brand-fg">{lead.matched_product}</span>
                  </div>
                  {lead.is_terminal && (
                    <button
                      type="button"
                      onClick={() => {
                        setSubTab('catalog');
                        setQuery(lead.matched_code.split('/')[0].trim());
                        void generateQuote({ queryText: lead.inquiry_text, leadID: lead.id, save: true });
                      }}
                      className="inline-flex w-full items-center justify-center gap-1.5 rounded-lg bg-brand-accent px-3 py-2 text-xs font-semibold text-brand-on-accent hover:brightness-110"
                    >
                      一键匹配并出报价单 <ArrowRight className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              </article>
            ))}
          </div>
        </div>
      ) : (
        /* Sophia Real Factory & Canton Fair Short Videos <=60s */
        <div className="space-y-4">
          <div className="ui-card flex flex-wrap items-center justify-between gap-3 p-4">
            <div className="space-y-1">
              <div className="flex items-center gap-2 text-xs font-bold text-brand-fg">
                <Film className="h-4 w-4 text-brand-accent" />
                <span>冠英老板娘 Sophia 实拍短视频素材库（TikTok @kooying.brake.fact + 抖音，全部 ≤ 60 秒）</span>
              </div>
              <p className="text-[11px] text-brand-muted">
                已真实拉取老板娘 Sophia 海外 TikTok 与抖音源头工厂/广交会发车实拍片，可直接用于外贸 WhatsApp 跟单、独立站信任背书与 AI 脚本二创。
              </p>
            </div>
            <button
              type="button"
              onClick={() => setActiveTab('social-media')}
              className="inline-flex items-center gap-1.5 rounded-lg bg-brand-accent px-3.5 py-2 text-xs font-semibold text-brand-on-accent hover:brightness-110"
            >
              进入短视频与内容运营 <ArrowRight className="h-3.5 w-3.5" />
            </button>
          </div>

          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {videos.map((v) => (
              <article key={v.id} className="ui-card flex flex-col justify-between space-y-3 p-4">
                <div className="space-y-2.5">
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-1.5">
                      <span className="rounded bg-brand-accent/12 px-2 py-0.5 text-[10px] font-bold text-brand-accent">
                        {v.platform} · {v.account}
                      </span>
                      <span className="inline-flex items-center gap-1 rounded bg-emerald-500/12 px-2 py-0.5 text-[10px] font-semibold text-emerald-600">
                        <Clock className="h-3 w-3" /> {v.duration_sec}s
                      </span>
                    </div>
                    <a
                      href={v.source_url}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 text-[11px] font-medium text-brand-muted hover:text-brand-accent"
                    >
                      原链接 <ExternalLink className="h-3 w-3" />
                    </a>
                  </div>

                  <video
                    controls
                    playsInline
                    preload="metadata"
                    src={v.url}
                    className="h-56 w-full rounded-xl border border-brand-border bg-black object-contain"
                  />

                  <h3 className="text-xs font-bold text-brand-fg">{v.title}</h3>
                  <div className="flex flex-wrap gap-1">
                    {v.tags.map((t) => (
                      <span key={t} className="rounded bg-brand-fg/[0.06] px-1.5 py-0.5 text-[10px] text-brand-muted">
                        #{t}
                      </span>
                    ))}
                  </div>
                  <p className="rounded-lg bg-brand-bg/70 p-2.5 text-[11px] leading-relaxed text-brand-fg/85">
                    {v.suggested_caption}
                  </p>
                </div>

                <div className="flex items-center gap-2 pt-1">
                  <button
                    type="button"
                    onClick={() => {
                      void navigator.clipboard?.writeText(v.suggested_caption);
                      toast.show('外贸英文贴文已复制，可直接发 TikTok / WhatsApp', 'success');
                    }}
                    className="inline-flex flex-1 items-center justify-center gap-1.5 rounded-lg border border-brand-border px-2.5 py-2 text-[11px] font-semibold text-brand-fg hover:bg-brand-fg/[0.04]"
                  >
                    <Copy className="h-3.5 w-3.5" /> 复制外贸配文
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setAgentDraft({
                        mode: 'chat',
                        message: `请基于老板娘 Sophia 这条实拍素材（${v.title}，时长 ${v.duration_sec}s，原链接：${v.source_url}），为中东/非洲重卡车队与维修门店二创 1 套 45 秒英西双语短视频口播分镜，并给出结尾引导发 WVA/尺寸到 WhatsApp +86 159-5711-8669 的 CTA 钩子。\n参考配文：${v.suggested_caption}`,
                      });
                      setActiveTab('bot');
                    }}
                    className="inline-flex flex-1 items-center justify-center gap-1.5 rounded-lg bg-brand-accent/15 px-2.5 py-2 text-[11px] font-semibold text-brand-accent hover:bg-brand-accent/25"
                  >
                    <MessageSquarePlus className="h-3.5 w-3.5" /> 交给 Bot 二创脚本
                  </button>
                </div>
              </article>
            ))}
          </div>
        </div>
      )}

      {/* Official PDF Preview Lightbox */}
      {previewImg && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 p-6 backdrop-blur-xs"
          onClick={() => setPreviewImg(null)}
        >
          <div className="max-h-[90vh] max-w-5xl overflow-auto rounded-2xl bg-white p-2 shadow-2xl" onClick={(e) => e.stopPropagation()}>
            <div className="mb-2 flex items-center justify-between px-2 pt-1">
              <span className="text-xs font-bold text-gray-800">浙江冠英官方 PDF 产品册原图</span>
              <button
                type="button"
                onClick={() => setPreviewImg(null)}
                className="rounded-md bg-gray-100 px-3 py-1 text-xs font-semibold text-gray-700 hover:bg-gray-200"
              >
                关闭
              </button>
            </div>
            <img src={previewImg} alt="官方产品册" className="w-full rounded-lg object-contain" />
          </div>
        </div>
      )}
    </div>
  );
};

export interface KooyingSEOIssueItem {
  code: string;
  severity: 'critical' | 'warning' | 'pass';
  category: string;
  title: string;
  current_value: string;
  recommended_value: string;
  impact: string;
}

export interface KooyingPageAudit {
  id: string;
  url: string;
  path: string;
  page_type: string;
  http_status: number;
  size_bytes: number;
  title: string;
  title_len: number;
  meta_description: string;
  meta_desc_len: number;
  meta_keywords: string;
  h1_count: number;
  h1_texts: string[];
  h2_count: number;
  h2_sample: string[];
  image_total: number;
  image_missing_alt: number;
  has_canonical: boolean;
  canonical_url: string;
  has_jsonld: boolean;
  raw_jsonld: string;
  score: number;
  issues: KooyingSEOIssueItem[];
  optimized_title: string;
  optimized_meta_desc: string;
  optimized_h1: string;
  optimized_slug: string;
  optimized_jsonld: string;
  checked_at: string;
  is_live_fetched: boolean;
}

export interface KooyingSitemapTypo {
  url: string;
  wrong_token: string;
  correct_token: string;
  model_code: string;
  suggested_url: string;
  reason: string;
}

export interface KooyingSiteAuditOverview {
  target_domain: string;
  overall_score: number;
  projected_score: number;
  sitemap_url_count: number;
  critical_count: number;
  warning_count: number;
  passed_count: number;
  live_probe_error?: string;
  pages: KooyingPageAudit[];
  sitemap_typos: KooyingSitemapTypo[];
  robots_txt_current: string;
  robots_txt_optimized: string;
  llms_txt_current_sample: string;
  llms_txt_optimized: string;
  checked_at: string;
}

export interface SiteMaintenanceLog {
  id: string;
  category: string;
  title: string;
  target_path: string;
  page_url: string;
  summary: string;
  metrics_json: string;
  operator_bot: string;
  status: string;
  created_at: string;
}

export interface SitePageHealth {
  id: string;
  kind: string;
  label: string;
  rel_path: string;
  preview_url: string;
  title: string;
  h1_count: number;
  has_canonical: boolean;
  has_jsonld: boolean;
  size_bytes: number;
  updated_at: string;
  status: string;
}

export interface SiteMaintenanceOverview {
  site_dir: string;
  mount_url: string;
  pages: SitePageHealth[];
  logs: SiteMaintenanceLog[];
  asset_stats: Record<string, number | string>;
  site_lead_count: number;
  checked_at: string;
}

type SEOWorkbenchTab = 'overview' | 'audit' | 'competitors' | 'plan';

interface SEOGateCheck {
  gate: '可抓取' | '可索引' | '可渲染与要素' | '结构化与性能';
  item: string;
  status: 'pass' | 'warn' | 'fail' | 'info';
  detail: string;
  action?: string;
}

const KOOYING_SEO_GATE_CHECKS: SEOGateCheck[] = [
  { gate: '可抓取', item: 'HTTP 200 & HTTPS 协议不降级', status: 'pass', detail: 'HTTP->HTTPS 301 跳转正常，未知 URL 返回真 404，HTML 仅 58KB (<2MB 上限)' },
  { gate: '可抓取', item: 'robots.txt 存在且未误封 CSS/JS', status: 'pass', detail: '131 字节，已放行 Googlebot 与 GPTBot，屏蔽 AhrefsBot/SemrushBot' },
  { gate: '可抓取', item: 'robots.txt 声明 Sitemap 指令', status: 'warn', detail: 'robots.txt 中未写入 Sitemap: https://www.kooying.com/sitemap.xml，且缺少 OAI-SearchBot / PerplexityBot / ClaudeBot' },
  { gate: '可抓取', item: 'sitemap.xml 468 条 URL 收录基建', status: 'pass', detail: '总计 468 URL（产品与分类 278 + /news/ 博客 190），无测试/占位 slug' },
  { gate: '可索引', item: 'Meta Robots / X-Robots-Tag 与 Canonical', status: 'pass', detail: '核心页无误标 noindex，canonical 自指一致且协议统一 HTTPS' },
  { gate: '可索引', item: '多语言 hreflang 与区域子目录', status: 'info', detail: '当前全站纯英文单语言，缺失 /es/ (南美)、/ru/ (俄语中亚)、/ar/ (中东) 蓝海子目录' },
  { gate: '可渲染与要素', item: '首页与公司简介 <h1> 唯一主标题', status: 'fail', detail: '首页 H1=0（却存在 4 个无语义 <h2>kooying</h2>）；关于我们页 H1=0，严重丧失权重焦点' },
  { gate: '可渲染与要素', item: '首页 <title> 与 <meta description>', status: 'pass', detail: '首页 Title 56 字符、Description 159 字符；但产品详情页普遍 73-78 字符超时截断且套用静态模板' },
  { gate: '可渲染与要素', item: 'URL Slug 与 Title 英文拼写规范', status: 'fail', detail: '发现 16 处高危拼写硬伤：trcuk(5)、beake(3)、fiction(2)、btake-lining(1)、mercedsbenz、trailor、ivceo(2)、-for.html 截断' },
  { gate: '可渲染与要素', item: '图片 width/height 防 CLS 尺寸属性', status: 'warn', detail: '首页 36 张图片全部缺失显式宽高属性，存在首屏布局偏移风险' },
  { gate: '结构化与性能', item: '首页与产品页 Schema.org JSON-LD', status: 'warn', detail: '首页缺 @context 且写为 "query-Input" / "Jenny Ji: 2217726858@qq.com"；产品页写为大写 "Offers" 且价格为 0.0 失效' },
  { gate: '结构化与性能', item: 'GEO / llms.txt 实体引用规范', status: 'warn', detail: '/llms.txt 最外层被 CMS 包裹 HTML <pre> 标签，且未突出 400C 磨损 0.040mm 与 WVA 互换表' },
];

const KOOYING_COMPETITORS = [
  {
    name: 'Alltour (alltourbrake.com)',
    origin: '山东泰安 · 最直接对标',
    positioning: '专注商用车/重卡刹车片与衬片（WVA 全覆盖）',
    tactic: '全网高权重品牌词 + 展会/认证背书：重点抢占 commercial vehicle brake pad manufacturer、ECE R90 brake pads；大量铺设欧洲重卡标准与 CNAS 台架数据，E-E-A-T 权威度极高。',
    gap: '冠英拥有同样的 43,000㎡ 工厂、2 台 Krauss 试验机 (400C 磨损 0.040mm) 与 1500万套产能，但官网 Title/H1/Schema 未突出认证与台架量化证据。',
    counterMove: '全站产品页与首页置顶 IATF16949 + ECE R90 + 400℃ 0.040mm 实测对比框，并把 /brand-site/ 瑞士工业级台架视觉迁回主站。',
  },
  {
    name: 'FrenoBrake (frenobrake.com)',
    origin: '华东/出口OEM定制标杆',
    positioning: '主打无铜陶瓷配方与 OEM 定制刹车片',
    tactic: '“排行测评长文”截流大词：专门发布《Top 10 China Brake Pads Manufacturers in 2025/2026》采购商必看榜单长文，将自己排第 1 位，拦截正在横向比价的中国供应链搜索流量。',
    gap: '冠英官网 190 篇博客偏重泛科普与展会通知，完全缺失 Top / Best / Alternatives / VS 类决策榜单长文与第三方外链背书。',
    counterMove: '立即发布《Top 10 Heavy Duty Truck Brake Pad Manufacturers in China (2026 Guide)》与《WVA 29087 vs 29108》截流长文。',
  },
  {
    name: 'TopBrakeParts (topbrakeparts.com)',
    origin: '广东清远 · 中小批发商收割',
    positioning: '全品类混批（Mixed-SKU）制动件供应商',
    tactic: 'B2B 采购决策指南（Buyer\'s Guide）：按工厂规模、起订量 MOQ、混批规则、认证齐全度输出高清对比表格，并搭配电子目录下载留资钩子，转化率极高。',
    gap: '冠英仅在页尾保留普通 Contact Us 表单，产品页缺少面向小 B 批发商与修配厂的 MOQ、混批说明与 PDF 目录下载诱饵。',
    counterMove: '在每个车型大类页顶部植入「Download 2026 PDF Catalog & Cross-Reference List」按钮（邮箱+WhatsApp 解锁下载），预计提升 3–5 倍线索率。',
  },
  {
    name: 'GDST (gdstauto.com)',
    origin: '安徽芜湖 · 多语种集群站',
    positioning: '一站式制动系统（刹车片+刹车盘+分泵）',
    tactic: '海量长尾词矩阵 + 多语种降维打击：在英文主站之外同步建设西语(/es/)、俄语(/ru/)、阿语(/ar/)子目录，精准收割拉美、中亚俄语区与中东重卡汽配低竞争自然询盘。',
    gap: '冠英 468 页 Sitemap 全部集中在英文路径，却实打实供货东南亚日系、南美矿山、中亚 HOWO 汉德桥与中东高温挂车。',
    counterMove: '优先开设 /es/ (南美 Scania/Mercedes/Volvo)、/ru/ (HOWO/Shacman/Kamaz)、/ar/ (中东 BPW/Actros) 黄金子目录。',
  },
];

const GEFEI_KEYWORD_CLUSTERS = [
  {
    cluster: '一、型号 × 卡钳 × OE 黄金公式词（精准询盘转化最高）',
    intent: 'Transactional · 终端维修厂/汽配批发商直接找替换件',
    formula: '[WVA号] + [对应卡钳/车桥如 Knorr SB7000 / BPW / Meritor] + [对应OE号如 0044206020] + Truck Brake Pad',
    keywords: [
      'WVA 29087 Knorr SB7000 truck brake pad OE 0044206020',
      'WVA 29108 29162 heavy duty air disc brake pad dimensions',
      'WVA 19032 BC 36/1 BPW trailer brake lining 8 holes',
      'FMSI 4515 4707 American truck brake lining manufacturer China',
    ],
    targetPage: '/product/brake-pad-for-truck/ (需配 HTML 参数表)',
  },
  {
    cluster: '二、榜单 / 对比截流决策大词（复制 FrenoBrake 打法）',
    intent: 'Commercial Investigation · 海外采购寻找对比中国源头工厂',
    formula: 'Top 10 / Best + [品类名] + Manufacturers in China (2026) | [型号 A] vs [型号 B]',
    keywords: [
      'Top 10 heavy duty truck brake pad manufacturers China 2026',
      'commercial vehicle brake lining supplier IATF16949 China',
      'WVA 29087 vs WVA 29108 dimensions fitment cross reference',
      'how to verify Chinese truck brake lining factory MOQ ECE R90',
    ],
    targetPage: '/news/industry-news/ (新增榜单与型号对比专题)',
  },
  {
    cluster: '三、特种工业与高毛利细分蓝海词（kooying 现有强项放大）',
    intent: 'Niche B2B · 低竞争度 + 高客单价（风机运维 & 矿山车队）',
    formula: '[设备/工况] + [工业部件号] + High-Friction Brake Pad Selection',
    keywords: [
      '9710081817 high friction brake pad for wind turbine',
      'wind turbine yaw rotor brake pad manufacturer copper free',
      'Hande 35T mining dump truck 20-hole brake lining 240x220x18.5',
      'low noise heavy bus asbestos free brake lining 400C wear 0.040mm',
    ],
    targetPage: '/product/brake-pad-for-wind-turbine/ + /news/ 深度选型指南',
  },
  {
    cluster: '四、小语种降维打击词矩阵（复制 GDST 错位竞争）',
    intent: 'Multi-language Local Search · 避开英文红海，直连南美/俄语区/中东买家',
    formula: '/es/ pastillas de freno camión · /ru/ тормозные накладки HOWO · /ar/ تيل فرامل شاحنات BPW',
    keywords: [
      '/es/ pastillas de freno WVA 29087 camión pesado fábrica China',
      '/es/ balatas freno BPW 19032 Mercedes Actros catálogo PDF',
      '/ru/ тормозные накладки HOWO Shacman мост Hande 35T оптом',
      '/ar/ بطانات فرامل شاحنات ثقيلة BPW Meritor شهادة ECE R90',
    ],
    targetPage: '规划新增 /es/、/ru/、/ar/ 三大区域子目录落地页',
  },
];

const SITEMAP_MIX_ITEMS = [
  { label: '重卡/挂车鼓刹衬片 (各车系)', cnt: 162, color: '#C84B21' },
  { label: 'WVA 商用车盘刹片', cnt: 99, color: '#2563EB' },
  { label: '风电/特种刹车片', cnt: 9, color: '#10B981' },
  { label: '高频博客与资讯 (/news/)', cnt: 190, color: '#F59E0B' },
  { label: '企业介绍/认证/页面', cnt: 8, color: '#71717A' },
];

const INITIAL_OVERTAKE_ACTIONS = [
  {
    id: 'act_typo_fix',
    step: '第1步 · 拼写清零与 301 重定向',
    eta: '30 分钟见效',
    priority: 'P0',
    title: '批量清洗全站 16 处致命拼写错误 (trcuk / beake / fiction / btake / mercedsbenz / trailor / ivceo)',
    detail: '修正 URL Slug、Title、H1 并对旧错链配置永久 301 重定向，同步在首页 & 公司简介补齐唯一 <h1>，消除 4 个无语义 <h2>kooying</h2>。',
    defaultProgress: 20,
  },
  {
    id: 'act_schema_geo',
    step: '第1步 · Schema 与 GEO 基建',
    eta: '半天完成',
    priority: 'P0',
    title: '修复首页/产品页 JSON-LD 报错 + 剥离 llms.txt 的 HTML <pre> 标签',
    detail: '将产品页大写 "Offers" 改为小写 "offers"、零价格 0.0 改为 FOB $13.80-$19.50，修正首页 "query-Input" 与邮箱字段，robots.txt 追加 Sitemap: 声明。',
    defaultProgress: 15,
  },
  {
    id: 'act_wva_table',
    step: '第1步 · 双引擎型号参数表',
    eta: '1–2 天见效',
    priority: 'P0',
    title: '在 WVA 单品页嵌入「WVA + 卡钳(Knorr/BPW) + OE 0044206020 + 400℃台架」HTML 对比表',
    detail: '将单一文描升级为结构化 HTML Table + [WVA号]+[卡钳/车桥]+[OE号] 命名格式，直接冲击 Google 精选摘要（Featured Snippets）。',
    defaultProgress: 10,
  },
  {
    id: 'act_lead_magnet',
    step: '第3步 · 转化漏斗留资诱饵',
    eta: '1–2 天见效',
    priority: 'P1',
    title: '在车型/WVA 顶部植入「Download 2026 Truck Brake Pad PDF Catalog & OE List」钩子',
    detail: '取代单一页尾 Contact Us 表单，采购商输入公司+邮箱/WhatsApp 即可解锁完整 OE 交叉目录下载并直连 :17422 线索雷达，线索留资率预计提升 3–5 倍。',
    defaultProgress: 35,
  },
  {
    id: 'act_listicle',
    step: '第2步 · 榜单与型号对比截流',
    eta: '3–5 天部署',
    priority: 'P1',
    title: '发布《Top 10 China Heavy Truck Brake Pad Manufacturers (2026)》与《WVA 29087 vs 29108》',
    detail: '直接对标 FrenoBrake / TopBrakeParts 截流决策大词，在榜单首推冠英 43,000㎡ 工厂、2 台 Krauss 台架与 15M 套真实年产能。',
    defaultProgress: 5,
  },
  {
    id: 'act_i18n',
    step: '第4步 · 西/俄/阿小语种蓝海',
    eta: '3–5 天部署',
    priority: 'P2',
    title: '上线 /es/（南美）、/ru/（俄语中亚 HOWO/Shacman/Kamaz）、/ar/（中东 Actros/BPW）子目录',
    detail: '建立双向 hreflang + x-default 标签与本地化高频车型卡车配件长尾页，避开英文红海获取高毛利自然询盘。',
    defaultProgress: 0,
  },
];

const FEATURED_SNIPPET_HTML_SNIPPET = `<!-- KOOYING WVA 29087/29108 结构化 HTML 参数表 (抢占 Google Featured Snippets) -->
<section aria-label="Technical Specification & OE Cross Reference">
  <h2>WVA 29087 / 29108 Technical Dimensions & Caliper Fitment</h2>
  <table>
    <thead>
      <tr>
        <th>WVA Code</th>
        <th>Caliper / Axle</th>
        <th>Dimensions (L × W × T)</th>
        <th>OEM Cross Reference</th>
        <th>Krauss 400℃ Wear</th>
      </tr>
    </thead>
    <tbody>
      <tr>
        <td>WVA 29087 / 29108 / 29179</td>
        <td>Knorr SB7000 / SN7 / BPW SH</td>
        <td>247.6 × 109.7 × 30.0 mm</td>
        <td>Mercedes 0044206020 / MAN 81508205096</td>
        <td>0.040 mm (μ 0.35–0.38, ECE R90)</td>
      </tr>
    </tbody>
  </table>
</section>`;

export const GuanyingSeoBuilderView: React.FC = () => {
  const toast = useUIStore((s) => s.toast);
  const setActiveTab = useUIStore((s) => s.setActiveTab);
  const setActiveBotID = useUIStore((s) => s.setActiveBotID);
  const setAgentDraft = useUIStore((s) => s.setAgentDraft);

  const { data: auditData, mutate: mutateAudit, isLoading: auditLoading } = useSWR(
    'api/guanying/seo-audit',
    () => fetchJSON<KooyingSiteAuditOverview>('/api/guanying/seo-audit'),
  );
  const { mutate: mutateOverview } = useSWR('api/guanying/overview', () =>
    fetchJSON<GuanyingOverview>('/api/guanying/overview'),
  );

  const [subTab, setSubTab] = useState<SEOWorkbenchTab>('overview');
  const [selectedID, setSelectedID] = useState<string>('');
  const [probeURL, setProbeURL] = useState<string>('https://www.kooying.com/');
  const [probing, setProbing] = useState(false);
  const [selectedMixIdx, setSelectedMixIdx] = useState<number | null>(null);
  const [gateFilter, setGateFilter] = useState<'all' | 'issue' | 'pass'>('all');
  const [expandedTypoURL, setExpandedTypoURL] = useState<string | null>(null);
  const [codeTab, setCodeTab] = useState<'jsonld' | 'geo'>('jsonld');

  const [actionProgress, setActionProgress] = useState<Record<string, number>>(() => {
    const initial: Record<string, number> = {};
    for (const a of INITIAL_OVERTAKE_ACTIONS) initial[a.id] = a.defaultProgress;
    if (typeof window === 'undefined') return initial;
    try {
      const saved = window.localStorage.getItem('kooying-seo-action-progress-v1');
      if (saved) return { ...initial, ...(JSON.parse(saved) as Record<string, number>) };
    } catch {
      // ignore storage errors
    }
    return initial;
  });

  const [lmCompany, setLmCompany] = useState('Al-Jazeera Heavy Fleet Maintenance LLC');
  const [lmContact, setLmContact] = useState('procurement@aljazeerafleet.sa / +966 55 820 4419');
  const [lmCountry, setLmCountry] = useState('沙特阿拉伯 (Dammam)');
  const [lmInquiry, setLmInquiry] = useState(
    'Download 2026 PDF Catalog: Need Knorr SB7000 WVA 29087 (OE 0044206020) and BPW 19032 BC/36/1 8-hole brake linings, 800 sets.',
  );
  const [submittingLead, setSubmittingLead] = useState(false);
  const [lastSubmittedLead, setLastSubmittedLead] = useState<GuanyingLead | null>(null);

  const pages = auditData?.pages ?? [];
  const currentPage = pages.find((p) => p.id === selectedID) ?? pages[0] ?? null;
  const sitemapTypos = auditData?.sitemap_typos ?? [];
  const mixTotal = SITEMAP_MIX_ITEMS.reduce((acc, it) => acc + it.cnt, 0);

  function updateProgress(id: string, val: number) {
    setActionProgress((prev) => {
      const next = { ...prev, [id]: val };
      try {
        window.localStorage.setItem('kooying-seo-action-progress-v1', JSON.stringify(next));
      } catch {
        // ignore
      }
      return next;
    });
  }

  async function runLiveProbe(e?: React.FormEvent, overrideURL?: string) {
    if (e) e.preventDefault();
    const target = (overrideURL ?? probeURL).trim() || 'https://www.kooying.com/';
    setProbeURL(target);
    setProbing(true);
    try {
      const fresh = await fetchJSON<KooyingSiteAuditOverview>('/api/guanying/seo-audit', {
        method: 'POST',
        body: JSON.stringify({ url: target, live: true }),
      });
      await mutateAudit(fresh, false);
      if (fresh.live_probe_error) {
        toast.show(`抓取提示：${fresh.live_probe_error}`, 'info');
      } else {
        if (fresh.pages.length > 0) {
          const hit = fresh.pages.find((p) => p.url.replace(/\/+$/, '') === target.replace(/\/+$/, ''));
          setSelectedID(hit ? hit.id : fresh.pages[0].id);
        }
        toast.show(`已完成对 ${target} 的实时抓取诊断`, 'success');
      }
    } catch (err) {
      toast.show(err instanceof Error ? err.message : '抓取失败', 'error');
    } finally {
      setProbing(false);
    }
  }

  function citeSEOContext(
    id: string,
    title: string,
    summary: string,
    status: string,
    fields: Array<{ label: string; value: string }>,
  ) {
    openAgentContext({
      id,
      kind: 'evidence',
      title,
      summary,
      status,
      occurredAt: auditData?.checked_at || new Date().toISOString(),
      fields,
    });
  }

  function dispatchBotWithSEOPrompt(botID: string, prompt: string) {
    setActiveBotID(botID);
    setAgentDraft({ mode: 'chat', message: prompt });
    setActiveTab('bot');
  }

  async function handleSubmitLeadMagnet(e: React.FormEvent) {
    e.preventDefault();
    setSubmittingLead(true);
    try {
      const res = await fetchJSON<{ lead: GuanyingLead }>('/api/guanying/leads', {
        method: 'POST',
        body: JSON.stringify({
          company: `${lmCompany} (${lmContact})`,
          country: lmCountry,
          channel: '官网 SEO PDF目录留资 (kooying.com)',
          inquiry_text: lmInquiry,
          seo_page_id: 'seo_wva29087_disc',
        }),
      });
      setLastSubmittedLead(res.lead);
      await mutateOverview();
      toast.show(`目录留资已入库：${res.lead.buyer_type}（匹配 ${res.lead.matched_code}）`, 'success');
    } catch (err) {
      toast.show(err instanceof Error ? err.message : '提交失败', 'error');
    } finally {
      setSubmittingLead(false);
    }
  }

  const filteredGates = KOOYING_SEO_GATE_CHECKS.filter((g) => {
    if (gateFilter === 'issue') return g.status === 'fail' || g.status === 'warn';
    if (gateFilter === 'pass') return g.status === 'pass';
    return true;
  });

  return (
    <div className="mx-auto max-w-[1160px] space-y-6 pb-12">
      {/* Calm Header */}
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-brand-accent">
            SEO WORKBENCH · KOOYING.COM 真站诊断与优化
          </p>
          <h1 className="mt-1.5 text-[26px] font-bold tracking-tight text-brand-fg">
            官网 SEO 诊断与优化
          </h1>
          <p className="mt-1.5 text-sm text-brand-muted">
            围绕真实官网{' '}
            <a href="https://www.kooying.com/" target="_blank" rel="noreferrer" className="font-semibold text-brand-accent hover:underline">
              kooying.com
            </a>{' '}
            468 个页面开展体检，对标 Alltour / FrenoBrake / TopBrakeParts / GDST 制定可执行超车动作。
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
          <a
            href="/brand-site/index.html"
            target="_blank"
            rel="noreferrer"
            className="inline-flex h-9 items-center gap-1.5 rounded-xl border border-brand-border bg-brand-surface px-4 text-xs font-semibold text-brand-fg hover:border-brand-accent"
          >
            打开极简样板站 <ExternalLink className="h-3.5 w-3.5" />
          </a>
          <button
            type="button"
            onClick={() =>
              dispatchBotWithSEOPrompt(
                'bot_gy_seo_builder',
                '请基于 https://www.kooying.com/ 的真实 SEO 体检结论（16 处拼写错误 trcuk/beake/fiction/btake、首页与公司简介 H1=0、Product JSON-LD 大写 Offers + 0 元、llms.txt 外层 <pre> 标签），输出今天可直接交给技术修改的补丁清单。',
              )
            }
            className="inline-flex h-9 items-center gap-1.5 rounded-xl bg-brand-accent px-4 text-xs font-semibold text-brand-on-accent hover:brightness-110"
          >
            <Bot className="h-3.5 w-3.5" /> 让 SEO 助手出修复清单
          </button>
        </div>
      </div>

      {/* Single-line Status Bar (:8003 style) + Live Probe */}
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-brand-border bg-brand-surface px-4 py-3 text-xs">
        <div className="flex flex-wrap items-center gap-2.5">
          <span className="h-2 w-2 rounded-full bg-emerald-500 shadow-[0_0_8px_#10b981]" />
          <span className="text-brand-muted">
            <strong className="text-brand-fg">kooying.com</strong> · Sitemap{' '}
            <strong className="text-brand-fg">{auditData?.sitemap_url_count ?? 468}</strong> 页 ·{' '}
            <span className="text-emerald-600">29 通过</span> / <span className="text-amber-600">3 警告</span> /{' '}
            <span className="text-rose-600">1 失败</span>
          </span>
        </div>

        <form onSubmit={(e) => void runLiveProbe(e)} className="flex flex-1 items-center gap-2 sm:max-w-md">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-brand-muted" />
            <input
              value={probeURL}
              onChange={(e) => setProbeURL(e.target.value)}
              placeholder="输入任意 kooying.com 网址实时抓取诊断…"
              className="h-8 w-full rounded-xl border border-brand-border bg-brand-bg pl-8 pr-3 text-xs text-brand-fg"
            />
          </div>
          <button
            type="submit"
            disabled={probing}
            className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-xl bg-brand-fg px-3.5 text-xs font-semibold text-brand-bg hover:opacity-90 disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${probing ? 'animate-spin' : ''}`} />
            {probing ? '抓检中…' : '抓取分析'}
          </button>
        </form>
      </div>

      {/* 4 Spacious KPI Cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[
          {
            label: 'Sitemap 收录页面',
            num: String(auditData?.sitemap_url_count ?? 468),
            unit: '页',
            barColor: 'bg-emerald-500',
            dotColor: 'bg-emerald-500',
            note: '产品分类 278 · 博客长文 190（含风电 9）',
          },
          {
            label: '英文拼写错词页面',
            num: '16',
            unit: '处',
            barColor: 'bg-rose-500',
            dotColor: 'bg-rose-500',
            note: 'trcuk / beake / fiction / btake 等高危词',
          },
          {
            label: '/seo 首页技术体检',
            num: '29/33',
            unit: '通过',
            barColor: 'bg-amber-500',
            dotColor: 'bg-amber-500',
            note: '首页与公司页 H1 缺失 · Offers 大写失效',
          },
          {
            label: '竞品超车突破口',
            num: '4',
            unit: '大方向',
            barColor: 'bg-blue-500',
            dotColor: 'bg-blue-500',
            note: '榜单截流 · OE 表 · PDF 留资 · 小语种',
          },
        ].map((kpi) => (
          <div key={kpi.label} className="ui-card relative flex min-h-[142px] flex-col overflow-hidden rounded-2xl p-6">
            <div className={`absolute inset-x-0 top-0 h-[3px] ${kpi.barColor}`} />
            <div className="flex items-center gap-2 text-xs text-brand-muted">
              <span className={`h-2 w-2 rounded-full ${kpi.dotColor}`} />
              <span>{kpi.label}</span>
            </div>
            <div className="mt-3 flex items-baseline gap-1.5">
              <span className="text-4xl font-bold tracking-tight text-brand-fg">{kpi.num}</span>
              <span className="text-sm text-brand-muted">{kpi.unit}</span>
            </div>
            <p className="mt-auto pt-3 text-xs text-brand-muted">{kpi.note}</p>
          </div>
        ))}
      </div>

      {/* 4 Clean Navigation Pills */}
      <div className="flex flex-wrap items-center gap-1.5 rounded-2xl border border-brand-border bg-brand-surface p-1.5">
        {(
          [
            { id: 'overview', label: '◎ 概览 · 现状与页面优化' },
            { id: 'audit', label: '◍ 审计 · 拼写错词与三道门槛' },
            { id: 'competitors', label: '⚔ 竞品与关键词库' },
            { id: 'plan', label: '◈ 执行清单与 PDF 留资' },
          ] as const
        ).map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => setSubTab(tab.id)}
            className={`rounded-xl px-4 py-2 text-xs font-semibold transition ${
              subTab === tab.id
                ? 'bg-brand-accent text-brand-on-accent'
                : 'text-brand-muted hover:bg-brand-fg/[0.04] hover:text-brand-fg'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* TAB 1: OVERVIEW */}
      {subTab === 'overview' && (
        <div className="space-y-5">
          {/* Sitemap Composition Stack Bar */}
          <div className="ui-card space-y-4 rounded-2xl p-6">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <h2 className="text-base font-bold text-brand-fg">全站 468 条页面结构占比</h2>
                <p className="mt-0.5 text-xs text-brand-muted">点击图例可高亮对应栏目，快速看清流量分布</p>
              </div>
              <button
                type="button"
                onClick={() =>
                  citeSEOContext(
                    'ctx_kooying_overview_swot',
                    'kooying.com 现状诊断总览',
                    '优势：1) WVA 长尾型号矩阵；2) 190 篇博客与 9 款风电刹车片。\n短板：1) 缺 Top 10 榜单词与外链背书；2) 16 处英文拼写硬伤 + 模板重复 + H1=0；3) 缺卡钳/OE 参数表与顶部 PDF 目录留资诱饵。',
                    '已完成抓检',
                    [
                      { label: 'Sitemap 规模', value: '468 页（产品 278 / 博客 190）' },
                      { label: '高危拼写错页', value: '16 处' },
                    ],
                  )
                }
                className="text-xs font-semibold text-brand-accent hover:underline"
              >
                在抽屉看分析摘要 →
              </button>
            </div>

            <div className="flex h-3.5 w-full overflow-hidden rounded-full bg-brand-bg">
              {SITEMAP_MIX_ITEMS.map((item, idx) => (
                <div
                  key={item.label}
                  onClick={() => setSelectedMixIdx(selectedMixIdx === idx ? null : idx)}
                  title={`${item.label} · ${item.cnt} 页`}
                  style={{
                    flexGrow: item.cnt,
                    backgroundColor: item.color,
                    opacity: selectedMixIdx === null || selectedMixIdx === idx ? 1 : 0.25,
                  }}
                  className="cursor-pointer transition-opacity"
                />
              ))}
            </div>
            <div className="flex flex-wrap gap-2">
              {SITEMAP_MIX_ITEMS.map((item, idx) => (
                <button
                  key={item.label}
                  type="button"
                  onClick={() => setSelectedMixIdx(selectedMixIdx === idx ? null : idx)}
                  className={`inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs transition ${
                    selectedMixIdx === idx
                      ? 'border-brand-accent bg-brand-accent/10 font-semibold text-brand-fg'
                      : 'border-brand-border bg-brand-bg/40 text-brand-muted hover:text-brand-fg'
                  }`}
                >
                  <span className="h-2 w-2 rounded-full" style={{ backgroundColor: item.color }} />
                  <span>{item.label}</span>
                  <strong className="text-brand-fg">{Math.round((item.cnt / mixTotal) * 100)}%</strong>
                </button>
              ))}
            </div>
          </div>

          {/* 2-Column Overview Grid (:8003 style): Strengths/Weaknesses + Compact Page Chips */}
          <div className="grid gap-5 lg:grid-cols-2">
            <div className="ui-card space-y-4 rounded-2xl p-6">
              <h2 className="text-base font-bold text-brand-fg">kooying.com 现状诊断（2 优势 · 3 短板）</h2>
              <div className="space-y-3 text-[13px] leading-relaxed">
                <div className="rounded-xl bg-emerald-500/8 p-4">
                  <p className="font-bold text-emerald-700 dark:text-emerald-400">做得好的抓手</p>
                  <ul className="mt-1.5 space-y-1 text-brand-fg/90">
                    <li>• <strong>WVA 长尾型号矩阵：</strong>29087 / 29108 / 29171 / 19032 直写 Title 与 URL，精准收割修配厂询盘。</li>
                    <li>• <strong>特种工业长文：</strong>190 篇博客 + 9 款风力发电机刹车片指南，切入高毛利低竞争赛道。</li>
                  </ul>
                </div>
                <div className="rounded-xl bg-rose-500/8 p-4">
                  <p className="font-bold text-rose-700 dark:text-rose-400">暴露的 3 大短板（超越突破口）</p>
                  <ul className="mt-1.5 space-y-1 text-brand-fg/90">
                    <li>• <strong>缺头部榜单词：</strong><code>Top China brake pads manufacturers</code> 等决策词被同行占据。</li>
                    <li>• <strong>拼写硬伤与模板重复：</strong><code>Trcuk</code>、<code>Fiction</code>、<code>Beake</code>、<code>Btake</code> 等 16 处错词，首页/公司页 <code>H1=0</code>。</li>
                    <li>• <strong>缺结构化参数与留资钩子：</strong>缺 Knorr/BPW 卡钳与 OE 交叉表及顶部 PDF Catalog 下载按钮。</li>
                  </ul>
                </div>
              </div>
            </div>

            <div className="ui-card space-y-4 rounded-2xl p-6">
              <div>
                <h2 className="text-base font-bold text-brand-fg">重点页面诊断 · {pages.length} 个代表页</h2>
                <p className="mt-0.5 text-xs text-brand-muted">点选任意页面，在下方查看「改前 vs 改后」对比</p>
              </div>
              {auditLoading ? (
                <div className="py-8 text-center text-xs text-brand-muted">正在加载页面诊断…</div>
              ) : (
                <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
                  {pages.map((p) => {
                    const active = currentPage?.id === p.id;
                    const crit = p.issues.filter((i) => i.severity === 'critical').length;
                    const dotColor = crit > 0 ? 'bg-rose-500' : p.issues.some((i) => i.severity === 'warning') ? 'bg-amber-500' : 'bg-emerald-500';
                    return (
                      <button
                        key={p.id}
                        type="button"
                        onClick={() => {
                          setSelectedID(p.id);
                          setProbeURL(p.url);
                        }}
                        className={`flex items-center justify-between gap-2 rounded-xl border px-3.5 py-2.5 text-left text-xs transition ${
                          active
                            ? 'border-brand-accent bg-brand-accent/10 font-semibold text-brand-fg'
                            : 'border-brand-border bg-brand-bg/40 text-brand-fg/90 hover:border-brand-accent/50'
                        }`}
                      >
                        <span className="flex min-w-0 items-center gap-2">
                          <span className={`h-2 w-2 shrink-0 rounded-full ${dotColor}`} />
                          <span className="truncate">{p.page_type}</span>
                        </span>
                        <span className="shrink-0 text-[11px] text-brand-muted">
                          {crit > 0 ? `${crit} 处高危` : '待优化'}
                        </span>
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          </div>

          {/* Clean Before vs After Card for Selected Page */}
          {currentPage && (
            <div className="ui-card space-y-4 rounded-2xl p-6">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border pb-4">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-bold text-brand-fg">{currentPage.page_type}</span>
                    <a
                      href={currentPage.url}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 text-xs text-brand-accent hover:underline"
                    >
                      打开原网页 <ExternalLink className="h-3 w-3" />
                    </a>
                  </div>
                  <p className="mt-0.5 truncate text-xs text-brand-muted">{currentPage.url}</p>
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <button
                    type="button"
                    onClick={() =>
                      citeSEOContext(
                        `seo_page_${currentPage.id}`,
                        `页面优化对比 · ${currentPage.page_type}`,
                        `线上 URL：${currentPage.url}\n核心问题：\n${currentPage.issues.map((i) => `- ${i.title}（${i.recommended_value}）`).join('\n')}\n\n建议新 Title：${currentPage.optimized_title}\n建议新 H1：${currentPage.optimized_h1}`,
                        `${currentPage.issues.length} 条建议`,
                        [
                          { label: '原 Title 长度', value: `${currentPage.title_len} 字符` },
                          { label: 'H1 标签数', value: String(currentPage.h1_count) },
                          { label: '建议 URL', value: currentPage.optimized_slug },
                        ],
                      )
                    }
                    className="rounded-xl border border-brand-border px-3 py-1.5 text-xs font-semibold text-brand-fg hover:border-brand-accent"
                  >
                    在抽屉看完整诊断
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      const patch = `<title>${currentPage.optimized_title}</title>\n<meta name="description" content="${currentPage.optimized_meta_desc}" />\n<h1>${currentPage.optimized_h1}</h1>\n<!-- 301: ${currentPage.optimized_slug} -->`;
                      void navigator.clipboard?.writeText(patch);
                      toast.show('已复制优化后的 Title / Description / H1 代码', 'success');
                    }}
                    className="inline-flex items-center gap-1 rounded-xl bg-brand-fg px-3 py-1.5 text-xs font-semibold text-brand-bg hover:opacity-90"
                  >
                    <Copy className="h-3.5 w-3.5" /> 复制替换代码
                  </button>
                </div>
              </div>

              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-2.5 rounded-xl bg-rose-500/6 p-4 text-xs">
                  <span className="font-bold text-rose-600">Before · kooying.com 线上现状</span>
                  <p>
                    <span className="text-brand-muted">Title：</span>
                    <span className="text-brand-fg">{currentPage.title || '(空)'}</span>
                  </p>
                  <p>
                    <span className="text-brand-muted">H1：</span>
                    <span className={currentPage.h1_count === 0 ? 'font-semibold text-rose-600' : 'text-brand-fg'}>
                      {currentPage.h1_texts.join(' | ') || '缺失 H1 主标题'}
                    </span>
                  </p>
                  <p className="text-brand-muted">
                    主要问题：{currentPage.issues.map((i) => i.title).join('；')}
                  </p>
                </div>

                <div className="space-y-2.5 rounded-xl bg-emerald-500/8 p-4 text-xs">
                  <span className="font-bold text-emerald-600">After · 建议改成</span>
                  <p>
                    <span className="text-brand-muted">新 Title：</span>
                    <strong className="text-brand-fg">{currentPage.optimized_title}</strong>
                  </p>
                  <p>
                    <span className="text-brand-muted">新 H1：</span>
                    <strong className="text-brand-fg">{currentPage.optimized_h1}</strong>
                  </p>
                  <p className="truncate text-brand-muted">
                    新 URL：<strong className="text-emerald-700 dark:text-emerald-300">{currentPage.optimized_slug}</strong>
                  </p>
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {/* TAB 2: AUDIT (Clean 1-line rows inspired by :8003 AuditView) */}
      {subTab === 'audit' && (
        <div className="space-y-5">
          {/* Card 1: Typo Rows */}
          <div className="ui-card overflow-hidden rounded-2xl">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border px-6 py-4">
              <div>
                <h2 className="flex items-center gap-2 text-base font-bold text-brand-fg">
                  <AlertTriangle className="h-4 w-4 text-rose-500" />
                  全站 16 处英文拼写错词与截断 URL 清单
                </h2>
                <p className="mt-0.5 text-xs text-brand-muted">点击任意一行展开新旧网址对照与 301 重定向目标</p>
              </div>
              <button
                type="button"
                onClick={() => {
                  const rules = sitemapTypos
                    .map((t) => `Redirect 301 ${new URL(t.url).pathname} ${new URL(t.suggested_url).pathname}`)
                    .join('\n');
                  void navigator.clipboard?.writeText(rules);
                  toast.show('已复制全部 301 重定向规则', 'success');
                }}
                className="inline-flex items-center gap-1.5 rounded-xl bg-brand-accent px-3.5 py-2 text-xs font-semibold text-brand-on-accent"
              >
                <Copy className="h-3.5 w-3.5" /> 复制全部 301 规则
              </button>
            </div>
            <div className="divide-y divide-brand-border">
              {sitemapTypos.map((t) => {
                const open = expandedTypoURL === t.url;
                return (
                  <div key={t.url} className="px-6 py-3.5 text-xs">
                    <button
                      type="button"
                      onClick={() => setExpandedTypoURL(open ? null : t.url)}
                      className="flex w-full items-center justify-between gap-3 text-left"
                    >
                      <span className="flex min-w-0 items-center gap-3">
                        <span className="h-2 w-2 shrink-0 rounded-full bg-rose-500" />
                        <span className="rounded-md bg-rose-500/12 px-2 py-0.5 font-mono text-[11px] font-bold text-rose-600">
                          {t.wrong_token} → {t.correct_token}
                        </span>
                        <span className="truncate font-semibold text-brand-fg">{t.model_code}</span>
                      </span>
                      <span className="shrink-0 text-[11px] text-brand-muted">{open ? '收起 ▾' : '详情 ▸'}</span>
                    </button>
                    {open && (
                      <div className="mt-3 space-y-2 rounded-xl bg-brand-bg/60 p-3.5 text-xs">
                        <p className="text-brand-muted">{t.reason}</p>
                        <p className="break-all font-mono text-[11px] text-rose-600">旧：{t.url}</p>
                        <p className="break-all font-mono text-[11px] text-emerald-600">新：{t.suggested_url}</p>
                        <div className="flex items-center gap-2 pt-1">
                          <button
                            type="button"
                            onClick={() => void runLiveProbe(undefined, t.url)}
                            className="rounded-lg border border-brand-border bg-brand-surface px-2.5 py-1 text-[11px] font-semibold text-brand-fg hover:border-brand-accent"
                          >
                            实测抓检原链接
                          </button>
                          <button
                            type="button"
                            onClick={() =>
                              citeSEOContext(
                                `typo_${t.correct_token}`,
                                `拼写修复 · ${t.model_code}`,
                                `${t.reason}\n\n旧 URL：${t.url}\n建议改为：${t.suggested_url}`,
                                '需 301 修复',
                                [
                                  { label: '错词', value: t.wrong_token },
                                  { label: '正词', value: t.correct_token },
                                ],
                              )
                            }
                            className="rounded-lg border border-brand-border bg-brand-surface px-2.5 py-1 text-[11px] font-semibold text-brand-fg hover:border-brand-accent"
                          >
                            引用上下文
                          </button>
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>

          {/* Card 2: /seo 3-Gate Check List */}
          <div className="ui-card overflow-hidden rounded-2xl">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border px-6 py-4">
              <div>
                <h2 className="text-base font-bold text-brand-fg">/seo 三道门槛实测清单（可抓取 → 可索引 → 结构化）</h2>
                <p className="mt-0.5 text-xs text-brand-muted">不打虚构分，仅呈现实测状态与具体改进项</p>
              </div>
              <div className="flex items-center gap-1.5">
                {(['all', 'issue', 'pass'] as const).map((st) => (
                  <button
                    key={st}
                    type="button"
                    onClick={() => setGateFilter(st)}
                    className={`rounded-full px-3 py-1 text-xs font-semibold ${
                      gateFilter === st
                        ? 'bg-brand-accent text-brand-on-accent'
                        : 'border border-brand-border text-brand-muted hover:text-brand-fg'
                    }`}
                  >
                    {st === 'all' ? '全部 12 项' : st === 'issue' ? '只看异常/警告' : '只看已通过'}
                  </button>
                ))}
              </div>
            </div>
            <div className="divide-y divide-brand-border">
              {filteredGates.map((g) => (
                <div key={g.item} className="flex flex-wrap items-center justify-between gap-3 px-6 py-3.5 text-xs">
                  <div className="flex min-w-0 flex-1 items-center gap-3">
                    <span
                      className={`h-2 w-2 shrink-0 rounded-full ${
                        g.status === 'fail' ? 'bg-rose-500' : g.status === 'warn' ? 'bg-amber-500' : g.status === 'pass' ? 'bg-emerald-500' : 'bg-sky-500'
                      }`}
                    />
                    <div className="min-w-0">
                      <span className="font-bold text-brand-fg">{g.item}</span>
                      <span className="ml-2 text-brand-muted">{g.detail}</span>
                    </div>
                  </div>
                  <span className="rounded-full bg-brand-fg/[0.06] px-2.5 py-0.5 text-[11px] font-semibold text-brand-muted">
                    {g.gate}
                  </span>
                </div>
              ))}
            </div>
          </div>

          {/* Card 3: Collapsible Technical Code Fixes */}
          {currentPage && (
            <details className="ui-card rounded-2xl p-6">
              <summary className="flex cursor-pointer items-center justify-between text-sm font-bold text-brand-fg">
                <span>展开查看修复代码补丁：Schema.org JSON-LD 与 robots.txt / llms.txt（去 &lt;pre&gt; 标签）</span>
                <span className="text-xs font-semibold text-brand-accent">点击展开 ▾</span>
              </summary>
              <div className="mt-4 space-y-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={() => setCodeTab('jsonld')}
                      className={`rounded-lg px-3 py-1.5 text-xs font-semibold ${
                        codeTab === 'jsonld' ? 'bg-brand-fg text-brand-bg' : 'border border-brand-border text-brand-muted'
                      }`}
                    >
                      Product / 首页 JSON-LD 修复
                    </button>
                    <button
                      type="button"
                      onClick={() => setCodeTab('geo')}
                      className={`rounded-lg px-3 py-1.5 text-xs font-semibold ${
                        codeTab === 'geo' ? 'bg-brand-fg text-brand-bg' : 'border border-brand-border text-brand-muted'
                      }`}
                    >
                      robots.txt &amp; llms.txt 修复
                    </button>
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      const txt =
                        codeTab === 'jsonld'
                          ? currentPage.optimized_jsonld
                          : `# robots.txt\n${auditData?.robots_txt_optimized ?? ''}\n\n# llms.txt\n${auditData?.llms_txt_optimized ?? ''}`;
                      void navigator.clipboard?.writeText(txt);
                      toast.show('已复制修复后代码', 'success');
                    }}
                    className="inline-flex items-center gap-1 rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white"
                  >
                    <Copy className="h-3.5 w-3.5" /> 复制修复代码
                  </button>
                </div>
                <pre className="max-h-72 overflow-auto rounded-xl border border-brand-border bg-brand-bg p-4 font-mono text-[11px] leading-relaxed text-brand-fg">
                  {codeTab === 'jsonld'
                    ? currentPage.optimized_jsonld
                    : `${auditData?.robots_txt_optimized ?? ''}\n\n---\n\n${auditData?.llms_txt_optimized ?? ''}`}
                </pre>
              </div>
            </details>
          )}
        </div>
      )}

      {/* TAB 3: COMPETITORS & KEYWORDS */}
      {subTab === 'competitors' && (
        <div className="space-y-5">
          {/* Competitors Comparison Table */}
          <div className="ui-card overflow-hidden rounded-2xl">
            <div className="border-b border-brand-border px-6 py-4">
              <h2 className="text-base font-bold text-brand-fg">4 大核心竞品 SEO 截流打法与冠英反制动作</h2>
              <p className="mt-0.5 text-xs text-brand-muted">对标 Google 自然排名领先同行，直接复用其有效获客链路</p>
            </div>
            <div className="divide-y divide-brand-border">
              {KOOYING_COMPETITORS.map((comp) => (
                <div key={comp.name} className="grid gap-4 px-6 py-5 text-xs lg:grid-cols-[220px_1fr_1fr_auto] lg:items-start">
                  <div>
                    <h3 className="text-sm font-bold text-brand-fg">{comp.name}</h3>
                    <p className="mt-1 text-brand-muted">{comp.positioning}</p>
                  </div>
                  <p className="leading-relaxed text-brand-fg/90">
                    <strong className="text-brand-muted">流量密码：</strong>{comp.tactic}
                  </p>
                  <p className="leading-relaxed text-emerald-700 dark:text-emerald-300">
                    <strong>冠英反制：</strong>{comp.counterMove}
                  </p>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={() =>
                        citeSEOContext(
                          `comp_${comp.name}`,
                          `竞品拆解 · ${comp.name}`,
                          `定位：${comp.positioning}\n流量打法：${comp.tactic}\n反制动作：${comp.counterMove}`,
                          '竞品对标',
                          [{ label: '定位', value: comp.origin }],
                        )
                      }
                      className="rounded-lg border border-brand-border px-2.5 py-1.5 text-[11px] font-semibold text-brand-fg hover:border-brand-accent"
                    >
                      详情
                    </button>
                    <button
                      type="button"
                      onClick={() =>
                        dispatchBotWithSEOPrompt(
                          'bot_gy_seo_growth',
                          `请针对竞品【${comp.name}】打法，基于冠英 43,000㎡ 工厂与 400℃ 0.040mm 实测数据，起草面向海外买家的反制页面核心文案：${comp.counterMove}`,
                        )
                      }
                      className="rounded-lg bg-brand-accent/12 px-2.5 py-1.5 text-[11px] font-semibold text-brand-accent hover:bg-brand-accent/20"
                    >
                      让 Bot 写反制稿
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Gefei Keyword Library */}
          <div className="ui-card space-y-4 rounded-2xl p-6">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <h2 className="text-base font-bold text-brand-fg">哥飞 SEO 选词公式与 4 组高转化关键词库</h2>
                <p className="mt-0.5 text-xs text-brand-muted">
                  推荐命名公式：<code className="font-mono text-brand-accent">[WVA号] + [Knorr/BPW 卡钳车桥] + [OE号] + Truck Brake Pad | KOOYING</code>
                </p>
              </div>
              <button
                type="button"
                onClick={() => {
                  const allKws = GEFEI_KEYWORD_CLUSTERS.flatMap((c) => c.keywords).join('\n');
                  void navigator.clipboard?.writeText(allKws);
                  toast.show('已复制全部 16 条核心关键词', 'success');
                }}
                className="inline-flex items-center gap-1 rounded-xl border border-brand-border px-3 py-1.5 text-xs font-semibold text-brand-fg hover:border-brand-accent"
              >
                <Copy className="h-3.5 w-3.5" /> 复制全部关键词
              </button>
            </div>
            <div className="grid gap-4 md:grid-cols-2">
              {GEFEI_KEYWORD_CLUSTERS.map((grp) => (
                <div key={grp.cluster} className="space-y-3 rounded-xl border border-brand-border bg-brand-bg/40 p-4 text-xs">
                  <div>
                    <h3 className="font-bold text-brand-fg">{grp.cluster}</h3>
                    <p className="mt-0.5 text-[11px] text-brand-muted">{grp.intent}</p>
                  </div>
                  <div className="flex flex-wrap gap-1.5">
                    {grp.keywords.map((kw) => (
                      <button
                        key={kw}
                        type="button"
                        onClick={() => {
                          void navigator.clipboard?.writeText(kw);
                          toast.show(`已复制：${kw}`, 'success');
                        }}
                        className="inline-flex items-center gap-1 rounded-lg border border-brand-border bg-brand-surface px-2.5 py-1 text-left font-mono text-[11px] text-brand-fg hover:border-brand-accent"
                      >
                        <span>{kw}</span>
                        <Copy className="h-3 w-3 shrink-0 text-brand-muted" />
                      </button>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* TAB 4: EXECUTION PLAN + LEAD MAGNET */}
      {subTab === 'plan' && (
        <div className="space-y-5">
          {/* 1-line Action Checklist (:8003 PlanView style) */}
          <div className="ui-card overflow-hidden rounded-2xl">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border px-6 py-4">
              <div>
                <h2 className="text-base font-bold text-brand-fg">4 步弯道超车行动清单（按见效速度排序）</h2>
                <p className="mt-0.5 text-xs text-brand-muted">拖滑块或点击勾选即可保存本地执行进度</p>
              </div>
              <span className="text-xs font-bold text-brand-accent">
                已完成 {Object.values(actionProgress).filter((v) => v >= 100).length} / {INITIAL_OVERTAKE_ACTIONS.length}
              </span>
            </div>
            <div className="divide-y divide-brand-border">
              {INITIAL_OVERTAKE_ACTIONS.map((act, idx) => {
                const pct = actionProgress[act.id] ?? act.defaultProgress;
                return (
                  <div key={act.id} className="grid gap-3 px-6 py-4 text-xs sm:grid-cols-[32px_1fr_70px_140px_76px] sm:items-center">
                    <span className="font-mono text-brand-muted">0{idx + 1}</span>
                    <div className="min-w-0">
                      <p className="font-bold text-brand-fg">{act.title}</p>
                      <p className="mt-0.5 text-[11px] text-brand-muted">
                        {act.eta} · {act.detail}
                      </p>
                    </div>
                    <span
                      className={`w-fit rounded-full px-2.5 py-0.5 text-[11px] font-bold ${
                        act.priority === 'P0'
                          ? 'bg-rose-500/15 text-rose-600'
                          : act.priority === 'P1'
                          ? 'bg-amber-500/15 text-amber-600'
                          : 'bg-sky-500/15 text-sky-600'
                      }`}
                    >
                      {act.priority}
                    </span>
                    <input
                      type="range"
                      min={0}
                      max={100}
                      step={5}
                      value={pct}
                      aria-label={`${act.title} 进度`}
                      onChange={(e) => updateProgress(act.id, Number(e.target.value))}
                      className="w-full cursor-pointer accent-brand-accent"
                    />
                    <button
                      type="button"
                      onClick={() => updateProgress(act.id, pct >= 100 ? 0 : 100)}
                      className={`rounded-lg px-2.5 py-1 text-[11px] font-bold ${
                        pct >= 100 ? 'bg-emerald-600 text-white' : 'border border-brand-border text-brand-fg'
                      }`}
                    >
                      {pct >= 100 ? '已完成' : `${pct}%`}
                    </button>
                  </div>
                );
              })}
            </div>
          </div>

          {/* 2-Column Interactive Deliverable: Featured Snippets HTML Table + PDF Catalog Lead Magnet */}
          <div className="grid gap-5 lg:grid-cols-2">
            <div className="ui-card flex flex-col justify-between space-y-4 rounded-2xl p-6">
              <div className="space-y-3">
                <div className="flex items-center justify-between gap-2">
                  <h3 className="text-sm font-bold text-brand-fg">Featured Snippets 结构化参数表</h3>
                  <button
                    type="button"
                    onClick={() => {
                      void navigator.clipboard?.writeText(FEATURED_SNIPPET_HTML_SNIPPET);
                      toast.show('已复制可直接植入官网单品页的 HTML Table 代码', 'success');
                    }}
                    className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-2.5 py-1 text-xs font-semibold text-brand-fg hover:border-brand-accent"
                  >
                    <Copy className="h-3.5 w-3.5" /> 复制 HTML 代码
                  </button>
                </div>
                <p className="text-xs text-brand-muted">
                  直接在产品页嵌入结构化尺寸/卡钳/OE 对比表，比大段散文更容易命中 Google 精选摘要。
                </p>
                <div className="overflow-x-auto rounded-xl border border-brand-border bg-brand-bg/50 p-3 text-xs">
                  <table className="w-full text-left">
                    <thead className="border-b border-brand-border text-[11px] text-brand-muted">
                      <tr>
                        <th className="py-1.5 pr-3">WVA</th>
                        <th className="py-1.5 pr-3">适配卡钳/车桥</th>
                        <th className="py-1.5 pr-3">尺寸 (mm)</th>
                        <th className="py-1.5">OE 交叉号</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-brand-border font-mono text-[11px]">
                      <tr>
                        <td className="py-2 pr-3 font-bold text-brand-accent">29087 / 29108</td>
                        <td className="py-2 pr-3">Knorr SB7000</td>
                        <td className="py-2 pr-3">247.6×109.7×30</td>
                        <td className="py-2">0044206020</td>
                      </tr>
                      <tr>
                        <td className="py-2 pr-3 font-bold text-brand-accent">19032 (BC/36/1)</td>
                        <td className="py-2 pr-3">BPW 12T–16T</td>
                        <td className="py-2 pr-3">219×180×19.6 (8孔)</td>
                        <td className="py-2">0309226340</td>
                      </tr>
                    </tbody>
                  </table>
                </div>
              </div>
              <div className="flex flex-wrap gap-2 pt-2">
                <button
                  type="button"
                  onClick={() =>
                    dispatchBotWithSEOPrompt(
                      'bot_gy_seo_growth',
                      '请撰写《Top 10 Heavy Duty Truck Brake Pad Manufacturers in China (2026 Guide)》与《WVA 29087 vs 29108》两篇截流长文大纲与 HTML 参数表。',
                    )
                  }
                  className="inline-flex items-center gap-1 rounded-xl bg-brand-accent/12 px-3 py-1.5 text-xs font-semibold text-brand-accent hover:bg-brand-accent/20"
                >
                  让 Bot 生成榜单截流文 &amp; 西/俄/阿语落地页 →
                </button>
              </div>
            </div>

            <form onSubmit={(e) => void handleSubmitLeadMagnet(e)} className="ui-card space-y-3 rounded-2xl p-6">
              <div>
                <h3 className="text-sm font-bold text-brand-fg">
                  Download 2026 Heavy Duty Truck Brake Pad PDF Catalog
                </h3>
                <p className="mt-1 text-xs text-brand-muted">
                  高价值留资诱饵演示：输入需求即触发后台 AI 终端买家甄别与型号匹配
                </p>
              </div>
              <input
                required
                value={lmCompany}
                onChange={(e) => setLmCompany(e.target.value)}
                placeholder="Company / Fleet Name"
                className="h-9 w-full rounded-xl border border-brand-border bg-brand-bg px-3 text-xs text-brand-fg"
              />
              <div className="grid grid-cols-2 gap-2.5">
                <input
                  required
                  value={lmContact}
                  onChange={(e) => setLmContact(e.target.value)}
                  placeholder="Email / WhatsApp"
                  className="h-9 rounded-xl border border-brand-border bg-brand-bg px-3 text-xs text-brand-fg"
                />
                <input
                  required
                  value={lmCountry}
                  onChange={(e) => setLmCountry(e.target.value)}
                  placeholder="Country / Port"
                  className="h-9 rounded-xl border border-brand-border bg-brand-bg px-3 text-xs text-brand-fg"
                />
              </div>
              <textarea
                required
                rows={2}
                value={lmInquiry}
                onChange={(e) => setLmInquiry(e.target.value)}
                placeholder="Target WVA / Axle / Truck Series"
                className="w-full rounded-xl border border-brand-border bg-brand-bg p-3 text-xs text-brand-fg"
              />
              <button
                type="submit"
                disabled={submittingLead}
                className="inline-flex h-10 w-full items-center justify-center gap-1.5 rounded-xl bg-brand-accent text-xs font-bold text-brand-on-accent hover:brightness-110 disabled:opacity-60"
              >
                <Send className="h-3.5 w-3.5" />
                {submittingLead ? 'AI 甄别并解锁目录中…' : 'Unlock 2026 PDF Catalog & Cross-Reference'}
              </button>

              {lastSubmittedLead && (
                <div className="rounded-xl bg-emerald-500/12 p-3 text-xs text-emerald-900 dark:text-emerald-200">
                  <p className="font-bold">
                    已实时捕获留资：{lastSubmittedLead.buyer_type}（置信度 {lastSubmittedLead.confidence}%）· 匹配{' '}
                    {lastSubmittedLead.matched_code}
                  </p>
                  <button
                    type="button"
                    onClick={() => setActiveTab('crawler')}
                    className="mt-1 font-semibold text-brand-accent underline"
                  >
                    去「识片报价与防漏单」查看此线索 →
                  </button>
                </div>
              )}
            </form>
          </div>
        </div>
      )}
    </div>
  );
};

interface MediaMatrixRow {
  id: string;
  platform: 'TikTok' | '抖音';
  handle: string;
  displayName: string;
  followers: string;
  likes: string;
  worksCount: number;
  totalViews: string;
  status: string;
  url: string;
  highlight: string;
  recommendation: string;
}

const KOOTING_MEDIA_ROWS: MediaMatrixRow[] = [
  {
    id: 'tk-autoparts',
    platform: 'TikTok',
    handle: '@kooying.autoparts',
    displayName: '冠英官方老号（266 万播放沉淀）',
    followers: '2,423',
    likes: '4.83万',
    worksCount: 19,
    totalViews: '2,661,068',
    status: '待重启 · Bio 空白',
    url: 'https://www.tiktok.com/@kooying.autoparts',
    highlight: '单条最高 130 万播放 / 1,936 评论 / 5,748 转发，停更自 2023-10-12 且 Bio 空白',
    recommendation: '立即补全英文 Bio 与 WhatsApp/独立站，置顶车间实拍重启更新',
  },
  {
    id: 'tk-brake-fact',
    platform: 'TikTok',
    handle: '@kooying.brake.fact',
    displayName: 'Sophia 工厂实拍主号',
    followers: '73',
    likes: '65',
    worksCount: 13,
    totalViews: '1,618',
    status: '活跃更新中',
    url: 'https://www.tiktok.com/@kooying.brake.fact',
    highlight: '已挂载 +86 159 5711 8669，专注制动片产线与发运现场',
    recommendation: '补充 ≤60s 高清产线与 OEM 包装切片并强化评论区询盘引导',
  },
  {
    id: 'tk-judy26',
    platform: 'TikTok',
    handle: '@kooying_judy26',
    displayName: 'Judy 外贸业务号',
    followers: '3',
    likes: '17',
    worksCount: 16,
    totalViews: '1,283',
    status: '稳定输出',
    url: 'https://www.tiktok.com/@kooying_judy26',
    highlight: 'Bio 已挂载 www.kooying.com，累计 16 条产品短视频',
    recommendation: '统一 WVA/OE 型号标签与独立站询盘落地页链接',
  },
  {
    id: 'tk-brake-lin',
    platform: 'TikTok',
    handle: '@kooying.brake.lin',
    displayName: 'Lin 承接新号',
    followers: '0',
    likes: '1',
    worksCount: 1,
    totalViews: '60',
    status: '冷启动',
    url: 'https://www.tiktok.com/@kooying.brake.lin',
    highlight: '已挂载 +86 159 5711 8669，目前仅发布 1 条试水作品',
    recommendation: '复用高完播工厂实拍模板持续测试冷启动流量',
  },
  {
    id: 'dy-boss',
    platform: '抖音',
    handle: '抖音老板娘主号',
    displayName: '冠英工厂中文主阵地',
    followers: '5,052',
    likes: '3.4万',
    worksCount: 302,
    totalViews: '持续日更',
    status: '主力背书号',
    url: 'https://www.douyin.com/',
    highlight: '已入库广交会发货实拍等高清竖屏素材，工厂实景背书扎实',
    recommendation: '将高赞工厂实拍切片二次配音翻译为英文/西语分发 TikTok',
  },
];

export const GuanyingDataFeishuView: React.FC = () => {
  const toast = useUIStore((s) => s.toast);
  const { data: feishuStatus } = useSWR('api/feishu/status', api.getFeishuSyncStatus, {
    revalidateOnFocus: false,
  });
  const [syncing, setSyncing] = useState(false);
  const [lastPreviewSyncAt, setLastPreviewSyncAt] = useState<string>('');
  const [expandedRowId, setExpandedRowId] = useState<string>('tk-autoparts');

  const feishuSheetUrl = feishuStatus?.url || 'https://feishu.cn/base/IAtCbesjhatm9MsajvYcaBbynyg';

  function handleLocalFeishuSyncPreview() {
    if (syncing) return;
    setSyncing(true);
    window.setTimeout(() => {
      const now = new Date();
      const ts = `${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}:${String(now.getSeconds()).padStart(2, '0')}`;
      setLastPreviewSyncAt(ts);
      setSyncing(false);
      toast.show('已准备新媒体汇总表（展示模式，未触发真实外部写入）', 'success');
    }, 450);
  }

  return (
    <div className="mx-auto max-w-[1160px] space-y-6 pb-10">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-brand-fg">飞书与数据</h1>
          <p className="mt-1 text-xs text-brand-muted">
            冠英新媒体账号核心数据直览，支持直接展开查看与跳转打开飞书多维表格。
          </p>
        </div>

        <div className="flex items-center gap-2.5">
          <a
            href={feishuSheetUrl}
            target="_blank"
            rel="noreferrer"
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-brand-border bg-brand-surface px-3.5 text-xs font-semibold text-brand-fg transition hover:border-brand-accent"
          >
            打开飞书表格 <ExternalLink className="h-3.5 w-3.5" />
          </a>
          <button
            type="button"
            disabled={syncing}
            onClick={handleLocalFeishuSyncPreview}
            className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-brand-accent px-4 text-xs font-semibold text-brand-on-accent transition hover:brightness-110 disabled:opacity-60"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${syncing ? 'animate-spin' : ''}`} />
            {syncing ? '同步中…' : '同步到飞书'}
          </button>
        </div>
      </div>

      {/* 4 Clean New-Media KPI Cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[
          { label: 'TikTok 官方矩阵', value: '4 个账号', note: '合计 49 条实测作品' },
          { label: 'TikTok 累计播放', value: '266.4 万', note: '单条最高 130 万 / 1,936 评论' },
          { label: '抖音老板娘主号', value: '302 条', note: '5,052 粉丝 · 3.4 万点赞' },
          { label: '已入库实拍短片', value: '5 条 ≤60s', note: lastPreviewSyncAt ? `最近整理 ${lastPreviewSyncAt}` : '可在「媒体内容」直接预览' },
        ].map((card) => (
          <div key={card.label} className="ui-card space-y-1 p-5">
            <span className="text-[11px] font-medium text-brand-muted">{card.label}</span>
            <p className="text-2xl font-bold tracking-tight text-brand-fg">{card.value}</p>
            <span className="block text-[11px] text-emerald-600">{card.note}</span>
          </div>
        ))}
      </div>

      {/* Direct Inline New-Media Table */}
      <div className="ui-card overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border bg-brand-bg/50 px-5 py-3.5">
          <div className="flex items-center gap-2">
            <Table2 className="h-4 w-4 text-brand-accent" />
            <span className="text-xs font-bold text-brand-fg">冠英新媒体账号与作品数据表</span>
          </div>
          <span className="text-[11px] text-brand-muted">点击任意行可展开运营结论，支持直接打开原主页</span>
        </div>

        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-left text-xs">
            <thead>
              <tr className="border-b border-brand-border bg-brand-bg/30 text-[11px] font-semibold text-brand-muted">
                <th className="px-5 py-3">平台</th>
                <th className="px-4 py-3">账号</th>
                <th className="px-4 py-3">粉丝 / 获赞</th>
                <th className="px-4 py-3 text-right">作品数</th>
                <th className="px-4 py-3 text-right">总播放</th>
                <th className="px-4 py-3">状态</th>
                <th className="px-5 py-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-brand-border">
              {KOOTING_MEDIA_ROWS.map((row) => {
                const isExpanded = expandedRowId === row.id;
                return (
                  <React.Fragment key={row.id}>
                    <tr
                      onClick={() => setExpandedRowId(isExpanded ? '' : row.id)}
                      className={`cursor-pointer transition-colors hover:bg-brand-fg/[0.025] ${isExpanded ? 'bg-brand-fg/[0.03]' : ''}`}
                    >
                      <td className="px-5 py-3.5">
                        <span className={`inline-block rounded px-2 py-0.5 text-[10px] font-semibold ${row.platform === 'TikTok' ? 'bg-brand-fg/10 text-brand-fg' : 'bg-brand-accent/12 text-brand-accent'}`}>
                          {row.platform}
                        </span>
                      </td>
                      <td className="px-4 py-3.5">
                        <div className="font-semibold text-brand-fg">{row.handle}</div>
                        <div className="text-[11px] text-brand-muted">{row.displayName}</div>
                      </td>
                      <td className="px-4 py-3.5 tabular-nums text-brand-fg">
                        {row.followers} 粉 · {row.likes} 赞
                      </td>
                      <td className="px-4 py-3.5 text-right font-semibold tabular-nums text-brand-fg">
                        {row.worksCount}
                      </td>
                      <td className="px-4 py-3.5 text-right font-semibold tabular-nums text-brand-fg">
                        {row.totalViews}
                      </td>
                      <td className="px-4 py-3.5">
                        <span className="rounded-full bg-brand-surface-raised px-2.5 py-0.5 text-[11px] font-medium text-brand-fg">
                          {row.status}
                        </span>
                      </td>
                      <td className="px-5 py-3.5 text-right" onClick={(e) => e.stopPropagation()}>
                        <a
                          href={row.url}
                          target="_blank"
                          rel="noreferrer"
                          className="inline-flex items-center gap-1 rounded-md border border-brand-border px-2.5 py-1 text-[11px] font-medium text-brand-fg transition hover:border-brand-accent hover:text-brand-accent"
                        >
                          打开主页 <ExternalLink className="h-3 w-3" />
                        </a>
                      </td>
                    </tr>
                    {isExpanded && (
                      <tr className="bg-brand-bg/40">
                        <td colSpan={7} className="px-5 py-3.5">
                          <div className="grid gap-3 md:grid-cols-2 text-[11px] leading-relaxed">
                            <div>
                              <span className="font-semibold text-brand-fg">数据实测结论：</span>
                              <span className="text-brand-muted">{row.highlight}</span>
                            </div>
                            <div>
                              <span className="font-semibold text-emerald-600">下一步建议：</span>
                              <span className="text-brand-fg/90">{row.recommendation}</span>
                            </div>
                          </div>
                        </td>
                      </tr>
                    )}
                  </React.Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};

const SITE_OPS_BOTS = [
  {
    id: 'bot_gy_gtm_growth',
    name: '独立站巡检官',
    desc: '日常核验页面上线状态与 135 款实拍图库完整性',
    prompt: '请检索独立站维护手册并结合 /brand-site/ 巡检状态，帮我核查页面 Canonical、JSON-LD、llms.txt 与 135 款实拍图链接是否全部正常。',
  },
  {
    id: 'bot_gy_gtm_growth',
    name: '品牌站内容运营',
    desc: '更新四大海外市场文案、选型钩子与询盘转化话术',
    prompt: '请基于 /brand-site/ 与 4 个 SEO 专题页，为中东沙特高温挂车与南美矿山重卡买家更新本季度首屏英文卖点与 WhatsApp 回复模板。',
  },
  {
    id: 'bot_gy_gtm_growth',
    name: '官网 SEO 架构师',
    desc: '处理 kooying.com 拼写纠错 301、H1 补齐与 Schema 修复',
    prompt: '请对比 /brand-site/ 与线上官网 https://www.kooying.com/ 的差异，列出今天可直接执行的 H1/Slug/JSON-LD 修复步骤。',
  },
  {
    id: 'bot_gy_gtm_growth',
    name: '多语种获客专家',
    desc: '负责 Top 10 榜单截流长文、WVA 对比与西/俄/阿语扩展',
    prompt: '请围绕 135 款重卡盘鼓刹与 WVA 29087/19032，为冠英策划高转化海外 B2B 榜单截流文章与小语种落地页。',
  },
];

const SITE_PAGE_SHORT_LABEL: Record<string, string> = {
  home: '品牌独立站主页',
  catalog: '135 款选型目录',
  seo_disc: 'WVA 29087 盘刹专题',
  seo_axle: 'BPW/Meritor 换桥选型',
  seo_truck: 'HINO/HOWO 重卡鼓刹',
  seo_dyno: '400℃ 台架配方白皮书',
  plan: '设计规划文档 PLAN.md',
  sitemap: 'Sitemap 站点地图',
  robots: '爬虫规则 robots.txt',
  llms: 'AI 引用摘要 llms.txt',
};

const LOG_CATEGORY_LABEL: Record<string, string> = {
  qa_audit: '渲染巡检',
  content_ops: '内容运营',
  asset_sync: '资产同步',
  seo_geo: 'SEO / GEO',
  architecture: '架构规划',
};

function formatShortTime(iso: string): string {
  if (!iso) return '';
  return iso.replace('T', ' ').slice(5, 16);
}

export const GuanyingSiteMaintenanceView: React.FC = () => {
  const toast = useUIStore((s) => s.toast);
  const setActiveTab = useUIStore((s) => s.setActiveTab);
  const setActiveBotID = useUIStore((s) => s.setActiveBotID);
  const setAgentDraft = useUIStore((s) => s.setAgentDraft);

  const { data: maint, mutate, isLoading } = useSWR('api/guanying/site-maintenance', () =>
    fetchJSON<SiteMaintenanceOverview>('/api/guanying/site-maintenance'),
  );

  const [showLogForm, setShowLogForm] = useState(false);
  const [showAllLogs, setShowAllLogs] = useState(false);
  const [logTitle, setLogTitle] = useState('常规巡检：核验独立站页面与 135 款实拍图联动');
  const [logCategory, setLogCategory] = useState('qa_audit');
  const [logSummary, setLogSummary] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const pages = maint?.pages ?? [];
  const logs = maint?.logs ?? [];
  const stats = maint?.asset_stats ?? {};
  const healthyCount = pages.filter((p) => p.status === 'healthy').length;
  const warnCount = Math.max(0, pages.length - healthyCount);

  const pageGroups = [
    { group: '核心页面', items: pages.filter((p) => p.kind === 'page') },
    { group: 'SEO 专题页', items: pages.filter((p) => p.kind === 'seo') },
    { group: '搜索基建', items: pages.filter((p) => p.kind === 'infra' || p.kind === 'doc') },
  ].filter((g) => g.items.length > 0);

  async function handleRunInspection(e?: React.FormEvent) {
    if (e) e.preventDefault();
    setSubmitting(true);
    try {
      const res = await fetchJSON<{ log: SiteMaintenanceLog; overview?: SiteMaintenanceOverview }>(
        '/api/guanying/site-maintenance',
        {
          method: 'POST',
          body: JSON.stringify({
            category: logCategory,
            title: logTitle.trim(),
            target_path: 'web/',
            page_url: '/brand-site/index.html',
            summary: logSummary.trim(),
            operator_bot: 'bot_gy_gtm_growth',
          }),
        },
      );
      if (res.overview) {
        await mutate(res.overview, false);
      } else {
        await mutate();
      }
      setLogSummary('');
      setShowLogForm(false);
      toast.show(`已完成巡检并记录：${res.log.title}`, 'success');
    } catch (err) {
      toast.show(err instanceof Error ? err.message : '巡检记录失败', 'error');
    } finally {
      setSubmitting(false);
    }
  }

  function citePageContext(p: SitePageHealth) {
    openAgentContext({
      id: `site_page_${p.id}`,
      kind: 'system',
      title: SITE_PAGE_SHORT_LABEL[p.id] || p.label,
      summary: `${p.label}\n\n访问地址：${p.preview_url}\n本地文件：${p.rel_path}\n网页标题：${p.title || '搜索配置文件'}`,
      status: p.status === 'healthy' ? '正常在线' : p.status,
      occurredAt: p.updated_at || maint?.checked_at,
      fields: [
        { label: '所属类别', value: p.kind },
        { label: 'H1 标题数', value: String(p.h1_count) },
        { label: 'Canonical', value: p.has_canonical ? '已配置' : 'N/A' },
        { label: 'JSON-LD', value: p.has_jsonld ? '已嵌入' : 'N/A' },
        { label: '文件大小', value: `${(p.size_bytes / 1024).toFixed(1)} KB` },
      ],
    });
  }

  function citeLogContext(log: SiteMaintenanceLog) {
    openAgentContext({
      id: `site_log_${log.id}`,
      kind: 'evidence',
      title: log.title,
      summary: `${log.summary}\n\n涉及路径：${log.target_path}\n关联页面：${log.page_url}`,
      status: log.status === 'verified' ? '已核验' : log.status,
      occurredAt: log.created_at,
      fields: [
        { label: '维护分类', value: LOG_CATEGORY_LABEL[log.category] || log.category },
        { label: '执行助手', value: log.operator_bot },
        { label: '页面链接', value: log.page_url },
      ],
    });
  }

  function wakeOpsBot(botID: string, prompt: string) {
    setActiveBotID(botID);
    setAgentDraft({ mode: 'chat', message: prompt });
    setActiveTab('bot');
  }

  const visibleLogs = showAllLogs ? logs : logs.slice(0, 5);

  return (
    <div className="mx-auto max-w-[1160px] space-y-6 pb-12">
      {/* Calm Header */}
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-brand-accent">
            SITE MONITOR · 独立站状态
          </p>
          <h1 className="mt-1.5 text-[26px] font-bold tracking-tight text-brand-fg">
            KOOYING 品牌独立站维护
          </h1>
          <p className="mt-1.5 text-sm text-brand-muted">
            一屏查看海外品牌站页面健康、产品图库与最近维护动态。
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2.5">
          <a
            href="/brand-site/index.html"
            target="_blank"
            rel="noreferrer"
            className="inline-flex h-9 items-center gap-1.5 rounded-xl border border-brand-border bg-brand-surface px-4 text-xs font-semibold text-brand-fg hover:border-brand-accent"
          >
            打开品牌独立站 <ExternalLink className="h-3.5 w-3.5" />
          </a>
          <button
            type="button"
            disabled={submitting}
            onClick={() => void handleRunInspection()}
            className="inline-flex h-9 items-center gap-1.5 rounded-xl bg-brand-accent px-4 text-xs font-semibold text-brand-on-accent hover:brightness-110 disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${submitting ? 'animate-spin' : ''}`} />
            {submitting ? '巡检中…' : '一键重新巡检'}
          </button>
        </div>
      </div>

      {/* Single-line Status Bar (:8003 style) */}
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-brand-border bg-brand-surface px-4 py-3 text-xs">
        <div className="flex items-center gap-2.5">
          <span className="h-2 w-2 rounded-full bg-emerald-500 shadow-[0_0_8px_#10b981]" />
          <span className="text-brand-muted">
            品牌独立站在线 · 共 <strong className="text-brand-fg">{pages.length || 10}</strong> 个核心页与搜索文件
          </span>
        </div>
        <div className="flex items-center gap-2">
          <span className="rounded-full bg-emerald-500/12 px-2.5 py-0.5 text-[11px] font-semibold text-emerald-600">
            正常 {healthyCount || 10}
          </span>
          <span className="rounded-full bg-brand-fg/[0.06] px-2.5 py-0.5 text-[11px] font-semibold text-brand-muted">
            待优化 {warnCount}
          </span>
        </div>
      </div>

      {/* 4 Spacious KPI Cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[
          {
            label: '独立站页面与基建',
            value: String(pages.length || 10),
            unit: '个',
            bar: 'bg-emerald-500',
            dot: 'bg-emerald-500',
            note: '全部正常 · 主页 / 目录 / 4 个专题页',
          },
          {
            label: '盘/鼓刹在库型号',
            value: String(stats.total_models ?? 135),
            unit: '款',
            bar: 'bg-blue-500',
            dot: 'bg-blue-500',
            note: `${stats.disc_models ?? 41} 款盘刹 · ${stats.drum_models ?? 94} 款鼓刹`,
          },
          {
            label: '棚拍高清实拍图库',
            value: String(Number(stats.total_disc_photos ?? 126) + Number(stats.total_drum_photos ?? 184)),
            unit: '张',
            bar: 'bg-amber-500',
            dot: 'bg-amber-500',
            note: '含三视图、工厂实景与工程孔位图',
          },
          {
            label: '独立站直连询盘',
            value: String(maint?.site_lead_count ?? 8),
            unit: '条',
            bar: 'bg-brand-accent',
            dot: 'bg-brand-accent',
            note: '独立站表单与选型器已接通线索库',
          },
        ].map((card) => (
          <div key={card.label} className="ui-card relative flex min-h-[142px] flex-col overflow-hidden rounded-2xl p-6">
            <div className={`absolute inset-x-0 top-0 h-[3px] ${card.bar}`} />
            <div className="flex items-center gap-2 text-xs text-brand-muted">
              <span className={`h-2 w-2 rounded-full ${card.dot}`} />
              <span>{card.label}</span>
            </div>
            <div className="mt-3 flex items-baseline gap-1.5">
              <span className="text-4xl font-bold tracking-tight text-brand-fg">{card.value}</span>
              <span className="text-sm text-brand-muted">{card.unit}</span>
            </div>
            <p className="mt-auto pt-3 text-xs text-brand-muted">{card.note}</p>
          </div>
        ))}
      </div>

      {/* 2-Column Main Grid (:8003 Overview style) */}
      <div className="grid gap-5 lg:grid-cols-2">
        {/* Left Card: Grouped Compact Page Chips */}
        <div className="ui-card space-y-5 rounded-2xl p-6">
          <div>
            <h2 className="text-base font-bold text-brand-fg">页面健康度 · {pages.length || 10} 个页面</h2>
            <p className="mt-1 text-xs text-brand-muted">点击页面在右侧查看详情，或直接打开浏览效果</p>
          </div>

          {isLoading ? (
            <div className="py-10 text-center text-xs text-brand-muted">正在检查站点页面…</div>
          ) : (
            <div className="space-y-4">
              {pageGroups.map((grp) => (
                <div key={grp.group} className="space-y-2">
                  <div className="flex items-center gap-2 text-[11px] font-semibold text-brand-muted">
                    <span>{grp.group}</span>
                    <span className="h-px flex-1 bg-brand-border" />
                  </div>
                  <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                    {grp.items.map((p) => (
                      <div
                        key={p.id}
                        onClick={() => citePageContext(p)}
                        role="button"
                        tabIndex={0}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') citePageContext(p);
                        }}
                        className="flex cursor-pointer items-center justify-between gap-2 rounded-xl border border-brand-border bg-brand-bg/40 px-3.5 py-2.5 text-xs transition hover:border-brand-accent/60 hover:bg-brand-fg/[0.03]"
                      >
                        <span className="flex min-w-0 items-center gap-2">
                          <span
                            className={`h-2 w-2 shrink-0 rounded-full ${
                              p.status === 'healthy' ? 'bg-emerald-500' : 'bg-amber-500'
                            }`}
                          />
                          <span className="truncate font-medium text-brand-fg">
                            {SITE_PAGE_SHORT_LABEL[p.id] || p.label}
                          </span>
                        </span>
                        <a
                          href={p.preview_url}
                          target="_blank"
                          rel="noreferrer"
                          onClick={(e) => e.stopPropagation()}
                          className="shrink-0 text-[11px] font-semibold text-brand-accent hover:underline"
                        >
                          预览 ↗
                        </a>
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Right Card: Clean 1-line Bot Dispatch Rows */}
        <div className="ui-card flex flex-col justify-between space-y-4 rounded-2xl p-6">
          <div>
            <h2 className="text-base font-bold text-brand-fg">站点运营助手</h2>
            <p className="mt-1 text-xs text-brand-muted">已记住独立站结构与 135 款产品实拍资产，点选直接交代任务</p>
          </div>

          <div className="divide-y divide-brand-border">
            {SITE_OPS_BOTS.map((bot) => (
              <div key={bot.id} className="flex items-center justify-between gap-4 py-3.5 first:pt-1 last:pb-1">
                <div className="min-w-0">
                  <p className="text-sm font-semibold text-brand-fg">{bot.name}</p>
                  <p className="mt-0.5 truncate text-xs text-brand-muted">{bot.desc}</p>
                </div>
                <button
                  type="button"
                  onClick={() => wakeOpsBot(bot.id, bot.prompt)}
                  className="inline-flex shrink-0 items-center gap-1 rounded-xl border border-brand-border px-3 py-1.5 text-xs font-semibold text-brand-fg transition hover:border-brand-accent hover:text-brand-accent"
                >
                  去交代 <ArrowRight className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Collapsible Maintenance Notes Card (Clean 1-line Human Rows) */}
      <div className="ui-card overflow-hidden rounded-2xl">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border px-6 py-4">
          <div>
            <h2 className="text-base font-bold text-brand-fg">最近维护记录 · {logs.length} 条</h2>
            <p className="mt-0.5 text-xs text-brand-muted">点任意一行在右侧看完整详情与上下文引用</p>
          </div>
          <button
            type="button"
            onClick={() => setShowLogForm((v) => !v)}
            className="rounded-xl border border-brand-border px-3.5 py-1.5 text-xs font-semibold text-brand-fg hover:border-brand-accent"
          >
            {showLogForm ? '收起表单' : '+ 记一条维护备注'}
          </button>
        </div>

        {showLogForm && (
          <form onSubmit={(e) => void handleRunInspection(e)} className="space-y-3 border-b border-brand-border bg-brand-bg/40 px-6 py-4">
            <div className="grid gap-3 sm:grid-cols-[180px_1fr]">
              <select
                value={logCategory}
                onChange={(e) => setLogCategory(e.target.value)}
                className="h-9 rounded-xl border border-brand-border bg-brand-surface px-3 text-xs text-brand-fg"
              >
                <option value="qa_audit">渲染与 SEO 巡检</option>
                <option value="content_ops">内容与专题更新</option>
                <option value="asset_sync">实拍图库同步</option>
                <option value="seo_geo">搜索与 AI 优化</option>
              </select>
              <input
                required
                value={logTitle}
                onChange={(e) => setLogTitle(e.target.value)}
                placeholder="输入本次维护摘要标题"
                className="h-9 rounded-xl border border-brand-border bg-brand-surface px-3 text-xs text-brand-fg"
              />
            </div>
            <div className="flex flex-wrap gap-2.5">
              <input
                value={logSummary}
                onChange={(e) => setLogSummary(e.target.value)}
                placeholder="补充说明（可选，留空自动汇总最新页面状态）"
                className="h-9 flex-1 rounded-xl border border-brand-border bg-brand-surface px-3 text-xs text-brand-fg"
              />
              <button
                type="submit"
                disabled={submitting}
                className="h-9 rounded-xl bg-brand-fg px-4 text-xs font-semibold text-brand-bg hover:opacity-90 disabled:opacity-50"
              >
                {submitting ? '保存中…' : '保存记录'}
              </button>
            </div>
          </form>
        )}

        <div className="divide-y divide-brand-border">
          {visibleLogs.map((log) => (
            <div
              key={log.id}
              onClick={() => citeLogContext(log)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') citeLogContext(log);
              }}
              className="flex cursor-pointer flex-wrap items-center justify-between gap-3 px-6 py-3.5 text-xs transition hover:bg-brand-fg/[0.02]"
            >
              <div className="flex min-w-0 flex-1 items-center gap-3">
                <span className="h-2 w-2 shrink-0 rounded-full bg-emerald-500" />
                <span className="shrink-0 rounded-full bg-brand-accent/10 px-2.5 py-0.5 text-[11px] font-semibold text-brand-accent">
                  {LOG_CATEGORY_LABEL[log.category] || '站点维护'}
                </span>
                <span className="truncate font-medium text-brand-fg">{log.title}</span>
              </div>
              <div className="flex shrink-0 items-center gap-3 text-[11px] text-brand-muted">
                <span className="font-mono">{formatShortTime(log.created_at)}</span>
                <span className="font-semibold text-brand-fg">详情 →</span>
              </div>
            </div>
          ))}
        </div>

        {logs.length > 5 && (
          <div className="border-t border-brand-border px-6 py-3">
            <button
              type="button"
              onClick={() => setShowAllLogs((v) => !v)}
              className="text-xs font-semibold text-brand-accent hover:underline"
            >
              {showAllLogs ? '收起记录' : `展开全部 ${logs.length} 条记录`}
            </button>
          </div>
        )}
      </div>
    </div>
  );
};
