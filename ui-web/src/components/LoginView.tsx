import { useState } from 'react';
import { api, setToken } from '../api';

export function LoginView({ onSuccess }: { onSuccess: (username: string) => void }) {
  const [username, setUsername] = useState('demo');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      const res = await api.login(username.trim(), password);
      setToken(res.token);
      onSuccess(res.username);
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-brand-bg px-4">
      <form
        onSubmit={submit}
        className="w-full max-w-sm rounded-2xl border border-brand-border bg-brand-surface p-8 shadow-sm"
      >
        <div className="mb-6">
          <div className="flex items-center">
            <span className="alpha-glass-mark" aria-hidden="true">
              <span className="alpha-glass-mark__word">Alpha</span>
              <span className="alpha-glass-mark__sub">Intelligence</span>
            </span>
            <span className="sr-only">Alpha-Nexus EgoLite</span>
          </div>
          <div className="text-sm text-brand-muted mt-1">本机 / 内网登录（工具账号，非社媒）</div>
        </div>
        <label className="block text-sm mb-1 text-brand-fg-subtle">用户名</label>
        <input
          className="w-full mb-4 rounded-lg border border-brand-border bg-brand-input-bg px-3 py-2 text-sm text-brand-fg outline-none focus:ring-2 focus:ring-brand-accent/40"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          autoComplete="username"
        />
        <label className="block text-sm mb-1 text-brand-fg-subtle">密码</label>
        <input
          type="password"
          className="w-full mb-4 rounded-lg border border-brand-border bg-brand-input-bg px-3 py-2 text-sm text-brand-fg outline-none focus:ring-2 focus:ring-brand-accent/40"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
        />
        {error ? <div className="mb-3 text-sm text-red-600">{error}</div> : null}
        <button
          type="submit"
          disabled={loading}
          className="w-full rounded-lg bg-brand-accent hover:bg-brand-accent-hover text-brand-on-accent text-sm font-medium py-2.5 disabled:opacity-60"
        >
          {loading ? '登录中…' : '登录'}
        </button>
        <p className="mt-4 text-xs text-brand-muted leading-relaxed">
          首次：CLI 执行{' '}
          <code className="text-brand-fg">
            auth bootstrap --username demo --password demo123
          </code>
        </p>
      </form>
    </div>
  );
}
