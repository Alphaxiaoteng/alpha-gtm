/**
 * Alpha-Nexus creation-workflow DSH plugin — MINIMAL SKELETON.
 *
 * Scope: exactly ONE tool, `alpha_nexus.creation_workflow_run`, which drives one
 * Alpha-Nexus creation-interview round over plain HTTP:
 *
 *   POST {base_url}/api/creation/workflows          -> { workflow: { id, ... } }
 *   POST {base_url}/api/creation/workflows/{id}/runs -> { workflow, run, citations }
 *
 * This file is deliberately a skeleton: it proves the link, nothing more. The
 * endpoints NOT implemented here (GET/PATCH one workflow, /advance, drafts,
 * model roles) live in the Alpha-Nexus repo at
 * `internal/ui/creation_workflow_handlers.go` — extend that handler first, then
 * add the matching branch here, one endpoint at a time.
 *
 * Hard constraints this file keeps:
 *   - No hardcoded token, key or filesystem path: everything comes from the
 *     validated plugin config (see `Config` below).
 *   - Pure HTTP client: no fs, no child_process, no listening socket, no writes.
 *   - Remote hosts are refused unless the user explicitly sets allow_remote.
 *
 * Verified against: @deepseek-ai/cordis 4.0.5 (`Plugin.Function`,
 * `Config["~standard"].validate`) and @deepseek-ai/dsh-tools 0.2.1-rc.5
 * (`defineTool`, `ctx.tools.register`).
 */
import { defineTool } from '@deepseek-ai/dsh-tools'

export const name = 'alpha-nexus-creation'
// cordis only starts this plugin once ctx.tools exists.
export const inject = ['tools']

/**
 * Alpha-Nexus listens on config.DefaultUIPort (internal/config/config.go) =
 * 17421. 17321 is only a `--port` override used by docs/tests, so it is NOT the
 * default here.
 */
const DEFAULT_BASE_URL = 'http://127.0.0.1:17421'

/** Hosts that are allowed without opting into allow_remote. */
const LOOPBACK_HOSTNAMES = new Set(['127.0.0.1', 'localhost', '::1'])

/**
 * Canonical stages of the Alpha-Nexus creation workflow, taken from
 * `validCreationWorkflowStageUI` in internal/ui/creation_workflow_handlers.go
 * (topic -> copy -> script -> review).
 *
 * The backend has no "outline" stage. It is accepted here only as an alias for
 * `copy` (文案) so callers that say "outline" still work; see STAGE_ALIASES.
 */
const STAGES = ['topic', 'copy', 'script', 'review']
const STAGE_ALIASES = { outline: 'copy' }

/** Schema the model sees; `outline` is listed because it is accepted as an alias. */
const STAGE_INPUT_ENUM = ['topic', 'copy', 'script', 'review', 'outline']

function issue(message, path) {
  return path === undefined ? { message } : { message, path }
}

/**
 * Validate + normalize `base_url`.
 *
 * @param {string} baseURL raw configured value.
 * @param {boolean} allowRemote explicit opt-in for non-loopback hosts.
 * @returns {{ value: string } | { error: string }}
 */
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
  // Credentials in the URL would leak through logs and are never needed here.
  if (url.username !== '' || url.password !== '') {
    return { error: 'base_url 不允许内嵌用户名/密码;请用 auth_token 配置' }
  }
  // URL.hostname keeps IPv6 brackets; drop them before the loopback check.
  const hostname = url.hostname.replace(/^\[/, '').replace(/\]$/, '')
  if (!LOOPBACK_HOSTNAMES.has(hostname) && allowRemote !== true) {
    return {
      error: `base_url 主机 "${hostname}" 不是回环地址;如确需远程,请在配置里显式设置 allow_remote: true`,
    }
  }
  // Drop trailing slashes so path joins stay single-slashed; keep any path prefix.
  return { value: url.origin + url.pathname.replace(/\/+$/, '') }
}

/**
 * Standard-schema validator for the plugin config. cordis calls
 * `Config["~standard"].validate(config)` and passes the returned `value` to
 * `apply()`, so defaults are applied here (see `resolveConfig` in cordis).
 *
 * @param {unknown} raw
 * @returns {{ value: { base_url: string, auth_token: string, allow_remote: boolean } }
 *          | { issues: Array<{ message: string, path?: string[] }> }}
 */
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
  const rawBaseURL = (input.base_url ?? DEFAULT_BASE_URL).trim()
  if (rawBaseURL === '') return { issues: [issue('base_url 不能为空', ['base_url'])] }

  const normalized = normalizeBaseURL(rawBaseURL, allowRemote)
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

/** Dependency-free StandardSchemaV1 implementation consumed by cordis. */
export const Config = {
  '~standard': {
    version: 1,
    vendor: 'alpha-nexus',
    validate: validateConfig,
  },
}

/**
 * Map a caller-facing stage onto a real backend stage.
 *
 * @param {string} stage
 * @returns {string} one of STAGES
 */
export function resolveStage(stage) {
  const wanted = String(stage ?? '').trim().toLowerCase()
  const resolved = STAGE_ALIASES[wanted] ?? wanted
  if (!STAGES.includes(resolved)) {
    throw new Error(`ALPHA_NEXUS_INVALID_STAGE: "${wanted}"`)
  }
  return resolved
}

/**
 * POST JSON and decode JSON.
 *
 * Failures carry a stable `ALPHA_NEXUS_*` code so a caller can tell a transport
 * problem from a backend rejection.
 *
 * @param {{ base_url: string, auth_token: string }} settings
 * @param {string} path
 * @param {unknown} body
 * @param {AbortSignal | undefined} signal caller cancellation from the tool exec
 * @returns {Promise<any>}
 */
async function postJSON(settings, path, body, signal) {
  const headers = { 'Content-Type': 'application/json' }
  // Optional: only sent when configured. Today's Alpha-Nexus creation API is
  // loopback-only (requireLocalTerminal) and does not read this header.
  if (settings.auth_token !== '') {
    headers.Authorization = `Bearer ${settings.auth_token}`
  }

  const response = await fetch(`${settings.base_url}${path}`, {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
    signal,
  })
  const text = await response.text()

  if (!response.ok) {
    throw new Error(`ALPHA_NEXUS_HTTP_${response.status}: ${text.slice(0, 300)}`)
  }
  if (text === '') return {}
  try {
    return JSON.parse(text)
  } catch {
    throw new Error(`ALPHA_NEXUS_INVALID_JSON: ${text.slice(0, 200)}`)
  }
}

/**
 * Create one workflow, then start one creation round on it.
 *
 * @param {{ base_url: string, auth_token: string }} settings
 * @param {{ title: string, stage: string, message: string, trend_context?: string }} input
 * @param {AbortSignal | undefined} signal
 * @returns {Promise<{ workflow_id: string, run: unknown }>}
 */
export async function runCreationWorkflow(settings, input, signal) {
  const stage = resolveStage(input.stage)

  const created = await postJSON(
    settings,
    '/api/creation/workflows',
    { title: input.title, stage },
    signal,
  )
  const workflowID = created?.workflow?.id
  if (typeof workflowID !== 'string' || workflowID === '') {
    throw new Error('ALPHA_NEXUS_CREATE_NO_WORKFLOW_ID')
  }

  const round = await postJSON(
    settings,
    `/api/creation/workflows/${encodeURIComponent(workflowID)}/runs`,
    {
      stage,
      message: input.message,
      trend_context: input.trend_context ?? '',
    },
    signal,
  )

  return { workflow_id: workflowID, run: round?.run ?? round }
}

/**
 * Build the single tool definition. Exported so tests can inspect it without
 * standing up a cordis context.
 *
 * @param {{ base_url: string, auth_token: string }} settings validated config
 */
export function creationWorkflowTool(settings) {
  return defineTool({
    name: 'alpha_nexus.creation_workflow_run',
    description:
      'Run one round of the local Alpha-Nexus creation interview: create a creation workflow, then send one message at the requested stage. '
      + 'Returns the workflow id and the run record. Stages: topic (选题) -> copy (文案) -> script (脚本) -> review (审校); "outline" is accepted as an alias for copy. '
      + 'It never advances the stage and never publishes anything.',
    parameters: {
      title: {
        type: 'string',
        required: true,
        description: 'Workflow title, e.g. "露营避坑短视频". The backend falls back to 「创作访谈」 only when this is empty, so always pass a real title.',
      },
      stage: {
        type: 'string',
        required: true,
        enum: STAGE_INPUT_ENUM,
        description: 'Current creation stage. Must match the workflow stage used when it was created.',
      },
      message: {
        type: 'string',
        required: true,
        description: 'What to say to the creation assistant this round (max 12000 characters).',
      },
      trend_context: {
        type: 'string',
        description: 'Optional extra trend reference text for this round only.',
      },
    },
    output: {
      schema: {
        type: 'object',
        additionalProperties: false,
        properties: {
          workflow_id: { type: 'string', required: true },
          // The backend run record; kept opaque so backend fields can evolve.
          run: { type: 'json', required: true },
        },
      },
      render: (_args, value) => {
        const status = value?.run?.status ?? 'unknown'
        const runID = value?.run?.id ?? '(no id)'
        return [{ type: 'text', text: `workflow ${value.workflow_id} · run ${runID} · ${status}` }]
      },
    },
    async execute(args, exec) {
      return await runCreationWorkflow(settings, args, exec.signal)
    },
  })
}

/**
 * cordis plugin entrypoint: `(ctx, config)`.
 *
 * @param {import('@deepseek-ai/cordis').Context} ctx
 * @param {{ base_url: string, auth_token: string }} config output of validateConfig
 */
export function apply(ctx, config) {
  const settings = config ?? { base_url: DEFAULT_BASE_URL, auth_token: '', allow_remote: false }
  // ctx.effect ties the registration to this plugin's fiber, so unloading the
  // plugin unregisters the tool.
  ctx.effect(
    () => ctx.tools.register(creationWorkflowTool(settings)),
    'alpha-nexus-creation.creationWorkflowTool()',
  )
}
