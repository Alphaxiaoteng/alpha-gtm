// Shared types for the vendored OpenMausBot conversation UI.
//
// Derived from OpenMausBot `src/state/store.tsx` (`Message`, `Bot`) with the fields this
// surface draws kept and the store-only ones (peer exchange, room holds, queue receipts,
// team memory) dropped. Field names and string unions are upstream's, so a row reads the
// same way it does there.

/** Mascot body ids the avatar renderer knows (`shared/mascot-bodies.ts`). */
export type MascotBodyId =
  | "cursor"
  | "blob"
  | "circle"
  | "squircle"
  | "capsule"
  | "drop"
  | "shield"
  | "hexagon"
  | "diamond"
  | "star";

/** Upstream `Message["kind"]`; the vendored rows draw text, activity and a generic card. */
export type MessageKind =
  | "text"
  | "options"
  | "activity"
  | "screen"
  | "connector"
  | "secret"
  | "signin"
  | "routine.run"
  | "goal.run"
  | "burst"
  | "digest"
  | "compaction"
  | "call"
  | "widget"
  | "app";

export type MessageRole = "bot" | "user";

export interface MessageTool {
  name: string;
  ok?: boolean;
  summary?: string;
  input?: string;
  output?: string;
  step?: string;
  label?: string;
  problem?: string;
}

export interface MessageAttachment {
  id?: string;
  name?: string;
  url?: string;
  mime?: string;
  size?: number;
}

export interface Message {
  id: string;
  role: MessageRole;
  kind: MessageKind;
  text?: string;
  /** Epoch milliseconds, as upstream's `Message.at`. */
  at: number;
  attachments?: MessageAttachment[];
  tool?: MessageTool;
  /** A tool or status row that the transcript may hide when tool calls are off. */
  steered?: boolean;
  errorCode?: string;
  /** Set while the message is still arriving, so the row draws its streaming tail. */
  streaming?: boolean;
  /** Alpha-Nexus renders deltas as plain text to avoid reparsing Markdown per token. */
  plain?: boolean;
}

/** The transcript row the vendored list draws: a message, or a day/unread separator. */
export type TranscriptItem =
  | { kind: "message"; message: Message }
  | { kind: "day"; at: number; label: string }
  | { kind: "unread" };

export type BotColor =
  | "green"
  | "blue"
  | "red"
  | "orange"
  | "purple"
  | "cyan"
  | "pink"
  | "yellow"
  | "teal"
  | "coral"
  | "silver"
  | "chrome";

export interface Bot {
  id: string;
  name: string;
  color: BotColor;
  title?: string;
  description?: string;
  avatarUrl?: string | null;
  mascotBody?: MascotBodyId | null;
  busy?: boolean;
  unread?: boolean;
}
