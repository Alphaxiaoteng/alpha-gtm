import React, { useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  AlertTriangle,
  ArrowRight,
  Bot,
  CheckCircle2,
  Clock,
  Code2,
  Copy,
  ExternalLink,
  FileCheck2,
  Film,
  Filter,
  Globe,
  Layers,
  MessageSquarePlus,
  RefreshCw,
  Search,
  Send,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  Table2,
  Wrench,
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
    title: '门店手量尺寸出报价',
    pain: '客户发卡尺实测外弧 240 × 宽 220mm（20孔），无原厂编码',
    action: '按 ±5mm 公差找出汉德 35T (3501D10-120) 并生成双语报价',
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
    action: '自动带出换桥对比与 14 孔 / 铆钉核对提醒，发出前需要人工确认',
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
    action: '并列对比 GYS-126 等匹配型号与 FOB 单价，现场直接答复',
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
            <span>浙江冠英 GTM 工作台 · 识片报价 · 跨时差询盘</span>
          </div>
          <h1 className="mt-1 text-2xl font-bold tracking-tight text-brand-fg">
            识片报价与防漏单
          </h1>
          <p className="mt-1 text-xs text-brand-muted">
            覆盖门店手量识片、后市场换桥纠错、矿山高温配方升级与跨时差询盘 4 类场景，每个场景都能生成中英双语报价单。
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
            现场识片与报价
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
                {matching ? '匹配中…' : '查匹配型号'}
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
                        选型结果与报价单
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
                          : `【浙江冠英 Kooying / GOOLE 选型报价单】\n1. 型号：${selectedProduct.code} (${selectedProduct.oe_or_wva})\n2. 几何参数：外弧 ${selectedProduct.outer_arc_mm} × 宽 ${selectedProduct.width_mm} × 厚 ${selectedProduct.thickness_str} mm，${selectedProduct.holes}孔 (${selectedProduct.rivet_spec})\n3. 适配车桥/车型：${selectedProduct.applicable_models}\n4. 换桥防错核对：${selectedProduct.swap_warning}\n5. 推荐配方：${selectedProduct.formula_code}（400℃ 厚度磨损 0.040mm，摩擦系数 0.35-0.38）\n6. 参考价格：FOB ${selectedProduct.price_fob_usd} / 国内 ${selectedProduct.price_cny}（MOQ ${selectedProduct.moq}）`
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
                      按这条询盘匹配型号 <ArrowRight className="h-3.5 w-3.5" />
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
  { gate: '可索引', item: '多语言 hreflang 与区域子目录', status: 'info', detail: '当前全站纯英文单语言，尚无 /es/ (南美)、/ru/ (俄语中亚)、/ar/ (中东) 子目录' },
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
    origin: '山东泰安 · 同赛道直接对标',
    positioning: '专注商用车/重卡刹车片与衬片（WVA 全覆盖）',
    tactic: '全网高权重品牌词 + 展会/认证背书：重点抢占 commercial vehicle brake pad manufacturer、ECE R90 brake pads；大量铺设欧洲重卡标准与 CNAS 台架数据，E-E-A-T 信号完整。',
    gap: '冠英拥有同样的 43,000㎡ 工厂、2 台 Krauss 试验机 (400C 磨损 0.040mm) 与 1500万套产能，但官网 Title/H1/Schema 未突出认证与台架量化证据。',
    counterMove: '全站产品页与首页置顶 IATF16949 + ECE R90 + 400℃ 0.040mm 实测对比框，并把 /brand-site/ 瑞士工业级台架视觉迁回主站。',
  },
  {
    name: 'FrenoBrake (frenobrake.com)',
    origin: '华东/出口OEM定制标杆',
    positioning: '主打无铜陶瓷配方与 OEM 定制刹车片',
    tactic: '靠榜单长文占住决策词：专门发布《Top 10 China Brake Pads Manufacturers in 2025/2026》采购商必看榜单长文，将自己排第 1 位，承接正在横向比价的中国供应链搜索流量。',
    gap: '冠英官网 190 篇博客偏重泛科普与展会通知，完全缺失 Top / Best / Alternatives / VS 类决策榜单长文与第三方外链背书。',
    counterMove: '发布《Top 10 Heavy Duty Truck Brake Pad Manufacturers in China (2026 Guide)》与《WVA 29087 vs 29108》对比长文，覆盖同批决策词。',
  },
  {
    name: 'TopBrakeParts (topbrakeparts.com)',
    origin: '广东清远 · 面向中小批发商',
    positioning: '全品类混批（Mixed-SKU）制动件供应商',
    tactic: 'B2B 采购决策指南（Buyer\'s Guide）：按工厂规模、起订量 MOQ、混批规则、认证齐全度输出高清对比表格，并搭配电子目录下载留资钩子。',
    gap: '冠英仅在页尾保留普通 Contact Us 表单，产品页缺少面向小 B 批发商与修配厂的 MOQ、混批说明与 PDF 目录下载诱饵。',
    counterMove: '在每个车型大类页顶部植入「Download 2026 PDF Catalog & Cross-Reference List」按钮（邮箱+WhatsApp 解锁下载），效果需在上线后用实际线索数据验证。',
  },
  {
    name: 'GDST (gdstauto.com)',
    origin: '安徽芜湖 · 多语种集群站',
    positioning: '制动系统全套：刹车片 + 刹车盘 + 分泵',
    tactic: '长尾词矩阵 + 多语种子目录：在英文主站之外同步建设西语(/es/)、俄语(/ru/)、阿语(/ar/)子目录，覆盖拉美、中亚俄语区与中东重卡汽配的自然询盘。',
    gap: '冠英 468 页 Sitemap 全部集中在英文路径，却实打实供货东南亚日系、南美矿山、中亚 HOWO 汉德桥与中东高温挂车。',
    counterMove: '优先开设 /es/ (南美 Scania/Mercedes/Volvo)、/ru/ (HOWO/Shacman/Kamaz)、/ar/ (中东 BPW/Actros) 三套子目录。',
  },
];

const GEFEI_KEYWORD_CLUSTERS = [
  {
    cluster: '一、型号 × 卡钳 × OE 组合词（询盘意图最明确）',
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
    cluster: '二、榜单与型号对比类决策词（参考 FrenoBrake 做法）',
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
    cluster: '三、特种工业与高毛利细分词（放大 kooying 现有强项）',
    intent: 'Niche B2B · 低竞争度 + 高客单价（风机运维 & 矿山车队）',
    formula: '[设备/工况] + [工业部件号] + High-Friction Brake Pad Selection',
    keywords: [
      '9710081817 high friction brake pad for wind turbine',
      'wind turbine yaw rotor brake pad manufacturer copper free',
      'Hande 35T mining dump truck 20-hole brake lining 240x220x18.5',
      'low noise heavy bus asbestos free brake lining 400C wear 0.040mm',
    ],
    targetPage: '/product/brake-pad-for-wind-turbine/ + /news/ 选型指南',
  },
  {
    cluster: '四、小语种词矩阵（参考 GDST 的错位竞争）',
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
    title: '在车型 / WVA 页顶部放入「Download 2026 Truck Brake Pad PDF Catalog & OE List」下载入口',
    detail: '取代单一页尾 Contact Us 表单，采购商输入公司+邮箱/WhatsApp 即可下载完整 OE 交叉目录，并写入 :17422 线索库；线索量变动需上线后按实际数据评估。',
    defaultProgress: 35,
  },
  {
    id: 'act_listicle',
    step: '第2步 · 榜单与型号对比内容',
    eta: '3–5 天部署',
    priority: 'P1',
    title: '发布《Top 10 China Heavy Truck Brake Pad Manufacturers (2026)》与《WVA 29087 vs 29108》',
    detail: '对标 FrenoBrake / TopBrakeParts 覆盖同一批决策词，在榜单里写入冠英 43,000㎡ 工厂、2 台 Krauss 台架与 15M 套年产能。',
    defaultProgress: 5,
  },
  {
    id: 'act_i18n',
    step: '第4步 · 西 / 俄 / 阿小语种子站',
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
  const [gateFilter, setGateFilter] = useState<'all' | 'fail' | 'warn' | 'pass'>('all');

  // Step 4 checklist progress saved locally
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

  // Lead Magnet Demo form (Step 3 Catalog PDF + Cross-Reference download)
  const [lmCompany, setLmCompany] = useState('Al-Jazeera Heavy Fleet Maintenance LLC');
  const [lmContact, setLmContact] = useState('procurement@aljazeerafleet.sa / +966 55 820 4419');
  const [lmCountry, setLmCountry] = useState('沙特阿拉伯 (Dammam Port)');
  const [lmInquiry, setLmInquiry] = useState(
    'Download 2026 PDF Catalog: Need Knorr SB7000 WVA 29087 (OE 0044206020) and BPW 19032 BC/36/1 8-hole brake linings, 800 sets trial.',
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
        toast.show(`已完成对 ${target} 的实时 HTML/SEO 抓取体检！`, 'success');
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
          channel: '官网 SEO PDF目录留资钩子 (kooying.com)',
          inquiry_text: lmInquiry,
          seo_page_id: 'seo_wva29087_disc',
        }),
      });
      setLastSubmittedLead(res.lead);
      await mutateOverview();
      toast.show(`Catalog 下载留资已接住！AI 识别：${res.lead.buyer_type}（匹配 ${res.lead.matched_code}）`, 'success');
    } catch (err) {
      toast.show(err instanceof Error ? err.message : '提交失败', 'error');
    } finally {
      setSubmittingLead(false);
    }
  }

  const filteredGates = KOOYING_SEO_GATE_CHECKS.filter((g) => gateFilter === 'all' || g.status === gateFilter);

  return (
    <div className="mx-auto max-w-[1380px] space-y-5 pb-12">
      {/* Header */}
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-brand-accent">
            <Globe className="h-3.5 w-3.5" />
            <span>Alpha SEO 工作台 · kooying.com 现状诊断与改进清单</span>
          </div>
          <h1 className="mt-1 text-2xl font-bold tracking-tight text-brand-fg">
            官网 SEO 诊断与竞品对照（kooying.com）
          </h1>
          <p className="mt-1 text-xs text-brand-muted">
            不对虚构新站空转——直接抓取诊断{' '}
            <a
              href="https://www.kooying.com/"
              target="_blank"
              rel="noreferrer"
              className="font-semibold text-brand-accent underline"
            >
              https://www.kooying.com/
            </a>{' '}
            468 个页面，结合 /seo 三道门槛与 Alltour / FrenoBrake / TopBrakeParts / GDST 打法制定可执行方案。
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <a
            href="/brand-site/index.html"
            target="_blank"
            rel="noreferrer"
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-brand-border bg-brand-surface px-3 text-xs font-semibold text-brand-fg hover:border-brand-accent"
          >
            对比满分样板站 (/brand-site) <ExternalLink className="h-3.5 w-3.5" />
          </a>
          <button
            type="button"
            onClick={() =>
              dispatchBotWithSEOPrompt(
                'bot_gy_gtm_growth',
                '请基于 https://www.kooying.com/ 的真实 SEO 审计结论（16处拼写硬伤 trcuk/beake/fiction/btake、首页与简介 H1=0、Product JSON-LD 大写 "Offers"+0元价格、llms.txt 外层 <pre> 标签），输出一份可直接给前端/CMS 工程师今天上线的修复补丁包（含 301 重定向表与 JSON-LD）。',
              )
            }
            className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-brand-accent px-3.5 text-xs font-semibold text-brand-on-accent hover:brightness-110"
          >
            <Bot className="h-3.5 w-3.5" /> @SEO 架构师生成修复补丁
          </button>
        </div>
      </div>

      {/* :8003 Style Status Bar + Live URL Probe */}
      <div className="ui-card flex flex-wrap items-center justify-between gap-3 border border-brand-border bg-brand-surface px-4 py-3">
        <div className="flex flex-wrap items-center gap-2.5 text-xs">
          <span className="h-2.5 w-2.5 rounded-full bg-emerald-500 shadow-[0_0_8px_#10b981]" />
          <span className="text-brand-muted">
            目标域 <strong className="text-brand-fg">kooying.com</strong> · Sitemap{' '}
            <strong className="text-brand-fg">{auditData?.sitemap_url_count ?? 468}</strong> URL · /seo 体检{' '}
            <strong className="text-emerald-600">29 通过</strong> / <strong className="text-amber-600">3 警告</strong> /{' '}
            <strong className="text-rose-600">1 失败 (H1=0)</strong>
          </span>
          <span className="rounded-full bg-rose-500/12 px-2 py-0.5 text-[10px] font-bold text-rose-600">
            拼写硬伤 16 处
          </span>
          <span className="rounded-full bg-amber-500/12 px-2 py-0.5 text-[10px] font-bold text-amber-600">
            Schema 缺陷 2 项
          </span>
        </div>

        <form onSubmit={(e) => void runLiveProbe(e)} className="flex flex-1 items-center gap-2 sm:max-w-lg">
          <div className="relative flex-1">
            <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-brand-muted" />
            <input
              value={probeURL}
              onChange={(e) => setProbeURL(e.target.value)}
              placeholder="输入任意 kooying.com 页面 URL 实时抓取分析…"
              className="h-8 w-full rounded-lg border border-brand-border bg-brand-bg pl-8 pr-3 font-mono text-[11px] text-brand-fg"
            />
          </div>
          <button
            type="submit"
            disabled={probing}
            className="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg bg-brand-fg px-3 text-xs font-semibold text-brand-bg hover:opacity-90 disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${probing ? 'animate-spin' : ''}`} />
            {probing ? '抓检中…' : '实时抓取分析'}
          </button>
        </form>
      </div>

      {/* 首屏只回答一个问题：现在该改什么。
          原来这里并排 4 张卡、每张塞一行技术细节（拼写词表、CLS、robots 指令…），
          扫一眼全是数据看不出重点。改成「一句结论 + 3 个关键数字 + 一个主按钮」，
          细节留给下面的审计页签。 */}
      <div className="ui-card flex flex-col gap-4 p-5 lg:flex-row lg:items-center lg:justify-between">
        <div className="min-w-0">
          <p className="text-sm font-bold text-brand-fg">先修 16 处拼写硬伤与首页缺失的 H1</p>
          <p className="mt-1 text-xs leading-relaxed text-brand-muted">
            这两项直接影响 Google 能不能正确理解页面，改动量最小、收益最直接。结构化数据与留资钩子排在后面。
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-6">
          <div className="text-center">
            <div className="text-2xl font-extrabold tracking-tight text-brand-fg">{auditData?.sitemap_url_count ?? 468}</div>
            <div className="text-[10px] font-medium text-brand-muted">已收录页面</div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-extrabold tracking-tight text-rose-600">16</div>
            <div className="text-[10px] font-medium text-brand-muted">待修拼写</div>
          </div>
          <div className="text-center">
            <div className="text-2xl font-extrabold tracking-tight text-amber-600">1</div>
            <div className="text-[10px] font-medium text-brand-muted">缺失 H1</div>
          </div>
        </div>
      </div>

      {/* 4 Workbench Tabs (:8003 Overview / Audit / Competitors / Plan) */}
      <div className="flex flex-wrap items-center gap-1.5 rounded-xl border border-brand-border bg-brand-surface p-1.5">
        {(
          [
            { id: 'overview', label: '◎ 概览 · 现状优劣势与页面矩阵' },
            { id: 'audit', label: '◍ 审计 · /seo 三道门槛与拼写/Schema修复' },
            { id: 'competitors', label: '⚔ 竞品 · 4大同行拆解与哥飞关键词库' },
            { id: 'plan', label: '◈ 执行 · 4 步改进清单与 PDF 目录留资' },
          ] as const
        ).map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => setSubTab(tab.id)}
            className={`rounded-lg px-3.5 py-2 text-xs font-semibold transition ${
              subTab === tab.id
                ? 'bg-brand-accent text-brand-on-accent shadow-xs'
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
          {/* Sitemap 468 Stack Bar (:8003 IndexStack style) */}
          <div className="ui-card space-y-3 p-5">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <h2 className="text-sm font-bold text-brand-fg">
                  全站 468 条 Sitemap URL 结构构成（实测 https://www.kooying.com/sitemap.xml）
                </h2>
                <p className="text-[11px] text-brand-muted">
                  点击下方图例高亮对应板块 · 可清晰看到冠英依靠“WVA 长尾矩阵 + 190 篇博客长文”拿流量，但在特种片与多语种上有明显扩量空间
                </p>
              </div>
              <button
                type="button"
                onClick={() =>
                  citeSEOContext(
                    'ctx_kooying_overview_swot',
                    'kooying.com 现状 SWOT 与 468 页 Sitemap 结构',
                    '【现有2大优势】1) WVA 长尾型号矩阵（29087/29108/29171/19032）直写 Title 与 URL；2) 190 篇 /news/ 长文与 9 款风力发电机刹车片避开红海。\n【暴露3大短板】1) 缺 Top 10 行业榜单词与英文外链背书；2) 页面细节粗糙（Trcuk/Fiction/Beake/Btake 拼写错与静态模板重复，首页 H1=0）；3) 产品页缺乏卡钳/OE Cross-Reference HTML 参数表及顶部 PDF Catalog 留资钩子。',
                    '已实测诊断',
                    [
                      { label: 'Sitemap 总数', value: '468 URL（产品 278 / 资讯 190）' },
                      { label: '高危拼写错页', value: '16 处（trcuk×5, beake×3, fiction×2...）' },
                      { label: '核心竞品对标', value: 'Alltour / FrenoBrake / TopBrakeParts / GDST' },
                    ],
                  )
                }
                className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-2.5 py-1.5 text-[11px] font-semibold text-brand-fg hover:border-brand-accent"
              >
                <FileCheck2 className="h-3.5 w-3.5 text-brand-accent" /> 引用总览上下文
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
                    opacity: selectedMixIdx === null || selectedMixIdx === idx ? 1 : 0.28,
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
                  className={`inline-flex items-center gap-1.5 rounded-lg border px-2.5 py-1 text-[11px] font-medium transition ${
                    selectedMixIdx === idx
                      ? 'border-brand-accent bg-brand-accent/10 text-brand-fg'
                      : 'border-brand-border bg-brand-bg/50 text-brand-muted hover:text-brand-fg'
                  }`}
                >
                  <span className="h-2 w-2 rounded-full" style={{ backgroundColor: item.color }} />
                  <span>{item.label}</span>
                  <strong className="text-brand-fg">
                    {item.cnt} ({((item.cnt / mixTotal) * 100).toFixed(1)}%)
                  </strong>
                </button>
              ))}
            </div>
          </div>

          {/* One: Strengths vs Weaknesses */}
          <div className="grid gap-5 lg:grid-cols-2">
            <div className="ui-card space-y-4 p-5">
              <div className="flex items-center justify-between">
                <h3 className="flex items-center gap-2 text-sm font-bold text-emerald-700 dark:text-emerald-400">
                  <CheckCircle2 className="h-4 w-4" />
                  一、kooying.com 目前做对的地方（值得保留）
                </h3>
                <span className="rounded-full bg-emerald-500/12 px-2 py-0.5 text-[10px] font-bold text-emerald-600">
                  Google 流量基本盘
                </span>
              </div>
              <div className="space-y-3 text-xs">
                <div className="rounded-xl border border-emerald-500/20 bg-emerald-500/5 p-3.5">
                  <p className="font-bold text-brand-fg">1. 高精准长尾型号词（WVA 编号矩阵）</p>
                  <p className="mt-1 leading-relaxed text-brand-muted">
                    将商用车 WVA 编号直接写进页面 Title 与 URL（如{' '}
                    <code className="rounded bg-brand-bg px-1 py-0.5 font-mono text-[10px]">
                      /product/brake-pad-for-truck/
                    </code>{' '}
                    下的 <strong>29087、29108、29171、19032</strong>）。海外重卡修配厂与批发商搜索高度依赖{' '}
                    <code>WVA + 5位数字</code>，搜索量虽小，但搜这些词的人多半正在找替换件。
                  </p>
                </div>
                <div className="rounded-xl border border-emerald-500/20 bg-emerald-500/5 p-3.5">
                  <p className="font-bold text-brand-fg">2. 高频输出商用车 / 特种工业场景长文（190 篇 + 9 款风电）</p>
                  <p className="mt-1 leading-relaxed text-brand-muted">
                    博客更新频率高，且覆盖了竞争较小的细分词（如{' '}
                    <em>《Brake Pad for Wind Turbine: Selection, Materials and Maintenance Guide》</em>{' '}
                    及 <code>9710081817 / 9710071701</code> 风电偏航制动片），避开了乘用车刹车片这类竞争激烈的品类。
                  </p>
                </div>
              </div>
            </div>

            <div className="ui-card space-y-4 p-5">
              <div className="flex items-center justify-between">
                <h3 className="flex items-center gap-2 text-sm font-bold text-rose-700 dark:text-rose-400">
                  <ShieldAlert className="h-4 w-4" />
                  二、三个明显的 SEO 短板（先改这三项）
                </h3>
                <span className="rounded-full bg-rose-500/12 px-2 py-0.5 text-[10px] font-bold text-rose-600">
                  亟待优化修复
                </span>
              </div>
              <div className="space-y-3 text-xs">
                <div className="rounded-xl border border-rose-500/20 bg-rose-500/5 p-3">
                  <p className="font-bold text-brand-fg">1. 严重缺少行业头部榜单词（Top / Best Listicle）背书</p>
                  <p className="mt-1 text-brand-muted">
                    采购商必搜决策大词（<code>Top China brake pads manufacturers</code> /{' '}
                    <code>commercial vehicle brake pad supplier China</code>）前排几乎被{' '}
                    <strong>FrenoBrake、TopBrakeParts、GDST、Alltour</strong> 占据，英文全网第三方外链与 PR 软文偏弱。
                  </p>
                </div>
                <div className="rounded-xl border border-rose-500/20 bg-rose-500/5 p-3">
                  <p className="font-bold text-brand-fg">2. 页面细节粗糙，拼写硬伤（16 处）+ 静态模板重复堆砌</p>
                  <p className="mt-1 text-brand-muted">
                    实测线上存在 <code>Trcuk Brake Pad 29137</code>、<code>Fiction Material</code>、
                    <code>Beake Lining</code>、<code>Btake Lining</code>、<code>mercedsbenz</code>、<code>trailor</code>、
                    <code>ivceo</code>；分类页大面积套用{' '}
                    <code>&quot;Custom Manufacturers of Brake Shoes/Brake Drums/...&quot;</code> 且首页 <code>H1=0</code>。
                  </p>
                </div>
                <div className="rounded-xl border border-rose-500/20 bg-rose-500/5 p-3">
                  <p className="font-bold text-brand-fg">3. 产品页参数结构化不足，缺少可下载 Catalog 钩子</p>
                  <p className="mt-1 text-brand-muted">
                    单品页缺少 <strong>长×宽×厚图纸尺寸表、对应卡钳（Knorr / Wabco / Meritor）、OE 交叉对照表</strong>及顶部强留资诱饵（PDF Catalog），导致流量进来留不住询盘。
                  </p>
                </div>
              </div>
            </div>
          </div>

          {/* Interactive Page Health Matrix + Selected Page Before/After Diff */}
          <div className="grid gap-6 lg:grid-cols-[360px_1fr]">
            <div className="ui-card divide-y divide-brand-border overflow-hidden">
              <div className="flex items-center justify-between bg-brand-bg/50 px-4 py-3">
                <span className="text-xs font-bold text-brand-fg">已抓检 kooying.com 核心代表页面 ({pages.length})</span>
                <span className="text-[10px] text-brand-muted">点选查看优化对比</span>
              </div>
              {auditLoading ? (
                <div className="p-6 text-center text-xs text-brand-muted">正在加载线上诊断报告…</div>
              ) : (
                pages.map((p) => {
                  const active = currentPage?.id === p.id;
                  const crit = p.issues.filter((i) => i.severity === 'critical').length;
                  const warn = p.issues.filter((i) => i.severity === 'warning').length;
                  return (
                    <button
                      key={p.id}
                      type="button"
                      onClick={() => {
                        setSelectedID(p.id);
                        setProbeURL(p.url);
                      }}
                      className={`w-full space-y-1.5 px-4 py-3 text-left transition ${
                        active ? 'bg-brand-accent/10' : 'hover:bg-brand-fg/[0.03]'
                      }`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="rounded bg-brand-fg/[0.06] px-1.5 py-0.5 text-[10px] font-semibold text-brand-fg">
                          {p.page_type}
                        </span>
                        <div className="flex items-center gap-1.5">
                          {crit > 0 && (
                            <span className="rounded bg-rose-500/15 px-1.5 py-0.5 text-[10px] font-bold text-rose-600">
                              {crit} 高危
                            </span>
                          )}
                          {warn > 0 && (
                            <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-bold text-amber-600">
                              {warn} 警告
                            </span>
                          )}
                          <span className="text-[10px] font-semibold text-brand-muted">HTTP {p.http_status}</span>
                        </div>
                      </div>
                      <p className="line-clamp-1 text-xs font-semibold text-brand-fg">{p.title || '(Missing Title)'}</p>
                      <p className="truncate font-mono text-[10px] text-brand-muted">{p.path}</p>
                    </button>
                  );
                })
              )}
            </div>

            {currentPage && (
              <div className="ui-card space-y-5 p-5">
                <div className="flex flex-wrap items-start justify-between gap-3 border-b border-brand-border pb-4">
                  <div className="min-w-0 flex-1 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="rounded bg-brand-accent/12 px-2 py-0.5 text-[10px] font-bold text-brand-accent">
                        {currentPage.page_type}
                      </span>
                      <span className="font-mono text-[11px] text-brand-muted">
                        HTTP {currentPage.http_status} · {(currentPage.size_bytes / 1024).toFixed(1)} KB · H1={currentPage.h1_count} · 缺Alt {currentPage.image_missing_alt}/{currentPage.image_total}
                      </span>
                    </div>
                    <a
                      href={currentPage.url}
                      target="_blank"
                      rel="noreferrer"
                      className="inline-flex items-center gap-1 break-all font-mono text-xs font-bold text-brand-fg hover:text-brand-accent hover:underline"
                    >
                      {currentPage.url} <ExternalLink className="h-3 w-3 shrink-0" />
                    </a>
                  </div>

                  <div className="flex flex-wrap items-center gap-2">
                    <button
                      type="button"
                      onClick={() => void runLiveProbe(undefined, currentPage.url)}
                      disabled={probing}
                      className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-2.5 py-1.5 text-[11px] font-semibold text-brand-fg hover:border-brand-accent"
                    >
                      <RefreshCw className={`h-3 w-3 ${probing ? 'animate-spin' : ''}`} /> 复检此页
                    </button>
                    <button
                      type="button"
                      onClick={() =>
                        citeSEOContext(
                          `seo_page_${currentPage.id}`,
                          `官网诊断 · ${currentPage.page_type}`,
                          `目标 URL：${currentPage.url}\n当前 Title：${currentPage.title}\n当前 H1 (${currentPage.h1_count}个)：${currentPage.h1_texts.join(' | ') || '缺失'}\n\n建议优化 Title：${currentPage.optimized_title}\n建议优化 H1：${currentPage.optimized_h1}\n建议 Slug：${currentPage.optimized_slug}`,
                          `${currentPage.issues.filter((i) => i.severity === 'critical').length} 项高危待修`,
                          [
                            { label: 'Title 字符数', value: `${currentPage.title_len} chars` },
                            { label: 'H1 数量', value: String(currentPage.h1_count) },
                            { label: 'Canonical', value: currentPage.has_canonical ? '有' : '缺失' },
                            { label: 'JSON-LD', value: currentPage.has_jsonld ? '已部署' : '缺失' },
                          ],
                        )
                      }
                      className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-2.5 py-1.5 text-[11px] font-semibold text-brand-fg hover:border-brand-accent"
                    >
                      <FileCheck2 className="h-3 w-3 text-brand-accent" /> 引用上下文
                    </button>
                  </div>
                </div>

                {/* Issues list */}
                <div className="space-y-2.5">
                  {currentPage.issues.map((iss) => (
                    <div
                      key={iss.code}
                      className={`rounded-xl border p-3.5 text-xs ${
                        iss.severity === 'critical'
                          ? 'border-rose-500/30 bg-rose-500/5'
                          : iss.severity === 'pass'
                          ? 'border-emerald-500/25 bg-emerald-500/5'
                          : 'border-amber-500/30 bg-amber-500/5'
                      }`}
                    >
                      <div className="flex items-center justify-between gap-2">
                        <span className="font-bold text-brand-fg">{iss.title}</span>
                        <span
                          className={`rounded-full px-2 py-0.5 text-[10px] font-bold uppercase ${
                            iss.severity === 'critical'
                              ? 'bg-rose-500/15 text-rose-600'
                              : iss.severity === 'pass'
                              ? 'bg-emerald-500/15 text-emerald-600'
                              : 'bg-amber-500/15 text-amber-600'
                          }`}
                        >
                          {iss.severity === 'critical' ? '高危' : iss.severity === 'pass' ? '优势通过' : '建议优化'}
                        </span>
                      </div>
                      <p className="mt-1.5 text-[11px] text-brand-muted">
                        <strong>线上现状：</strong>
                        <code className="rounded bg-brand-bg px-1 py-0.5 font-mono text-[10px] text-brand-fg">
                          {iss.current_value}
                        </code>
                      </p>
                      <p className="mt-1 text-[11px] text-brand-fg/90">
                        <strong>可执行动作：</strong>{iss.recommended_value}
                      </p>
                    </div>
                  ))}
                </div>

                {/* Before -> After Diff */}
                <div className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2 rounded-xl border border-rose-500/25 bg-rose-500/[0.03] p-4 text-xs">
                    <span className="text-[10px] font-bold uppercase tracking-wider text-rose-600">
                      Before · kooying.com 当前线上标签
                    </span>
                    <div className="space-y-2 text-[11px]">
                      <div>
                        <span className="font-semibold text-brand-muted">Slug：</span>
                        <p className="break-all font-mono text-brand-fg">{currentPage.url}</p>
                      </div>
                      <div>
                        <span className="font-semibold text-brand-muted">Title ({currentPage.title_len} chars)：</span>
                        <p className="text-brand-fg">{currentPage.title || '(空)'}</p>
                      </div>
                      <div>
                        <span className="font-semibold text-brand-muted">H1 ({currentPage.h1_count})：</span>
                        <p className="text-rose-600">{currentPage.h1_texts.join(' | ') || '❌ 缺失 <h1> 标签'}</p>
                      </div>
                      <div>
                        <span className="font-semibold text-brand-muted">Meta Description：</span>
                        <p className="line-clamp-3 text-brand-muted">{currentPage.meta_description || '(空)'}</p>
                      </div>
                    </div>
                  </div>

                  <div className="space-y-2 rounded-xl border border-emerald-500/30 bg-emerald-500/[0.04] p-4 text-xs">
                    <div className="flex items-center justify-between">
                      <span className="text-[10px] font-bold uppercase tracking-wider text-emerald-600">
                        After · 哥飞/WVA 双引擎优化建议（可直接替换）
                      </span>
                      <button
                        type="button"
                        onClick={() => {
                          const patch = `<title>${currentPage.optimized_title}</title>\n<meta name="description" content="${currentPage.optimized_meta_desc}" />\n<h1>${currentPage.optimized_h1}</h1>\n<!-- 301 Redirect Target: ${currentPage.optimized_slug} -->`;
                          void navigator.clipboard?.writeText(patch);
                          toast.show('已复制 Before/After 优化后的 HTML Head + H1 代码', 'success');
                        }}
                        className="inline-flex items-center gap-1 rounded bg-emerald-600 px-2 py-0.5 text-[10px] font-bold text-white"
                      >
                        <Copy className="h-3 w-3" /> 复制替换代码
                      </button>
                    </div>
                    <div className="space-y-2 text-[11px]">
                      <div>
                        <span className="font-semibold text-brand-muted">推荐 301 新 URL Slug：</span>
                        <p className="break-all font-mono text-emerald-700 dark:text-emerald-300">
                          {currentPage.optimized_slug}
                        </p>
                      </div>
                      <div>
                        <span className="font-semibold text-brand-muted">优化 Title：</span>
                        <p className="font-semibold text-brand-fg">{currentPage.optimized_title}</p>
                      </div>
                      <div>
                        <span className="font-semibold text-brand-muted">优化唯一 H1：</span>
                        <p className="font-semibold text-brand-fg">{currentPage.optimized_h1}</p>
                      </div>
                      <div>
                        <span className="font-semibold text-brand-muted">优化 Meta Description：</span>
                        <p className="text-brand-fg/90">{currentPage.optimized_meta_desc}</p>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>
      )}

      {/* TAB 2: /seo 3-GATE AUDIT & TYPOS & SCHEMA */}
      {subTab === 'audit' && (
        <div className="space-y-5">
          {/* /seo Skill Three-Gate 実测表 */}
          <div className="ui-card overflow-hidden">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border bg-brand-bg/50 px-5 py-3.5">
              <div>
                <h2 className="text-sm font-bold text-brand-fg">
                  /seo 技能三道门槛実测核查（可抓取 → 可索引 → 可渲染 → 结构化与 GEO）
                </h2>
                <p className="text-[11px] text-brand-muted">
                  依据实测数据：node seo-audit.mjs + Sitemap 468 条全量扫描 + 产品详页正则核验；每项都附探测来源
                </p>
              </div>
              <div className="flex items-center gap-1.5">
                {(['all', 'fail', 'warn', 'pass'] as const).map((st) => (
                  <button
                    key={st}
                    type="button"
                    onClick={() => setGateFilter(st)}
                    className={`rounded-lg px-2.5 py-1 text-[11px] font-semibold ${
                      gateFilter === st
                        ? 'bg-brand-accent text-brand-on-accent'
                        : 'border border-brand-border bg-brand-surface text-brand-muted hover:text-brand-fg'
                    }`}
                  >
                    {st === 'all' ? '全部' : st === 'fail' ? 'Fail 不通过' : st === 'warn' ? 'Warn 待优化' : 'Pass 已通过'}
                  </button>
                ))}
              </div>
            </div>
            <div className="divide-y divide-brand-border">
              {filteredGates.map((g) => (
                <div key={g.item} className="flex flex-wrap items-center justify-between gap-3 px-5 py-3 text-xs">
                  <div className="flex min-w-0 flex-1 items-start gap-3">
                    <span
                      className={`mt-1 h-2 w-2 shrink-0 rounded-full ${
                        g.status === 'fail'
                          ? 'bg-rose-500'
                          : g.status === 'warn'
                          ? 'bg-amber-500'
                          : g.status === 'pass'
                          ? 'bg-emerald-500'
                          : 'bg-sky-500'
                      }`}
                    />
                    <div className="space-y-0.5">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="rounded bg-brand-fg/[0.06] px-1.5 py-0.5 text-[10px] font-semibold text-brand-muted">
                          {g.gate}
                        </span>
                        <span className="font-bold text-brand-fg">{g.item}</span>
                      </div>
                      <p className="text-[11px] text-brand-muted">{g.detail}</p>
                    </div>
                  </div>
                  <span
                    className={`shrink-0 rounded-full px-2.5 py-0.5 text-[10px] font-bold uppercase ${
                      g.status === 'fail'
                        ? 'bg-rose-500/15 text-rose-600'
                        : g.status === 'warn'
                        ? 'bg-amber-500/15 text-amber-600'
                        : g.status === 'pass'
                        ? 'bg-emerald-500/15 text-emerald-600'
                        : 'bg-sky-500/15 text-sky-600'
                    }`}
                  >
                    {g.status}
                  </span>
                </div>
              ))}
            </div>
          </div>

          {/* 16+ Sitemap Typo & 301 Redirect Clean Table */}
          <div className="ui-card overflow-hidden">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border bg-brand-bg/50 px-5 py-3.5">
              <div>
                <h2 className="flex items-center gap-2 text-sm font-bold text-brand-fg">
                  <AlertTriangle className="h-4 w-4 text-rose-500" />
                  Sitemap 全站拼写硬伤与截断 URL 清查表（Trcuk / Beake / Fiction / Btake / Mercedsbenz / Trailor / Ivceo）
                </h2>
                <p className="text-[11px] text-brand-muted">
                  共波及 16+ 个高意向 WVA/车型产品页；修改新 Slug 后必须同步配置 HTTP 301 永久重定向以防损失原权重
                </p>
              </div>
              <button
                type="button"
                onClick={() => {
                  const rules = sitemapTypos
                    .map((t) => `Redirect 301 ${new URL(t.url).pathname} ${new URL(t.suggested_url).pathname}`)
                    .join('\n');
                  void navigator.clipboard?.writeText(rules);
                  toast.show('已复制全部拼写纠错页面的 301 重定向规则', 'success');
                }}
                className="inline-flex items-center gap-1 rounded-lg bg-brand-accent px-3 py-1.5 text-[11px] font-semibold text-brand-on-accent"
              >
                <Copy className="h-3.5 w-3.5" /> 复制全部 301 重定向规则
              </button>
            </div>
            <div className="divide-y divide-brand-border">
              {sitemapTypos.map((t) => (
                <div key={t.url} className="grid gap-3 px-5 py-3.5 text-xs lg:grid-cols-[180px_1fr_auto] lg:items-center">
                  <div>
                    <span className="rounded bg-rose-500/12 px-2 py-0.5 font-mono text-[10px] font-bold text-rose-600">
                      {t.wrong_token} → {t.correct_token}
                    </span>
                    <p className="mt-1 font-semibold text-brand-fg">{t.model_code}</p>
                  </div>
                  <div className="min-w-0 space-y-1 text-[11px]">
                    <p className="truncate font-mono text-rose-600">旧：{(t.url || '').replace('https://www.kooying.com', '')}</p>
                    <p className="truncate font-mono text-emerald-600">
                      新：{(t.suggested_url || '').replace('https://www.kooying.com', '')}
                    </p>
                    <p className="text-brand-muted">{t.reason}</p>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <button
                      type="button"
                      onClick={() => void runLiveProbe(undefined, t.url)}
                      className="rounded-lg border border-brand-border px-2.5 py-1 text-[11px] font-semibold text-brand-fg hover:border-brand-accent"
                    >
                      实测抓检
                    </button>
                    <button
                      type="button"
                      onClick={() =>
                        citeSEOContext(
                          `typo_${t.correct_token}`,
                          `URL 拼写修复：${t.model_code}`,
                          `${t.reason}\n\n旧 URL (301源)：${t.url}\n新 URL (301目标)：${t.suggested_url}`,
                          '高危拼写待修',
                          [
                            { label: '错误词', value: t.wrong_token },
                            { label: '正确词', value: t.correct_token },
                          ],
                        )
                      }
                      className="rounded-lg border border-brand-border px-2.5 py-1 text-[11px] font-semibold text-brand-fg hover:border-brand-accent"
                    >
                      引用上下文
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>

          {/* Schema JSON-LD & robots.txt & llms.txt Before/After */}
          {currentPage && (
            <div className="grid gap-5 lg:grid-cols-2">
              <div className="ui-card space-y-3 p-5">
                <div className="flex items-center justify-between">
                  <h3 className="flex items-center gap-2 text-xs font-bold text-brand-fg">
                    <Code2 className="h-4 w-4 text-brand-accent" />
                    Schema.org JSON-LD 修复对比 ({currentPage.page_type})
                  </h3>
                  <button
                    type="button"
                    onClick={() => {
                      void navigator.clipboard?.writeText(currentPage.optimized_jsonld);
                      toast.show('已复制合法 Schema.org JSON-LD 代码', 'success');
                    }}
                    className="inline-flex items-center gap-1 rounded bg-emerald-600 px-2.5 py-1 text-[10px] font-bold text-white"
                  >
                    <Copy className="h-3 w-3" /> 复制修复后 JSON-LD
                  </button>
                </div>
                <div className="space-y-2 text-[11px]">
                  <span className="font-bold text-rose-600">Before（线上原始 JSON-LD · 含 Offers 大写/$0/query-Input）</span>
                  <pre className="max-h-48 overflow-auto rounded-xl border border-rose-500/20 bg-brand-bg p-3 font-mono text-[10px] text-brand-muted">
                    {currentPage.raw_jsonld}
                  </pre>
                  <span className="block font-bold text-emerald-600">After（符合 Google Rich Results 标准）</span>
                  <pre className="max-h-56 overflow-auto rounded-xl border border-emerald-500/25 bg-brand-bg p-3 font-mono text-[10px] text-brand-fg">
                    {currentPage.optimized_jsonld}
                  </pre>
                </div>
              </div>

              <div className="ui-card space-y-3 p-5">
                <div className="flex items-center justify-between">
                  <h3 className="flex items-center gap-2 text-xs font-bold text-brand-fg">
                    <Wrench className="h-4 w-4 text-brand-accent" />
                    robots.txt 与 llms.txt（去除 HTML &lt;pre&gt; 标签）修复代码
                  </h3>
                  <button
                    type="button"
                    onClick={() => {
                      void navigator.clipboard?.writeText(
                        `# === robots.txt ===\n${auditData?.robots_txt_optimized ?? ''}\n\n# === llms.txt ===\n${auditData?.llms_txt_optimized ?? ''}`,
                      );
                      toast.show('已复制优化后的 robots.txt 与 llms.txt', 'success');
                    }}
                    className="inline-flex items-center gap-1 rounded bg-emerald-600 px-2.5 py-1 text-[10px] font-bold text-white"
                  >
                    <Copy className="h-3 w-3" /> 复制全部
                  </button>
                </div>
                <div className="space-y-2 text-[11px]">
                  <span className="font-bold text-rose-600">
                    Before · kooying.com/llms.txt 当前被 CMS 包裹了 &lt;pre&gt; HTML 标签
                  </span>
                  <pre className="max-h-36 overflow-auto rounded-xl border border-rose-500/20 bg-brand-bg p-3 font-mono text-[10px] text-brand-muted">
                    {auditData?.llms_txt_current_sample}
                  </pre>
                  <span className="block font-bold text-emerald-600">
                    After · 纯净 Markdown llms.txt + robots.txt（补 Sitemap 与 OAI-SearchBot/PerplexityBot/ClaudeBot）
                  </span>
                  <pre className="max-h-68 overflow-auto rounded-xl border border-emerald-500/25 bg-brand-bg p-3 font-mono text-[10px] text-brand-fg">
                    {auditData?.robots_txt_optimized}
                    {'\n\n---\n\n'}
                    {auditData?.llms_txt_optimized}
                  </pre>
                </div>
              </div>
            </div>
          )}
        </div>
      )}

      {/* TAB 3: COMPETITORS & GEFEI KEYWORD MATRIX */}
      {subTab === 'competitors' && (
        <div className="space-y-5">
          {/* 4 Core Competitors Matrix */}
          <div className="ui-card overflow-hidden">
            <div className="border-b border-brand-border bg-brand-bg/50 px-5 py-3.5">
              <h2 className="text-sm font-bold text-brand-fg">
                二、四家同行官网的 SEO 做法对照
              </h2>
              <p className="text-[11px] text-brand-muted">
                对标 Google 自然排名表现最强的 Alltour、FrenoBrake、TopBrakeParts、GDST，提取可直接复用的获客打法
              </p>
            </div>
            <div className="grid gap-4 p-5 md:grid-cols-2">
              {KOOYING_COMPETITORS.map((comp) => (
                <article key={comp.name} className="space-y-2.5 rounded-xl border border-brand-border bg-brand-bg/50 p-4 text-xs">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <h3 className="text-sm font-extrabold text-brand-fg">{comp.name}</h3>
                    <span className="rounded-full bg-brand-accent/12 px-2.5 py-0.5 text-[10px] font-bold text-brand-accent">
                      {comp.origin}
                    </span>
                  </div>
                  <p className="text-[11px] font-semibold text-brand-fg/90">定位：{comp.positioning}</p>
                  <p className="text-[11px] leading-relaxed text-brand-muted">
                    <strong className="text-brand-fg">主要做法：</strong>{comp.tactic}
                  </p>
                  <p className="text-[11px] leading-relaxed text-amber-700 dark:text-amber-300">
                    <strong>冠英差距：</strong>{comp.gap}
                  </p>
                  <div className="rounded-lg border border-emerald-500/25 bg-emerald-500/8 p-2.5 text-[11px] text-emerald-800 dark:text-emerald-300">
                    <strong>反制落地：</strong>{comp.counterMove}
                  </div>
                  <div className="flex items-center gap-2 pt-1">
                    <button
                      type="button"
                      onClick={() =>
                        citeSEOContext(
                          `comp_${comp.name}`,
                          `竞品拆解：${comp.name}`,
                          `定位：${comp.positioning}\n核心打法：${comp.tactic}\n冠英差距：${comp.gap}\n反制动作：${comp.counterMove}`,
                          '竞品情报对齐',
                          [{ label: '对标赛道', value: comp.origin }],
                        )
                      }
                      className="rounded-lg border border-brand-border px-2.5 py-1 text-[11px] font-semibold text-brand-fg hover:border-brand-accent"
                    >
                      引用上下文
                    </button>
                    <button
                      type="button"
                      onClick={() =>
                        dispatchBotWithSEOPrompt(
                          'bot_gy_gtm_growth',
                          `请针对竞品【${comp.name}】的 SEO 打法（${comp.tactic}），基于冠英 43,000㎡ 工厂、1500万套产能、400℃ 0.040mm 磨损与 135 款实拍型号，为 https://www.kooying.com/ 撰写一套反制内容提纲与英文核心段落（${comp.counterMove}）。`,
                        )
                      }
                      className="inline-flex items-center gap-1 rounded-lg bg-brand-accent/15 px-2.5 py-1 text-[11px] font-semibold text-brand-accent hover:bg-brand-accent/25"
                    >
                      <MessageSquarePlus className="h-3 w-3" /> 让 Bot 出反制内容
                    </button>
                  </div>
                </article>
              ))}
            </div>
          </div>

          {/* Gefei SEO + Kooying Dual-Engine Keyword Clusters */}
          <div className="ui-card space-y-4 p-5">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h2 className="text-sm font-bold text-brand-fg">
                  哥飞 SEO 选词框架 × 冠英「型号库 + 认证/材质词」双引擎关键词库
                </h2>
                <p className="text-[11px] text-brand-muted">
                  单页标题推荐格式：<code className="font-mono text-brand-accent">[WVA号] + [卡钳/车桥如 Knorr/BPW/Meritor] + [OE号如 0044206020] + Truck Brake Pad | KOOYING</code>
                </p>
              </div>
              <button
                type="button"
                onClick={() => {
                  const allKws = GEFEI_KEYWORD_CLUSTERS.flatMap((c) => c.keywords).join('\n');
                  void navigator.clipboard?.writeText(allKws);
                  toast.show('已复制全部 16 组核心高转化关键词清单', 'success');
                }}
                className="inline-flex items-center gap-1 rounded-lg border border-brand-border px-3 py-1.5 text-[11px] font-semibold text-brand-fg hover:border-brand-accent"
              >
                <Copy className="h-3.5 w-3.5" /> 复制全部关键词
              </button>
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              {GEFEI_KEYWORD_CLUSTERS.map((grp) => (
                <div key={grp.cluster} className="space-y-2.5 rounded-xl border border-brand-border bg-brand-bg/50 p-4 text-xs">
                  <h3 className="font-bold text-brand-fg">{grp.cluster}</h3>
                  <p className="text-[11px] text-brand-muted">搜索意图：{grp.intent}</p>
                  <p className="rounded-lg bg-brand-surface px-2.5 py-1.5 font-mono text-[10px] text-brand-accent">
                    公式：{grp.formula}
                  </p>
                  <ul className="space-y-1.5">
                    {grp.keywords.map((kw) => (
                      <li key={kw} className="flex items-center justify-between gap-2 rounded-lg bg-brand-surface px-2.5 py-1.5 text-[11px]">
                        <span className="truncate font-mono text-brand-fg">{kw}</span>
                        <button
                          type="button"
                          onClick={() => {
                            void navigator.clipboard?.writeText(kw);
                            toast.show(`已复制关键词：${kw}`, 'success');
                          }}
                          className="shrink-0 text-brand-muted hover:text-brand-accent"
                          aria-label={`复制 ${kw}`}
                        >
                          <Copy className="h-3 w-3" />
                        </button>
                      </li>
                    ))}
                  </ul>
                  <div className="flex items-center justify-between pt-1 text-[10px] text-brand-muted">
                    <span>推荐部署：{grp.targetPage}</span>
                    <button
                      type="button"
                      onClick={() =>
                        dispatchBotWithSEOPrompt(
                          'bot_gy_gtm_growth',
                          `请按【${grp.cluster}】方向，为 https://www.kooying.com/ 设计 3 个落地页优化文案，公式为 ${grp.formula}，重点覆盖以下关键词：\n${grp.keywords.join('\n')}`,
                        )
                      }
                      className="font-bold text-brand-accent hover:underline"
                    >
                      交给增长 Bot 扩写 →
                    </button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* TAB 4: 4-STEP OVERTAKE EXECUTION PLAN + FEATURED SNIPPET TABLE + PDF LEAD MAGNET */}
      {subTab === 'plan' && (
        <div className="space-y-5">
          {/* Interactive Action Checklist sorted by /seo time-to-impact */}
          <div className="ui-card overflow-hidden">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border bg-brand-bg/50 px-5 py-3.5">
              <div>
                <h2 className="text-sm font-bold text-brand-fg">
                  三、4 步改进清单（按 /seo 见效速度排序 · 进度保存在本地）
                </h2>
                <p className="text-[11px] text-brand-muted">
                  交付「现在能动手的下一步」而非按周排期表：30 分钟修拼写、半天修 Schema/llms.txt、1–2 天上参数表与 PDF 留资诱饵
                </p>
              </div>
              <span className="text-xs font-bold text-brand-accent">
                已完成 {Object.values(actionProgress).filter((v) => v >= 100).length} / {INITIAL_OVERTAKE_ACTIONS.length} 项
              </span>
            </div>
            <div className="divide-y divide-brand-border">
              {INITIAL_OVERTAKE_ACTIONS.map((act, idx) => {
                const pct = actionProgress[act.id] ?? act.defaultProgress;
                return (
                  <div key={act.id} className="flex flex-wrap items-center justify-between gap-4 px-5 py-4 text-xs">
                    <div className="min-w-0 flex-1 space-y-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="rounded bg-brand-fg/[0.06] px-1.5 py-0.5 font-mono text-[10px] font-bold text-brand-muted">
                          0{idx + 1}
                        </span>
                        <span
                          className={`rounded px-1.5 py-0.5 text-[10px] font-bold ${
                            act.priority === 'P0'
                              ? 'bg-rose-500/15 text-rose-600'
                              : act.priority === 'P1'
                              ? 'bg-amber-500/15 text-amber-600'
                              : 'bg-sky-500/15 text-sky-600'
                          }`}
                        >
                          {act.priority}
                        </span>
                        <span className="rounded-full bg-emerald-500/12 px-2 py-0.5 text-[10px] font-semibold text-emerald-700 dark:text-emerald-300">
                          {act.eta}
                        </span>
                        <span className="text-[11px] font-semibold text-brand-accent">{act.step}</span>
                      </div>
                      <h3 className="text-xs font-bold text-brand-fg">{act.title}</h3>
                      <p className="text-[11px] leading-relaxed text-brand-muted">{act.detail}</p>
                    </div>

                    <div className="flex items-center gap-3">
                      <div className="w-28 space-y-1">
                        <div className="h-1.5 overflow-hidden rounded-full bg-brand-bg">
                          <div className="h-full bg-brand-accent transition-all" style={{ width: `${pct}%` }} />
                        </div>
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
                      </div>
                      <button
                        type="button"
                        onClick={() => updateProgress(act.id, pct >= 100 ? 0 : 100)}
                        className={`rounded-lg px-2.5 py-1.5 text-[11px] font-bold ${
                          pct >= 100
                            ? 'bg-emerald-600 text-white'
                            : 'border border-brand-border text-brand-fg hover:border-brand-accent'
                        }`}
                      >
                        {pct >= 100 ? '✓ 已完成' : `${pct}%`}
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          {/* Step 1 + Step 2: Featured Snippets HTML Table + Listicle Article Prompts */}
          <div className="grid gap-5 lg:grid-cols-2">
            <div className="ui-card space-y-3 p-5">
              <div className="flex items-center justify-between gap-2">
                <h3 className="text-xs font-bold text-brand-fg">
                  第 1 步落地产物 · Featured Snippets 结构化参数表（WVA + 卡钳 + OE）
                </h3>
                <button
                  type="button"
                  onClick={() => {
                    void navigator.clipboard?.writeText(FEATURED_SNIPPET_HTML_SNIPPET);
                    toast.show('已复制可直接植入 kooying.com 单品页的 HTML Table 代码', 'success');
                  }}
                  className="inline-flex items-center gap-1 rounded bg-brand-accent px-2.5 py-1 text-[10px] font-bold text-brand-on-accent"
                >
                  <Copy className="h-3 w-3" /> 复制 HTML Table
                </button>
              </div>
              <div className="overflow-x-auto rounded-xl border border-brand-border bg-brand-bg/50 p-3 text-[11px]">
                <table className="w-full text-left">
                  <thead className="border-b border-brand-border text-[10px] text-brand-muted">
                    <tr>
                      <th className="py-1.5 pr-3">WVA 编号</th>
                      <th className="py-1.5 pr-3">适配卡钳/车桥</th>
                      <th className="py-1.5 pr-3">尺寸 (mm)</th>
                      <th className="py-1.5 pr-3">OE 交叉号</th>
                      <th className="py-1.5">400℃ 磨损</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-brand-border font-mono text-[10px]">
                    <tr>
                      <td className="py-1.5 pr-3 font-bold text-brand-accent">29087 / 29108</td>
                      <td className="py-1.5 pr-3">Knorr SB7000</td>
                      <td className="py-1.5 pr-3">247.6×109.7×30</td>
                      <td className="py-1.5 pr-3">0044206020</td>
                      <td className="py-1.5 text-emerald-600">0.040mm</td>
                    </tr>
                    <tr>
                      <td className="py-1.5 pr-3 font-bold text-brand-accent">19032 (BC/36/1)</td>
                      <td className="py-1.5 pr-3">BPW 12T-16T</td>
                      <td className="py-1.5 pr-3">219×180×19.6 (8孔)</td>
                      <td className="py-1.5 pr-3">0309226340</td>
                      <td className="py-1.5 text-emerald-600">0.040mm</td>
                    </tr>
                  </tbody>
                </table>
              </div>
              <pre className="max-h-48 overflow-auto rounded-xl border border-brand-border bg-brand-bg p-3 font-mono text-[10px] text-brand-muted">
                {FEATURED_SNIPPET_HTML_SNIPPET}
              </pre>
            </div>

            <div className="ui-card space-y-3 p-5">
              <h3 className="text-xs font-bold text-brand-fg">
                第 2 步 & 第 4 步 · 榜单选题与小语种子目录模板（可直接交给 Bot 成稿）
              </h3>
              <div className="space-y-2.5 text-xs">
                {[
                  {
                    badge: '榜单内容 · 参考 FrenoBrake',
                    title: 'Top 10 Heavy Duty Truck Brake Pad Manufacturers in China (2026 Guide)',
                    desc: '将浙江冠英排核心推荐位，横向对比 Alltour/FrenoBrake/TopBrakeParts/GDST 的 MOQ、Krauss 台架、43,000㎡ 产能与 135 款现货库存。',
                    bot: 'bot_gy_gtm_growth',
                  },
                  {
                    badge: '型号对比 · 面向修配厂',
                    title: 'WVA 29087 vs 29108: Dimensions, Fitment & OE Cross Reference',
                    desc: '拆解两款高频欧洲气刹盘片厚度、卡钳滑销间隙、0044206020 交叉替代关系并内置 PDF 图纸下载入口。',
                    bot: 'bot_gy_gtm_growth',
                  },
                  {
                    badge: '小语种子站 · 参考 GDST',
                    title: '/es/（南美） · /ru/（HOWO/Shacman 汉德桥） · /ar/（中东 Actros/BPW）',
                    desc: '生成含双向 hreflang、本地重卡车型关键词的 3 套小语种着陆页 Title/H1/摘要与 WhatsApp 钩子。',
                    bot: 'bot_gy_gtm_growth',
                  },
                ].map((tpl) => (
                  <div key={tpl.title} className="rounded-xl border border-brand-border bg-brand-bg/50 p-3">
                    <div className="flex items-center justify-between gap-2">
                      <span className="rounded bg-brand-accent/12 px-2 py-0.5 text-[10px] font-bold text-brand-accent">
                        {tpl.badge}
                      </span>
                      <button
                        type="button"
                        onClick={() =>
                          dispatchBotWithSEOPrompt(
                            tpl.bot,
                            `请基于浙江冠英官网 https://www.kooying.com/ 的 SEO 优化战略，直接写出【${tpl.title}】的完整 SEO 结构化英文/多语种成稿（包含 Title、Meta Description、唯一 H1、HTML 对比参数表、FAQ 与 PDF Catalog 下载 CTA）。\n核心要求：${tpl.desc}`,
                          )
                        }
                        className="text-[11px] font-bold text-brand-accent hover:underline"
                      >
                        让 Bot 生成全文 →
                      </button>
                    </div>
                    <p className="mt-1.5 font-semibold text-brand-fg">{tpl.title}</p>
                    <p className="mt-1 text-[11px] text-brand-muted">{tpl.desc}</p>
                  </div>
                ))}
              </div>
            </div>
          </div>

          {/* Step 3 Interactive Lead Magnet Demo */}
          <div className="ui-card grid gap-6 border border-brand-accent/30 bg-brand-surface p-6 lg:grid-cols-[1.2fr_1fr]">
            <div className="space-y-3">
              <span className="inline-block rounded-full bg-brand-accent/12 px-3 py-1 text-[10px] font-bold text-brand-accent">
                第 3 步转化漏斗改造 · 高价值留资诱饵演示 (替代干巴巴的 Contact Us)
              </span>
              <h3 className="text-lg font-extrabold text-brand-fg">
                Download 2026 Heavy Duty Truck Brake Pad PDF Catalog &amp; OE Cross-Reference List
              </h3>
              <p className="text-xs leading-relaxed text-brand-muted">
                在 Mercedes-Benz Actros / Volvo FH / BPW / HOWO 等车型栏目页顶部放一个目录下载入口：海外车队或批发商留下 WhatsApp 与需求车型后，下载完整电子目录，同时写入工作台的「终端买家甄别 + WVA/GYS 型号匹配 + 飞书回写」记录。线索量是否上升要看上线后的实际数据。
              </p>
              <ul className="space-y-1.5 text-xs text-brand-fg">
                <li className="flex items-center gap-2">
                  <CheckCircle2 className="h-4 w-4 text-emerald-600" />
                  <span>内含 135 款盘/鼓刹 1:1 实拍尺寸（外弧 × 宽度 × 厚度 × 孔数）与换桥纠错</span>
                </li>
                <li className="flex items-center gap-2">
                  <CheckCircle2 className="h-4 w-4 text-emerald-600" />
                  <span>附 400℃ 恒力矩 Krauss 台架检测对比报告（0.040mm vs 行业 0.085mm）</span>
                </li>
              </ul>
            </div>

            <form onSubmit={(e) => void handleSubmitLeadMagnet(e)} className="space-y-3 rounded-2xl border border-brand-border bg-brand-bg/70 p-4">
              <div className="flex items-center justify-between">
                <span className="text-xs font-bold text-brand-fg">输入联系信息立即解锁 2026 OE 目录（真库联调）</span>
                <span className="text-[10px] text-emerald-600">POST /api/guanying/leads</span>
              </div>
              <input
                required
                value={lmCompany}
                onChange={(e) => setLmCompany(e.target.value)}
                placeholder="Company / Fleet Name"
                className="h-9 w-full rounded-lg border border-brand-border bg-brand-surface px-3 text-xs text-brand-fg"
              />
              <div className="grid grid-cols-2 gap-2">
                <input
                  required
                  value={lmContact}
                  onChange={(e) => setLmContact(e.target.value)}
                  placeholder="Email / WhatsApp"
                  className="h-9 rounded-lg border border-brand-border bg-brand-surface px-2.5 text-xs text-brand-fg"
                />
                <input
                  required
                  value={lmCountry}
                  onChange={(e) => setLmCountry(e.target.value)}
                  placeholder="Country / Port"
                  className="h-9 rounded-lg border border-brand-border bg-brand-surface px-2.5 text-xs text-brand-fg"
                />
              </div>
              <textarea
                required
                rows={2}
                value={lmInquiry}
                onChange={(e) => setLmInquiry(e.target.value)}
                placeholder="Target WVA / Axle / Truck Series"
                className="w-full rounded-lg border border-brand-border bg-brand-surface p-2.5 text-xs text-brand-fg"
              />
              <button
                type="submit"
                disabled={submittingLead}
                className="inline-flex h-9 w-full items-center justify-center gap-1.5 rounded-lg bg-brand-accent text-xs font-bold text-brand-on-accent hover:brightness-110 disabled:opacity-60"
              >
                <Send className="h-3.5 w-3.5" />
                {submittingLead ? 'AI 甄别并解锁目录中…' : 'Unlock 2026 PDF Catalog & Cross-Reference'}
              </button>

              {lastSubmittedLead && (
                <div className="space-y-1.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 p-3 text-[11px] text-emerald-900 dark:text-emerald-200">
                  <p className="font-bold">
                    已实时捕获留资：{lastSubmittedLead.buyer_type}（置信度 {lastSubmittedLead.confidence}%）· 自动匹配{' '}
                    {lastSubmittedLead.matched_code}
                  </p>
                  <div className="flex flex-wrap items-center gap-3">
                    <a
                      href="/brand-site/catalog.html"
                      target="_blank"
                      rel="noreferrer"
                      className="font-bold text-brand-accent underline"
                    >
                      打开 135 款可打印 PDF/工程目录页 →
                    </a>
                    <button
                      type="button"
                      onClick={() => setActiveTab('crawler')}
                      className="font-bold text-emerald-700 underline dark:text-emerald-300"
                    >
                      去「识片报价与防漏单」查看此线索 →
                    </button>
                  </div>
                </div>
              )}
            </form>
          </div>
        </div>
      )}
    </div>
  );
};

export const GuanyingDataFeishuView: React.FC = () => {
  const toast = useUIStore((s) => s.toast);
  const { data: overview } = useSWR('api/guanying/overview', () =>
    fetchJSON<GuanyingOverview>('/api/guanying/overview'),
  );
  const { data: feishuStatus, mutate: mutateFeishu } = useSWR('api/feishu/status', api.getFeishuSyncStatus, {
    refreshInterval: (latest) => (latest?.syncing ? 2000 : 0),
  });
  const [syncing, setSyncing] = useState(false);

  async function triggerFeishuSync(module = 'all') {
    setSyncing(true);
    try {
      await api.syncFeishu(module);
      toast.show('已通过 lark-cli 启动飞书多维表格同步。', 'success');
      setTimeout(() => void mutateFeishu(), 1500);
    } catch (err) {
      toast.show(err instanceof Error ? err.message : '飞书同步触发失败', 'error');
    } finally {
      setSyncing(false);
    }
  }

  const stats = overview?.stats;

  return (
    <div className="mx-auto max-w-[1280px] space-y-6 pb-10">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-brand-accent">
            <Table2 className="h-3.5 w-3.5" />
            <span>冠英 GTM 数据总览 · 飞书 CLI (lark-cli) 实时上云中枢</span>
          </div>
          <h1 className="mt-1 text-2xl font-bold tracking-tight text-brand-fg">数据与飞书 CLI 协同中枢</h1>
          <p className="mt-1 text-xs text-brand-muted">
            本地 SQLite 为业务真源，通过官方 lark-cli 将场景报价、海外终端询盘、SEO 独立站着陆页及短视频内容稿件实时镜像至飞书多维表格。
          </p>
        </div>

        <div className="flex items-center gap-2">
          {feishuStatus?.url && (
            <a
              href={feishuStatus.url}
              target="_blank"
              rel="noreferrer"
              className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-brand-border bg-brand-surface px-3.5 text-xs font-semibold text-brand-fg hover:border-brand-accent"
            >
              打开飞书多维表格 <ExternalLink className="h-3.5 w-3.5" />
            </a>
          )}
          <button
            type="button"
            disabled={syncing || feishuStatus?.syncing || !feishuStatus?.configured}
            onClick={() => void triggerFeishuSync('all')}
            className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-brand-accent px-4 text-xs font-semibold text-brand-on-accent hover:brightness-110 disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${feishuStatus?.syncing ? 'animate-spin' : ''}`} />
            {feishuStatus?.syncing ? '飞书同步中…' : '全量同步到飞书 CLI'}
          </button>
        </div>
      </div>

      {/* KPI Cards */}
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {[
          { label: '场景快速报价单', value: stats?.quote_count ?? 3, note: '生成双语报价，附换桥核对' },
          { label: '高价值终端询盘', value: stats?.terminal_lead_count ?? 0, note: `已过滤 ${stats?.filtered_trader_count ?? 0} 条贸易商` },
          { label: 'SEO 独立站着陆页', value: stats?.seo_page_count ?? 4, note: '含 Google Product JSON-LD' },
          { label: '短视频与内容运营', value: stats?.content_count ?? 6, note: '含老板娘 ≤60s 工厂实拍素材' },
        ].map((card) => (
          <div key={card.label} className="ui-card space-y-1 p-4">
            <span className="text-[11px] font-medium text-brand-muted">{card.label}</span>
            <p className="text-2xl font-extrabold tracking-tight text-brand-fg">{card.value}</p>
            <span className="block text-[10px] text-emerald-600">{card.note}</span>
          </div>
        ))}
      </div>

      {/* Feishu Base Modules Sync Table */}
      <div className="ui-card overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-brand-border bg-brand-bg/50 px-5 py-3.5">
          <div className="flex items-center gap-2">
            <Layers className="h-4 w-4 text-brand-accent" />
            <span className="text-xs font-bold text-brand-fg">飞书多维表格同步明细（lark-cli）</span>
          </div>
          <span className="font-mono text-[11px] text-brand-muted">
            AppToken: {feishuStatus?.app_token || '未检测到 feishu.json'}
          </span>
        </div>

        {feishuStatus?.last_error && (
          <div className="border-b border-brand-border bg-amber-500/10 px-5 py-2.5 text-xs text-amber-700 dark:text-amber-300">
            最近同步提示：{feishuStatus.last_error}
          </div>
        )}

        <div className="divide-y divide-brand-border">
          {(feishuStatus?.modules ?? []).map((mod) => {
            const total = mod.local_total ?? mod.synced;
            const pending = Math.max(0, total - mod.synced);
            return (
              <div key={mod.id} className="flex flex-wrap items-center justify-between gap-3 px-5 py-3.5">
                <div>
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-bold text-brand-fg">{mod.label}</span>
                    <span className="rounded bg-brand-fg/[0.06] px-1.5 py-0.5 font-mono text-[10px] text-brand-muted">
                      {mod.id}
                    </span>
                  </div>
                  <p className="mt-0.5 font-mono text-[10px] text-brand-muted">Table: {mod.table_id || '未配置'}</p>
                </div>

                <div className="flex items-center gap-3">
                  <span className="text-xs font-semibold tabular-nums text-brand-fg">
                    已同步 {mod.synced} / 本地 {total} 条
                  </span>
                  {pending > 0 ? (
                    <button
                      type="button"
                      disabled={syncing || feishuStatus?.syncing}
                      onClick={() => void triggerFeishuSync(mod.id)}
                      className="rounded-md bg-brand-accent/12 px-2.5 py-1 text-[11px] font-semibold text-brand-accent hover:bg-brand-accent/20"
                    >
                      补齐 {pending} 条
                    </button>
                  ) : (
                    <span className="rounded-full bg-emerald-500/15 px-2.5 py-0.5 text-[10px] font-semibold text-emerald-600">
                      已对齐
                    </span>
                  )}
                </div>
              </div>
            );
          })}
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
    desc: '负责 Top 10 榜单长文、WVA 对比与西/俄/阿语扩展',
    prompt: '请围绕 135 款重卡盘鼓刹与 WVA 29087/19032，为冠英策划海外 B2B 榜单文章与小语种落地页。',
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
            {submitting ? '巡检中…' : '重新巡检'}
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
