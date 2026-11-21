# Vendored OpenMausBot conversation UI

The real chat surface from [OpenMausBot](https://github.com/milind-soni/OpenMausBot) — the
mascot avatars, the bubbles, the working dots, the composer pill and the markdown renderer —
carried into Alpha-Nexus so the agent drawer stops drawing its own chat by hand.

- Upstream commit: `accba5df9fd2c3d490c77cd1358cdf506a67e5e1` (see `COMMIT`)
- Licence: Apache-2.0 (see `NOTICE` for the file-by-file provenance)
- Theme: every colour resolves through Alpha-Nexus's `--brand-*` tokens, so it inherits the
  white theme; every corner is rounded, since upstream's bubbles already are.

## What is here

| Path | What it is |
| --- | --- |
| `mascot/CursorAvatar.tsx`, `mascot-motion.ts`, `mascot-clock.ts`, `cursor-face-data.ts`, `mascot-bodies.ts` | The animated mascot face: 30+ states, blink/spin, pointer gaze, the reduced-motion clock |
| `mascot/Avatar.tsx` | `BotAvatar` (picture, or the mascot) and `InitialsAvatar` |
| `mascot/mascot.ts` | Colours, motions, states, `stateForBot` |
| `components/Bubble.tsx` | One message row: user/bot bubble, joined corners, copy action, activity row |
| `components/ChatTranscript.tsx` | The list: day separators, per-group avatars, unread line, follow-to-bottom, thinking row |
| `components/ChatMarkdown.tsx` | Markdown for bot text: headings, lists, tables, code plate, links |
| `components/Composer.tsx` | The composer pill, growing field, Send/Stop |
| `components/WorkingIndicator.tsx` | `WorkingDots` / `WorkingTimer` — the typing animation |
| `components/chat-bubble.ts` | Bubble geometry and the row-join layout |
| `components/ChatEmptyState.tsx`, `MessageBoundary.tsx`, `NewMessagesDivider.tsx` | Empty chat, error boundary, unread rule |
| `lib/*` | `cn`, clock/day formatting, receipts, unread predicate, clipboard feedback |
| `types.ts` | `Message`, `Bot`, `TranscriptItem` in upstream's shape |
| `theme.css` | Bubble width tokens, `msg-in` / `status-pulse` keyframes, prose rules |

## What is not here, on purpose

`enterprise/`, Electron, the Node harness, the server, the reducer store, the i18n catalogue
and every hook that reads them. Components take props; nothing reads a global store.

## Wiring it into `AgentDrawer`

`src/lib/botBridge.ts` is the only module that knows both shapes. Nothing is wired yet —
`AgentDrawer.tsx` is untouched so the drawer keeps working exactly as it does today.

1. **Rows.** Replace the hand-rolled `(conversation?.messages || []).map(...)` block with:

   ```tsx
   import { ChatTranscript, transcriptItems } from '../vendor/openmausbot';
   import { toMessages, toTranscript, toBot } from '../lib/botBridge';

   const messages = useMemo(() => toMessages(conversation), [conversation]);
   <ChatTranscript bot={toBot(currentBot)} items={toTranscript(messages)} working={Boolean(activeRun)} />
   ```

2. **Streaming.** `streamAgentHubEvents` already runs in the drawer. Feed each frame through
   `toChatEvent` and append `delta.text` to a pending row (`toMessage(..., { streaming: true })`)
   instead of the current `streamingText` string.

3. **Composer.** Swap the `<textarea>` block for `<Composer value={draft} onChange={setDraft}
   onSend={submit} busy={Boolean(activeRun)} onStop={cancel} />`. The drawer keeps owning
   `agentHubRun` / `agentHubRunCancel`; the component owns layout only.

4. **Tools and approvals.** `fetchToolCalls(run.id)` and `fetchApprovals()` return the timeline
   rows the drawer already renders by hand; `activityToToolCall` maps the activity feed.

5. **Theme.** `theme.css` arrives with the barrel import. If the drawer wants the wide bubble
   column, put `data-omb-wide` on the transcript's container.

Keep the drawer's `BotRail`, `BotDetailPanel` and model selector as they are: they are not part
of this surface and the vendored components do not know about them.
