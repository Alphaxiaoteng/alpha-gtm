// Vendored from OpenMausBot `src/components/Composer.tsx` (the `simpleDock` shell and the
// send row) and `src/components/composer-simple.ts` (`SIMPLE_COMPOSER`, `SIMPLE_CONTROL`).
//
// Upstream's Composer is the app's biggest component (992 lines) and is welded to the store:
// draft queues, mentions, slash commands, dictation, approval mode, call mode, place pinning,
// attachments upload. None of that belongs to a component vendor, so what is kept here is the
// part that is pure UI: the pill, the growing field, the round attach and send/stop buttons,
// the placeholder rules and the Enter/Shift+Enter contract.
//
// Adaptations:
//   * `bg-composer`, `ring-ink/10`, `text-ink-tertiary` → the Alpha-Nexus brand tokens.
//   * The follow-up menu (steer / queue / restart) is not vendored; `busy` only swaps Send
//     for Stop.
//   * `rows={1}` auto-grow is upstream's own measurement (scrollHeight capped at 200px).

import { useEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react";
import { ArrowUp, Paperclip, Square } from "lucide-react";
import { cn } from "../lib/cn";

/** Simple mode's composer pill on a wider screen: the field, the mic and the round button in one shell. */
export const SIMPLE_COMPOSER =
  "bg-brand-surface p-2 ring-[0.5px] ring-brand-accent/10 hover:ring-brand-accent/[0.22] focus-within:ring-brand-accent/[0.22] shadow-[0_2px_8px_-1px_rgb(0_0_0/0.05),0_1px_2px_rgb(0_0_0/0.03)] [transition:border-radius_300ms,box-shadow_150ms]";

/** Simple mode's small round controls in the composer: the +, the mic and Stop beside the round button. */
export const SIMPLE_CONTROL =
  "flex size-7 shrink-0 items-center justify-center rounded-full bg-brand-accent/[0.05] ring-[0.5px] ring-inset ring-brand-accent/[0.07] text-brand-fg/45 hover:bg-brand-accent/[0.09] hover:text-brand-fg";

const MAX_FIELD_HEIGHT = 200;

export interface ComposerProps {
  value: string;
  onChange: (value: string) => void;
  onSend: () => void;
  /** The turn is running: Send becomes Stop. */
  busy?: boolean;
  onStop?: () => void;
  /** Nothing to send: the round button stays muted. */
  disabled?: boolean;
  placeholder?: string;
  /** Files land here when the caller has an attachment pipeline; the button hides otherwise. */
  onAttach?: () => void;
  /** The disabled reason, read by the button's tooltip. */
  sendTitle?: string;
  className?: string;
  fieldRef?: RefObject<HTMLTextAreaElement | null>;
  textareaAriaLabel?: string;
}

export function Composer({
  value,
  onChange,
  onSend,
  busy = false,
  onStop,
  disabled = false,
  placeholder = "发消息…",
  onAttach,
  sendTitle,
  className,
  fieldRef,
  textareaAriaLabel,
}: ComposerProps) {
  const localField = useRef<HTMLTextAreaElement>(null);
  const field = fieldRef ?? localField;
  const [multiline, setMultiline] = useState(false);

  useEffect(() => {
    const node = field.current;
    if (!node) return;
    if (!value) {
      node.style.height = "";
      setMultiline(false);
      return;
    }
    node.style.height = "auto";
    if (node.scrollHeight > 0) {
      const next = Math.max(36, Math.min(node.scrollHeight, MAX_FIELD_HEIGHT));
      node.style.height = `${next}px`;
      setMultiline(next > 44);
    }
  }, [value]);

  const send = () => {
    if (busy) {
      onStop?.();
      return;
    }
    if (disabled || !value.trim()) return;
    onSend();
  };

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    // An input method's confirming Enter belongs to the method, not to this field.
    if (event.nativeEvent.isComposing) return;
    if (event.key !== "Enter" || event.shiftKey || event.metaKey || event.ctrlKey) return;
    event.preventDefault();
    send();
  };

  return (
    <div
      data-look="composer"
      data-multiline={multiline ? "" : undefined}
      className={cn(
        "@container/composer relative z-[1] flex items-end gap-1.5",
        SIMPLE_COMPOSER,
        multiline ? "rounded-[18px]" : "rounded-[22px]",
        className,
      )}
    >
      {onAttach ? (
        <button type="button" onClick={onAttach} aria-label="添加附件" title="添加附件" className={SIMPLE_CONTROL}>
          <Paperclip size={14} strokeWidth={1.75} />
        </button>
      ) : null}
      <textarea
        ref={field}
        rows={1}
        dir="auto"
        value={value}
        placeholder={placeholder}
        aria-label={textareaAriaLabel}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={onKeyDown}
        className="chat-text min-h-[36px] max-h-[200px] min-w-0 flex-1 resize-none bg-transparent px-1 py-[7px] text-[15px] leading-[22px] text-brand-fg outline-none placeholder:text-brand-fg/55"
      />
      <button
        type="button"
        onClick={send}
        disabled={!busy && (disabled || !value.trim())}
        aria-label={busy ? "停止" : "发送"}
        title={sendTitle ?? (busy ? "停止" : "发送")}
        className={cn(
          "flex size-8 shrink-0 items-center justify-center rounded-full transition-colors disabled:cursor-default",
          busy ? "bg-brand-accent/[0.06] text-brand-fg/55 hover:bg-brand-accent/[0.1]" : "bg-brand-accent text-brand-on-accent hover:brightness-110 disabled:bg-brand-fg/[0.12] disabled:text-brand-fg/60 disabled:opacity-100",
        )}
      >
        {busy ? <Square size={13} strokeWidth={2} /> : <ArrowUp size={16} strokeWidth={1.75} />}
      </button>
    </div>
  );
}
