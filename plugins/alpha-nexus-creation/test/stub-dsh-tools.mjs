/**
 * Test-only stand-in for `@deepseek-ai/dsh-tools`.
 *
 * It is a pass-through, so the tests exercise the definition this plugin
 * actually builds. The real `defineTool` compiles and validates the schema; run
 * `DSH_TOOLS_PATH=<...>/dsh-tools/lib/index.js node --test test/local.test.mjs`
 * to check the same definition against the real compiler.
 */
export function defineTool(options) {
  return options
}
