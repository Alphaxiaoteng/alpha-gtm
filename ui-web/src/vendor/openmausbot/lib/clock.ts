// Vendored from OpenMausBot `src/lib/working-time.ts` (`formatElapsed`) and
// `src/lib/clock-time.ts` (`formatTime`, reduced to a fixed-locale formatter).
//
// Adaptation: upstream `formatTime` resolves the locale through its i18n store; the vendored
// copy takes the locale as an option and defaults to the runtime locale.

export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  if (total < 60) return `${total}s`;
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}m ${String(seconds).padStart(2, "0")}s`;
}

let clock: { locale: string; format: Intl.DateTimeFormat } | undefined;

export function formatTime(at: number, locale = "zh-CN"): string {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return "--:--";
  if (clock?.locale !== locale) {
    clock = { locale, format: new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit" }) };
  }
  return clock.format.format(date);
}
