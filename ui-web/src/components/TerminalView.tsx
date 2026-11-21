import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Terminal as XTerm } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import {
  Terminal as TermIcon,
  Sparkles,
  Square,
  Loader2,
  RefreshCw,
  FolderOpen,
} from 'lucide-react';
import { api, getToken, type OpenCodeAvailability } from '../api';
import { useUIStore } from '../store';

type Engine = 'shell' | 'claude' | 'opencode';

interface ProbeInfo {
  shell?: { binary?: string; available?: boolean };
  claude?: { binary?: string; available?: boolean; note?: string };
  opencode?: {
    binary?: string;
    available?: boolean;
    ready?: boolean;
    availability?: OpenCodeAvailability;
    note?: string;
  };
}

export const TerminalView: React.FC = () => {
  const { toast } = useUIStore();
  const hostRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const sessionIdRef = useRef<string | null>(null);

  const [engine, setEngine] = useState<Engine>('claude');
  const [probe, setProbe] = useState<ProbeInfo | null>(null);
  const [workDir, setWorkDir] = useState('');
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [running, setRunning] = useState(false);
  const [starting, setStarting] = useState(false);

  // init xterm once
  useEffect(() => {
    if (!hostRef.current || xtermRef.current) return;
    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
      theme: {
        background: '#0c0c0e',
        foreground: '#e4e4e7',
        cursor: '#111111',
        selectionBackground: '#3f3f46',
      },
      allowProposedApi: true,
      scrollback: 5000,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(hostRef.current);
    fit.fit();
    term.writeln('\x1b[90mAlpha-Nexus 内置终端 · Claude Code / OpenCode / Shell\x1b[0m');
    term.writeln('\x1b[90m选引擎后点「启动」。cc = Claude Code（不是 /usr/bin/cc 编译器）\x1b[0m');
    term.writeln('');
    xtermRef.current = term;
    fitRef.current = fit;

    const onResize = () => {
      try {
        fit.fit();
      } catch {
        /* ignore */
      }
    };
    window.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('resize', onResize);
      wsRef.current?.close();
      term.dispose();
      xtermRef.current = null;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const p = await api.terminalProbe();
        if (cancelled) return;
        setProbe(p.probe || {});
        setWorkDir(p.work_dir || '');
      } catch (e) {
        if (!cancelled) toast.show(String(e), 'error');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [toast]);

  const disconnect = useCallback(() => {
    wsRef.current?.close();
    wsRef.current = null;
    sessionIdRef.current = null;
    setSessionId(null);
    setRunning(false);
  }, []);

  const stop = useCallback(async () => {
    const id = sessionIdRef.current;
    disconnect();
    if (id) {
      try {
        await api.terminalStop(id);
      } catch {
        /* ignore */
      }
    }
    xtermRef.current?.writeln('\r\n\x1b[33m[stopped]\x1b[0m\r\n');
  }, [disconnect]);

  const start = useCallback(async (kind: Engine) => {
    if (starting) return;
    if (kind === 'opencode') {
      toast.show('OpenCode 受控只读接入尚未完成真实验证，旧 TUI 入口已禁用', 'error');
      return;
    }
    setStarting(true);
    setEngine(kind);
    try {
      // stop previous
      if (sessionIdRef.current) {
        await stop();
      }
      const term = xtermRef.current;
      term?.clear();
      term?.writeln(`\x1b[36m→ starting ${kind}…\x1b[0m\r\n`);

      const res = await api.terminalStart({
        kind,
        work_dir: workDir || undefined,
      });
      if (!res.ok || !res.session?.id) {
        throw new Error(res.error || 'start failed');
      }
      const sid = res.session.id;
      sessionIdRef.current = sid;
      setSessionId(sid);
      setRunning(true);

      const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
      const tok = getToken();
      const qs = new URLSearchParams({ id: sid });
      if (tok) qs.set('token', tok);
      const ws = new WebSocket(`${proto}://${window.location.host}/api/terminal/ws?${qs}`);
      ws.binaryType = 'arraybuffer';
      wsRef.current = ws;

      ws.onopen = () => {
        term?.writeln(`\x1b[32m✓ session ${sid} · ${res.session?.cmd_line || kind}\x1b[0m\r\n`);
        term?.focus();
      };
      ws.onmessage = (ev) => {
        const t = xtermRef.current;
        if (!t) return;
        if (typeof ev.data === 'string') {
          t.write(ev.data);
        } else if (ev.data instanceof ArrayBuffer) {
          t.write(new Uint8Array(ev.data));
        } else if (ev.data instanceof Blob) {
          ev.data.arrayBuffer().then((buf) => t.write(new Uint8Array(buf)));
        }
      };
      ws.onclose = () => {
        setRunning(false);
        xtermRef.current?.writeln('\r\n\x1b[90m[ws closed]\x1b[0m\r\n');
      };
      ws.onerror = () => {
        toast.show('终端 WebSocket 错误', 'error');
      };

      // wire keyboard
      const disposable = term?.onData((data) => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(data);
        }
      });
      ws.addEventListener('close', () => disposable?.dispose());

      toast.show(`${kind} 已启动`, 'success');
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      xtermRef.current?.writeln(`\x1b[31m✗ ${msg}\x1b[0m\r\n`);
      toast.show(msg, 'error');
      setRunning(false);
    } finally {
      setStarting(false);
    }
  }, [starting, workDir, stop, toast]);

  const openCodeAvailability: OpenCodeAvailability =
    probe?.opencode?.availability ?? (probe?.opencode?.binary ? 'available_unverified' : 'not_found');
  const engines: {
    id: Engine;
    label: string;
    sub: string;
    ready?: boolean;
    path?: string;
    enabled: boolean;
    statusLabel?: string;
  }[] = [
    {
      id: 'claude',
      label: 'Claude Code',
      sub: 'cc · claude CLI',
      ready: probe?.claude?.available,
      path: probe?.claude?.binary,
      enabled: probe?.claude?.available === true,
    },
    {
      id: 'opencode',
      label: 'OpenCode',
      sub: '受控 Bridge 待真实只读 smoke',
      ready: false,
      path: probe?.opencode?.binary,
      enabled: false,
      statusLabel: openCodeAvailability === 'available_unverified' ? '未验证' : '未安装',
    },
    {
      id: 'shell',
      label: 'Shell',
      sub: probe?.shell?.binary || 'zsh',
      ready: probe?.shell?.available !== false,
      path: probe?.shell?.binary,
      enabled: probe?.shell?.available !== false,
    },
  ];
  const selectedEngineEnabled = engines.find((candidate) => candidate.id === engine)?.enabled ?? false;

  return (
    <div className="flex flex-col flex-1 h-full min-h-0 gap-3" data-testid="terminal-view">
      <div className="flex flex-wrap items-start justify-between gap-3 shrink-0">
        <div>
          <div className="flex items-center gap-2">
            <TermIcon className="w-5 h-5 text-[var(--brand-accent)]" />
            <h3 className="text-base font-bold text-gray-900">终端</h3>
          </div>
          <p className="mt-1 text-xs text-gray-500 max-w-xl">
            Claude Code 可在应用内交互；OpenCode 仅保留受控 Bridge，完成真实只读 smoke 前不开放旧 TUI。
          </p>
        </div>
        <div className="flex items-center gap-2">
          {running ? (
            <button
              type="button"
              onClick={() => void stop()}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold bg-red-600 text-white hover:bg-red-700"
            >
              <Square className="w-3 h-3" /> 停止
            </button>
          ) : null}
          <button
            type="button"
            disabled={starting || !selectedEngineEnabled}
            onClick={() => void start(engine)}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold bg-[var(--brand-accent)] text-white hover:opacity-90 disabled:opacity-50"
          >
            {starting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
            {running ? '重启' : '启动'}
          </button>
        </div>
      </div>

      {/* engine buttons */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-2 shrink-0" data-testid="terminal-engines">
        {engines.map((e) => {
          const selected = engine === e.id;
          return (
            <button
              key={e.id}
              type="button"
              data-testid={`terminal-engine-${e.id}`}
              disabled={starting || !e.enabled}
              onClick={() => {
                if (!e.enabled) return;
                setEngine(e.id);
                if (!running) void start(e.id);
              }}
              className={[
                'text-left rounded-xl border p-3 transition',
                selected
                  ? 'border-[var(--brand-accent)] bg-[color-mix(in_srgb,var(--brand-accent)_10%,transparent)]'
                  : 'border-brand-border bg-brand-surface hover:border-gray-400',
              ].join(' ')}
            >
              <div className="flex items-center gap-2">
                {e.id === 'claude' ? (
                  <Sparkles className="w-4 h-4 text-[var(--brand-accent)]" />
                ) : e.id === 'opencode' ? (
                  <TermIcon className="w-4 h-4 text-[var(--brand-accent)]" />
                ) : (
                  <TermIcon className="w-4 h-4 text-gray-500" />
                )}
                <span className="text-sm font-semibold text-gray-900">{e.label}</span>
                {e.statusLabel ? (
                  <span className="ml-auto text-[9px] text-brand-warning">{e.statusLabel}</span>
                ) : e.ready === false ? (
                  <span className="ml-auto text-[9px] text-brand-warning">未安装</span>
                ) : (
                  <span className="ml-auto text-[9px] text-emerald-600">就绪</span>
                )}
              </div>
              <div className="mt-1 text-[10px] text-gray-500 font-mono truncate">{e.path || e.sub}</div>
            </button>
          );
        })}
      </div>

      <div className="flex flex-wrap items-center gap-2 text-[11px] text-gray-500 shrink-0">
        <FolderOpen className="w-3.5 h-3.5" />
        <input
          value={workDir}
          onChange={(e) => setWorkDir(e.target.value)}
          className="flex-1 min-w-[200px] max-w-xl rounded-md border border-brand-border bg-white/80 px-2 py-1 font-mono text-[11px] text-gray-700"
          placeholder="工作目录 cwd"
        />
        {sessionId ? (
          <span className="font-mono text-[10px] text-zinc-400">session {sessionId}</span>
        ) : null}
      </div>

      <div
        ref={hostRef}
        data-testid="terminal-xterm"
        className="flex-1 min-h-[360px] rounded-xl overflow-hidden border border-brand-border bg-[#0c0c0e] p-2"
      />
    </div>
  );
};
