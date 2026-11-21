package tools

import (
	"context"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
)

func TestKnowledgeToolsImportTextAndSearchWithoutFileAccess(t *testing.T) {
	dataDir := t.TempDir()
	runtime := &Runtime{DataDir: dataDir}
	created, err := Default.Call(context.Background(), runtime, "knowledge.base.create", map[string]interface{}{
		"name": "脚本知识",
	})
	if err != nil || !created.OK {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	baseID := int(created.Data["base_id"].(int64))
	imported, err := Default.Call(context.Background(), runtime, "knowledge.import", map[string]interface{}{
		"base_id": baseID, "source_name": "品牌/guide.md", "content": "# 视频开场\n\n前三秒直接说明用户收益。",
		"path": "/tmp/must-not-be-read",
	})
	if err != nil || !imported.OK {
		t.Fatalf("imported=%+v err=%v", imported, err)
	}
	searched, err := Default.Call(context.Background(), runtime, "knowledge.search", map[string]interface{}{
		"base_ids": []interface{}{float64(baseID)}, "query": "用户收益", "top_k": 6,
	})
	if err != nil || !searched.OK || searched.Data["count"].(int) != 1 {
		t.Fatalf("searched=%+v err=%v", searched, err)
	}
	results := searched.Data["results"].([]db.KnowledgeSearchResult)
	if results[0].SourceName != "品牌/guide.md" {
		t.Fatalf("source hierarchy lost from citation: %+v", results[0])
	}
}

func TestKnowledgeImportRejectsUnsafeLogicalSourceNames(t *testing.T) {
	runtime := &Runtime{DataDir: t.TempDir()}
	created, err := Default.Call(context.Background(), runtime, "knowledge.base.create", map[string]interface{}{"name": "安全资料"})
	if err != nil || !created.OK {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	baseID := int(created.Data["base_id"].(int64))
	for _, sourceName := range []string{"../secret.md", "/absolute.md", `C:\\secret.md`} {
		result, callErr := Default.Call(context.Background(), runtime, "knowledge.import", map[string]interface{}{
			"base_id": baseID, "source_name": sourceName, "content": "不得导入",
		})
		if callErr != nil || result.OK || result.Error != "KNOWLEDGE_IMPORT_INVALID" {
			t.Fatalf("source=%q result=%+v err=%v", sourceName, result, callErr)
		}
	}
}

func TestKnowledgeSearchAndDraftToolsRequireSelectedBase(t *testing.T) {
	runtime := &Runtime{DataDir: t.TempDir()}
	for toolName, args := range map[string]map[string]interface{}{
		"knowledge.search":        {"query": "引用资料"},
		"knowledge.content-draft": {"topic": "引用资料"},
		"knowledge.script-draft":  {"topic": "引用资料"},
	} {
		result, err := Default.Call(context.Background(), runtime, toolName, args)
		if err == nil || result == nil || result.OK {
			t.Fatalf("%s accepted an omitted base_ids: result=%+v err=%v", toolName, result, err)
		}
		args["base_ids"] = []interface{}{}
		result, err = Default.Call(context.Background(), runtime, toolName, args)
		if err != nil || result.OK {
			t.Fatalf("%s accepted empty base_ids: result=%+v err=%v", toolName, result, err)
		}
	}
}

func TestKnowledgeSearchAndDraftToolSchemasRequireSelectedBase(t *testing.T) {
	for _, toolName := range []string{"knowledge.search", "knowledge.content-draft", "knowledge.script-draft"} {
		definition, ok := Default.Get(toolName)
		if !ok {
			t.Fatalf("%s missing from registry", toolName)
		}
		var baseIDs *Param
		for index := range definition.Params {
			if definition.Params[index].Name == "base_ids" {
				baseIDs = &definition.Params[index]
				break
			}
		}
		if baseIDs == nil || baseIDs.Type != "array" || !baseIDs.Required {
			t.Fatalf("%s base_ids schema=%+v", toolName, definition.Params)
		}
	}
}

func TestKnowledgeAgentSurfaceHasNoPublishOrArbitraryPathCapability(t *testing.T) {
	for _, name := range []string{
		"knowledge.list",
		"knowledge.search",
		"knowledge.import",
		"knowledge.content-draft",
		"knowledge.script-draft",
	} {
		definition, ok := Default.Get(name)
		if !ok || !definition.AgentVisible {
			t.Fatalf("%s missing from agent surface", name)
		}
		for _, parameter := range definition.Params {
			if parameter.Name == "path" || parameter.Name == "file_path" {
				t.Fatalf("%s exposes arbitrary path parameter", name)
			}
		}
	}
}

func TestKnowledgeDraftToolsBuildCitedBriefsWithoutPublishing(t *testing.T) {
	dataDir := t.TempDir()
	runtime := &Runtime{DataDir: dataDir}
	created, err := Default.Call(context.Background(), runtime, "knowledge.base.create", map[string]interface{}{
		"name": "创作资料",
	})
	if err != nil || !created.OK {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	baseID := int(created.Data["base_id"].(int64))
	imported, err := Default.Call(context.Background(), runtime, "knowledge.import", map[string]interface{}{
		"base_id": baseID, "source_name": "voice.md",
		"content": "# 品牌语气\n\n表达要自然可信，开头先讲清楚观众收益。",
	})
	if err != nil || !imported.OK {
		t.Fatalf("imported=%+v err=%v", imported, err)
	}

	for _, toolName := range []string{"knowledge.content-draft", "knowledge.script-draft"} {
		result, callErr := Default.Call(context.Background(), runtime, toolName, map[string]interface{}{
			"base_ids": []interface{}{float64(baseID)},
			"topic":    "自然可信的产品介绍",
			"platform": "小红书",
		})
		if callErr != nil || !result.OK {
			t.Fatalf("%s result=%+v err=%v", toolName, result, callErr)
		}
		if result.Data["context"] == "" || len(result.Data["citations"].([]knowledge.Citation)) == 0 {
			t.Fatalf("%s must return cited local context: %+v", toolName, result.Data)
		}
		if result.Data["publish_authorized"] != false {
			t.Fatalf("%s must not grant publish authority", toolName)
		}
	}
}

func TestKnowledgeDraftFindsNaturalChineseTopicWithoutManualSpaces(t *testing.T) {
	dataDir := t.TempDir()
	runtime := &Runtime{DataDir: dataDir}
	created, err := Default.Call(context.Background(), runtime, "knowledge.base.create", map[string]interface{}{
		"name": "短视频编导库",
	})
	if err != nil || !created.OK {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	baseID := int(created.Data["base_id"].(int64))
	imported, err := Default.Call(context.Background(), runtime, "knowledge.import", map[string]interface{}{
		"base_id": baseID, "source_name": "director.md",
		"content": "# 短视频编导\n\n先设计三秒钩子，再把口播正文拆成分镜脚本。",
	})
	if err != nil || !imported.OK {
		t.Fatalf("imported=%+v err=%v", imported, err)
	}
	result, err := Default.Call(context.Background(), runtime, "knowledge.content-draft", map[string]interface{}{
		"base_ids": []interface{}{float64(baseID)},
		"topic":    "如何用知识库设计短视频三秒钩子与分镜脚本",
		"platform": "douyin",
	})
	if err != nil || !result.OK || result.Data["context"] == "" || len(result.Data["citations"].([]knowledge.Citation)) == 0 {
		t.Fatalf("natural Chinese draft lost cited context: result=%+v err=%v", result, err)
	}
}
