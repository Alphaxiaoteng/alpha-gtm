import { useMemo, useState } from 'react';
import type { FormEvent } from 'react';
import useSWR from 'swr';
import { Check, Network, Pencil, Plus, ShieldCheck, Trash2, Unlink } from 'lucide-react';
import { api, type ProxyEndpoint } from '../api';
import { useUIStore } from '../store';

const emptyForm = { name: '', server: '', enabled: true };

export function ProxyPoolView({ embedded = false }: { embedded?: boolean }) {
  const { toast } = useUIStore();
  const { data: endpoints, mutate: mutateEndpoints } = useSWR('api/proxies', api.getProxyEndpoints);
  const { data: accounts, mutate: mutateAccounts } = useSWR('api/accounts', api.getAccounts);
  const [form, setForm] = useState(emptyForm);
  const [editing, setEditing] = useState<ProxyEndpoint | null>(null);
  const [accountID, setAccountID] = useState<number>(0);
  const [endpointID, setEndpointID] = useState<number>(0);
  const activeEndpoints = useMemo(() => (endpoints ?? []).filter((endpoint) => endpoint.enabled), [endpoints]);

  const resetForm = () => {
    setForm(emptyForm);
    setEditing(null);
  };

  const submitEndpoint = async (event: FormEvent) => {
    event.preventDefault();
    try {
      if (editing) {
        await api.updateProxyEndpoint(editing.id, form);
        toast.show('代理端点已更新，绑定账号会在下次启动时使用新地址', 'success');
      } else {
        await api.createProxyEndpoint(form);
        toast.show('代理端点已保存', 'success');
      }
      resetForm();
      mutateEndpoints();
      mutateAccounts();
    } catch (error: any) {
      toast.show(error.message || '保存失败', 'error');
    }
  };

  const editEndpoint = (endpoint: ProxyEndpoint) => {
    setEditing(endpoint);
    setForm({ name: endpoint.name, server: endpoint.server, enabled: endpoint.enabled });
  };

  const removeEndpoint = async (endpoint: ProxyEndpoint) => {
    if (!window.confirm(`删除代理端点“${endpoint.name}”？已绑定账号时会被安全拒绝。`)) return;
    try {
      await api.deleteProxyEndpoint(endpoint.id);
      if (editing?.id === endpoint.id) resetForm();
      mutateEndpoints();
      toast.show('代理端点已删除', 'success');
    } catch (error: any) {
      toast.show(error.message || '删除失败', 'error');
    }
  };

  const bindEndpoint = async (event: FormEvent) => {
    event.preventDefault();
    if (!accountID) {
      toast.show('请选择账号', 'error');
      return;
    }
    try {
      await api.bindProxyEndpoint(accountID, endpointID);
      mutateAccounts();
      toast.show(endpointID ? '代理池已绑定到账号' : '账号已解绑代理池并恢复直连', 'success');
    } catch (error: any) {
      toast.show(error.message || '绑定失败', 'error');
    }
  };

  const content = (
    <>
      {!embedded ? <header className="flex items-start justify-between gap-6">
        <div>
          <div className="flex items-center gap-2 text-gray-900">
            <Network className="w-5 h-5 text-[var(--brand-accent)]" />
            <h2 className="text-xl font-semibold">代理池</h2>
          </div>
          <p className="mt-2 text-sm text-gray-500">保存并复用无凭证代理端点；不存密码、不自动连通性检测、不触发外部网络请求。</p>
        </div>
        <div className="flex items-center gap-2 text-xs text-emerald-700 bg-emerald-50 px-3 py-2 rounded-lg">
          <ShieldCheck className="w-4 h-4" /> 本地安全配置
        </div>
      </header> : null}

      <section className="grid grid-cols-1 xl:grid-cols-[minmax(0,1.35fr)_minmax(320px,0.65fr)] gap-6">
        <div className="rounded-xl border border-brand-border bg-brand-surface overflow-hidden">
          <div className="px-5 py-4 border-b border-brand-border flex items-center justify-between">
            <div>
              <h3 className="font-semibold text-gray-900">端点列表</h3>
              <p className="mt-0.5 text-xs text-gray-500">仅支持 http、https、socks5 的 host:port。</p>
            </div>
            <span className="text-xs text-gray-500">{endpoints?.length ?? 0} 个端点</span>
          </div>
          <div className="divide-y divide-brand-border">
            {(endpoints ?? []).map((endpoint) => {
              const bound = (accounts ?? []).filter((account) => account.proxyId === endpoint.id);
              return (
                <div key={endpoint.id} className="px-5 py-4 flex items-center gap-4">
                  <span className={`w-2 h-2 rounded-full ${endpoint.enabled ? 'bg-emerald-500' : 'bg-gray-300'}`} />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="font-medium text-sm text-gray-900 truncate">{endpoint.name}</span>
                      {!endpoint.enabled && <span className="text-[10px] px-1.5 py-0.5 rounded bg-gray-100 text-gray-500">已停用</span>}
                    </div>
                    <p className="mt-1 font-mono text-xs text-gray-500 truncate">{endpoint.server}</p>
                    {bound.length > 0 && <p className="mt-1 text-xs text-[var(--brand-accent)]">已绑定：{bound.map((account) => account.name).join('、')}</p>}
                  </div>
                  <button type="button" onClick={() => editEndpoint(endpoint)} className="p-2 text-gray-500 hover:text-gray-900 rounded-lg hover:bg-black/5" aria-label={`编辑 ${endpoint.name}`}><Pencil className="w-4 h-4" /></button>
                  <button type="button" onClick={() => removeEndpoint(endpoint)} className="p-2 text-gray-500 hover:text-red-600 rounded-lg hover:bg-red-50" aria-label={`删除 ${endpoint.name}`}><Trash2 className="w-4 h-4" /></button>
                </div>
              );
            })}
            {endpoints?.length === 0 && <div className="py-14 text-center text-sm text-gray-400">尚未保存代理端点</div>}
          </div>
        </div>

        <form onSubmit={submitEndpoint} className="rounded-xl border border-brand-border bg-brand-surface p-5 h-fit space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="font-semibold text-gray-900">{editing ? '编辑端点' : '新增端点'}</h3>
            {editing && <button type="button" onClick={resetForm} className="text-xs text-gray-500 hover:text-gray-900">取消编辑</button>}
          </div>
          <label className="block text-sm text-gray-700">名称
            <input required maxLength={120} value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} placeholder="例如：新加坡节点 A" className="mt-1.5 w-full rounded-lg border border-brand-border px-3 py-2 text-sm bg-transparent" />
          </label>
          <label className="block text-sm text-gray-700">代理地址
            <input required value={form.server} onChange={(event) => setForm({ ...form, server: event.target.value })} placeholder="socks5://127.0.0.1:1080" className="mt-1.5 w-full rounded-lg border border-brand-border px-3 py-2 text-sm font-mono bg-transparent" />
          </label>
          <label className="flex items-center gap-2 text-sm text-gray-700 cursor-pointer">
            <input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} /> 启用并允许绑定
          </label>
          <button type="submit" className="w-full rounded-lg bg-[var(--brand-accent)] text-white py-2 text-sm font-medium flex justify-center items-center gap-2"><Plus className="w-4 h-4" />{editing ? '保存修改' : '保存端点'}</button>
        </form>
      </section>

      <section className="rounded-xl border border-brand-border bg-brand-surface p-5">
        <div className="flex items-center gap-2"><Check className="w-4 h-4 text-[var(--brand-accent)]" /><h3 className="font-semibold text-gray-900">绑定到账号</h3></div>
        <p className="mt-1 text-xs text-gray-500">绑定仅写入本地浏览器画像；已运行的浏览器不会被重启或修改。</p>
        <form onSubmit={bindEndpoint} className="mt-4 grid grid-cols-1 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-3 items-end">
          <label className="block text-sm text-gray-700">账号
            <select value={accountID} onChange={(event) => setAccountID(Number(event.target.value))} className="mt-1.5 w-full rounded-lg border border-brand-border px-3 py-2 text-sm bg-brand-surface">
              <option value={0}>选择账号</option>
              {(accounts ?? []).map((account) => <option key={account.id} value={account.id}>{account.name} · {account.platformCode}{account.proxyId ? '（已绑定）' : ''}</option>)}
            </select>
          </label>
          <label className="block text-sm text-gray-700">代理端点
            <select value={endpointID} onChange={(event) => setEndpointID(Number(event.target.value))} className="mt-1.5 w-full rounded-lg border border-brand-border px-3 py-2 text-sm bg-brand-surface">
              <option value={0}>解绑，恢复直连</option>
              {activeEndpoints.map((endpoint) => <option key={endpoint.id} value={endpoint.id}>{endpoint.name} · {endpoint.server}</option>)}
            </select>
          </label>
          <button type="submit" className="rounded-lg border border-brand-border px-4 py-2 text-sm font-medium hover:bg-black/5 flex items-center gap-2 justify-center"><Unlink className="w-4 h-4" />应用绑定</button>
        </form>
      </section>
    </>
  );

  return embedded
    ? <section data-testid="settings-proxy-pool" className="space-y-6">{content}</section>
    : <main className="h-full overflow-y-auto p-7 space-y-6">{content}</main>;
}
