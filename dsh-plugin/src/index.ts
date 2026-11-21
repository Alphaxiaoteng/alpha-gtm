/**
 * Alpha-Nexus DSH tools plugin: read-only model-facing tools over the local
 * Alpha-Nexus EgoLite Go backend (http://127.0.0.1:17421).
 *
 * Tools register into each agent's scoped context (agent-plane): the standard
 * agent preset mounts a private tools instance per agent, so global
 * registrations are invisible to it. Write operations (publish, login,
 * browser side effects) deliberately have no plugin tool — they stay behind
 * the Go backend's approval / dry-run / lease paths.
 */
import type { Context } from '@deepseek-ai/cordis'
import type { Agent } from '@deepseek-ai/dsh-agent'
import z from '@deepseek-ai/schemastery'
import { defineTool } from '@deepseek-ai/dsh-tools'

export const name = 'alpha-nexus-tools'
export const inject = ['agents']

export interface Config {
  readonly baseUrl?: string
}
export const Config: z<Config> = z.object({
  baseUrl: z.string().default('http://127.0.0.1:17421'),
})

async function getJSON<T>(baseUrl: string, path: string, signal: AbortSignal): Promise<T> {
  const response = await fetch(`${baseUrl}${path}`, { signal })
  if (!response.ok) {
    throw new Error(`ALPHA_NEXUS_HTTP_${response.status}`)
  }
  return await response.json() as T
}

const IDENTITY_VIEW_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  properties: {
    id: { type: 'number', required: true },
    name: { type: 'string', required: true },
    enabled: { type: 'boolean', required: true },
  },
} as const

const SPACE_VIEW_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  properties: {
    identityId: { type: 'number', required: true },
    state: { type: 'string', required: true },
    ownership: { type: 'string', required: true },
    errorCode: { type: 'string', required: true },
  },
} as const

const ACCOUNT_VIEW_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  properties: {
    id: { type: 'number', required: true },
    identityId: { type: 'number', required: true },
    platform: { type: 'string', required: true },
    name: { type: 'string', required: true },
    identifier: { type: 'string', required: true },
    loginState: { type: 'string', required: true },
  },
} as const

function install(agent: Agent, baseUrl: string): () => void {
  const scoped = agent.ctx
  const disposers: Array<() => void> = []

  disposers.push(scoped.tools.register(
    defineTool({
      name: 'alpha_nexus_browser_identities',
      description:
        'List the local Alpha-Nexus browser identities (浏览器1/2/3...) with their enabled state.',
      parameters: {},
      output: {
        schema: {
          type: 'object',
          additionalProperties: false,
          properties: {
            identities: { type: 'array', required: true, items: IDENTITY_VIEW_SCHEMA },
          },
        },
        render: (_args, value: { identities: Array<{ id: number; name: string; enabled: boolean }> }) => [
          {
            type: 'text' as const,
            text:
              value.identities
                .map((identity) => `${identity.name}(#${identity.id}${identity.enabled ? '' : ', 已停用'})`)
                .join('\n') || '（无）',
          },
        ],
      },
      async execute(_args: Record<string, never>, exec: { signal: AbortSignal }) {
        const payload = await getJSON<Array<{ id: number; name: string; enabled: number | boolean }>>(
          baseUrl, '/api/ego/identities', exec.signal,
        )
        return {
          identities: (Array.isArray(payload) ? payload : []).map((identity) => ({
            id: Number(identity.id),
            name: String(identity.name ?? ''),
            enabled: Boolean(identity.enabled),
          })),
        }
      },
    }),
  ))

  disposers.push(scoped.tools.register(
    defineTool({
      name: 'alpha_nexus_space_states',
      description:
        'List Alpha-Nexus ego space states per browser identity: state (ready/manual_takeover/...), ownership, and error code. Read-only diagnostics.',
      parameters: {},
      output: {
        schema: {
          type: 'object',
          additionalProperties: false,
          properties: {
            spaces: { type: 'array', required: true, items: SPACE_VIEW_SCHEMA },
          },
        },
        render: (_args, value: { spaces: Array<{ identityId: number; state: string; ownership: string; errorCode: string }> }) => [
          {
            type: 'text' as const,
            text:
              value.spaces
                .map((space) => `identity#${space.identityId}: ${space.state}${space.errorCode ? ` (${space.errorCode})` : ''}`)
                .join('\n') || '（无）',
          },
        ],
      },
      async execute(_args: Record<string, never>, exec: { signal: AbortSignal }) {
        const payload = await getJSON<Array<{
          identity_id: number; state: string; ownership: string; error_code: string
        }>>(baseUrl, '/api/ego/spaces', exec.signal)
        return {
          spaces: (Array.isArray(payload) ? payload : []).map((space) => ({
            identityId: Number(space.identity_id),
            state: String(space.state ?? ''),
            ownership: String(space.ownership ?? ''),
            errorCode: String(space.error_code ?? ''),
          })),
        }
      },
    }),
  ))

  disposers.push(scoped.tools.register(
    defineTool({
      name: 'alpha_nexus_platform_accounts',
      description:
        'List Alpha-Nexus platform accounts per browser identity with their login state (logged_in/logged_out/unknown). Read-only; never triggers logins.',
      parameters: {
        identity_id: {
          type: 'number',
          description: 'Optional browser identity id filter (e.g. 1 for 浏览器1). Omit to list all.',
        },
      },
      output: {
        schema: {
          type: 'object',
          additionalProperties: false,
          properties: {
            accounts: { type: 'array', required: true, items: ACCOUNT_VIEW_SCHEMA },
          },
        },
        render: (_args, value: { accounts: Array<{ name: string; platform: string; identifier: string; loginState: string }> }) => [
          {
            type: 'text' as const,
            text:
              value.accounts
                .map((account) => `${account.platform}: ${account.name}${account.identifier ? ` (${account.identifier})` : ''} — ${account.loginState}`)
                .join('\n') || '（无）',
          },
        ],
      },
      async execute(args: { identity_id?: number }, exec: { signal: AbortSignal }) {
        const payload = await getJSON<Array<{
          id: number; identityId: number; platformCode: string; name: string
          accountIdentifier: string; loginState: string
        }>>(baseUrl, '/api/accounts', exec.signal)
        const rows = Array.isArray(payload) ? payload : []
        return {
          accounts: rows
            .filter((row) => args?.identity_id === undefined || Number(row.identityId) === args.identity_id)
            .map((row) => ({
              id: Number(row.id),
              identityId: Number(row.identityId),
              platform: String(row.platformCode ?? ''),
              name: String(row.name ?? ''),
              identifier: String(row.accountIdentifier ?? ''),
              loginState: String(row.loginState ?? 'unknown'),
            })),
        }
      },
    }),
  ))

  return () => {
    for (const dispose of disposers) dispose()
  }
}

export function apply(ctx: Context, config: Config): void {
  const baseUrl = (config.baseUrl || 'http://127.0.0.1:17421').replace(/\/+$/, '')
  const installed = new Map<Agent, () => void>()
  const maybeInstall = (agent: Agent): void => {
    if (installed.has(agent)) return
    installed.set(agent, install(agent, baseUrl))
  }
  for (const agent of ctx.agents.list()) maybeInstall(agent)
  ctx.on('agent/created', ({ agent }: { agent: Agent }) => { maybeInstall(agent) })
  ctx.on('agent/disposed', ({ agent }: { agent: Agent }) => {
    installed.get(agent)?.()
    installed.delete(agent)
  })
  ctx.effect(() => {
    for (const dispose of installed.values()) dispose()
    installed.clear()
  }, 'alpha-nexus-tools.scopedTools()')
}
