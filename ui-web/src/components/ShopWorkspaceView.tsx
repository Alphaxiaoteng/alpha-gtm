import { useState } from 'react';
import useSWR from 'swr';
import { ArrowRight, Bot, Database, RefreshCw, Store, TriangleAlert } from 'lucide-react';
import { api, type ShopListingPlanSummary } from '../api';
import { useUIStore } from '../store';
import { isShopPlatform } from '../workspaceMode';
import { PlatformIcon } from './PlatformIcon';
import { SQLLinkView } from './SQLLinkView';

const platformNames: Record<string, string> = {
  douyin_shop: '抖音电商', taobao: '淘宝', tiktok_shop: 'TikTok Shop', shopee: 'Shopee',
  shopify: 'Shopify', amazon_seller: 'Amazon Seller', jd_shop: '京东', pinduoduo_shop: '拼多多',
  xhs_shop: '小红书店铺', kuaishou_shop: '快手小店', wechat_shop: '微信小店',
  alibaba_1688: '1688', aliexpress: 'AliExpress', xianyu: '闲鱼', qianchuan: '巨量千川',
};

const planStatus: Record<string, { label: string; tone: string }> = {
  dry_run_ok: { label: '本地校验通过', tone: 'text-emerald-700' },
  frozen: { label: '快照已冻结', tone: 'text-brand-muted' },
  prepare_approved: { label: '已授权准备', tone: 'text-brand-muted' },
  manual_takeover: { label: '平台页面待确认', tone: 'text-brand-warning' },
};

function dateLabel(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '时间未知' : date.toLocaleString('zh-CN', { hour12: false });
}

function PlanRow({ plan }: { plan: ShopListingPlanSummary }) {
  const status = planStatus[plan.status] ?? { label: plan.status || '状态未知', tone: 'text-gray-600' };
  return (
    <div className="grid gap-3 border-b border-brand-border px-4 py-3 last:border-b-0 sm:grid-cols-[minmax(0,1fr)_8rem_9rem] sm:items-center">
      <div className="flex min-w-0 items-center gap-3">
        <PlatformIcon platform={plan.platform_code} title={platformNames[plan.platform_code]} className="h-7 w-7" />
        <div className="min-w-0">
          <p className="truncate text-xs font-semibold text-gray-900">{platformNames[plan.platform_code] || plan.platform_code} · 计划 #{plan.plan_id}</p>
          <p className="mt-0.5 text-[10px] text-gray-500">{plan.item_count} 个商品 · {dateLabel(plan.updated_at)}</p>
        </div>
      </div>
      <p className={`text-[10px] font-semibold ${status.tone}`}>{status.label}</p>
      <p className="truncate font-mono text-[9px] text-gray-400" title={plan.snapshot_hash}>{plan.error_code || plan.snapshot_hash.slice(0, 12)}</p>
    </div>
  );
}

export function ShopWorkspaceView() {
  const { setActiveTab, setAgentDraft, openAgentDrawer } = useUIStore();
  const accounts = useSWR('shop/accounts', api.getAccounts, { revalidateOnFocus: false });
  const listings = useSWR('shop/listing/plans', api.getShopListingPlans, { revalidateOnFocus: false });
  const contents = useSWR('shop/contents/summary', api.getShopContents, { revalidateOnFocus: false });
  const knowledge = useSWR('shop/knowledge/summary', api.getShopKnowledgeBases, { revalidateOnFocus: false });
  const dataLink = useSWR('shop/data-link/summary', api.getDataLinkState, { revalidateOnFocus: false });
  const [dataLinkOpen, setDataLinkOpen] = useState(false);
  const shopAccounts = (accounts.data ?? []).filter((account) => account.enabled && isShopPlatform(account.platformCode));
  const loggedIn = shopAccounts.filter((account) => account.loginState === 'logged_in').length;
  const needsLogin = shopAccounts.filter((account) => account.loginState === 'logged_out').length;
  const plans = listings.data?.plans ?? [];
  const completedSnapshots = (dataLink.data?.snapshots ?? []).filter((snapshot) => snapshot.status === 'completed');
  const latestSnapshots = Array.from(new Map(completedSnapshots.map((snapshot) => [snapshot.source_id, snapshot])).values());
  const collectedRows = latestSnapshots.reduce((sum, snapshot) => sum + snapshot.row_count, 0);
  const qualityIssues = latestSnapshots.reduce((sum, snapshot) => sum + (snapshot.quality?.issues?.length ?? 0), 0);
  const error = accounts.error || listings.error || contents.error || knowledge.error || dataLink.error;
  const loading = [accounts, listings, contents, knowledge, dataLink].some((resource) => resource.isLoading && !resource.data);

  const refresh = () => void Promise.all([accounts.mutate(), listings.mutate(), contents.mutate(), knowledge.mutate(), dataLink.mutate()]);
  const askAgent = () => {
    setAgentDraft({ mode: 'tools', message: '帮我根据现有店铺账号创建一个商品上架计划：先检查资料，做本地 dry-run，确认后再冻结计划；不要提交到平台。' });
    openAgentDrawer();
  };

  return (
    <main data-testid="shop-workspace" className="mx-auto w-full max-w-[1440px] space-y-4 p-4 pb-10 sm:p-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="font-mono text-[10px] font-semibold uppercase tracking-[0.18em] text-brand-accent">Shop / Local operations</p>
          <h1 className="mt-2 text-2xl font-bold tracking-tight text-gray-950">经营总览</h1>
          <p className="mt-1 text-xs text-gray-500">只显示本地真实店铺、商品计划和采集状态；尚未接入订单与经营指标时不生成估算数字。</p>
        </div>
        <div className="flex gap-2">
          <button type="button" className="ui-btn" onClick={refresh}><RefreshCw className="h-3.5 w-3.5" />刷新</button>
          <button type="button" className="ui-btn-primary" onClick={() => setActiveTab('accounts')}><Store className="h-3.5 w-3.5" />管理店铺</button>
        </div>
      </header>

      <section className="ui-card grid divide-y divide-brand-border sm:grid-cols-2 sm:divide-x sm:divide-y-0 xl:grid-cols-6">
        {[
          ['已配置店铺', shopAccounts.length, '来自本地账号资产'],
          ['登录可用', loggedIn, needsLogin ? `${needsLogin} 个明确需要登录` : '没有明确掉线'],
          ['商品计划', listings.data?.count ?? 0, 'Registry 冻结计划'],
          ['店铺内容', contents.data?.length ?? 0, '独立 Shop 内容表'],
          ['真实采集行数', collectedRows.toLocaleString(), `${latestSnapshots.length} 个最新快照`],
          ['质量问题', qualityIssues, qualityIssues ? '进入数据接入查看详情' : '当前快照未发现问题'],
        ].map(([label, value, detail]) => <div key={label} className="px-5 py-4"><p className="text-[10px] font-semibold text-gray-500">{label}</p><p className="mt-2 text-2xl font-bold text-gray-950">{loading ? '—' : value}</p><p className="mt-1 text-[10px] text-gray-400">{detail}</p></div>)}
      </section>

      {error ? <div className="flex items-center gap-2 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-xs text-rose-700"><TriangleAlert className="h-4 w-4" /><span>{error instanceof Error ? error.message : 'SHOP_DATA_LOAD_FAILED'}</span><button type="button" className="ml-auto font-semibold" onClick={refresh}>重试</button></div> : null}

      <section className="ui-card flex flex-wrap items-center gap-4 px-5 py-4">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg border border-brand-border bg-brand-bg"><Database className="h-4 w-4 text-gray-700" /></div>
        <div className="min-w-0 flex-1"><h2 className="text-sm font-semibold text-gray-900">经营数据接入与复盘</h2><p className="mt-1 text-[10px] text-gray-500">用 SQL Link 接入本地订单、商品、广告或财务数据；每次采集保留校验和、质量问题与知识库确认记录。</p></div>
        <button type="button" className="ui-btn" onClick={() => setDataLinkOpen((open) => !open)}>{dataLinkOpen ? '收起数据接入' : '打开 SQL Link'}<ArrowRight className={`h-3.5 w-3.5 transition-transform ${dataLinkOpen ? 'rotate-90' : ''}`} /></button>
      </section>
      {dataLinkOpen ? <SQLLinkView /> : null}

      <div className="grid gap-4 xl:grid-cols-[minmax(20rem,0.42fr)_minmax(0,0.58fr)]">
        <section className="ui-card overflow-hidden">
          <div className="border-b border-brand-border px-4 py-3"><h2 className="text-sm font-semibold text-gray-900">店铺连接</h2><p className="mt-1 text-[10px] text-gray-500">只把明确掉线标为需要登录，unknown 保持待确认。</p></div>
          {shopAccounts.length ? <div>{shopAccounts.map((account) => <div key={account.id} className="flex items-center gap-3 border-b border-brand-border px-4 py-3 last:border-b-0"><PlatformIcon platform={account.platformCode} title={platformNames[account.platformCode]} className="h-7 w-7" /><div className="min-w-0 flex-1"><p className="truncate text-xs font-semibold text-gray-900">{account.accountIdentifier || account.name || platformNames[account.platformCode] || account.platformCode}</p><p className="mt-0.5 text-[10px] text-gray-400">{platformNames[account.platformCode] || account.platformCode}</p></div><span className={`text-[10px] font-semibold ${account.loginState === 'logged_in' ? 'text-emerald-700' : account.loginState === 'logged_out' ? 'text-rose-700' : 'text-brand-warning'}`}>{account.loginState === 'logged_in' ? '已登录' : account.loginState === 'logged_out' ? '需要登录' : '待确认'}</span></div>)}</div> : <div className="px-5 py-10 text-center"><p className="text-xs font-semibold text-gray-700">还没有配置店铺</p><button type="button" className="mt-3 text-xs font-semibold text-brand-accent" onClick={() => setActiveTab('accounts')}>添加店铺账号</button></div>}
        </section>

        <section className="ui-card overflow-hidden">
          <div className="flex items-center justify-between border-b border-brand-border px-4 py-3"><div><h2 className="text-sm font-semibold text-gray-900">商品计划</h2><p className="mt-1 text-[10px] text-gray-500">本地校验和冻结记录，不代表平台已经上架。</p></div><button type="button" className="ui-btn" onClick={askAgent}><Bot className="h-3.5 w-3.5" />创建计划</button></div>
          {plans.length ? <div>{plans.map((plan) => <PlanRow key={plan.plan_id} plan={plan} />)}</div> : <div className="px-5 py-10 text-center"><p className="text-xs font-semibold text-gray-700">还没有商品计划</p><p className="mt-1 text-[10px] text-gray-400">可以让 Alpha Agent 先校验资料并生成本地冻结计划。</p><button type="button" className="mt-3 text-xs font-semibold text-brand-accent" onClick={askAgent}>用 Alpha Agent 创建</button></div>}
        </section>
      </div>

    </main>
  );
}
