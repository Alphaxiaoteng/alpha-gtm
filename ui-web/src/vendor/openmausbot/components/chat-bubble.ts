// Vendored from OpenMausBot `src/components/chat-bubble.ts`.
//
// Adaptations: the store-side predicates (`bubbleGroup`, `quietRow`, `simpleRowLayout`,
// `needsStamp`) read the full `Message`/`TranscriptItem` shape, which the vendored copy keeps
// but with the peer-exchange, automation and leaked-harness helpers dropped. The bubble
// geometry — the part the eye sees — is unchanged, with `bg-card`/`bg-bubble-user`/`text-ink`
// mapped onto the Alpha-Nexus brand tokens.

import { cn } from "../lib/cn";
import { localDay } from "../lib/day";
import type { Message, TranscriptItem } from "../types";

/** Simple mode's message text: 15/22, the size whose cap height matches the design. */
export const SIMPLE_BUBBLE_TEXT = "text-[15px] leading-[22px]";

const SIMPLE_BUBBLE_SHAPE = `min-w-9 rounded-[18px] px-3 py-[7px] ${SIMPLE_BUBBLE_TEXT}`;

/** Simple mode's bot bubble: a quiet grey surface, not another white card on white. */
export const SIMPLE_BOT_BUBBLE = `${SIMPLE_BUBBLE_SHAPE} bg-brand-surface-raised text-brand-fg`;

/** Simple mode's own bubble; the bot's colour, when on, comes from its inline style. */
export const SIMPLE_USER_BUBBLE = `${SIMPLE_BUBBLE_SHAPE} bg-brand-surface-raised font-normal`;

/** Long replies may use the available row instead of stopping at a narrow centered measure. */
export const SIMPLE_BUBBLE_MAX = "max-w-full";

/** A plain-text bot bubble drawn outside the transcript's own Bubble: a widget's text fallback or a row that failed. */
export function plainBotBubble(simple: boolean): string {
  return simple
    ? cn("chat-text w-fit whitespace-pre-wrap", SIMPLE_BUBBLE_MAX, SIMPLE_BOT_BUBBLE)
    : "chat-text w-fit max-w-[min(var(--chat-bubble-max),var(--chat-bubble-pct))] rounded-2xl bg-brand-surface px-4 py-2.5 text-[15px] leading-relaxed whitespace-pre-wrap text-brand-fg";
}

/** Simple mode's transcript spaces rows 16px apart (ChatView's data-simple-rows); a row in the same group pulls up 12px, to 4px. */
export const SIMPLE_STACKED = "-mt-3";

/** Where a Simple bubble meets its group's neighbours: those corners tighten to 6px on the bubble's own side. */
export interface BubbleJoin {
  above: boolean;
  below: boolean;
}

export function joinedCorners(side: "start" | "end", join: BubbleJoin): string {
  return cn(
    join.above && (side === "start" ? "rounded-ss-[6px]" : "rounded-se-[6px]"),
    join.below && (side === "start" ? "rounded-es-[6px]" : "rounded-ee-[6px]"),
  );
}

/** Which bubble group a transcript row belongs to in Simple mode: this bot's words, the person's own words, or none. */
export function bubbleGroup(message: Message): "bot" | "user" | null {
  if (message.kind !== "text") return null;
  return message.role === "user" ? "user" : "bot";
}

export function isBotTextRow(message: Message): boolean {
  return bubbleGroup(message) === "bot";
}

/** A transcript row that draws nothing: a reaction, or a tool step while tool calls are hidden. */
export function quietRow(message: Message, showToolCalls: boolean): boolean {
  if (message.kind === "digest") return !showToolCalls;
  if (message.kind !== "activity" || showToolCalls) return false;
  return !(message.tool && message.tool.ok === false);
}

export interface TranscriptSlot {
  /** The bubble group this row joins, or null for a row that is no bubble (a card, a chip, a separator line). */
  group: "bot" | "user" | null;
  /** The row draws nothing, so it neither joins nor breaks a group. */
  hidden: boolean;
  /** Something stands between this row and the one above it: a day stamp, the unread line, a caption. */
  breakBefore: boolean;
  /** Something follows this row before the next one: an offer card. */
  breakAfter: boolean;
  /** A time stamp or the unread line stands above the row. */
  stamped?: boolean;
}

/** Each row's joins to its neighbours: a bubble joins the nearest drawn row on either side when that row is a bubble
 * of the same group and nothing stands between them. */
export function bubbleJoins(slots: readonly TranscriptSlot[]): BubbleJoin[] {
  const shown = slots.map((slot, index) => ({ slot, index })).filter(({ slot }) => !slot.hidden);
  const joins: BubbleJoin[] = slots.map(() => ({ above: false, below: false }));
  for (let at = 1; at < shown.length; at++) {
    const upper = shown[at - 1]!;
    const lower = shown[at]!;
    if (!upper.slot.group || upper.slot.group !== lower.slot.group || upper.slot.breakAfter || lower.slot.breakBefore) continue;
    joins[upper.index]!.below = true;
    joins[lower.index]!.above = true;
  }
  return joins;
}

/** Simple mode stamps the time again after a quiet spell, not only when the day changes. */
export const STAMP_GAP_MS = 15 * 60_000;

export function needsStamp(prev: Message | undefined, first: Message, simple: boolean): boolean {
  return !prev || localDay(prev.at) !== localDay(first.at) || (simple && first.at - prev.at >= STAMP_GAP_MS);
}

/** How Simple mode lays out a transcript's rows: which bubbles join, and which rows draw nothing. */
export function simpleRowLayout(
  items: readonly TranscriptItem[],
  { showToolCalls, unreadIds }: { showToolCalls: boolean; unreadIds: ReadonlySet<string> | null },
): { joins: BubbleJoin[]; dividerAt: number } {
  const rowsOf = (item: TranscriptItem) => (item.kind === "message" ? [item.message] : []);
  const dividerAt = unreadIds ? items.findIndex((item) => rowsOf(item).some((row) => unreadIds.has(row.id))) : -1;
  const slots = items.map((item, i): TranscriptSlot => {
    const previous = items[i - 1];
    const previousRow = previous && rowsOf(previous).at(-1);
    const firstRow = rowsOf(item)[0];
    const stamped = firstRow ? (needsStamp(previousRow, firstRow, true) && i > 0) || i === dividerAt : i === dividerAt;
    if (item.kind !== "message") return { group: null, hidden: false, breakBefore: true, breakAfter: false, stamped };
    const m = item.message;
    const group = bubbleGroup(m);
    return {
      group,
      hidden: !group && quietRow(m, showToolCalls),
      breakBefore: stamped || i === 0,
      breakAfter: false,
      stamped,
    };
  });
  return { joins: bubbleJoins(slots), dividerAt };
}
