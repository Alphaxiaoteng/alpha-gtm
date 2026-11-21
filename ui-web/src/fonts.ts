/**
 * Role-split typeface packs.
 * display / ui / body / mono are intentionally different faces —
 * never “one Inter for everything”.
 */
export type FontId =
  | 'atelier'
  | 'editorial'
  | 'noir'
  | 'system';

interface FontMeta {
  id: FontId;
  /** Page titles · wordmark · big numbers */
  display: string;
  /** Nav · buttons · form labels */
  ui: string;
  /** Body copy · descriptions */
  body: string;
  /** Paths · IDs · code */
  mono: string;
  tracking?: string;
  displayTracking?: string;
  weight?: number;
}

const FONTS: FontMeta[] = [
  {
    id: 'atelier',
    display: '"Syne", "Space Grotesk", sans-serif',
    ui: '"DM Sans", "Manrope", sans-serif',
    body: '"DM Sans", "Manrope", sans-serif',
    mono: '"JetBrains Mono", "IBM Plex Mono", monospace',
    tracking: '-0.011em',
    displayTracking: '-0.035em',
    weight: 450,
  },
  {
    id: 'editorial',
    // 标题衬线 + 正文无衬线：与暖纸 UI 一体
    display: '"Cormorant Garamond", "Fraunces", "Playfair Display", Georgia, serif',
    ui: '"Outfit", "Manrope", "DM Sans", sans-serif',
    body: '"Outfit", "Manrope", "DM Sans", sans-serif',
    mono: '"IBM Plex Mono", "JetBrains Mono", monospace',
    tracking: '-0.01em',
    displayTracking: '-0.02em',
    weight: 450,
  },
  {
    id: 'noir',
    // VibeCafé：全站等宽黑客字体，拒绝圆角 sans
    display: '"JetBrains Mono", "Fira Code", "SF Mono", ui-monospace, monospace',
    ui: '"JetBrains Mono", "Fira Code", "SF Mono", ui-monospace, monospace',
    body: '"JetBrains Mono", "Fira Code", "SF Mono", ui-monospace, monospace',
    mono: '"JetBrains Mono", "Fira Code", "SF Mono", ui-monospace, monospace',
    tracking: '0',
    displayTracking: '0',
    weight: 500,
  },
  {
    id: 'system',
    display: 'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    ui: 'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    body: 'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    mono: 'ui-monospace, "SF Mono", "Cascadia Mono", Menlo, Consolas, monospace',
    tracking: '-0.011em',
    displayTracking: '-0.02em',
    weight: 450,
  },
];

function fontMeta(id: FontId): FontMeta {
  return FONTS.find((f) => f.id === id) || FONTS[0];
}

/**
 * Apply role-split typeface pack to the entire document.
 */
export function applyFont(id: FontId) {
  const meta = fontMeta(id);
  const root = document.documentElement;
  const body = document.body;

  root.setAttribute('data-font', id);
  body?.setAttribute('data-font', id);

  // Role tokens
  root.style.setProperty('--font-display-runtime', meta.display);
  root.style.setProperty('--font-ui-runtime', meta.ui);
  root.style.setProperty('--font-sans-runtime', meta.body);
  root.style.setProperty('--font-body-runtime', meta.body);
  root.style.setProperty('--font-mono-runtime', meta.mono);
  // legacy aliases
  root.style.setProperty('--font-sans', meta.body);
  root.style.setProperty('--font-display', meta.display);
  root.style.setProperty('--font-mono', meta.mono);
  root.style.setProperty('--ui-letter', meta.tracking || '-0.011em');
  root.style.setProperty('--display-letter', meta.displayTracking || '-0.03em');
  root.style.setProperty('--font-ui-weight', String(meta.weight || 450));

  root.style.fontFamily = meta.body;
  if (body) {
    body.style.fontFamily = meta.body;
    body.style.letterSpacing = meta.tracking || '';
  }
  const app = document.getElementById('root');
  if (app) {
    app.style.fontFamily = meta.body;
    app.style.letterSpacing = meta.tracking || '';
  }
}
