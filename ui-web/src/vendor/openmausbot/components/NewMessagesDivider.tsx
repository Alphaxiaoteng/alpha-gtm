// Vendored from OpenMausBot `src/components/NewMessagesDivider.tsx`.
//
// Adaptation: the label came from the i18n catalogue and the fade duration from
// `lib/unread-divider`; both are props here, and `bg-linear-to-*` is written with
// Alpha-Nexus's accent token.

import { cn } from "../lib/cn";

export const UNREAD_DIVIDER_FADE_MS = 220;

export function NewMessagesDivider({ fading, label = "新消息" }: { fading: boolean; label?: string }) {
  return (
    <div
      role="separator"
      aria-label={label}
      data-unread-divider
      className={cn(
        "grid ease-out transition-[grid-template-rows,opacity,margin-bottom] motion-reduce:transition-none",
        fading ? "-mb-3 grid-rows-[0fr] opacity-0" : "grid-rows-[1fr] opacity-100",
      )}
      style={{ transitionDuration: `${UNREAD_DIVIDER_FADE_MS}ms` }}
    >
      <div className="min-h-0 overflow-hidden">
        <div aria-hidden="true" className="omb-msg-in flex items-center gap-3 py-0.5">
          <span className="h-px flex-1 bg-linear-to-r from-transparent to-brand-accent/60 rtl:bg-linear-to-l" />
          <span className="text-[11px] font-semibold uppercase leading-4 tracking-[0.08em] text-brand-fg">
            {label}
          </span>
          <span className="h-px flex-1 bg-linear-to-l from-transparent to-brand-accent/60 rtl:bg-linear-to-r" />
        </div>
      </div>
    </div>
  );
}
