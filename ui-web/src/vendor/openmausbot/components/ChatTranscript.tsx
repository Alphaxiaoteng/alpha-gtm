// The transcript list, lifted from OpenMausBot `src/components/ChatView.tsx` (the row loop,
// the day separator and the not-yet-read line) and `src/hooks/use-transcript-viewport.ts`
// (the follow-to-bottom rule, reduced to its sticky behaviour).
//
// Adaptations:
//   * Rows come in as `TranscriptItem[]` instead of being read from the store, and the layout
//     they get is `simpleRowLayout` unchanged.
//   * The avatar is drawn once per bubble group, in the gutter, which is how upstream's chat
//     column arranges it.
//   * Following the bottom uses the same "only when the reader is already near it" rule, at a
//     32px threshold.

import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { dayLabel, localDay } from "../lib/day";
import { formatTime } from "../lib/clock";
import { BotAvatar } from "../mascot/Avatar";
import { simpleRowLayout } from "./chat-bubble";
import { Bubble } from "./Bubble";
import { EmptyChat } from "./ChatEmptyState";
import { MessageBoundary } from "./MessageBoundary";
import { NewMessagesDivider } from "./NewMessagesDivider";
import { WorkingDots } from "./WorkingIndicator";
import type { Bot, Message, TranscriptItem } from "../types";

const FOLLOW_THRESHOLD_PX = 32;

export const DAY_WORDS = { today: "今天", yesterday: "昨天" };

/** Turns a message list into the rows a transcript draws: a day separator, the messages, the unread line. */
export function transcriptItems(messages: readonly Message[], unreadIds?: ReadonlySet<string> | null): TranscriptItem[] {
  const items: TranscriptItem[] = [];
  let day: number | null = null;
  let unreadPlaced = !unreadIds || unreadIds.size === 0;
  for (const message of messages) {
    const messageDay = localDay(message.at);
    if (messageDay !== day) {
      day = messageDay;
      items.push({ kind: "day", at: message.at, label: dayLabel(message.at, DAY_WORDS) });
    }
    if (!unreadPlaced && unreadIds?.has(message.id)) {
      items.push({ kind: "unread" });
      unreadPlaced = true;
    }
    items.push({ kind: "message", message });
  }
  return items;
}

export interface ChatTranscriptProps {
  bot: Bot;
  items: readonly TranscriptItem[];
  /** Tool steps and digests stay in the list only when this is on. */
  showToolCalls?: boolean;
  /** The turn is running: the last bot row shows the working dots and a fresh row appears under it. */
  working?: boolean;
  /** Simple mode: the 18px bubbles. Advanced: the 2xl card bubbles. */
  simple?: boolean;
  unreadIds?: ReadonlySet<string> | null;
  /** Replaces the empty state; defaults to `EmptyChat`. */
  empty?: ReactNode;
  /** Marks the unread line as read a beat after it is drawn, as upstream does. */
  fadeUnread?: boolean;
  className?: string;
  /** Optional override when the product already owns the transcript padding. */
  contentClassName?: string;
  onCopy?: (text: string) => void;
  showCopy?: boolean;
  /** Keep Alpha-Nexus pixel/platform avatars instead of replacing them with mascot blobs. */
  renderAvatar?: (message?: Message) => ReactNode;
  renderAccessory?: (message: Message) => ReactNode;
}

export function ChatTranscript({
  bot,
  items,
  showToolCalls = false,
  working = false,
  simple = true,
  unreadIds = null,
  empty,
  fadeUnread = false,
  className,
  contentClassName,
  onCopy,
  showCopy = false,
  renderAvatar,
  renderAccessory,
}: ChatTranscriptProps) {
  const scroller = useRef<HTMLDivElement>(null);
  const [following, setFollowing] = useState(true);
  const { joins } = simpleRowLayout(items, { showToolCalls, unreadIds });
  const [unreadFading, setUnreadFading] = useState(false);

  useEffect(() => {
    if (!fadeUnread) return;
    const timer = setTimeout(() => setUnreadFading(true), 1200);
    return () => clearTimeout(timer);
  }, [fadeUnread]);

  useLayoutEffect(() => {
    const node = scroller.current;
    if (!node || !following) return;
    node.scrollTop = node.scrollHeight;
  }, [items, following]);

  if (!items.length) {
    return <div className={cn("flex min-h-0 flex-1 flex-col", className)}>{empty ?? <EmptyChat bot={bot} />}</div>;
  }

  const lastBotIndex = (() => {
    for (let index = items.length - 1; index >= 0; index -= 1) {
      const item = items[index]!;
      if (item.kind === "message" && item.message.role === "bot") return index;
    }
    return -1;
  })();
  const lastUserIndex = (() => {
    for (let index = items.length - 1; index >= 0; index -= 1) {
      const item = items[index]!;
      if (item.kind === "message" && item.message.role === "user") return index;
    }
    return -1;
  })();
  const lastBotRow = lastBotIndex === -1 ? null : items[lastBotIndex]!;
  const lastBotKind = lastBotRow?.kind === "message" ? lastBotRow.message.kind : null;
  // Nothing of the bot's own text for the current turn is in the transcript yet
  // (lastBotIndex <= lastUserIndex means only previous turns have bot replies),
  // so keep the thinking indicator visible until this turn's reply text arrives.
  const thinkingRow = working && !(lastBotIndex > lastUserIndex && lastBotKind === "text");

  return (
    <div
      ref={scroller}
      data-transcript
      onScroll={(event) => {
        const node = event.currentTarget;
        setFollowing(node.scrollHeight - node.scrollTop - node.clientHeight <= FOLLOW_THRESHOLD_PX);
      }}
      className={cn("min-h-0 flex-1 overflow-y-auto", className)}
    >
      <div className={contentClassName ?? "flex flex-col gap-4 px-5 py-5"} data-simple-rows={simple ? "" : undefined}>
        {items.map((item, index) => {
          if (item.kind === "day") {
            return (
              <div key={`day-${item.at}-${index}`} className="omb-msg-in flex items-center justify-center py-1">
                <span className="rounded-full bg-brand-accent/[0.04] px-2.5 py-1 text-[11px] font-medium text-brand-fg/45">
                  {item.label}
                </span>
              </div>
            );
          }
          if (item.kind === "unread") {
            return <NewMessagesDivider key={`unread-${index}`} fading={unreadFading} />;
          }
          const message = item.message;
          const join = joins[index]!;
          const previous = items[index - 1];
          const stampsTime =
            previous === undefined ||
            previous.kind !== "message" ||
            message.at - previous.message.at >= 15 * 60_000 ||
            localDay(previous.message.at) !== localDay(message.at);
          const firstOfGroup = !join.above;
          const isUser = message.role === "user";
          return (
            <div key={message.id} className="flex w-full flex-col gap-1.5">
              {stampsTime && previous !== undefined ? (
                <span className={cn("text-[11px] text-brand-fg/35", isUser ? "self-end pr-1" : "self-start pl-[50px]")}>
                  {formatTime(message.at)}
                </span>
              ) : null}
              <div className={cn("flex w-full gap-2.5", isUser ? "justify-end" : "items-start justify-start")}>
                {!isUser ? (
                  <span className="w-10 shrink-0 self-start pt-0.5">
                    {firstOfGroup ? (
                      renderAvatar ? renderAvatar(message) : <BotAvatar bot={bot} size={40} motion={message.streaming ? "thinking" : "none"} motionKey={0} animated />
                    ) : null}
                  </span>
                ) : null}
                <MessageBoundary fallbackText={message.text ?? ""} simple={simple}>
                  <Bubble
                    message={message}
                    join={join}
                    simple={simple}
                    working={working && index === lastBotIndex}
                    onCopy={onCopy}
                    showCopy={showCopy}
                    accessory={renderAccessory?.(message)}
                  />
                </MessageBoundary>
              </div>
            </div>
          );
        })}
        {thinkingRow ? (
          <div className="flex items-center gap-2.5">
            {renderAvatar ? renderAvatar() : <BotAvatar bot={bot} size={40} state="working" motion="working" motionKey={0} animated />}
            <span className="flex items-center gap-1.5 text-[13px] text-brand-fg/45">
              <WorkingDots size={5} />
              思考中…
            </span>
          </div>
        ) : null}
      </div>
    </div>
  );
}
