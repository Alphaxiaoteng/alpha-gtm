// Vendored from OpenMausBot `src/lib/transcript-derivations.ts` (`localDay`, `dayLabel`).
//
// Adaptation: `dayLabel` read its "Today"/"Yesterday" words from the i18n catalogue; the
// vendored copy takes them as arguments so the caller keeps its own copy.

export function localDay(at: number): number {
  const date = new Date(at);
  return Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()) / 86_400_000;
}

let dates: { locale: string; format: (at: number) => string } | null = null;

export function dayLabel(
  at: number,
  words: { today: string; yesterday: string },
  locale = "zh-CN",
  today = localDay(Date.now()),
): string {
  const daysAgo = today - localDay(at);
  if (daysAgo === 0) return words.today;
  if (daysAgo === 1) return words.yesterday;
  // toLocaleDateString's output, where Intl.DateTimeFormat.format would throw
  if (Number.isNaN(daysAgo)) return "Invalid Date";
  if (dates?.locale !== locale) {
    const formatter = new Intl.DateTimeFormat(locale, { weekday: "short", month: "short", day: "numeric" });
    dates = { locale, format: (value) => formatter.format(value) };
  }
  return dates.format(at);
}
