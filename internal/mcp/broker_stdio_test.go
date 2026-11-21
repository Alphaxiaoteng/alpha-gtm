package mcp

import (
	"testing"

	"alpha-nexus-egolite/internal/tools"
)

func TestBrokerPublishFreezeSchemaUsesNumericMediaAndObjectTargets(t *testing.T) {
	media := brokerParamSchema("publish.plan.freeze", tools.Param{Name: "media_ids", Type: "array"})
	if media["items"].(map[string]any)["type"] != "number" {
		t.Fatalf("media schema=%#v", media)
	}
	targets := brokerParamSchema("publish.plan.freeze", tools.Param{Name: "targets", Type: "array"})
	item := targets["items"].(map[string]any)
	if item["type"] != "object" || item["properties"].(map[string]any)["settings_json"] == nil {
		t.Fatalf("target schema=%#v", targets)
	}
}
