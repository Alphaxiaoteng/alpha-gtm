import { register } from 'node:module'
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'

// Map @deepseek-ai/dsh-tools to the pass-through stub (or the real compiler
// when DSH_TOOLS_PATH is set) before loading the plugin.
register('./stub-hooks.mjs', import.meta.url)
const {
  validateConfig,
  formatToolList,
  callDecision,
  loadSchema,
  alphaNexusTool,
} = await import('../index.js')

function fakeNexus({ tools = [] } = {}) {
  const calls = []
  const server = createServer((req, res) => {
    let body = ''
    req.on('data', (c) => { body += c })
    req.on('end', () => {
      if (req.url === '/api/v1/tools/schema') {
        res.writeHead(200, { 'Content-Type': 'application/json' })
        res.end(JSON.stringify(tools))
        return
      }
      if (req.url === '/api/v1/tools/call') {
        const parsed = JSON.parse(body || '{}')
        calls.push(parsed)
        res.writeHead(200, { 'Content-Type': 'application/json' })
        res.end(JSON.stringify({ ok: true, tool: parsed.name, data: { echoed: parsed.args } }))
        return
      }
      res.writeHead(404)
      res.end('nope')
    })
  })
  return { server, calls }
}

const SAMPLE = [
  { name: 'account.list', category: 'account', side_effect: 'read', agent_visible: true, description: '列账号', parameters: [] },
  { name: 'account.tag.create', category: 'account', side_effect: 'write', agent_visible: true, description: '建标签', parameters: [] },
  { name: 'internal.debug', category: 'test', side_effect: 'read', agent_visible: false, description: '不可见', parameters: [] },
]

test('config: loopback default, rejects remote without opt-in', () => {
  const ok = validateConfig({})
  assert.equal(ok.value.base_url, 'http://127.0.0.1:17421')
  const remote = validateConfig({ base_url: 'https://evil.example.com' })
  assert.ok(remote.issues?.length)
  const allowed = validateConfig({ base_url: 'https://evil.example.com', allow_remote: true })
  assert.equal(allowed.value.base_url, 'https://evil.example.com')
})

test('formatToolList hides agent-invisible tools and filters by category', () => {
  const all = formatToolList(SAMPLE, '')
  assert.equal(all.length, 2)
  assert.ok(all.every((t) => t.name !== 'internal.debug'))
  const account = formatToolList(SAMPLE, 'account')
  assert.equal(account.length, 2)
  const none = formatToolList(SAMPLE, 'radar')
  assert.equal(none.length, 0)
})

test('callDecision: write needs confirm, invisible is refused, read passes', () => {
  const read = SAMPLE[0]
  const write = SAMPLE[1]
  const hidden = SAMPLE[2]
  assert.equal(callDecision(read, {}).ok, true)
  assert.equal(callDecision(write, {}).code, 'ALPHA_NEXUS_CONFIRM_REQUIRED')
  assert.equal(callDecision(write, { confirm: true }).ok, true)
  assert.equal(callDecision(hidden, {}).code, 'ALPHA_NEXUS_TOOL_NOT_AGENT_VISIBLE')
  assert.equal(callDecision(undefined, {}).code, 'ALPHA_NEXUS_TOOL_UNKNOWN')
})

test('tool: list returns agent-visible tools from a live server', async () => {
  const { server } = fakeNexus({ tools: SAMPLE })
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  const port = server.address().port
  const tool = alphaNexusTool({ base_url: `http://127.0.0.1:${port}`, auth_token: '', allow_remote: false })
  const out = await tool.execute({ action: 'list' }, { signal: undefined })
  assert.equal(out.ok, true)
  assert.equal(out.data.count, 2)
  server.close()
})

test('tool: read call executes; write call refuses without confirm', async () => {
  const { server, calls } = fakeNexus({ tools: SAMPLE })
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  const port = server.address().port
  const settings = { base_url: `http://127.0.0.1:${port}`, auth_token: '', allow_remote: false }
  const tool = alphaNexusTool(settings)

  const read = await tool.execute({ action: 'call', tool: 'account.list', args: { limit: 5 } }, { signal: undefined })
  assert.equal(read.ok, true)
  assert.equal(calls.length, 1)
  assert.equal(calls[0].name, 'account.list')

  const blocked = await tool.execute({ action: 'call', tool: 'account.tag.create', args: {} }, { signal: undefined })
  assert.equal(blocked.ok, false)
  assert.equal(blocked.data.error, 'ALPHA_NEXUS_CONFIRM_REQUIRED')
  assert.equal(calls.length, 1, 'refused write must not reach the server')

  const allowed = await tool.execute({ action: 'call', tool: 'account.tag.create', args: { name: 'x' }, confirm: true }, { signal: undefined })
  assert.equal(allowed.ok, true)
  assert.equal(calls.length, 2)
  server.close()
})

test('tool: unknown tool name is refused before any HTTP call', async () => {
  const { server, calls } = fakeNexus({ tools: SAMPLE })
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  const port = server.address().port
  const tool = alphaNexusTool({ base_url: `http://127.0.0.1:${port}`, auth_token: '', allow_remote: false })
  const out = await tool.execute({ action: 'call', tool: 'nope.nope' }, { signal: undefined })
  assert.equal(out.ok, false)
  assert.equal(out.data.error, 'ALPHA_NEXUS_TOOL_UNKNOWN')
  assert.equal(calls.length, 0)
  server.close()
})

test('schema is cached between calls', async () => {
  let hits = 0
  const server = createServer((req, res) => {
    hits += 1
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify(SAMPLE))
  })
  await new Promise((r) => server.listen(0, '127.0.0.1', r))
  const settings = { base_url: `http://127.0.0.1:${server.address().port}`, auth_token: '', allow_remote: false }
  await loadSchema(settings, undefined)
  await loadSchema(settings, undefined)
  assert.equal(hits, 1, 'second load must hit the cache')
  server.close()
})
