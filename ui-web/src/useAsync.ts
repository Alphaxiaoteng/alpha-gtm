import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, apiErrorText } from './api';

/**
 * 统一异步状态：loading / error / data 三态 + 竞态防护 + 重新加载。
 *
 * 背景：项目里有 101 处 useEffect 手写 `let cancelled = false` 防竞态，
 * loading / error / data 三态也各自命名（实测仅 7 处含 load 的 state），
 * 没有统一的请求 hook（全仓仅 AIImageWorkspaceView 用了 SWR）。
 *
 * 本 hook 不要求一次性迁移那 101 处（风险高、收益低），而是提供统一模型，
 * 供新代码与逐步迁移的调用点使用，让"加载中/失败/空/成功"表现一致。
 */

export interface AsyncState<T> {
  data: T | undefined;
  loading: boolean;
  error: string;
  /** 结构化的错误码，便于按错误类型分支（如 401 跳登录）。 */
  errorCode: string;
  /** 后端回传的请求号，展示给用户便于对照服务端日志排障。 */
  requestId: string;
  /** 是否已成功加载过一次（用于区分首屏 loading 与后台刷新）。 */
  loaded: boolean;
  /** 手动重新执行，供错误重试按钮使用。 */
  reload: () => void;
  /** 本地乐观更新，用于提交成功后免重新请求。 */
  setData: (value: T | undefined) => void;
}

type Options = {
  /** 为 false 时不自动发起（等待手动 reload），用于依赖用户交互的请求。 */
  enabled?: boolean;
  /** 错误回调，供调用方接 toast。 */
  onError?: (message: string, error: unknown) => void;
};

export function useAsync<T>(
  fn: () => Promise<T>,
  deps: readonly unknown[],
  options: Options = {},
): AsyncState<T> {
  const { enabled = true, onError } = options;
  const [data, setData] = useState<T | undefined>(undefined);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [errorCode, setErrorCode] = useState('');
  const [requestId, setRequestId] = useState('');
  const [loaded, setLoaded] = useState(false);
  // 自增序号：只有最后一次发起的请求能写入状态，天然防竞态，
  // 比各处手写 `let cancelled` 更不容易漏。
  const seqRef = useRef(0);
  const [tick, setTick] = useState(0);
  // 保持 fn 最新但不触发重跑：fn 常是内联箭头函数，放进 deps 会死循环。
  const fnRef = useRef(fn);
  fnRef.current = fn;
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;

  const reload = useCallback(() => setTick((value) => value + 1), []);

  useEffect(() => {
    if (!enabled) return;
    const seq = ++seqRef.current;
    let active = true;
    setLoading(true);
    setError('');
    setErrorCode('');
    setRequestId('');
    void (async () => {
      try {
        const result = await fnRef.current();
        if (!active || seq !== seqRef.current) return;
        setData(result);
        setLoaded(true);
      } catch (cause) {
        if (!active || seq !== seqRef.current) return;
        const message = apiErrorText(cause);
        setError(message);
        setErrorCode(cause instanceof ApiError ? cause.errorCode : 'UNKNOWN');
        setRequestId(cause instanceof ApiError ? cause.requestId : '');
        onErrorRef.current?.(message, cause);
      } finally {
        if (active && seq === seqRef.current) setLoading(false);
      }
    })();
    return () => {
      active = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick, enabled]);

  return {
    data,
    loading,
    error,
    errorCode,
    requestId,
    loaded,
    reload,
    setData,
  };
}

/**
 * 提交型请求（POST/PUT/DELETE）：不做自动加载，返回命令式 run()。
 * 与 useAsync 分开是因为写操作不能因依赖变化自动重跑——会重复提交。
 */
export function useSubmit<TArgs extends unknown[], TResult>(
  fn: (...args: TArgs) => Promise<TResult>,
  options: { onError?: (message: string, error: unknown) => void; onSuccess?: (result: TResult) => void } = {},
) {
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState('');
  const [errorCode, setErrorCode] = useState('');
  const fnRef = useRef(fn);
  fnRef.current = fn;
  const optionsRef = useRef(options);
  optionsRef.current = options;
  // 卸载后不再 setState，避免 React 警告与状态错乱
  const mountedRef = useRef(true);
  useEffect(() => () => {
    mountedRef.current = false;
  }, []);

  const run = useCallback(async (...args: TArgs): Promise<TResult | undefined> => {
    setSubmitting(true);
    setError('');
    setErrorCode('');
    try {
      const result = await fnRef.current(...args);
      if (mountedRef.current) optionsRef.current.onSuccess?.(result);
      return result;
    } catch (cause) {
      const message = apiErrorText(cause);
      if (mountedRef.current) {
        setError(message);
        setErrorCode(cause instanceof ApiError ? cause.errorCode : 'UNKNOWN');
        optionsRef.current.onError?.(message, cause);
      }
      return undefined;
    } finally {
      if (mountedRef.current) setSubmitting(false);
    }
  }, []);

  return { run, submitting, error, errorCode };
}
