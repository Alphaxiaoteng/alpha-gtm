import React, { useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  ArrowRight,
  CheckCircle2,
  Copy,
  Database,
  ExternalLink,
  FileCheck2,
  Filter,
  Globe,
  Layers,
  MessageSquarePlus,
  RefreshCw,
  Search,
  Send,
  ShieldCheck,
  Sparkles,
  Table2,
} from 'lucide-react';
import { api } from '../api';
import { useUIStore } from '../store';

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

const PRESET_SCENARIOS = [
  { label: '东风 EQ153 / 换桥防错', query: 'EQ153', outer: '', width: '', holes: '' },
  { label: 'WVA 29087 盘刹报价', query: '29087', outer: '', width: '', holes: '' },
  { label: '汉德 35T (240×220·20孔)', query: '', outer: '240', width: '220', holes: '20' },
  { label: 'HOWO 14孔 vs 8孔核对', query: 'HOWO', outer: '', width: '220', holes: '' },
  { label: 'BPW 挂车桥 (19032/307AF)', query: 'BPW', outer: '', width: '', holes: '' },
];

export const GuanyingDatabaseView: React.FC = () => {
  const toast = useUIStore((s) => s.toast);
  const setActiveTab = useUIStore((s) => s.setActiveTab);
  const setAgentDraft = useUIStore((s) => s.setAgentDraft);

  const { data: overview, mutate } = useSWR('api/guanying/overview', () =>
    fetchJSON<GuanyingOverview>('/api/guanying/overview'),
  );

  const [subTab, setSubTab] = useState<'catalog' | 'leads'>('catalog');
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

  async function generateQuote(opts: { productID?: string; leadID?: string; queryText?: string; save?: boolean }) {
    setMatching(true);
    try {
      const resp = await fetchJSON<{ quote?: GuanyingQuote; matches: Array<{ product: GuanyingProduct }> }>(
        '/api/guanying/match-quote',
        {
          method: 'POST',
          body: JSON.stringify({
            query: opts.queryText ?? query,
            outer_arc_mm: Number(outerArc) || 0,
            width_mm: Number(widthMM) || 0,
            holes: Number(holes) || 0,
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
          toast.show(`已生成并保存报价单：${resp.quote.product_code}`, 'success');
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
            <Database className="h-3.5 w-3.5" />
            <span>1:1 官方 PDF 目录结构化数据库 · 浙江冠英 (Kooying / GOOLE)</span>
          </div>
          <h1 className="mt-1 text-2xl font-bold tracking-tight text-brand-fg">
            产品与选型数据库
          </h1>
          <p className="mt-1 text-xs text-brand-muted">
            覆盖 Kooying 英文册、冠隆产品册及 2026 企业台架报告；解决无 OE 编码、后市场换桥与 2 小时报价痛点。
          </p>
        </div>

        <div className="flex items-center gap-2 rounded-xl border border-brand-border bg-brand-surface p-1">
          <button
            type="button"
            onClick={() => setSubTab('catalog')}
            className={`rounded-lg px-3.5 py-1.5 text-xs font-semibold transition ${
              subTab === 'catalog' ? 'bg-brand-accent text-brand-on-accent shadow-xs' : 'text-brand-muted hover:text-brand-fg'
            }`}
          >
            刹车片与车桥库 ({products.length})
          </button>
          <button
            type="button"
            onClick={() => setSubTab('leads')}
            className={`rounded-lg px-3.5 py-1.5 text-xs font-semibold transition ${
              subTab === 'leads' ? 'bg-brand-accent text-brand-on-accent shadow-xs' : 'text-brand-muted hover:text-brand-fg'
            }`}
          >
            海外终端询盘库 ({overview?.stats.terminal_lead_count ?? 0})
          </button>
        </div>
      </div>

      {subTab === 'catalog' ? (
        <>
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
                <span className="text-[11px] font-medium text-brand-muted">高频演示场景：</span>
                {PRESET_SCENARIOS.map((sc) => (
                  <button
                    key={sc.label}
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
                    {sc.label}
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
      ) : (
        /* Leads Database Sub-tab */
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
              去 SEO 独立站提交一条新询盘演示 <ArrowRight className="h-3.5 w-3.5" />
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

export const GuanyingSeoBuilderView: React.FC = () => {
  const toast = useUIStore((s) => s.toast);
  const setActiveTab = useUIStore((s) => s.setActiveTab);

  const { data: overview, mutate } = useSWR('api/guanying/overview', () =>
    fetchJSON<GuanyingOverview>('/api/guanying/overview'),
  );
  const pages = overview?.seo_pages ?? [];
  const [selectedID, setSelectedID] = useState('');
  const [viewMode, setViewMode] = useState<'preview' | 'schema'>('preview');

  // Create new SEO page form
  const [seriesInput, setSeriesInput] = useState('BPW & Hande 35T Mining Brake Linings');
  const [marketInput, setMarketInput] = useState('南美 / 中亚矿山车队');
  const [langInput, setLangInput] = useState('English / Español');
  const [creating, setCreating] = useState(false);

  // Live buyer inquiry form inside preview
  const [buyerCompany, setBuyerCompany] = useState('Desert Haulage Logistics Co.');
  const [buyerCountry, setBuyerCountry] = useState('沙特阿拉伯 (Jeddah)');
  const [buyerInquiry, setBuyerInquiry] = useState('We need 600 sets of WVA 29087 disc brake pads and HOWO 14-hole GYS-126 rear brake linings. Please send 400C wear data and FOB Ningbo quote.');
  const [submittingLead, setSubmittingLead] = useState(false);
  const [lastSubmittedLead, setLastSubmittedLead] = useState<GuanyingLead | null>(null);

  const currentPage = pages.find((p) => p.id === selectedID) ?? pages[0] ?? null;
  const highlights: string[] = useMemo(() => {
    if (!currentPage) return [];
    try {
      return JSON.parse(currentPage.highlights_json) as string[];
    } catch {
      return [];
    }
  }, [currentPage]);

  async function handleGeneratePage(e: React.FormEvent) {
    e.preventDefault();
    setCreating(true);
    try {
      const res = await fetchJSON<{ seo_page: GuanyingSEOPage }>('/api/guanying/seo-pages', {
        method: 'POST',
        body: JSON.stringify({
          series_focus: seriesInput,
          target_market: marketInput,
          lang: langInput,
        }),
      });
      await mutate();
      setSelectedID(res.seo_page.id);
      toast.show(`已生成多语种 SEO 独立站着陆页：${res.seo_page.slug}`, 'success');
    } catch (err) {
      toast.show(err instanceof Error ? err.message : '生成失败', 'error');
    } finally {
      setCreating(false);
    }
  }

  async function handleSubmitBuyerInquiry(e: React.FormEvent) {
    e.preventDefault();
    setSubmittingLead(true);
    try {
      const res = await fetchJSON<{ lead: GuanyingLead }>('/api/guanying/leads', {
        method: 'POST',
        body: JSON.stringify({
          company: buyerCompany,
          country: buyerCountry,
          channel: 'SEO独立站询盘',
          inquiry_text: buyerInquiry,
          seo_page_id: currentPage?.id || '',
        }),
      });
      setLastSubmittedLead(res.lead);
      await mutate();
      toast.show(`已接住独立站询盘！AI 识别：${res.lead.buyer_type}（匹配 ${res.lead.matched_code}）`, 'success');
    } catch (err) {
      toast.show(err instanceof Error ? err.message : '提交失败', 'error');
    } finally {
      setSubmittingLead(false);
    }
  }

  return (
    <div className="mx-auto max-w-[1360px] space-y-6 pb-10">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <div className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-brand-accent">
            <Globe className="h-3.5 w-3.5" />
            <span>外贸独立站建站 · Schema.org 结构化 SEO · 询盘闭环</span>
          </div>
          <h1 className="mt-1 text-2xl font-bold tracking-tight text-brand-fg">SEO 独立站建站工坊</h1>
          <p className="mt-1 text-xs text-brand-muted">
            避开海关贸易商内卷，直接针对 WVA 编号、车桥与车型部署可被 Google / AI 引擎收录的工厂直供独立站。
          </p>
        </div>

        <div className="flex items-center gap-2 rounded-xl border border-brand-border bg-brand-surface p-1">
          <button
            type="button"
            onClick={() => setViewMode('preview')}
            className={`rounded-lg px-3 py-1.5 text-xs font-semibold ${viewMode === 'preview' ? 'bg-brand-accent text-brand-on-accent' : 'text-brand-muted'}`}
          >
            独立站实时预览 & 询盘测试
          </button>
          <button
            type="button"
            onClick={() => setViewMode('schema')}
            className={`rounded-lg px-3 py-1.5 text-xs font-semibold ${viewMode === 'schema' ? 'bg-brand-accent text-brand-on-accent' : 'text-brand-muted'}`}
          >
            SEO Meta & Schema JSON-LD
          </button>
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-[340px_1fr]">
        {/* Left Sidebar: Existing Pages & Generator */}
        <div className="space-y-4">
          <form onSubmit={handleGeneratePage} className="ui-card space-y-3 p-4">
            <h3 className="text-xs font-bold text-brand-fg">一键新建产品 SEO 着陆页</h3>
            <input
              value={seriesInput}
              onChange={(e) => setSeriesInput(e.target.value)}
              placeholder="核心产品线 / WVA / 车桥"
              className="h-9 w-full rounded-lg border border-brand-border bg-brand-bg px-3 text-xs text-brand-fg"
            />
            <div className="grid grid-cols-2 gap-2">
              <input
                value={marketInput}
                onChange={(e) => setMarketInput(e.target.value)}
                placeholder="目标出口市场"
                className="h-9 rounded-lg border border-brand-border bg-brand-bg px-2.5 text-xs text-brand-fg"
              />
              <select
                value={langInput}
                onChange={(e) => setLangInput(e.target.value)}
                className="h-9 rounded-lg border border-brand-border bg-brand-bg px-2 text-xs text-brand-fg"
              >
                <option value="English">English</option>
                <option value="English / Español">English / 西语</option>
                <option value="English / العربية">English / 阿拉伯语</option>
                <option value="English / Русский">English / 俄语</option>
              </select>
            </div>
            <button
              type="submit"
              disabled={creating}
              className="inline-flex h-9 w-full items-center justify-center gap-1.5 rounded-lg bg-brand-accent text-xs font-semibold text-brand-on-accent hover:brightness-110"
            >
              <Sparkles className="h-3.5 w-3.5" />
              {creating ? '生成中…' : '生成独立站页面'}
            </button>
          </form>

          <div className="ui-card divide-y divide-brand-border overflow-hidden">
            <div className="bg-brand-bg/50 px-4 py-2.5 text-[11px] font-bold text-brand-muted">
              已部署独立站着陆页 ({pages.length})
            </div>
            {pages.map((p) => {
              const active = currentPage?.id === p.id;
              return (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => setSelectedID(p.id)}
                  className={`w-full space-y-1 px-4 py-3 text-left transition ${
                    active ? 'bg-brand-accent/10' : 'hover:bg-brand-fg/[0.03]'
                  }`}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="rounded bg-brand-fg/[0.06] px-1.5 py-0.5 text-[10px] font-semibold text-brand-fg">
                      {p.lang}
                    </span>
                    <span className="text-[10px] font-semibold text-emerald-600">{p.inquiry_count} 条询盘</span>
                  </div>
                  <p className="line-clamp-2 text-xs font-semibold text-brand-fg">{p.series_focus}</p>
                  <p className="truncate font-mono text-[10px] text-brand-muted">/{p.slug}</p>
                </button>
              );
            })}
          </div>
        </div>

        {/* Right Stage: Live Site Preview or Schema */}
        {currentPage ? (
          viewMode === 'preview' ? (
            <div className="ui-card overflow-hidden border border-brand-border">
              {/* Mock Browser Chrome */}
              <div className="flex items-center justify-between border-b border-brand-border bg-brand-bg/80 px-4 py-2.5">
                <div className="flex items-center gap-2">
                  <span className="h-2.5 w-2.5 rounded-full bg-rose-400" />
                  <span className="h-2.5 w-2.5 rounded-full bg-amber-400" />
                  <span className="h-2.5 w-2.5 rounded-full bg-emerald-400" />
                  <span className="ml-2 rounded-md border border-brand-border bg-brand-surface px-3 py-1 font-mono text-[11px] text-brand-muted">
                    https://www.kooying.cn/en/{currentPage.slug}
                  </span>
                </div>
                <span className="rounded-full bg-emerald-500/15 px-2.5 py-0.5 text-[10px] font-semibold text-emerald-600">
                  SEO Live · {currentPage.target_market}
                </span>
              </div>

              {/* Site Body */}
              <div className="space-y-6 bg-white p-6 text-slate-900">
                <div className="flex flex-wrap items-center justify-between gap-4 border-b border-slate-200 pb-4">
                  <div className="flex items-center gap-3">
                    <span className="rounded-lg bg-blue-700 px-2.5 py-1 text-xs font-black tracking-wider text-white">
                      KOOYING
                    </span>
                    <span className="text-xs font-semibold text-slate-600">
                      Zhejiang Kooying Auto Parts Co., Ltd. · Huzhou, China
                    </span>
                  </div>
                  <div className="flex items-center gap-2 text-[11px] font-semibold text-blue-700">
                    <ShieldCheck className="h-4 w-4" />
                    <span>IATF 16949 · ECE R90 · 15M Sets/Year</span>
                  </div>
                </div>

                <div className="grid gap-6 md:grid-cols-[1.2fr_1fr] md:items-center">
                  <div className="space-y-3">
                    <span className="inline-block rounded-full bg-blue-50 px-3 py-1 text-[11px] font-bold text-blue-700">
                      Factory Direct OEM & Aftermarket Supply
                    </span>
                    <h2 className="text-2xl font-extrabold leading-tight text-slate-900">
                      {currentPage.hero_heading}
                    </h2>
                    <p className="text-xs leading-relaxed text-slate-600">{currentPage.hero_subheading}</p>
                    <ul className="space-y-1.5 pt-1">
                      {highlights.map((h) => (
                        <li key={h} className="flex items-center gap-2 text-xs font-medium text-slate-800">
                          <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-600" />
                          <span>{h}</span>
                        </li>
                      ))}
                    </ul>
                  </div>

                  {/* Embedded Interactive Buyer Inquiry Form */}
                  <form
                    onSubmit={handleSubmitBuyerInquiry}
                    className="space-y-3 rounded-2xl border border-slate-200 bg-slate-50 p-4 shadow-sm"
                  >
                    <div>
                      <h3 className="text-sm font-bold text-slate-900">Request Factory Direct Quote</h3>
                      <p className="text-[11px] text-slate-500">提交即实时触发「海外终端甄别 + 型号自动匹配」并写入数据库</p>
                    </div>
                    <input
                      required
                      value={buyerCompany}
                      onChange={(e) => setBuyerCompany(e.target.value)}
                      placeholder="Company / Fleet Name"
                      className="h-9 w-full rounded-lg border border-slate-300 bg-white px-3 text-xs text-slate-900"
                    />
                    <input
                      required
                      value={buyerCountry}
                      onChange={(e) => setBuyerCountry(e.target.value)}
                      placeholder="Country / Port"
                      className="h-9 w-full rounded-lg border border-slate-300 bg-white px-3 text-xs text-slate-900"
                    />
                    <textarea
                      required
                      rows={3}
                      value={buyerInquiry}
                      onChange={(e) => setBuyerInquiry(e.target.value)}
                      placeholder="Vehicle / Axle / WVA or Dimensions"
                      className="w-full rounded-lg border border-slate-300 bg-white p-2.5 text-xs text-slate-900"
                    />
                    <button
                      type="submit"
                      disabled={submittingLead}
                      className="inline-flex h-9 w-full items-center justify-center gap-1.5 rounded-lg bg-blue-700 text-xs font-bold text-white transition hover:bg-blue-800 disabled:opacity-60"
                    >
                      <Send className="h-3.5 w-3.5" />
                      {submittingLead ? 'AI 甄别入库中…' : 'Send Direct Factory Inquiry'}
                    </button>

                    {lastSubmittedLead && (
                      <div className="rounded-lg border border-emerald-200 bg-emerald-50 p-2.5 text-[11px] text-emerald-900">
                        <p className="font-bold">
                          已捕获：{lastSubmittedLead.buyer_type}（置信度 {lastSubmittedLead.confidence}%）
                        </p>
                        <p>自动匹配型号：{lastSubmittedLead.matched_code}</p>
                        <button
                          type="button"
                          onClick={() => setActiveTab('crawler')}
                          className="mt-1 font-bold text-blue-700 underline"
                        >
                          前往「产品与选型数据库」查看此询盘并出报价 →
                        </button>
                      </div>
                    )}
                  </form>
                </div>
              </div>
            </div>
          ) : (
            <div className="ui-card space-y-4 p-5">
              <h3 className="text-sm font-bold text-brand-fg">SEO Title / Meta / Google Schema.org Product JSON-LD</h3>
              <div className="space-y-2 rounded-xl bg-brand-bg p-4 text-xs">
                <p><strong className="text-brand-muted">Title：</strong>{currentPage.title}</p>
                <p><strong className="text-brand-muted">Keywords：</strong>{currentPage.target_keywords}</p>
                <p><strong className="text-brand-muted">Description：</strong>{currentPage.meta_description}</p>
              </div>
              <pre className="overflow-x-auto rounded-xl border border-brand-border bg-brand-bg p-4 font-mono text-[11px] leading-relaxed text-brand-fg">
                {currentPage.schema_jsonld}
              </pre>
            </div>
          )
        ) : null}
      </div>
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
          <h1 className="mt-1 text-2xl font-bold tracking-tight text-brand-fg">数据与飞书 CLI 中枢</h1>
          <p className="mt-1 text-xs text-brand-muted">
            本地 SQLite 为资产真源，通过官方 lark-cli 将冠英产品选型、海外终端询盘、SEO 页面、内容稿件及知识库增量镜像至飞书多维表格。
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
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-5">
        {[
          { label: '官方刹车片型号库', value: stats?.product_count ?? 47, note: 'Kooying + 冠隆 1:1 目录' },
          { label: '高价值终端询盘', value: stats?.terminal_lead_count ?? 0, note: `已过滤 ${stats?.filtered_trader_count ?? 0} 条贸易商` },
          { label: 'SEO 独立站着陆页', value: stats?.seo_page_count ?? 4, note: '含 Google Product JSON-LD' },
          { label: 'GTM 内容运营稿件', value: stats?.content_count ?? 6, note: 'LinkedIn / TikTok / WhatsApp' },
          { label: '品牌与台架知识文档', value: stats?.knowledge_doc_count ?? 4, note: '含 400℃ 磨损对比数据' },
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
