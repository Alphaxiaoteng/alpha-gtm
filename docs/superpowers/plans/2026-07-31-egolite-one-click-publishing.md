# EgoLite One-Click Publishing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the frozen multi-platform publish plan execution path to EgoLite while preserving explicit approval, single-submit and evidence guarantees.

**Architecture:** A new `EgoPublishRuntime` executes reviewed helper programs inside the account matrix Space. Platform manifests describe capabilities but never guess unverified final selectors; the existing plan snapshot and approval service remains the authority.

**Tech Stack:** Go, SQLite, `ego-browser nodejs`, existing React/Electron publish workspace.

## Global Constraints

- Product publishing must not launch Chromium, allocate CDP ports, use browser profiles or call Playwright.
- One Space belongs to one matrix and is shared by that matrix's platform accounts.
- Same Space is mutually exclusive; different Spaces may run concurrently up to the measured maximum and never above 3 by default.
- A frozen target may invoke its final action at most once.
- Ambiguous post-submit state is `result_unknown` and is never automatically retried.
- Login challenge, missing verified selectors or anti-bot challenge is `manual_takeover`.
- `published` requires reliable creator-backend evidence.

---

### Task 1: Platform capability manifests

**Files:**
- Create: `internal/egopublish/manifest.go`
- Test: `internal/egopublish/manifest_test.go`

**Interfaces:**
- Produces: `ManifestFor(platformCode) (Manifest, bool)`, `EgoLiteManifests() []Manifest`.

- [ ] Write failing table tests for the 10 required platforms, canonical creator URLs, content forms and explicit verified/manual capability flags.
- [ ] Run `go test ./internal/egopublish -run Manifest -count=1`; verify failure.
- [ ] Implement literal manifests. Mark unverified actions manual instead of inventing selectors.
- [ ] Run focused tests and verify pass.

### Task 2: Safe Ego publish runtime and Space locking

**Files:**
- Create: `internal/egopublish/runtime.go`
- Create: `internal/egopublish/programs.go`
- Create: `internal/egopublish/runtime_test.go`
- Modify: `internal/egolite/scheduler.go`
- Modify: `internal/egolite/scheduler_test.go`

**Interfaces:**
- Produces: `Observe`, `Prepare`, `SubmitOnce`, `Verify`; consumes `egolite.Runtime` and identity Space bindings.

- [ ] Write failing tests proving prepare cannot submit, same-Space mutual exclusion, cross-Space concurrency, max-3 default, helper capability fail-closed, takeover isolation and sensitive-output redaction.
- [ ] Run focused tests and verify failure.
- [ ] Implement reviewed stdin programs with fixed-prefix JSON and the minimum locking changes.
- [ ] Run focused tests and verify pass.

### Task 3: Migrate frozen plan executor

**Files:**
- Modify: `internal/publishplan/executor.go`
- Modify: `internal/publishplan/executor_test.go`
- Modify: `internal/publishplan/executor_submit_test.go`
- Modify: `internal/publishplan/prepare_submit_state_test.go`

**Interfaces:**
- Consumes: `egopublish.Runtime`.
- Preserves: existing publish-plan HTTP status and evidence contracts.

- [ ] Add failing tests that reject BrowserFacade/CDP inputs, use identity Space, continue other targets after manual takeover, submit each target once and prohibit retry after ambiguous submission.
- [ ] Run focused publishplan tests and verify failure.
- [ ] Replace the product execution dependency with `egopublish.Runtime`; keep snapshot, approval and evidence persistence unchanged.
- [ ] Run all publishplan tests and verify pass.

### Task 4: Registry and HTTP wiring

**Files:**
- Create: `internal/tools/ego_publish_tools.go`
- Create: `internal/tools/ego_publish_tools_test.go`
- Modify: `internal/ui/publish_plan_handlers.go`
- Modify: `internal/ui/publish_plan_handlers_test.go`
- Modify: `internal/ui/server.go`

**Interfaces:**
- Produces: structured observe/prepare and approved-submit tools; HTTP remains the Electron adapter to Registry.

- [ ] Write failing tests that Agent may prepare a plan but cannot submit without the matching action-bound approval and frozen hash.
- [ ] Run focused tools/UI tests and verify failure.
- [ ] Register the minimal tools and route handlers through Registry.
- [ ] Run focused tests and verify pass.

### Task 5: Publish workspace interaction

**Files:**
- Modify: `ui-web/src/components/PublishWorkspaceView.tsx`
- Modify: `ui-web/src/api.ts`
- Create: `ui-web/src/egoPublishStatus.test.ts`

**Interfaces:**
- Consumes: existing publish-plan API with Ego states.
- Produces: per-target readiness, prepare, takeover, submit-once and evidence states without optimistic success.

- [ ] Write failing UI behavior tests for one batch confirmation, unverified platform messaging, target-isolated takeover, disabled retry for `result_unknown` and no optimistic `published`.
- [ ] Run frontend tests and verify failure.
- [ ] Implement the state rendering and accessible actions.
- [ ] Run frontend tests, lint and build; verify pass.

### Task 6: Contract and resource gates

**Files:**
- Create: `e2e/egolite-publish-plan.spec.ts`
- Modify: `docs/AGENT_WORKFLOW.md`

**Interfaces:**
- Consumes: all prior tasks.

- [ ] Write an E2E dry-run covering all 10 manifests, two matrices, same-Space ordering, cross-Space continuation and one manual target.
- [ ] Run it and verify failure before final wiring.
- [ ] Complete only the missing integration wiring.
- [ ] Run Go, frontend, Electron and E2E suites; inspect processes to confirm the new app did not start Chromium.
- [ ] Record each platform without real creator-backend evidence as `unverified`; never convert contract-test success into a real publish claim.
