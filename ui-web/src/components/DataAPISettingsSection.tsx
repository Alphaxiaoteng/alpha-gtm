import { useEffect, useMemo, useState } from 'react';
import useSWR from 'swr';
import {
  CheckCircle2,
  ChevronDown,
  Database,
  Eye,
  EyeOff,
  KeyRound,
  Loader2,
  Network,
  ShieldCheck,
  XCircle,
} from 'lucide-react';
import { api, type DataAPIBinding } from '../api';
import { useUIStore } from '../store';
import { LocalLoadingState } from './LocalLoadingState';

const bindingMeta: Record<DataAPIBinding, { label: string; className: string }> = {
  bound: { label: '已接入', className: 'border-emerald-200 bg-emerald-50 text-emerald-700' },
  local_derived: { label: '本地分析', className: 'border-brand-border bg-brand-surface-raised text-brand-muted' },
  available_not_bound: { label: '待绑定', className: 'border-brand-warning/30 bg-brand-warning/10 text-brand-warning' },
  blocked_by_binding: { label: '依赖前置接口', className: 'border-gray-200 bg-gray-50 text-gray-600' },
  browser_only: { label: '浏览器采集', className: 'border-brand-border bg-brand-surface-raised text-brand-fg' },
};

const capabilityOrder = ['关键词搜索', '博主资料', '博主作品', '内容详情', '选题', '对标', '圈层'];

export function DataAPISettingsSection() {
  const { toast } = useUIStore();
  const { data, error, isLoading, mutate } = useSWR('api/data-api/config', api.getDataAPIConfig);
  const [apiKey, setAPIKey] = useState('');
  const [region, setRegion] = useState<'mainland' | 'global'>('mainland');
  const [showKey, setShowKey] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [catalogOpen, setCatalogOpen] = useState(false);

  useEffect(() => {
    if (!data) return;
    const selected = data.base_options.find((option) => option.base_url === data.base_url);
    if (selected) setRegion(selected.region);
  }, [data]);

  const state = useMemo(() => {
    if (data?.active) {
      return { label: '已激活', detail: '赛道搜索与博主监控接口可以发起真实请求', className: 'border-emerald-200 bg-emerald-50 text-emerald-700', Icon: CheckCircle2 };
    }
    if (data?.state === 'test_failed') {
      return { label: '测试失败', detail: 'Key、余额、权限或网络未通过，请检查后重试', className: 'border-red-200 bg-red-50 text-red-700', Icon: XCircle };
    }
    if (data?.configured) {
      return { label: '待测试', detail: 'Key 已保存在本机，测试成功后才会激活', className: 'border-brand-warning/30 bg-brand-warning/10 text-brand-warning', Icon: ShieldCheck };
    }
    return { label: '未配置', detail: '粘贴 TikHub API Key 后先保存，再测试并激活', className: 'border-gray-200 bg-gray-50 text-gray-600', Icon: KeyRound };
  }, [data]);

  const saveCredential = async () => {
    const key = apiKey.trim();
    if (key.length < 12) {
      toast.show('请输入完整的 TikHub API Key', 'error');
      return;
    }
    setSaving(true);
    try {
      const updated = await api.saveTikHubCredential(key, region);
      setAPIKey('');
      setShowKey(false);
      await mutate(updated, { revalidate: false });
      toast.show('TikHub API Key 已保存到本机；请继续测试并激活', 'success');
    } catch (cause) {
      toast.show(`保存失败：${cause instanceof Error ? cause.message : '未知错误'}`, 'error');
    } finally {
      setSaving(false);
    }
  };

  const testAndActivate = async () => {
    if (!data?.configured) {
      toast.show('请先保存 TikHub API Key', 'error');
      return;
    }
    if (!window.confirm('将向 TikHub 发送一次真实的账户校验请求。继续测试并激活吗？')) return;
    setTesting(true);
    try {
      const updated = await api.testAndActivateDataAPI();
      await mutate(updated, { revalidate: false });
      toast.show('TikHub 数据 API 已通过真实连接测试并激活', 'success');
    } catch (cause) {
      await mutate();
      toast.show(`测试失败：${cause instanceof Error ? cause.message : '请检查 Key、权限、余额或网络'}`, 'error');
    } finally {
      setTesting(false);
    }
  };

  const StateIcon = state.Icon;

  return (
    <section className="ui-card space-y-4 p-5" data-testid="settings-data-api">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-brand-border/60 pb-3">
        <div className="flex items-start gap-2">
          <Database className="mt-0.5 h-4 w-4 shrink-0 text-brand-accent" />
          <div>
            <h4 className="text-xs font-bold text-gray-900">TikHub 数据 API</h4>
            <p className="mt-1 text-[11px] leading-5 text-gray-550">
              面向抖音、小红书、视频号、快手、公众号 5 个 Nexus 核心平台；默认使用中国大陆加速。API Key 只保存在本机，保存后不会回显。
            </p>
          </div>
        </div>
        <span className={`inline-flex items-center gap-1.5 border px-2.5 py-1 text-[10px] font-bold ${state.className}`}>
          <StateIcon className="h-3.5 w-3.5" />
          {state.label}
        </span>
      </div>

      {isLoading && !data ? (
        <LocalLoadingState
          compact
          label="正在读取数据接口"
          detail="恢复本机配置与可用能力"
          className="my-2"
        />
      ) : null}
      {error ? (
        <p role="status" className="border border-red-200 bg-red-50 px-3 py-2 text-[11px] text-red-700">
          无法读取数据 API 配置。
        </p>
      ) : null}

      {data ? (
        <>
          <div className={`border px-3 py-2.5 text-[11px] leading-5 ${state.className}`}>
            <p className="font-bold">{state.detail}</p>
            {data.tested_at ? <p className="mt-0.5 opacity-80">最近测试：{new Date(data.tested_at).toLocaleString('zh-CN')}</p> : null}
          </div>

          <div className="grid gap-3 md:grid-cols-[180px_minmax(0,1fr)]">
            <label className="block">
              <span className="text-[10px] font-bold text-gray-700">连接区域</span>
              <select
                value={region}
                onChange={(event) => setRegion(event.target.value as 'mainland' | 'global')}
                disabled={saving || testing}
                className="mt-1.5 w-full border border-brand-border bg-brand-bg px-3 py-2.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/50"
              >
                {data.base_options.map((option) => (
                  <option key={option.region} value={option.region}>{option.label}</option>
                ))}
              </select>
            </label>

            <label className="block">
              <span className="text-[10px] font-bold text-gray-700">API Key</span>
              <span className="mt-1.5 flex border border-brand-border bg-brand-bg focus-within:ring-2 focus-within:ring-brand-accent/50">
                <input
                  type={showKey ? 'text' : 'password'}
                  value={apiKey}
                  onChange={(event) => setAPIKey(event.target.value)}
                  placeholder={data.configured ? '已配置；输入新 Key 可替换' : '粘贴 TikHub API Key'}
                  autoComplete="new-password"
                  spellCheck={false}
                  className="min-w-0 flex-1 bg-transparent px-3 py-2.5 font-mono text-xs outline-none"
                />
                <button
                  type="button"
                  onClick={() => setShowKey((current) => !current)}
                  className="px-3 text-gray-500 hover:text-gray-800"
                  aria-label={showKey ? '隐藏 API Key' : '显示 API Key'}
                >
                  {showKey ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </button>
              </span>
            </label>
          </div>

          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="min-w-0">
              <p className="truncate font-mono text-[10px] text-gray-600" title={data.base_url}>{data.base_url}</p>
              <p className="mt-0.5 text-[10px] text-gray-500">接口目录由应用固定维护，不接受任意 URL。</p>
            </div>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => void saveCredential()}
                disabled={saving || testing || apiKey.trim().length < 12}
                className="ui-btn disabled:opacity-40"
              >
                {saving ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <KeyRound className="h-3.5 w-3.5" />}
                {saving ? '保存中…' : data.configured ? '替换 API Key' : '保存 API Key'}
              </button>
              <button
                type="button"
                onClick={() => void testAndActivate()}
                disabled={saving || testing || !data.configured}
                className="ui-btn ui-btn-primary disabled:opacity-40"
                data-testid="settings-data-api-activate"
              >
                {testing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ShieldCheck className="h-3.5 w-3.5" />}
                {testing ? '测试中…' : data.active ? '重新测试' : '测试并激活'}
              </button>
            </div>
          </div>

          <p className="border border-brand-border bg-brand-surface-raised px-3 py-2 text-[10px] leading-5 text-brand-muted">
            接口抓取、参数选择与完整调用日志已统一放在「趋势雷达」的「抓取 / 日志」页面；设置这里只保留一个全局 TikHub Key。
          </p>

          <button
            type="button"
            onClick={() => setCatalogOpen((current) => !current)}
            aria-expanded={catalogOpen}
            className="flex w-full items-center justify-between gap-3 border-t border-brand-border/60 pt-3 text-left"
          >
            <span className="flex items-center gap-2 text-[11px] font-bold text-gray-800">
              <Network className="h-3.5 w-3.5 text-brand-accent" />
              平台能力与固定接口
            </span>
            <ChevronDown className={`h-4 w-4 text-gray-500 transition ${catalogOpen ? 'rotate-180' : ''}`} />
          </button>

          {catalogOpen ? (
            <div className="grid gap-3 lg:grid-cols-2" data-testid="settings-data-api-catalog">
              {data.endpoint_catalog.map((platform) => (
                <article key={platform.platform} className="border border-brand-border bg-brand-bg p-3">
                  <div className="flex items-center justify-between gap-2">
                    <h5 className="text-xs font-bold text-gray-900">{platform.name}</h5>
                    <span className="text-[9px] font-mono uppercase text-gray-400">{platform.platform}</span>
                  </div>
                  <div className="mt-3 space-y-2">
                    {[...platform.capabilities]
                      .sort((a, b) => capabilityOrder.indexOf(a.label) - capabilityOrder.indexOf(b.label))
                      .map((item) => {
                        const meta = bindingMeta[item.binding];
                        return (
                          <details key={item.capability} className="border-t border-brand-border/60 pt-2">
                            <summary className="flex cursor-pointer list-none items-center justify-between gap-2">
                              <span className="text-[10px] font-bold text-gray-700">{item.label}</span>
                              <span className={`border px-1.5 py-0.5 text-[9px] font-bold ${meta.className}`}>{meta.label}</span>
                            </summary>
                            <p className="mt-1.5 text-[10px] leading-4 text-gray-500">{item.message}</p>
                            {item.endpoints?.map((endpoint) => (
                              <div key={endpoint.id ?? `${endpoint.method}-${endpoint.path}`} className="mt-1.5 break-all bg-black/[0.035] px-2 py-1.5 font-mono text-[9px] leading-4 text-gray-600">
                                {endpoint.name ? `${endpoint.name} · ` : ''}{endpoint.method} {endpoint.path}
                                {endpoint.fixed_params ? ` · ${Object.entries(endpoint.fixed_params).map(([key, value]) => `${key}=${String(value)}`).join('&')}` : ''}
                              </div>
                            ))}
                          </details>
                        );
                      })}
                  </div>
                </article>
              ))}
            </div>
          ) : null}
        </>
      ) : null}
    </section>
  );
}
