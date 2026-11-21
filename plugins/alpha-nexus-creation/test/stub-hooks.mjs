/**
 * Resolve hook used ONLY by the local tests (never by dsh).
 *
 * `@deepseek-ai/dsh-tools` is a dsh peer dependency and is not installed inside
 * the Alpha-Nexus repo, so a bare import would fail under `node --test`.
 * This hook maps that one specifier to either:
 *
 *   - the REAL module, when DSH_TOOLS_PATH points at its lib/index.js, so the
 *     tool definition is compiled by the real `defineTool` validator, or
 *   - ./stub-dsh-tools.mjs (pass-through) as the portable default.
 */
import { pathToFileURL } from 'node:url'

const STUB_URL = new URL('./stub-dsh-tools.mjs', import.meta.url).href
const REAL_PATH = process.env.DSH_TOOLS_PATH

export async function resolve(specifier, context, nextResolve) {
  if (specifier === '@deepseek-ai/dsh-tools') {
    return {
      url: REAL_PATH ? pathToFileURL(REAL_PATH).href : STUB_URL,
      shortCircuit: true,
    }
  }
  return nextResolve(specifier, context)
}
