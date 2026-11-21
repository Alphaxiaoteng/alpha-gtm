import { Component, type ErrorInfo, type ReactNode } from 'react';
import { reportClientDiagnostic } from '../clientDiagnostics';

interface Props {
  children: ReactNode;
}

interface State {
  failed: boolean;
}

export class ClientErrorBoundary extends Component<Props, State> {
  state: State = { failed: false };

  static getDerivedStateFromError(): State {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    void reportClientDiagnostic('react_error', `${error.stack || error.message}\n${info.componentStack || ''}`);
  }

  render(): ReactNode {
    if (!this.state.failed) return this.props.children;
    return (
      <main className="min-h-screen bg-slate-950 text-slate-100 grid place-items-center p-8">
        <section className="max-w-md rounded-xl border border-slate-700 bg-slate-900 p-6 text-center">
          <h1 className="text-lg font-semibold">界面遇到错误</h1>
          <p className="mt-2 text-sm text-slate-400">已记录脱敏错误指纹，可重新加载后继续操作。</p>
          <button
            className="mt-5 rounded-md bg-brand-accent px-4 py-2 text-sm font-medium text-brand-on-accent hover:bg-brand-accent-hover"
            type="button"
            onClick={() => window.location.reload()}
          >
            重新加载
          </button>
        </section>
      </main>
    );
  }
}
