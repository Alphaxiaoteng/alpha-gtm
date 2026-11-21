package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
)

func registerKnowledgeTools(registry *Registry) {
	for _, definition := range []Def{
		{
			Name: "knowledge.list", Category: "knowledge", SideEffect: "read",
			Description:  "List local knowledge bases without document contents",
			AgentVisible: true, AgentWorkspace: "knowledge", AgentRisk: AgentRiskL0,
			AgentImpact: "Reads local knowledge-base metadata only", AgentIdempotency: "read",
			Handler: handleKnowledgeList,
		},
		{
			Name: "knowledge.base.create", Category: "knowledge", SideEffect: "write",
			Description:  "Create one local knowledge base",
			Params:       []Param{{Name: "name", Type: "string", Required: true}, {Name: "description", Type: "string"}},
			AgentVisible: true, AgentWorkspace: "knowledge", AgentRisk: AgentRiskL1,
			AgentImpact: "Creates local metadata only", AgentIdempotency: "Name is user supplied",
			Handler: handleKnowledgeBaseCreate,
		},
		{
			Name: "knowledge.import", Category: "knowledge", SideEffect: "write",
			Description: "Import supplied text into a local knowledge base; does not accept filesystem paths",
			Params: []Param{
				{Name: "base_id", Type: "int", Required: true},
				{Name: "source_name", Type: "string", Required: true},
				{Name: "content", Type: "string", Required: true},
			},
			AgentVisible: true, AgentWorkspace: "knowledge", AgentRisk: AgentRiskL1,
			AgentImpact: "Stores the supplied text locally", AgentIdempotency: "SHA-256 duplicate imports are skipped",
			Handler: handleKnowledgeImport,
		},
		{
			Name: "knowledge.search", Category: "knowledge", SideEffect: "read",
			Description: "Search local knowledge and return cited excerpts",
			Params: []Param{
				{Name: "base_ids", Type: "array", Required: true},
				{Name: "query", Type: "string", Required: true},
				{Name: "top_k", Type: "int"},
			},
			AgentVisible: true, AgentWorkspace: "knowledge", AgentRisk: AgentRiskL0,
			AgentImpact: "Reads bounded cited excerpts from selected local knowledge bases", AgentIdempotency: "read",
			Handler: handleKnowledgeSearch,
		},
		{
			Name: "knowledge.content-draft", Category: "knowledge", SideEffect: "read",
			Description: "Build a cited local-knowledge brief for content creation; never publishes",
			Params: []Param{
				{Name: "base_ids", Type: "array", Required: true},
				{Name: "topic", Type: "string", Required: true},
				{Name: "platform", Type: "string"},
			},
			AgentVisible: true, AgentWorkspace: "knowledge", AgentRisk: AgentRiskL0,
			AgentImpact: "Reads bounded cited excerpts and returns a content brief", AgentIdempotency: "read",
			Handler: func(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
				return handleKnowledgeDraft(ctx, runtime, args, "content")
			},
		},
		{
			Name: "knowledge.script-draft", Category: "knowledge", SideEffect: "read",
			Description: "Build a cited local-knowledge brief for script creation; never publishes",
			Params: []Param{
				{Name: "base_ids", Type: "array", Required: true},
				{Name: "topic", Type: "string", Required: true},
				{Name: "platform", Type: "string"},
			},
			AgentVisible: true, AgentWorkspace: "knowledge", AgentRisk: AgentRiskL0,
			AgentImpact: "Reads bounded cited excerpts and returns a script brief", AgentIdempotency: "read",
			Handler: func(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
				return handleKnowledgeDraft(ctx, runtime, args, "script")
			},
		},
	} {
		registry.Register(definition)
	}
}

func handleKnowledgeList(_ context.Context, runtime *Runtime, _ map[string]interface{}) (*Result, error) {
	database, closeDB, failure := openKnowledgeToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	items, err := database.ListKnowledgeBases()
	if err != nil {
		return failResult("KNOWLEDGE_LIST_FAILED", "could not list local knowledge bases"), nil
	}
	return okResult("ok", map[string]interface{}{"items": items, "count": len(items)}), nil
}

func handleKnowledgeBaseCreate(_ context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	database, closeDB, failure := openKnowledgeToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	item, err := database.CreateKnowledgeBase(argString(args, "name", ""), argString(args, "description", ""))
	if err != nil {
		return failResult("KNOWLEDGE_BASE_INVALID", "could not create local knowledge base"), nil
	}
	return okResult("knowledge base created", map[string]interface{}{"base_id": item.ID, "base": structMap(item)}), nil
}

func handleKnowledgeImport(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	baseID := int64(argInt(args, "base_id", 0))
	name, safeName := normalizeKnowledgeSourceName(argString(args, "source_name", ""))
	content := argString(args, "content", "")
	if baseID <= 0 || !safeName || content == "" || len([]byte(content)) > 2<<20 {
		return failResult("KNOWLEDGE_IMPORT_INVALID", "base_id, source_name and content up to 2 MiB are required"), nil
	}
	database, closeDB, failure := openKnowledgeToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	incoming := filepath.Join(runtime.DataDir, "knowledge", "incoming")
	if err := os.MkdirAll(incoming, 0700); err != nil {
		return failResult("KNOWLEDGE_IMPORT_FAILED", "could not prepare local import"), nil
	}
	temp, err := os.CreateTemp(incoming, "agent-text-*"+filepath.Ext(filepath.Base(name)))
	if err != nil {
		return failResult("KNOWLEDGE_IMPORT_FAILED", "could not prepare local import"), nil
	}
	path := temp.Name()
	defer os.Remove(path)
	if err := temp.Chmod(0600); err == nil {
		_, err = temp.WriteString(content)
	}
	closeErr := temp.Close()
	if err != nil || closeErr != nil {
		return failResult("KNOWLEDGE_IMPORT_FAILED", "could not store supplied text"), nil
	}
	result, err := knowledge.NewImporter(database).Import(ctx, knowledge.ImportRequest{
		BaseID: baseID, Files: []knowledge.InputFile{{Name: name, Path: path}},
	})
	if err != nil || result.Imported+result.Skipped != 1 {
		return failResult("KNOWLEDGE_IMPORT_FAILED", "supplied text could not be imported"), nil
	}
	return okResult("knowledge imported", map[string]interface{}{
		"imported": result.Imported, "skipped": result.Skipped, "items": result.Items,
	}), nil
}

// normalizeKnowledgeSourceName keeps a logical folder hierarchy for citations
// without ever resolving or reading the supplied name as a filesystem path.
func normalizeKnowledgeSourceName(raw string) (string, bool) {
	name := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	if name == "" || len([]byte(name)) > 512 || strings.HasPrefix(name, "/") || strings.ContainsRune(name, '\x00') {
		return "", false
	}
	parts := strings.Split(name, "/")
	if len(parts) > 16 || strings.Contains(parts[0], ":") {
		return "", false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return strings.Join(parts, "/"), true
}

func handleKnowledgeSearch(ctx context.Context, runtime *Runtime, args map[string]interface{}) (*Result, error) {
	query := strings.TrimSpace(argString(args, "query", ""))
	if query == "" {
		return failResult("KNOWLEDGE_SEARCH_INVALID", "query is required"), nil
	}
	baseIDs, err := knowledgeBaseIDs(args["base_ids"])
	if err != nil || len(baseIDs) == 0 {
		return failResult("KNOWLEDGE_SEARCH_INVALID", "base_ids must select at least one knowledge base"), nil
	}
	database, closeDB, failure := openKnowledgeToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	results, err := knowledge.NewService(database).Search(ctx, knowledge.SearchRequest{
		BaseIDs: baseIDs, Query: query, TopK: argInt(args, "top_k", 6),
	})
	if err != nil {
		return failResult("KNOWLEDGE_SEARCH_FAILED", "local knowledge search failed"), nil
	}
	contextText, citations := knowledge.BuildCitedContext(results, 12000)
	return okResult("ok", map[string]interface{}{
		"results": results, "citations": citations, "context": contextText, "count": len(results),
	}), nil
}

func handleKnowledgeDraft(ctx context.Context, runtime *Runtime, args map[string]interface{}, kind string) (*Result, error) {
	topic := strings.TrimSpace(argString(args, "topic", ""))
	if topic == "" || len([]rune(topic)) > 500 {
		return failResult("KNOWLEDGE_DRAFT_INVALID", "topic is required and must be at most 500 characters"), nil
	}
	baseIDs, err := knowledgeBaseIDs(args["base_ids"])
	if err != nil || len(baseIDs) == 0 {
		return failResult("KNOWLEDGE_DRAFT_INVALID", "base_ids must select at least one knowledge base"), nil
	}
	database, closeDB, failure := openKnowledgeToolDB(runtime)
	if failure != nil {
		return failure, nil
	}
	defer closeDB()
	service := knowledge.NewService(database)
	results, err := service.Search(ctx, knowledge.SearchRequest{BaseIDs: baseIDs, Query: topic, TopK: 6})
	if err != nil {
		return failResult("KNOWLEDGE_DRAFT_FAILED", "local knowledge search failed"), nil
	}
	if len(results) == 0 {
		runes := []rune(strings.Join(strings.Fields(topic), ""))
		if len(runes) > 4 {
			results, err = service.Search(ctx, knowledge.SearchRequest{BaseIDs: baseIDs, Query: string(runes[:4]), TopK: 6})
		}
	}
	if err != nil {
		return failResult("KNOWLEDGE_DRAFT_FAILED", "local knowledge search failed"), nil
	}
	contextText, citations := knowledge.BuildCitedContext(results, 12000)
	instruction := "根据引用资料生成内容草稿；区分资料事实与创作建议，不得编造引用。"
	if kind == "script" {
		instruction = "根据引用资料生成分镜或口播脚本；标注开场、主体和收尾，不得编造引用。"
	}
	return okResult("cited creation brief ready", map[string]interface{}{
		"kind": kind, "topic": topic, "platform": strings.TrimSpace(argString(args, "platform", "")),
		"instruction": instruction, "context": contextText, "citations": citations,
		"publish_authorized": false,
	}), nil
}

func knowledgeBaseIDs(raw interface{}) ([]int64, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("base_ids must be an array")
	}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		var id int64
		switch typed := value.(type) {
		case float64:
			id = int64(typed)
		case int:
			id = int64(typed)
		case int64:
			id = typed
		default:
			return nil, fmt.Errorf("invalid base id")
		}
		if id <= 0 {
			return nil, fmt.Errorf("invalid base id")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func openKnowledgeToolDB(runtime *Runtime) (*db.DB, func(), *Result) {
	if runtime == nil || strings.TrimSpace(runtime.DataDir) == "" {
		return nil, nil, failResult("KNOWLEDGE_RUNTIME_INVALID", "local data directory unavailable")
	}
	database, err := db.Open(runtime.DataDir)
	if err != nil {
		return nil, nil, failResult("KNOWLEDGE_DB_OPEN_FAILED", "could not open local database")
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		return nil, nil, failResult("KNOWLEDGE_DB_MIGRATE_FAILED", "could not migrate local database")
	}
	return database, func() { _ = database.Close() }, nil
}
