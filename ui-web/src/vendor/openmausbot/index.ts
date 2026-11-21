// Barrel for the vendored OpenMausBot conversation UI.
//
// Importing this module also loads `theme.css`: the bubble width tokens, the two arrival
// animations and the markdown prose rules the components rely on.

import "./theme.css";

export { BotAvatar, InitialsAvatar, MausAvatar, EYE_SCALE, MOUTH_WEIGHT, resolveBotAvatarOutcome } from "./mascot/Avatar";
export type { BotAvatarProps, BotAvatarOutcome, MausAvatarProps, MausAvatarHandle } from "./mascot/Avatar";
export { CURSOR_STATES } from "./mascot/CursorAvatar";
export type { CursorState, CursorAvatarHandle } from "./mascot/CursorAvatar";
export { MAUS_COLORS, MAUS_MOTIONS, MAUS_STATES, CHROME_EYES, CHROME_GRADIENT, PICKABLE_STATES, STATE_GROUPS, colorSwatch, normalizeState, stateForBot } from "./mascot/mascot";
export type { MausColor, MausMotion, MausState, MascotBotProfile } from "./mascot/mascot";
export { MASCOT_BODIES, MASCOT_BODY_IDS } from "./mascot/mascot-bodies";
export type { MascotBody, MascotBodyId } from "./mascot/mascot-bodies";

export { Bubble, CopyButton, MESSAGE_ROW, messageActionClass } from "./components/Bubble";
export type { BubbleProps } from "./components/Bubble";
export { ChatMarkdown, CodePlate, MARKDOWN_COMPONENTS } from "./components/ChatMarkdown";
export type { ChatMarkdownProps } from "./components/ChatMarkdown";
export { ChatTranscript, DAY_WORDS, transcriptItems } from "./components/ChatTranscript";
export type { ChatTranscriptProps } from "./components/ChatTranscript";
export { EmptyChat } from "./components/ChatEmptyState";
export { Composer, SIMPLE_COMPOSER, SIMPLE_CONTROL } from "./components/Composer";
export type { ComposerProps } from "./components/Composer";
export { MessageBoundary } from "./components/MessageBoundary";
export { NewMessagesDivider, UNREAD_DIVIDER_FADE_MS } from "./components/NewMessagesDivider";
export { WorkingDots, WorkingTimer } from "./components/WorkingIndicator";
export {
  SIMPLE_BOT_BUBBLE,
  SIMPLE_BUBBLE_MAX,
  SIMPLE_BUBBLE_TEXT,
  SIMPLE_STACKED,
  SIMPLE_USER_BUBBLE,
  STAMP_GAP_MS,
  bubbleGroup,
  bubbleJoins,
  isBotTextRow,
  joinedCorners,
  needsStamp,
  plainBotBubble,
  quietRow,
  simpleRowLayout,
} from "./components/chat-bubble";
export type { BubbleJoin, TranscriptSlot } from "./components/chat-bubble";

export { copyText, useCopyFeedback } from "./lib/copy-text";
export { formatElapsed, formatTime } from "./lib/clock";
export { dayLabel, localDay } from "./lib/day";
export { cn } from "./lib/cn";

export type {
  Bot,
  BotColor,
  Message,
  MessageAttachment,
  MessageKind,
  MessageRole,
  MessageTool,
  TranscriptItem,
} from "./types";
