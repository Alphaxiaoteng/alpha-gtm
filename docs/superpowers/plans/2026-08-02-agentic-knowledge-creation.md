# Agentic Knowledge Creation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add role-based verified LLM selection and a persisted, knowledge-attached content-creation session with draft/review quality gates.

**Architecture:** Reuse AgentHub verified model state and its isolated text-task runner. SQLite persists only model references, creation sessions and run evidence; `internal/ui` assembles selected knowledge excerpts and invokes a role-resolved model. React renders the persisted session as SOP + conversation instead of making one-shot `content-assist` calls.

**Tech Stack:** Go, modernc SQLite, existing `internal/agenthub`, `internal/knowledge`, local HTTP API, React/TypeScript, Vitest/Playwright.

## Global Constraints

- Never read or write `.env`, Keychain implementation files, credentials, cookies or EgoLite Profile data.
- SQLite stays the local source of truth; no cloud service, vector service or background updater is introduced.
- Only AgentHub-verified Provider/Model combinations may be selected or executed.
- External model use is request-scoped and opt-in; local fallback must make no remote request.
- A saved creation is a local draft, never a publish/collection/material-generation authorization.
- The worktree has no tracked baseline; do not stage or commit unrelated untracked files.

---

### Task 1: Creation role and session persistence

**Files:**
- Modify: `internal/db/db.go`
- Create: `internal/db/creation.go`
- Create: `migrations/000040_creation_sessions.sql`
- Test: `internal/db/creation_test.go`

**Interfaces:**
- Produces `CreationModelRole`, `CreationSession`, `CreationRun` and repository methods `PutCreationModelRole`, `GetCreationModelRole`, `CreateCreationSession`, `GetCreationSession`, `AppendCreationRun`.

- [ ] **Step 1: Write the failing repository test**

```go
func TestCreationSessionPersistsAttachmentsAndRuns(t *testing.T) {
    repo := newTestRepository(t)
    session, err := repo.CreateCreationSession(db.CreationSession{
        Title: "杭州露营新手避坑", KnowledgeBaseIDs: []int64{3, 8},
    })
    require.NoError(t, err)
    _, err = repo.AppendCreationRun(db.CreationRun{SessionID: session.ID, Action: "draft", Result: "草稿"})
    require.NoError(t, err)
    got, err := repo.GetCreationSession(session.ID)
    require.NoError(t, err)
    require.Equal(t, []int64{3, 8}, got.KnowledgeBaseIDs)
    require.Len(t, got.Runs, 1)
}
```

- [ ] **Step 2: Run the test and verify RED**

Run: `go test ./internal/db -run CreationSessionPersistsAttachmentsAndRuns -count=1`

Expected: fail because creation repository types/methods do not exist.

- [ ] **Step 3: Implement the schema and smallest repository surface**

Create the three tables with foreign-key cascade, JSON encode/decode only the knowledge IDs/citations/quality values, validate blank titles and unknown roles, and add the same schema in `internal/db/db.go` and `migrations/000040_creation_sessions.sql`.

- [ ] **Step 4: Run the focused repository test and refactor**

Run: `go test ./internal/db -run Creation -count=1`

Expected: PASS.

### Task 2: Verified role resolver and deterministic quality gate

**Files:**
- Create: `internal/creation/service.go`
- Test: `internal/creation/service_test.go`
- Modify: `internal/agenthub/service.go` only if a narrow exported verified-selection validator is needed

**Interfaces:**
- Consumes `agenthub.Service.TextTaskSelection`, `agenthub.Service.CompleteTextTask`, Task 1 repository types, `knowledge.Service.Search`.
- Produces `Service.ResolveRole(ctx, role)`, `Service.Run(ctx, RunRequest)`, `QualityReport`.

- [ ] **Step 1: Write failing service tests**

```go
func TestRunUsesOnlyAttachedKnowledgeBases(t *testing.T) { /* fixture has base 1 and base 2; assert call context contains only base 2 */ }
func TestRunWithoutExternalPermissionReturnsLocalDraft(t *testing.T) { /* assert no model runner called */ }
func TestReviewerFallsBackToCopywriterAndMarksFallback(t *testing.T) { /* reviewer unset -> copywriter */ }
```

- [ ] **Step 2: Run the focused service tests and verify RED**

Run: `go test ./internal/creation -run 'Run|Reviewer' -count=1`

Expected: fail because package/service does not exist.

- [ ] **Step 3: Implement the minimal role resolver and run pipeline**

Support `topic`, `draft`, `review`, `revise`; call `CompleteTextTask` only with a verified supported selection; build cited context from attached base IDs; apply deterministic empty/title/citation checks; persist the run. Limit automatic revise to one call per user run.

- [ ] **Step 4: Run focused service tests**

Run: `go test ./internal/creation -count=1`

Expected: PASS.

### Task 3: Creation session HTTP API

**Files:**
- Create: `internal/ui/creation_handlers.go`
- Create: `internal/ui/creation_handlers_test.go`
- Modify: `internal/ui/server.go`

**Interfaces:**
- Consumes `creation.Service` and Task 1 repository.
- Produces role, session and run routes defined in the design.

- [ ] **Step 1: Write failing HTTP tests**

```go
func TestCreationRoleRejectsUnverifiedModel(t *testing.T) { /* PUT role returns 400 */ }
func TestCreationRunRequiresExplicitExternalPermission(t *testing.T) { /* POST run false -> source local */ }
func TestCreationSessionReturnsPersistedCitations(t *testing.T) { /* GET session returns run citations */ }
```

- [ ] **Step 2: Verify RED**

Run: `go test ./internal/ui -run Creation -count=1`

Expected: fail because routes are missing.

- [ ] **Step 3: Implement route validation and response DTOs**

Register the routes in `server.go`; reject malformed JSON, blank title, unknown action, unknown role, unavailable model and unowned knowledge base IDs; never expose provider credentials or full document bodies in status responses.

- [ ] **Step 4: Verify GREEN**

Run: `go test ./internal/ui -run Creation -count=1`

Expected: PASS.

### Task 4: API client and settings role assignment

**Files:**
- Modify: `ui-web/src/api.ts`
- Modify: `ui-web/src/components/AlphaAgentSettingsSection.tsx`
- Test: `ui-web/src/components/AlphaAgentSettingsSection.test.tsx` or the existing colocated settings test path

**Interfaces:**
- Consumes `GET/PUT /api/creation/model-roles`.
- Produces `api.getCreationModelRoles()` and `api.putCreationModelRole(role, selection)`.

- [ ] **Step 1: Write the failing component test**

```tsx
it('shows only verified models for the copywriter role and saves the selected role model', async () => {
  render(<AlphaAgentSettingsSection />)
  await user.selectOptions(screen.getByLabelText('文案生成模型'), 'custom/kimi-k2')
  expect(api.putCreationModelRole).toHaveBeenCalledWith('copywriter', { provider_id: 'custom', model_id: 'kimi-k2' })
})
```

- [ ] **Step 2: Verify RED**

Run the existing frontend test command for this file from `ui-web/package.json`.

Expected: fail because the role selector/API does not exist.

- [ ] **Step 3: Implement accessible model-role controls**

Place role bindings below the unified verified-model configuration. Show provider logo/name, model, fallback explanation and a test/refresh status; populate choices solely from the API’s verified list.

- [ ] **Step 4: Verify GREEN**

Run the focused component test plus `pnpm --dir ui-web run lint`.

Expected: PASS.

### Task 5: SOP + Agent conversation workbench

**Files:**
- Modify: `ui-web/src/components/CreationWorkbenchView.tsx`
- Create: `ui-web/src/components/CreationWorkbenchView.test.tsx`
- Modify: `ui-web/src/api.ts`

**Interfaces:**
- Consumes Task 3 session/run routes.
- Produces explicit topic, knowledge attachments, external-model consent, persisted messages, citations and quality decision UI.

- [ ] **Step 1: Write failing UI tests**

```tsx
it('creates a session with explicitly selected knowledge bases', async () => { /* select two bases and start */ })
it('shows the quality decision and evidence after a draft run', async () => { /* mocked run returns citations + revise */ })
it('keeps save disabled until pass or explicit manual confirmation', async () => { /* existing safety behavior */ })
```

- [ ] **Step 2: Verify RED**

Run the focused frontend test command.

Expected: fail because the conversation/session UI does not exist.

- [ ] **Step 3: Replace one-shot assist controls with session actions**

Keep the left SOP rail; replace stage bodies with a message timeline and a composer that dispatches named creation actions. Support multi-select knowledge attachment, display actual provider/model/fallback, citations and quality result per message. Preserve existing local-draft save safeguards.

- [ ] **Step 4: Verify GREEN**

Run the focused test plus `pnpm --dir ui-web run build`.

Expected: PASS.

### Task 6: Integration verification

**Files:**
- Modify: `e2e/creation-workbench.spec.ts` if it exists, otherwise create it

**Interfaces:**
- Consumes all previous tasks.

- [ ] **Step 1: Write an end-to-end flow**

Create a local knowledge base, start a creation session with it attached, generate a local draft, assert evidence/quality display, and verify no publish operation is created.

- [ ] **Step 2: Verify RED before final UI wiring**

Run: `./scripts/e2e-run.sh --grep 'creation workbench'`

Expected: fail until the session UI is wired.

- [ ] **Step 3: Complete only integration gaps found by the E2E test**

Do not add browser automation, scheduled jobs or remote setup.

- [ ] **Step 4: Run regression suite**

Run: `go test -p 1 ./... -count=1`, `pnpm --dir ui-web run lint`, `pnpm --dir ui-web run build`, and the focused E2E test.

Expected: all pass, or report any unavailable desktop/E2E dependency exactly.
