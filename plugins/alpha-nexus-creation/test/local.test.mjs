/**
 * Local probe for the Alpha-Nexus creation-workflow plugin.
 *
 * Runs with plain `node --test` (no dsh, no install step). The only dsh module
 * the plugin imports is `@deepseek-ai/dsh-tools`; ./stub-hooks.mjs maps it to a
 * pass-through stub, or to the real compiler when DSH_TOOLS_PATH is set.
 */
import { register } from 'node:module'
import { afterEach, test } from 'node:test'
import assert from 'node:assert/strict'

register('./stub-hooks.mjs', import.meta.url)

const plugin = await import('../index.js')
const { apply, Config, validateConfig, resolveStage } = plugin

const TOOL_NAME = 'alpha_nexus.creation_workflow_run'
const originalFetch = globalThis.fetch

afterEach(() => {
  globalThis.fetch = originalFetch
})

/** Minimal cordis-shaped context that records tool registrations. */
function stubContext() {
  const registered = []
  const effects = []
  const ctx = {
    tools: {
      register(definition) {
        registered.push(definition)
        return () => {}
      },
    },
    // cordis runs the effect body immediately and collects its disposer.
    effect(execute, label) {
      effects.push(label)
      return execute()
    },
  }
  return { ctx, registered, effects }
}

/** Resolve config the way cordis does, so defaults are applied. */
function settings(overrides = {}) {
  const result = validateConfig(overrides)
  assert.ok(result.value, `expected valid config, got ${JSON.stringify(result.issues)}`)
  return result.value
}

/** Record every fetch and reply from a queue of canned responses. */
function stubFetch(responses) {
  const calls = []
  globalThis.fetch = async (url, init) => {
    calls.push({ url: String(url), init })
    const next = responses.shift()
    if (next === undefined) throw new Error(`unexpected fetch: ${String(url)}`)
    return next
  }
  return calls
}

function jsonResponse(status, body) {
  const text = JSON.stringify(body)
  return { ok: status >= 200 && status < 300, status, text: async () => text }
}

function textResponse(status, text) {
  return { ok: status >= 200 && status < 300, status, text: async () => text }
}

function parseBody(call) {
  return JSON.parse(call.init.body)
}

test('Config is a StandardSchemaV1 object cordis can validate synchronously', () => {
  assert.equal(typeof Config['~standard'].validate, 'function')
  assert.equal(Config['~standard'].version, 1)
  const result = Config['~standard'].validate({})
  assert.equal('then' in result, false, 'validation must stay synchronous')
  assert.deepEqual(result.value, {
    base_url: 'http://127.0.0.1:17421',
    auth_token: '',
    allow_remote: false,
  })
})

test('apply registers exactly one tool and returns no throw', () => {
  const { ctx, registered } = stubContext()
  apply(ctx, settings())

  assert.equal(registered.length, 1)
  const [tool] = registered
  assert.equal(tool.name, TOOL_NAME)
  assert.equal(typeof tool.execute, 'function')
  // The stub keeps the author-facing DSL; the real `defineTool` compiles it to
  // JSON Schema, so read the property through either shape.
  const stageSchema = tool.parameters.stage ?? tool.parameters.properties?.stage
  assert.deepEqual(stageSchema.enum, ['topic', 'copy', 'script', 'review', 'outline'])
  assert.equal(tool.output.schema.additionalProperties, false)
  assert.deepEqual(Object.keys(tool.output.schema.properties), ['workflow_id', 'run'])
  assert.equal(typeof tool.output.render, 'function')
})

test('execute creates a workflow then starts one run, with exact URL/method/body', async () => {
  const calls = stubFetch([
    jsonResponse(201, { workflow: { id: 'cw_abc', stage: 'topic' }, conversation: { id: 'conv_1' } }),
    jsonResponse(200, { workflow: { id: 'cw_abc' }, run: { id: 'run_1', status: 'running' }, citations: [] }),
  ])
  const { ctx, registered } = stubContext()
  apply(ctx, settings({ base_url: 'http://127.0.0.1:17421' }))

  const signal = new AbortController().signal
  const result = await registered[0].execute(
    { title: '露营避坑', stage: 'topic', message: '我想做一条真实的露营避坑视频' },
    { signal },
  )

  assert.equal(calls.length, 2)

  assert.equal(calls[0].url, 'http://127.0.0.1:17421/api/creation/workflows')
  assert.equal(calls[0].init.method, 'POST')
  assert.equal(calls[0].init.headers['Content-Type'], 'application/json')
  assert.equal(calls[0].init.headers.Authorization, undefined)
  assert.equal(calls[0].init.signal, signal)
  assert.deepEqual(parseBody(calls[0]), { title: '露营避坑', stage: 'topic' })

  assert.equal(calls[1].url, 'http://127.0.0.1:17421/api/creation/workflows/cw_abc/runs')
  assert.equal(calls[1].init.method, 'POST')
  assert.deepEqual(parseBody(calls[1]), {
    stage: 'topic',
    message: '我想做一条真实的露营避坑视频',
    trend_context: '',
  })

  assert.deepEqual(result, { workflow_id: 'cw_abc', run: { id: 'run_1', status: 'running' } })
})

test('trend_context is forwarded when supplied', async () => {
  const calls = stubFetch([
    jsonResponse(201, { workflow: { id: 'cw_1' } }),
    jsonResponse(200, { run: { id: 'run_1', status: 'running' } }),
  ])
  const { ctx, registered } = stubContext()
  apply(ctx, settings())

  await registered[0].execute(
    { title: 'T', stage: 'script', message: 'm', trend_context: '本周露营话题升温' },
    {},
  )

  assert.equal(parseBody(calls[1]).trend_context, '本周露营话题升温')
  assert.equal(parseBody(calls[1]).stage, 'script')
})

test('"outline" is accepted as an alias for the backend stage "copy"', async () => {
  const calls = stubFetch([
    jsonResponse(201, { workflow: { id: 'cw_1' } }),
    jsonResponse(200, { run: { id: 'run_1' } }),
  ])
  const { ctx, registered } = stubContext()
  apply(ctx, settings())

  await registered[0].execute({ title: 'T', stage: 'outline', message: 'm' }, {})

  assert.equal(resolveStage('outline'), 'copy')
  assert.equal(parseBody(calls[0]).stage, 'copy')
  assert.equal(parseBody(calls[1]).stage, 'copy')
})

test('rejects an unknown stage before any HTTP call', async () => {
  const calls = stubFetch([])
  const { ctx, registered } = stubContext()
  apply(ctx, settings())

  // The stub path throws our own guard; the real `defineTool` rejects the
  // argument against the compiled enum first (ToolArgsError). Either way no
  // request may leave this plugin.
  await assert.rejects(
    () => registered[0].execute({ title: 'T', stage: 'publish', message: 'm' }, {}),
    /ALPHA_NEXUS_INVALID_STAGE|must be one of/,
  )
  assert.equal(calls.length, 0)
})

test('auth_token is sent as a Bearer header only when configured', async () => {
  const calls = stubFetch([
    jsonResponse(201, { workflow: { id: 'cw_1' } }),
    jsonResponse(200, { run: { id: 'run_1' } }),
  ])
  const { ctx, registered } = stubContext()
  apply(ctx, settings({ auth_token: 'tok_123' }))

  await registered[0].execute({ title: 'T', stage: 'topic', message: 'm' }, {})

  assert.equal(calls[0].init.headers.Authorization, 'Bearer tok_123')
  assert.equal(calls[1].init.headers.Authorization, 'Bearer tok_123')
})

test('workflow id is URI-encoded in the runs path', async () => {
  const calls = stubFetch([
    jsonResponse(201, { workflow: { id: 'cw a/b' } }),
    jsonResponse(200, { run: { id: 'run_1' } }),
  ])
  const { ctx, registered } = stubContext()
  apply(ctx, settings())

  await registered[0].execute({ title: 'T', stage: 'topic', message: 'm' }, {})

  assert.equal(calls[1].url, 'http://127.0.0.1:17421/api/creation/workflows/cw%20a%2Fb/runs')
})

test('backend rejection surfaces a stable ALPHA_NEXUS_HTTP_<status> error', async () => {
  stubFetch([textResponse(422, '{"error":"CREATION_MODEL_UNAVAILABLE"}')])
  const { ctx, registered } = stubContext()
  apply(ctx, settings())

  await assert.rejects(
    () => registered[0].execute({ title: 'T', stage: 'topic', message: 'm' }, {}),
    /ALPHA_NEXUS_HTTP_422/,
  )
})

test('a 2xx body without a workflow id fails loudly', async () => {
  stubFetch([jsonResponse(201, { conversation: { id: 'conv_1' } })])
  const { ctx, registered } = stubContext()
  apply(ctx, settings())

  await assert.rejects(
    () => registered[0].execute({ title: 'T', stage: 'topic', message: 'm' }, {}),
    /ALPHA_NEXUS_CREATE_NO_WORKFLOW_ID/,
  )
})

test('non-loopback base_url is refused unless allow_remote is true', () => {
  const refused = validateConfig({ base_url: 'http://192.0.2.10:17421' })
  assert.ok(refused.issues)
  assert.match(refused.issues[0].message, /allow_remote: true/)

  const allowed = validateConfig({ base_url: 'http://192.0.2.10:17421', allow_remote: true })
  assert.equal(allowed.value.base_url, 'http://192.0.2.10:17421')
})

test('loopback hosts are allowed and trailing slashes are stripped', () => {
  for (const host of ['127.0.0.1', 'localhost', '[::1]']) {
    const result = validateConfig({ base_url: `http://${host}:17421///` })
    assert.equal(result.value.base_url, `http://${host}:17421`)
  }
})

test('rejects non-http(s) schemes, embedded credentials and wrong option types', () => {
  assert.match(validateConfig({ base_url: 'ftp://127.0.0.1:17421' }).issues[0].message, /http\/https/)
  assert.match(
    validateConfig({ base_url: 'http://user:pass@127.0.0.1:17421' }).issues[0].message,
    /用户名\/密码/,
  )
  assert.ok(validateConfig({ base_url: 42 }).issues)
  assert.ok(validateConfig({ allow_remote: 'yes' }).issues)
  assert.ok(validateConfig({ base_url: '   ' }).issues)
  assert.ok(validateConfig({ base_url: 'not a url' }).issues)
})
