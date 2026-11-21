package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestContentStagePipelineTool(t *testing.T) {
	RegisterCatalog()
	result, err := Default.Call(context.Background(), nil, "content.stage.pipeline", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// 阶段以结构体切片返回，按渲染后的值断言，避免依赖具体 Go 类型。
	rendered := fmt.Sprintf("%v", result.Data["stages"])
	for _, want := range []string{"inbox", "素材箱", "topic", "选题", "archived", "归档"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("pipeline should mention %q, got %s", want, rendered)
		}
	}
	if count := strings.Count(rendered, "}"); count != 8 {
		t.Errorf("expected 8 stages, got %d: %s", count, rendered)
	}
}

func TestContentStageCheckReportsConflict(t *testing.T) {
	RegisterCatalog()
	result, err := Default.Call(context.Background(), nil, "content.stage.check", map[string]interface{}{
		"events": []interface{}{
			map[string]interface{}{"stage": "topic", "planned_date": "2026-03-10"},
			map[string]interface{}{"stage": "script", "planned_date": "2026-03-01"},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if result.Message != "inconsistent" {
		t.Errorf("status = %q, want inconsistent", result.Message)
	}
	if count, _ := result.Data["count"].(int); count != 1 {
		t.Errorf("count = %v, want 1", result.Data["count"])
	}
}

func TestContentStageCheckAcceptsCleanSchedule(t *testing.T) {
	RegisterCatalog()
	result, err := Default.Call(context.Background(), nil, "content.stage.check", map[string]interface{}{
		"events": []interface{}{
			map[string]interface{}{"stage": "topic", "planned_date": "2026-03-01"},
			map[string]interface{}{"stage": "script", "planned_date": "2026-03-05"},
		},
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if result.Message != "ok" {
		t.Errorf("status = %q, want ok", result.Message)
	}
}

func TestContentStageCheckRequiresEvents(t *testing.T) {
	RegisterCatalog()
	if _, err := Default.Call(context.Background(), nil, "content.stage.check", map[string]interface{}{}); err == nil {
		t.Fatal("missing events should fail")
	}
}
