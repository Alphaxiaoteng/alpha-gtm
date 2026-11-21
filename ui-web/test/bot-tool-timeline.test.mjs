import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import test from 'node:test';

const read = (path) => readFile(new URL(path, import.meta.url), 'utf8');

// The Bot drawer must show what each tool actually did, not just its name.
// These assertions lock the contract between the Go read model and the panel.
test('bot tool timeline exposes params, result, duration and error hints', async () => {
  const drawer = await read('../src/components/AgentDrawer.tsx');
  const apiTs = await read('../src/api.ts');
  const handler = await read('../../internal/ui/agent_hub_handlers.go');
  const history = await read('../../internal/agenthub/tool_call_history.go');

  assert.match(apiTs, /agentHubConversationToolCalls/);
  assert.match(apiTs, /tool-calls\?limit=/);
  assert.match(apiTs, /export interface AgentToolCallDetail/);

  assert.match(handler, /handleAPIAgentConversationToolCalls/);
  assert.match(history, /ConversationToolCalls/);
  assert.match(history, /sanitizeToolPayload/);
  assert.match(history, /toolSecretKeyParts/);
  assert.match(history, /toolPayloadLimit/);

  assert.match(drawer, /data-testid="agent-tool-cards-section"/);
  assert.match(drawer, /data-testid=\{`agent-tool-card-\$\{call\.id\}`\}/);
  assert.match(drawer, /formatToolDuration\(call\.duration_ms\)/);
  assert.match(drawer, />参数</);
  assert.match(drawer, /结果/);
  assert.match(drawer, /toolErrorHint/);
  assert.match(drawer, /mergeToolCards/);
});

test('bot run strip and finished delegations stay visible', async () => {
  const drawer = await read('../src/components/AgentDrawer.tsx');
  const delegation = await read('../../internal/agenthub/delegation.go');

  assert.match(drawer, /data-testid="agent-run-strip"/);
  assert.match(drawer, /data-testid="agent-run-stop"/);
  assert.match(drawer, /工具 \$\{toolCards\.length\} 次/);
  assert.match(drawer, /longRun/);
  assert.match(drawer, /data-testid=\{`agent-delegation-done-\$\{item\.id\}`\}/);
  assert.match(delegation, /error_code/);
  assert.match(delegation, /result/);
  assert.match(drawer, /data-testid="agent-history"/);
  assert.match(drawer, /requestAgentConversation\(item\.id\)/);
  assert.match(drawer, /agent-history-item-/);
});
