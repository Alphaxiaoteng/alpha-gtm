// The transcript row, lifted from OpenMausBot `src/components/ChatView.tsx` (the `Bubble`
// render path, lines ~420-520) with the store-bound parts replaced by props:
//
//   * `messageActionClass` and the `data-message-row` marker are upstream's, copied from
//     `src/components/MessageActions.tsx`; the menu machinery that file carries (popovers,
//     team memory, speech) is not vendored, so the row keeps the one action it can honour —
//     copy.
//   * `botMenu` / `forkAction` / reactions / reply quotes are gone: they are store actions.
//   * `turn-answer`, `animate-msg-in` become `omb-msg-in` / `omb-turn-answer`, defined in
//     ../theme.css so the vendored CSS does not depend on OMB's global stylesheet.

import type { ReactNode } from "react";
import { Check, Copy } from "lucide-react";
import { cn } from "../lib/cn";
import { useCopyFeedback } from "../lib/copy-text";
import { formatTime } from "../lib/clock";
import { joinedCorners, SIMPLE_BOT_BUBBLE, SIMPLE_BUBBLE_MAX, SIMPLE_BUBBLE_TEXT, SIMPLE_USER_BUBBLE, type BubbleJoin } from "./chat-bubble";
import { ChatMarkdown } from "./ChatMarkdown";
import { WorkingDots } from "./WorkingIndicator";
import type { Message } from "../types";

/** Upstream `MESSAGE_ROW`, from `components/MessageActions.tsx`. */
export const MESSAGE_ROW = "data-message-row";

export const messageActionClass =
  "inline-flex size-7 items-center justify-center rounded-full text-brand-fg/45 transition-colors hover:bg-brand-accent/10 hover:text-brand-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-accent/40 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent";

export function CopyButton({ text, className, onCopied }: { text: string; className?: string; onCopied?: (text: string) => void }) {
  const { state, copy } = useCopyFeedback(text);
  return (
    <button
      type="button"
      onClick={() => {
        copy();
        onCopied?.(text);
      }}
      aria-label="复制"
      title="复制"
      className={cn(className, state === "copied" && "text-brand-fg")}
    >
      {state === "copied" ? <Check size={15} strokeWidth={1.75} /> : <Copy size={15} strokeWidth={1.75} />}
    </button>
  );
}

export interface BubbleProps {
  message: Message;
  /** Which of the group's corners tighten, from `simpleRowLayout`. */
  join?: BubbleJoin;
  /** Simple mode's single surface tone; Advanced mode uses the wider card bubble. */
  simple?: boolean;
  /** Draw the working dots under a bot row whose turn is still running with no text yet. */
  working?: boolean;
  onCopy?: (text: string) => void;
  /** Alpha-Nexus hides message-level copy actions; code-block copy remains available. */
  showCopy?: boolean;
  /** Product-owned citations / actions, rendered inside the bot bubble. */
  accessory?: ReactNode;
}

export function Bubble({ message, join, simple = true, working = false, onCopy, showCopy = false, accessory }: BubbleProps) {
  const user = message.role === "user";
  const text = message.text ?? "";
  const streaming = message.streaming ?? false;

  if (message.kind === "activity" || message.kind === "digest") {
    return <ActivityRow message={message} />;
  }

  return (
    <div
      {...{ [MESSAGE_ROW]: "" }}
      data-look={user ? "message-user" : "message-bot"}
      className={cn("group flex w-full flex-col", user ? "omb-msg-in items-end" : "items-start")}
    >
      <div className={cn("flex w-full items-center gap-1.5", user ? "justify-end" : "justify-start")}>
        {showCopy && user && text ? <CopyButton text={text} className={cn(messageActionClass, "opacity-0 group-hover:opacity-100")} /> : null}
        <div
          data-chat-bubble={simple ? "simple" : ""}
          data-look={user ? "user-bubble" : "bot-bubble"}
          className={cn(
            "w-fit",
            user ? "agent-message-user" : "agent-message-assistant",
            simple ? SIMPLE_BUBBLE_MAX : "max-w-[min(var(--chat-bubble-max),var(--chat-bubble-pct))] rounded-2xl text-[15px] leading-relaxed",
            streaming && "omb-turn-answer",
            user
              ? cn("whitespace-pre-wrap [overflow-wrap:anywhere] text-brand-fg", simple ? SIMPLE_USER_BUBBLE : "bg-brand-surface-raised px-4 py-2.5")
              : simple
                ? SIMPLE_BOT_BUBBLE
                : "bg-brand-surface px-4 py-2.5 text-brand-fg",
            simple && join && joinedCorners(user ? "end" : "start", join),
          )}
          title={`${formatTime(message.at)}${message.tool ? ` · ${message.tool.name}` : ""}`}
        >
          {text ? (
            user ? (
              <div className={cn("chat-text whitespace-pre-wrap [overflow-wrap:anywhere]", simple && SIMPLE_BUBBLE_TEXT)}>{text}</div>
            ) : message.plain ? (
              <div className={cn("chat-text whitespace-pre-wrap [overflow-wrap:anywhere]", simple && SIMPLE_BUBBLE_TEXT)}>{text}</div>
            ) : (
              <ChatMarkdown text={text} streaming={streaming} className={cn("chat-text", simple && SIMPLE_BUBBLE_TEXT)} />
            )
          ) : null}
          {working && !text ? <WorkingDots size={5} className="py-1 text-brand-fg/40" /> : null}
          {message.attachments?.length ? (
            <div className="mt-1.5 flex flex-wrap gap-1">
              {message.attachments.map((attachment, index) => (
                <span
                  key={attachment.id ?? `${attachment.name ?? "file"}-${index}`}
                  className="inline-flex items-center rounded-full bg-brand-accent/[0.06] px-2 py-0.5 text-[11px] text-brand-fg/60"
                >
                  {attachment.name ?? attachment.mime ?? "附件"}
                </span>
              ))}
            </div>
          ) : null}
          {accessory}
        </div>
        {showCopy && !user && text ? <CopyButton text={text} className={cn(messageActionClass, "opacity-0 group-hover:opacity-100")} onCopied={onCopy} /> : null}
      </div>
    </div>
  );
}

/** A tool step or turn summary: upstream draws these narrow, monogrammed, and quiet. */
function ActivityRow({ message }: { message: Message }) {
  const failed = message.tool?.ok === false;
  return (
    <div {...{ [MESSAGE_ROW]: "" }} data-look="message-activity" className="omb-msg-in flex w-full flex-col items-start">
      <div
        className={cn(
          "flex max-w-[min(var(--chat-bubble-max),var(--chat-bubble-pct))] items-center gap-2 rounded-full px-3 py-1.5 text-[12px]",
          failed ? "bg-brand-danger/8 text-brand-danger" : "bg-brand-accent/[0.04] text-brand-fg/60",
        )}
      >
        <span className={cn("size-1.5 shrink-0 rounded-full", failed ? "bg-brand-danger" : "bg-brand-fg/30")} aria-hidden="true" />
        <span className="truncate">{message.tool?.label ?? message.tool?.summary ?? message.tool?.name ?? message.text ?? "步骤"}</span>
      </div>
    </div>
  );
}
