# Local Knowledge RAG Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a local, resumable OpenWiki/Markdown/PDF/DOCX knowledge base that supplies cited context to content and script creation.

**Architecture:** Normalize each input to Markdown, store originals and normalized files by SHA-256, split deterministically, and search with SQLite FTS5/BM25. HTTP, Tool Registry and Electron consume one `knowledge.Service`; individual import failures are persisted and do not abort a batch.

**Tech Stack:** Go, modernc SQLite/FTS5, standard library ZIP/XML/file APIs, isolated pure-Go document converter, React/Electron.

## Global Constraints

- Never read or write `.env`, key files, `1key/`, credential directories or browser cookies.
- Data stays under `~/.alpha-nexus-egolite/knowledge`.
- Import limits are 64 MiB input, 16 MiB per expanded file, 128 MiB total expansion and 1024 files.
- Scanned PDFs are `no_extractable_text`; OCR is not included.
- One file failure must not abort other files in the same run.
- External LLM use requires the existing explicit per-call approval.

---

### Task 1: Knowledge schema and repository

**Files:**
- Modify: `internal/db/db.go`
- Create: `internal/db/knowledge.go`
- Test: `internal/db/knowledge_test.go`
- Create: `migrations/000036_knowledge.sql`

**Interfaces:**
- Produces: `CreateKnowledgeBase`, `ListKnowledgeBases`, `CreateKnowledgeImportRun`, `UpsertKnowledgeDocument`, `ReplaceKnowledgeChunks`, `SearchKnowledgeChunks`.

- [ ] Write failing integration tests using a real temporary SQLite database for base CRUD, SHA dedupe, atomic chunk replacement and FTS library filtering.
- [ ] Run `go test ./internal/db -run Knowledge -count=1`; verify failure because APIs/schema do not exist.
- [ ] Add the five tables, FTS5 table/triggers and minimal repository methods.
- [ ] Run the focused test and migration parity tests; verify pass.

### Task 2: Safe normalization, parsing and chunking

**Files:**
- Create: `internal/knowledge/importer.go`
- Create: `internal/knowledge/markdown.go`
- Create: `internal/knowledge/documents.go`
- Test: `internal/knowledge/importer_test.go`
- Test fixtures: `internal/knowledge/testdata/`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces: `Importer.Import(ctx, ImportRequest) ImportResult`, `ChunkMarkdown(string) []Chunk`.
- Consumes: the repository functions from Task 1.

- [ ] Write failing tests for OKF frontmatter, heading paths, deterministic chunks, duplicate hashes, malformed DOCX/PDF, scanned PDF, ZIP traversal and all four resource caps.
- [ ] Run `go test ./internal/knowledge -count=1`; verify failure.
- [ ] Implement native Markdown/OKF/ZIP handling and an isolated pure-Go PDF/DOCX converter adapter. Persist per-file error codes and continue.
- [ ] Run package and DB tests; verify pass.

### Task 3: Search service and cited Agent context

**Files:**
- Create: `internal/knowledge/service.go`
- Create: `internal/knowledge/context.go`
- Test: `internal/knowledge/service_test.go`
- Create: `internal/tools/knowledge_tools.go`
- Test: `internal/tools/knowledge_tools_test.go`
- Modify: `internal/ui/content_assist_handler.go`
- Modify: `internal/ui/content_assist_handler_test.go`

**Interfaces:**
- Produces: `Service.Search(ctx, SearchRequest)`, `BuildCitedContext([]SearchResult, maxChars)`, Registry tools `knowledge.list`, `knowledge.import`, `knowledge.search`, `knowledge.content-draft`, `knowledge.script-draft`.

- [ ] Write failing tests for Chinese search, top-k max 12, 12,000-character budget, citations, empty results, path confinement and external-model explicit approval.
- [ ] Run focused tests and verify the new behavior fails.
- [ ] Implement the service, tools and `content-assist` knowledge fields without allowing tools to submit publishing actions.
- [ ] Run focused tests and verify pass.

### Task 4: HTTP import/search APIs

**Files:**
- Create: `internal/ui/knowledge_handlers.go`
- Create: `internal/ui/knowledge_handlers_test.go`
- Modify: `internal/ui/server.go`

**Interfaces:**
- Produces: knowledge base CRUD, multipart import status, document list and search routes from the design.

- [ ] Write failing real HTTP tests for CRUD, multipart mixed-success imports, request caps, search and safe error bodies.
- [ ] Run `go test ./internal/ui -run Knowledge -count=1`; verify failure.
- [ ] Register handlers and stream/persist bounded inputs without holding entire archives in memory.
- [ ] Run focused HTTP tests and verify pass.

### Task 5: Electron knowledge workspace and creation integration

**Files:**
- Create: `ui-web/src/components/KnowledgeView.tsx`
- Create: `ui-web/src/knowledge.test.ts`
- Modify: `ui-web/src/api.ts`
- Modify: `ui-web/src/App.tsx`
- Modify: `ui-web/src/components/Layout.tsx`
- Modify: `ui-web/src/components/ContentView.tsx`
- Modify: `ui-web/src/components/StudioView.tsx`

**Interfaces:**
- Consumes: Task 4 HTTP API.
- Produces: accessible local import, per-file status, search preview and cited content/script creation UI.

- [ ] Write failing node/component behavior tests for navigation, mixed import results, source selection, citation rendering and disabled external use by default.
- [ ] Run the exact frontend test command from `ui-web/package.json`; verify failure.
- [ ] Implement the smallest accessible workspace and creation controls using existing design tokens.
- [ ] Run frontend tests, lint and build; verify pass.

### Task 6: End-to-end validation

**Files:**
- Create: `e2e/knowledge-rag.spec.ts`
- Modify: `docs/AGENT_WORKFLOW.md`

**Interfaces:**
- Consumes: all prior tasks.

- [ ] Write an E2E test that creates a base, imports one good Markdown and one bad document, searches the good content and injects cited context into a draft while exposing the bad file error.
- [ ] Run the E2E test and verify failure before UI integration is complete.
- [ ] Complete missing integration wiring only.
- [ ] Run Go, frontend, Electron and E2E suites; record any skipped real-platform/OCR validation as unverified.
