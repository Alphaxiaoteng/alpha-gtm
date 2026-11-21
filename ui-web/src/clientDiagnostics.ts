import { getToken } from './api';

export type ClientDiagnosticKind = 'window_error' | 'unhandled_rejection' | 'react_error';

let installed = false;

function errorMaterial(value: unknown): string {
  if (value instanceof Error) return value.stack || value.message || value.name;
  if (typeof value === 'string') return value;
  return Object.prototype.toString.call(value);
}

async function sha256(value: string): Promise<string> {
  const bytes = new TextEncoder().encode(value);
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('');
}

export async function reportClientDiagnostic(kind: ClientDiagnosticKind, value: unknown): Promise<void> {
  try {
    const fingerprint = await sha256(errorMaterial(value));
    const token = getToken();
    await fetch('/api/diagnostics/client-event', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      body: JSON.stringify({ kind, fingerprint }),
    });
  } catch {
    // Diagnostics must never trigger another user-visible failure.
  }
}

export function installClientDiagnostics(): void {
  if (installed) return;
  installed = true;
  window.addEventListener('error', (event) => {
    void reportClientDiagnostic('window_error', event.error ?? event.message);
  });
  window.addEventListener('unhandledrejection', (event) => {
    void reportClientDiagnostic('unhandled_rejection', event.reason);
  });
}
