import { Component, type ErrorInfo, type ReactNode } from 'react';
import { AlertTriangle, RotateCcw, Home } from 'lucide-react';
import { useUIStore, type TabType } from '../store';

// 一个页面组件的渲染错误不应该把整个应用变成白屏。这个边界只包页面内容
// （不包侧栏与对话舞台），失败时给出可恢复的提示，并把错误留在控制台。
interface Props {
  children: ReactNode;
  label?: string;
  /** 重置后想回到哪个 tab；默认回首页。 */
  fallbackTab?: TabType;
}

interface State {
  error: Error | null;
}

export class ViewErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // eslint-disable-next-line no-console
    console.error(`[view-error] ${this.props.label ?? 'view'}`, error, info.componentStack);
  }

  private reset = () => {
    this.setState({ error: null });
  };

  private goHome = () => {
    this.setState({ error: null });
    const store = useUIStore.getState();
    store.setActiveTab(this.props.fallbackTab ?? 'dashboard');
  };

  render() {
    if (!this.state.error) return this.props.children;

    return (
      <div
        data-testid="view-error-boundary"
        role="alert"
        className="flex h-full min-h-0 flex-col items-center justify-center gap-3 p-8 text-center text-brand-fg"
      >
        <span className="flex h-10 w-10 items-center justify-center rounded-full bg-brand-danger/10 text-brand-danger">
          <AlertTriangle className="h-5 w-5" aria-hidden="true" />
        </span>
        <div className="max-w-md">
          <p className="text-sm font-semibold">这个页面没有渲染成功</p>
          <p className="mt-1 text-xs leading-5 text-brand-muted">
            其他页面和 Bot 会话还在。可以先重试；如果一直失败，回到首页再进来。
          </p>
          <pre className="mt-2 max-h-24 overflow-auto whitespace-pre-wrap break-words rounded-md bg-brand-surface-raised px-2 py-1.5 text-left font-mono text-[10px] leading-4 text-brand-muted">
            {this.state.error.message || this.state.error.name}
          </pre>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={this.reset}
            className="inline-flex h-8 items-center gap-1.5 rounded-md bg-brand-accent px-3 text-xs font-semibold text-brand-on-accent transition hover:brightness-110"
          >
            <RotateCcw className="h-3.5 w-3.5" aria-hidden="true" />
            重试
          </button>
          <button
            type="button"
            onClick={this.goHome}
            className="inline-flex h-8 items-center gap-1.5 rounded-md px-3 text-xs font-semibold text-brand-muted transition hover:bg-brand-fg/[0.06] hover:text-brand-fg"
          >
            <Home className="h-3.5 w-3.5" aria-hidden="true" />
            回到首页
          </button>
        </div>
      </div>
    );
  }
}
