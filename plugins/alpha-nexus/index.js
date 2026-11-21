/**
 * Alpha-Nexus DSH plugin — one tool that proxies the whole business surface.
 *
 * Why ONE tool instead of one per capability: Alpha-Nexus exposes 66
 * agent-visible tools. Registering all of them would cost ~20k prompt tokens in
 * every DSH turn. A single `alpha_nexus` tool with action=list/call costs ~300
 * tokens and still reaches everything.
 *
 * Contract (Alpha-Nexus side, `internal/ui/tools_handlers.go`):
 *   GET  {base_url}/api/v1/tools/schema -> [{name, description, category,
 *                                            side_effect, agent_visible, parameters}]
 *   POST {base_url}/api/v1/tools/call   <- {name, args} -> {ok, tool, data|error}
 *
 * Safety: the HTTP endpoint has no approval gate, so this plugin adds one —
 * a `side_effect: "write"` tool runs only when the caller passes confirm=true.
 *
 * Hard constraints kept:
 *   - No hardcoded credential or filesystem path; all config comes from `Config`.
 *   - Pure HTTP client: no fs, no child_process, no listening socket.
 *   - Remote hosts need explicit allow_remote.
 */
import { defineTool } from '@deepseek-ai/dsh-tools'

export const name = 'alpha-nexus'
export const inject = ['tools']

const DEFAULT_BASE_URL = 'http://127.0.0.1:17421'
const LOOPBACK_HOSTNAMES = new Set(['127.0.0.1', 'localhost', '::1'])
const SCHEMA_TTL_MS = 5 * 60 * 1000

function issue(message, path) {
  return path === undefined ? { message } : { message, path }
}

function normalizeBaseURL(baseURL, allowRemote) {
  let url
  try {
    url = new URL(baseURL)
  } catch {
    return { error: 'base_url 不是合法 URL' }
  }
  if (url.protocol !== 'http:' && url.protocol !== 'https:') {
    return { error: 'base_url 只允许 http/https' }
  }
  if (url.username !== '' || url.password !== '') {
    return { error: 'base_url 不允许内嵌用户名/密码' }
  }
  const hostname = url.hostname.replace(/^\[/, '').replace(/\]$/, '')
  if (!LOOPBACK_HOSTNAMES.has(hostname) && allowRemote !== true) {
    return { error: `base_url 主机 "${hostname}" 不是回环地址；确需远程请设置 allow_remote: true` }
  }
  return { value: url.origin + url.pathname.replace(/\/+$/, '') }
}

export function validateConfig(raw) {
  const input = raw ?? {}
  if (typeof input !== 'object' || Array.isArray(input)) {
    return { issues: [issue('插件配置必须是对象')] }
  }
  const issues = []
  if (input.base_url !== undefined && typeof input.base_url !== 'string') {
    issues.push(issue('base_url 必须是字符串', ['base_url']))
  }
  if (input.auth_token !== undefined && typeof input.auth_token !== 'string') {
    issues.push(issue('auth_token 必须是字符串', ['auth_token']))
  }
  if (input.allow_remote !== undefined && typeof input.allow_remote !== 'boolean') {
    issues.push(issue('allow_remote 必须是布尔值', ['allow_remote']))
  }
  if (issues.length > 0) return { issues }

  const allowRemote = input.allow_remote === true
  const normalized = normalizeBaseURL((input.base_url ?? DEFAULT_BASE_URL).trim(), allowRemote)
  if (normalized.error !== undefined) {
    return { issues: [issue(normalized.error, ['base_url'])] }
  }
  return {
    value: {
      base_url: normalized.value,
      auth_token: (input.auth_token ?? '').trim(),
      allow_remote: allowRemote,
    },
  }
}

export const Config = {
  '~standard': {
    version: 1,
    vendor: 'alpha-nexus',
    validate: validateConfig,
  },
}

function authHeaders(settings) {
  const headers = { 'Content-Type': 'application/json' }
  if (settings.auth_token !== '') headers.Authorization = `Bearer ${settings.auth_token}`
  return headers
}

async function getJSON(settings, path, signal) {
  const response = await fetch(`${settings.base_url}${path}`, {
    method: 'GET',
    headers: authHeaders(settings),
    signal,
  })
  const text = await response.text()
  if (!response.ok) throw new Error(`ALPHA_NEXUS_HTTP_${response.status}: ${text.slice(0, 200)}`)
  return text === '' ? null : JSON.parse(text)
}

async function postJSON(settings, path, body, signal) {
  const response = await fetch(`${settings.base_url}${path}`, {
    method: 'POST',
    headers: authHeaders(settings),
    body: JSON.stringify(body),
    signal,
  })
  const text = await response.text()
  if (!response.ok) throw new Error(`ALPHA_NEXUS_HTTP_${response.status}: ${text.slice(0, 300)}`)
  return text === '' ? {} : JSON.parse(text)
}

// Small cache so a call does not re-fetch the 97-entry schema every turn.
const schemaCache = new Map()

/** @returns {Promise<Array<object>>} */
export async function loadSchema(settings, signal) {
  const cached = schemaCache.get(settings.base_url)
  if (cached && Date.now() - cached.at < SCHEMA_TTL_MS) return cached.tools
  const raw = await getJSON(settings, '/api/v1/tools/schema', signal)
  const tools = Array.isArray(raw) ? raw : raw?.tools ?? []
  schemaCache.set(settings.base_url, { at: Date.now(), tools })
  return tools
}

function byName(tools, name) {
  return tools.find((item) => item.name === name)
}

/** Pure formatter so tests can assert the list shape without HTTP. */
export function formatToolList(tools, category) {
  return (tools ?? [])
    .filter((item) => item.agent_visible !== false)
    .filter((item) => !category || item.category === category)
    .map((item) => ({
      name: item.name,
      category: item.category,
      side_effect: item.side_effect,
      description: (item.description || '').split('\n')[0].slice(0, 160),
      parameters: item.parameters ?? [],
    }))
}

/**
 * Decide whether a call may proceed. Split out so the approval rule is testable
 * without a server.
 */
export function callDecision(tool, args) {
  if (!tool) return { ok: false, code: 'ALPHA_NEXUS_TOOL_UNKNOWN' }
  if (tool.agent_visible === false) return { ok: false, code: 'ALPHA_NEXUS_TOOL_NOT_AGENT_VISIBLE' }
  if (tool.side_effect === 'write' && args?.confirm !== true) {
    return {
      ok: false,
      code: 'ALPHA_NEXUS_CONFIRM_REQUIRED',
      hint: `工具 ${tool.name} 会修改状态；再次调用时传 confirm=true`,
    }
  }
  return { ok: true }
}

export function alphaNexusTool(settings) {
  return defineTool({
    name: 'alpha_nexus',
    description:
      'Local Alpha-Nexus business surface: accounts, content, knowledge, Studio, publishing, trend radar, shop. '
      + 'action="list" returns agent-visible tools (filter with category). '
      + 'action="call" runs one tool by name with an args object. '
      + 'Tools with side_effect="write" need confirm=true, otherwise the call is refused.',
    parameters: {
      action: {
        type: 'string',
        required: true,
        enum: ['list', 'call'],
        description: 'list = discover tools; call = run one tool',
      },
      category: {
        type: 'string',
        description: 'list only: filter by category (account, content, ego, publish, studio, knowledge, social, radar, shop)',
      },
      tool: {
        type: 'string',
        description: 'call only: tool name from action="list", e.g. account.list',
      },
      args: {
        type: 'json',
        description: 'call only: argument object for the tool',
      },
      confirm: {
        type: 'boolean',
        description: 'call only: must be true to run a side_effect="write" tool',
      },
    },
    output: {
      schema: {
        type: 'object',
        additionalProperties: false,
        properties: {
          ok: { type: 'boolean', required: true },
          data: { type: 'json' },
        },
      },
      render: (_args, value) => {
        const data = value?.data ?? {}
        const summary = data?.count !== undefined
          ? `${data.count} tool(s)`
          : (value?.ok ? 'ok' : (data?.error || 'failed'))
        return [{ type: 'text', text: `alpha_nexus: ${summary}` }]
      },
    },
    async execute(params, exec) {
      const action = String(params?.action ?? '').trim()

      if (action === 'list') {
        const tools = await loadSchema(settings, exec.signal)
        const list = formatToolList(tools, params?.category?.trim() || '')
        return { ok: true, data: { count: list.length, tools: list } }
      }

      if (action !== 'call') {
        return { ok: false, data: { error: 'ALPHA_NEXUS_ACTION_REQUIRED' } }
      }

      const name = String(params?.tool ?? '').trim()
      if (!name) return { ok: false, data: { error: 'ALPHA_NEXUS_TOOL_REQUIRED' } }

      const tools = await loadSchema(settings, exec.signal)
      const tool = byName(tools, name)
      const decision = callDecision(tool, params)
      if (!decision.ok) {
        return { ok: false, data: { error: decision.code, hint: decision.hint } }
      }

      const result = await postJSON(
        settings,
        '/api/v1/tools/call',
        { name, args: params?.args ?? {} },
        exec.signal,
      )
      return { ok: result?.ok !== false && !result?.error, data: result }
    },
  })
}

export function apply(ctx, config) {
  const settings = config ?? { base_url: DEFAULT_BASE_URL, auth_token: '', allow_remote: false }
  ctx.effect(
    () => ctx.tools.register(alphaNexusTool(settings)),
    'alpha-nexus.alphaNexusTool()',
  )
}
