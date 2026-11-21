// The one bridge between Alpha-Nexus's agent API and the vendored conversation components.
//
// Read-only by construction: every function here performs a GET (or consumes the run event
// stream). Nothing writes, posts, approves, cancels or sends a message — the components take
// data in and callbacks out, and `AgentDrawer` keeps owning every action.
//
// Two shapes meet here:
//   Alpha-Nexus  `AgentHubConversation` / `AgentHubMessage` / `AgentHubToolCall` / `AgentEvent`
//   The vendored components use `Message.kind` / `Message.role` / `Message.at` (epoch ms) / `Bot` / `TranscriptItem`
//
// Where Alpha-Nexus cannot supply what the vendored components expect, the mapping says so
// rather than inventing it — see `runsOf` (the API has no GET runs list) and `toBot`.

import {
  api,
  streamAgentHubEvents,
  type AgentActivity,
  type AgentBot,
  type AgentHubConversation,
  type AgentHubEvent,
  type AgentHubMessage,
  type AgentHubProviderID,
  type AgentHubToolCall,
} from '../api';
import {
  DAY_WORDS,
  dayLabel,
  localDay,
  type Bot,
  type BotColor,
  type Message as BotMessage,
  type MessageAttachment,
  type MessageKind,
  type TranscriptItem,
} from '../vendor/openmausbot';

/** A conversation as the rail and the transcript header read it. */
export interface Conversation {
  id: string;
  title: string;
  providerID: AgentHubProviderID;
  modelID?: string;
  /** Persona the conversation belongs to; empty for the default Alpha Agent threads. */
  botID?: string;
  createdAt: number;
  updatedAt: number;
}

export type RunStatus = 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted' | 'unknown';

/** A run. Alpha-Nexus records runs but exposes no list endpoint, so these are rebuilt (see `runsOf`). */
export interface Run {
  id: string;
  conversationID: string;
  providerID?: AgentHubProviderID;
  modelID?: string;
  status: RunStatus;
  startedAt: number;
  endedAt?: number;
  errorCode?: string;
}

/** A message in the vendored components' shape. */
export type Message = BotMessage & {
  /** The run the message belongs to; Alpha-Nexus persists it on the row. */
  runID?: string;
  /** Attached knowledge citations came back for this run. */
  hasCitations?: boolean;
};

/** One proposed or executed tool call. */
export interface ToolCall {
  id: string;
  runID: string;
  name: string;
  workspace: string;
  riskLevel: 'L0' | 'L1' | 'L2' | 'L3';
  status: AgentHubToolCall['status'];
  impactPreview: string;
  approvalID?: string;
  errorCode?: string;
  updatedAt: number;
}

/** The approval a `ToolCall` is waiting on. */
export interface Approval {
  id: string;
  toolCallID: string;
  toolName: string;
  workspace: string;
  riskLevel: ToolCall['riskLevel'];
  impactPreview: string;
  status: 'pending' | 'approved' | 'rejected';
  requestedAt: number;
}

// ---------------------------------------------------------------------------------------
// Reading
// ---------------------------------------------------------------------------------------

function epoch(value: string | undefined): number {
  if (!value) return 0;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? 0 : parsed;
}

/** `AgentHubConversation` -> `Conversation`. */
export function toConversation(raw: AgentHubConversation): Conversation {
  return {
    id: raw.id,
    title: raw.title,
    providerID: raw.provider_id,
    modelID: raw.model_id,
    botID: raw.bot_id || undefined,
    createdAt: epoch(raw.created_at),
    updatedAt: epoch(raw.updated_at),
  };
}

/**
 * `AgentHubMessage` -> `Message`.
 *
 * The kind is the only lossy step: Alpha-Nexus stores plain text for every turn and keeps
 * tool activity in its own table, so a row is `text` unless the caller says otherwise (the
 * drawer's tool timeline passes `kind: 'activity'` for its own rows).
 */
export function toMessage(raw: AgentHubMessage, patch: Partial<Pick<Message, 'kind' | 'streaming'>> = {}): Message {
  return {
    id: raw.id,
    role: raw.role === 'user' ? 'user' : 'bot',
    kind: (patch.kind ?? 'text') as MessageKind,
    text: raw.content,
    at: epoch(raw.created_at),
    runID: raw.run_id,
    streaming: patch.streaming,
  };
}

export function toMessages(raw: AgentHubConversation): Message[] {
  return (raw.messages ?? []).map((message) => toMessage(message));
}

/**
 * The rows a transcript draws: a day separator when the day changes, then the messages.
 *
 * The unread line is the caller's to place — Alpha-Nexus tracks read state per conversation,
 * not per message, so `unreadIds` stays null in practice.
 */
export function toTranscript(messages: readonly Message[], unreadIds?: ReadonlySet<string> | null): TranscriptItem[] {
  const items: TranscriptItem[] = [];
  let day: number | null = null;
  for (const message of messages) {
    const messageDay = localDay(message.at);
    if (messageDay !== day) {
      day = messageDay;
      items.push({ kind: 'day', at: message.at, label: dayLabel(message.at, DAY_WORDS) });
    }
    if (unreadIds?.has(message.id)) items.push({ kind: 'unread' });
    items.push({ kind: 'message', message });
  }
  return items;
}

/**
 * Runs rebuilt from the conversation's messages.
 *
 * Alpha-Nexus exposes `POST /api/agent/conversations/{id}/runs` (create) and
 * `GET /api/agent/runs/{id}/events` (stream) but no runs list, so a run's identity comes from
 * the `run_id` its messages carry. Its status is whatever the caller learned from the stream
 * (`terminal`, filled by `streamRun`); without that it stays `unknown` rather than pretending
 * to be finished.
 */
export function runsOf(
  conversation: Conversation,
  messages: readonly Message[],
  terminal: Readonly<Record<string, RunStatus>> = {},
): Run[] {
  const order: string[] = [];
  const first = new Map<string, number>();
  const last = new Map<string, number>();
  const model = new Map<string, string | undefined>();
  for (const message of messages) {
    if (!message.runID) continue;
    if (!first.has(message.runID)) {
      order.push(message.runID);
      first.set(message.runID, message.at);
    }
    last.set(message.runID, message.at);
  }
  return order.map((id) => ({
    id,
    conversationID: conversation.id,
    providerID: conversation.providerID,
    modelID: model.get(id) ?? conversation.modelID,
    status: terminal[id] ?? 'unknown',
    startedAt: first.get(id) ?? 0,
    endedAt: last.get(id),
  }));
}

const BOT_COLORS: readonly BotColor[] = [
  'green',
  'blue',
  'red',
  'orange',
  'purple',
  'cyan',
  'pink',
  'yellow',
  'teal',
  'coral',
  'silver',
  'chrome',
];

function botColor(value: string | undefined): BotColor {
  const found = BOT_COLORS.find((color) => color === value);
  return found ?? 'green';
}

/**
 * `AgentBot` (the persona roster) -> `Bot` (what the avatar draws).
 *
 * `AgentBot.color` is free text in Alpha-Nexus; only the twelve mascot colours mean anything
 * to the renderer, so anything else falls back to `green`, which is upstream's default.
 */
export function toBot(raw: Pick<AgentBot, 'id' | 'name' | 'role' | 'instructions' | 'avatar' | 'color'> & { unread?: number }): Bot {
  return {
    id: raw.id,
    name: raw.name,
    color: botColor(raw.color),
    title: raw.role,
    description: raw.instructions,
    avatarUrl: raw.avatar || null,
    unread: Boolean(raw.unread),
  };
}

/** `AgentHubToolCall` -> `ToolCall`. */
export function toToolCall(raw: AgentHubToolCall): ToolCall {
  return {
    id: raw.id,
    runID: raw.run_id,
    name: raw.tool_name,
    workspace: raw.workspace,
    riskLevel: raw.risk_level,
    status: raw.status,
    impactPreview: raw.impact_preview,
    approvalID: raw.approval_id,
    errorCode: raw.error_code,
    updatedAt: epoch(raw.updated_at),
  };
}

/** `AgentActivity` (the read-only activity feed) -> `ToolCall`-shaped row the timeline can draw. */
export function activityToToolCall(activity: AgentActivity): ToolCall {
  return {
    id: `${activity.runId}:${activity.id}`,
    runID: activity.runId,
    name: activity.title,
    workspace: '',
    riskLevel: 'L0',
    status: activity.status === 'cancelled' ? 'cancelled' : activity.status,
    impactPreview: activity.summary,
    updatedAt: epoch(activity.occurredAt),
  };
}

/** Alpha-Nexus serialises `risk_level`/`tool_name`; the bridge's own rows are already camelCase. */
function isToolCall(value: ToolCall | AgentHubToolCall): value is ToolCall {
  return 'riskLevel' in value;
}

/** A pending tool call that carries an approval -> `Approval`. */
export function toApproval(call: ToolCall | AgentHubToolCall): Approval | null {
  const tool: ToolCall = isToolCall(call) ? call : toToolCall(call);
  if (!tool.approvalID) return null;
  const status = tool.status === 'approved' || tool.status === 'completed'
    ? 'approved'
    : tool.status === 'rejected'
      ? 'rejected'
      : 'pending';
  return {
    id: tool.approvalID,
    toolCallID: tool.id,
    toolName: tool.name,
    workspace: tool.workspace,
    riskLevel: tool.riskLevel,
    impactPreview: tool.impactPreview,
    status,
    requestedAt: tool.updatedAt,
  };
}

// ---------------------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------------------

/** One decoded SSE frame, in the vocabulary the transcript understands. */
export type ChatEvent =
  | { kind: 'delta'; text: string }
  | { kind: 'tool'; toolCall: ToolCall }
  | { kind: 'approval'; approval: Approval }
  | { kind: 'approval-resolved'; toolCallID: string; approvalID?: string; status: 'approved' | 'rejected' }
  | { kind: 'run'; status: RunStatus; errorCode?: string }
  | { kind: 'other'; type: string };

const RUN_EVENTS: Record<string, RunStatus> = {
  'run.completed': 'completed',
  'run.failed': 'failed',
  'run.cancelled': 'cancelled',
  'run.interrupted': 'interrupted',
};

const TOOL_EVENTS: Record<string, AgentHubToolCall['status']> = {
  'tool.proposed': 'proposed',
  'tool.started': 'running',
  'tool.completed': 'completed',
  'tool.failed': 'failed',
  'approval.required': 'pending',
};

function text(payload: Record<string, unknown>, key: string): string {
  const value = payload[key];
  return typeof value === 'string' ? value : '';
}

/**
 * `AgentHubEvent` -> `ChatEvent`.
 *
 * The event names are the ones `AgentDrawer` already switches on (`message.delta`, `tool.*`,
 * `approval.*`, `run.*`), so the two surfaces cannot drift apart without this table changing.
 */
export function toChatEvent(event: AgentHubEvent): ChatEvent {
  const payload = event.payload ?? {};
  if (event.type === 'message.delta') return { kind: 'delta', text: text(payload, 'text') };

  const runStatus = RUN_EVENTS[event.type];
  if (runStatus) return { kind: 'run', status: runStatus, errorCode: text(payload, 'code') || undefined };

  const toolCallID = text(payload, 'tool_call_id');
  const approvalID = text(payload, 'approval_id');

  if (event.type === 'approval.resolved') {
    return {
      kind: 'approval-resolved',
      toolCallID,
      approvalID: approvalID || undefined,
      status: payload.status === 'rejected' ? 'rejected' : 'approved',
    };
  }

  const toolStatus = TOOL_EVENTS[event.type];
  if (toolStatus) {
    const toolCall: ToolCall = {
      id: toolCallID,
      runID: event.runId,
      name: text(payload, 'tool_name'),
      workspace: text(payload, 'workspace'),
      riskLevel: (text(payload, 'risk_level') || 'L3') as ToolCall['riskLevel'],
      status: toolStatus,
      impactPreview: text(payload, 'impact_preview'),
      approvalID: approvalID || undefined,
      errorCode: text(payload, 'error_code') || undefined,
      updatedAt: epoch(event.timestamp),
    };
    const approval = toApproval(toolCall);
    if (event.type === 'approval.required' && approval) return { kind: 'approval', approval };
    return { kind: 'tool', toolCall };
  }

  return { kind: 'other', type: event.type };
}

// ---------------------------------------------------------------------------------------
// Fetching (all GET; nothing here writes)
// ---------------------------------------------------------------------------------------

export async function fetchConversations(
  providerID?: AgentHubProviderID,
  productMode: 'media' | 'shop' = 'media',
): Promise<Conversation[]> {
  const result = await api.agentHubConversations(providerID, productMode);
  return (result.conversations ?? []).map(toConversation);
}

/** The conversation with its messages, already in the components' shape. */
export async function fetchConversation(
  conversationID: string,
): Promise<{ conversation: Conversation; messages: Message[] }> {
  const raw = await api.agentHubConversation(conversationID);
  return { conversation: toConversation(raw), messages: toMessages(raw) };
}

/** The run's tool activity, oldest first, as timeline rows. */
export async function fetchToolCalls(runID: string, afterID = 0): Promise<ToolCall[]> {
  const result = await api.agentHubActivities(runID, afterID);
  return (result.activities ?? []).map(activityToToolCall);
}

/** The approvals a conversation is waiting on, from the bootstrap payload `AgentDrawer` already loads. */
export async function fetchApprovals(
  providerID?: AgentHubProviderID,
  productMode: 'media' | 'shop' = 'media',
): Promise<Approval[]> {
  const bootstrap = await api.agentHubBootstrap(providerID, productMode);
  return (bootstrap.pending_approvals ?? [])
    .map((call) => toApproval(call))
    .filter((approval): approval is Approval => approval !== null);
}

export interface RunStreamHandlers {
  onEvent: (event: ChatEvent, raw: AgentHubEvent) => void;
  /** The run reached a terminal frame; the stream has already closed itself. */
  onEnd?: (status: RunStatus) => void;
  onError?: (cause: unknown) => void;
}

/**
 * Follows one run's event stream and reports decoded frames.
 *
 * `streamAgentHubEvents` already reconnects with backoff and gives up on the terminal frame;
 * this wrapper only decodes. It returns an abort function, and the caller owns it — the
 * drawer's existing `AbortController` stays the single owner of a run's lifetime.
 */
export function streamRun(runID: string, handlers: RunStreamHandlers): () => void {
  const controller = new AbortController();
  const done = streamAgentHubEvents(
    runID,
    (event) => {
      const decoded = toChatEvent(event);
      handlers.onEvent(decoded, event);
      if (decoded.kind === 'run') {
        handlers.onEnd?.(decoded.status);
        return true;
      }
      return false;
    },
    controller.signal,
  ).catch((cause: unknown) => {
    if (controller.signal.aborted) return;
    handlers.onError?.(cause);
  });
  void done;
  return () => controller.abort();
}

// ---------------------------------------------------------------------------------------

/**
 * Attachments the transcript can show: Alpha-Nexus keeps media on disk and stores paths, so a
 * message's attachments only carry a name here, not a URL. `mediaURL` is the caller's resolver
 * (the drawer already has one for the media library).
 */
export function toAttachments(names: readonly string[], mediaURL?: (name: string) => string): MessageAttachment[] {
  return names.map((name) => ({ name, url: mediaURL?.(name) }));
}
