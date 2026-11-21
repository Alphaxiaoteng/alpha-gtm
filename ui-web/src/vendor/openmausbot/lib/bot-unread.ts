// Vendored from OpenMausBot `src/lib/bot-unread.ts`.
//
// Adaptation: the two browser-side helpers (`titleWithUnread`, `showBrowserUnread`) were
// dropped — they mutate `document.title` and the Badging API, which belong to the host app.
// The predicate the mascot reads (`botShowsUnread`) and the counts are unchanged.

type UnreadBot = {
  unread?: boolean;
  tasks?: ReadonlyArray<{ unread?: boolean; routineRunId?: string; archivedAt?: number; openAsks?: number }> | null;
};

export function botShowsUnread(bot: UnreadBot): boolean {
  if (!bot.tasks?.length) return Boolean(bot.unread);
  return bot.tasks.some((task) => Boolean(task.unread) && !task.routineRunId && task.archivedAt === undefined);
}

/** The asks waiting on the person in a bot's conversations: on a team, the ones in their inbox. */
export function openAsksOf(bot: Pick<UnreadBot, "tasks">): number {
  return (bot.tasks ?? []).reduce((sum, task) => sum + (task.openAsks ?? 0), 0);
}

/** What one bot adds to the unread count: one while it shows unread, or each ask waiting in it when they are more. */
export function botUnreadCount(bot: UnreadBot): number {
  return Math.max(botShowsUnread(bot) ? 1 : 0, openAsksOf(bot));
}
