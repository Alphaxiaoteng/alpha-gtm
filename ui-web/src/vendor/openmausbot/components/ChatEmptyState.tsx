// Vendored from OpenMausBot `src/components/ChatEmptyState.tsx` (`EmptyChat`).
//
// Adaptation: upstream's empty chat renamed its bot inline through the store and read the
// interface mode from a hook; the vendored copy takes the bot and a description as props and
// draws the mascot face itself.

import type { ReactNode } from "react";
import { BotAvatar } from "../mascot/Avatar";
import type { Bot } from "../types";

/** A chat with no messages yet: the bot, its name and what it is for. */
export function EmptyChat({
  bot,
  description,
  prompt = "发一条消息开始对话。",
  action,
}: {
  bot: Bot;
  description?: string;
  prompt?: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-3 py-24 text-center">
      <BotAvatar bot={bot} state="idle" size={64} motion="none" motionKey={0} />
      <div className="text-[17px] font-semibold text-brand-fg">{bot.name}</div>
      <div className="max-w-[360px] text-[14px] text-brand-fg/55">{description || bot.description || prompt}</div>
      {action}
    </div>
  );
}
