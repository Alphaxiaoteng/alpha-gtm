package agents

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/agentcfg"
	"alpha-nexus-egolite/internal/observability"
	"alpha-nexus-egolite/internal/tools"
)

func TestRemoveAutomaticStudioExecutionKeepsPlanning(t *testing.T) {
	steps, removed := removeAutomaticStudioExecution([]ToolAgentStep{
		{Tool: "studio.clip.plan"},
		{Tool: "studio.clip.execute", Args: map[string]interface{}{"confirm": true}},
		{Tool: "account.list"},
	})
	if !removed || len(steps) != 2 || steps[0].Tool != "studio.clip.plan" || steps[1].Tool != "account.list" {
		t.Fatalf("steps=%+v removed=%v", steps, removed)
	}
}

func TestReviewedSocialSkillsAreTheOnlyLLMExecutableBrowserException(t *testing.T) {
	allowed, removed := removeAutomaticExternalActions([]ToolAgentStep{
		{Tool: "skill.run", Args: map[string]interface{}{"skill_id": "social.account-check", "account_ids": []interface{}{float64(1)}}},
		{Tool: "skill.run", Args: map[string]interface{}{"skill_id": "social.publish-prepare", "plan_id": float64(1)}},
		{Tool: "browser.start", Args: map[string]interface{}{"account_id": float64(1)}},
	})
	if len(allowed) != 1 || allowed[0].Tool != "skill.run" {
		t.Fatalf("allowed=%+v", allowed)
	}
	if len(removed) != 2 || removed[0].Tool != "skill.run" || removed[1].Tool != "browser.start" {
		t.Fatalf("removed=%+v", removed)
	}
	if !strings.Contains(compactToolSchema(mustToolSchema(t), 100), "skill.run") {
		t.Fatal("skill.run must be discoverable to the LLM planner")
	}
}

func mustToolSchema(t *testing.T) []byte {
	t.Helper()
	schema, err := tools.Default.SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestCompactToolSchemaIncludesLocalStudioTools(t *testing.T) {
	schema, err := tools.Default.SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	compact := compactToolSchema(schema, 48)
	for _, name := range []string{"studio.media.list", "studio.script.create", "studio.clip.plan"} {
		if !strings.Contains(compact, name) {
			t.Fatalf("compact schema missing %s:\n%s", name, compact)
		}
	}
}

func TestCompactToolSchemaExcludesActionsNeedingSeparateConfirmation(t *testing.T) {
	schema, err := tools.Default.SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	compact := compactToolSchema(schema, 48)
	for _, name := range []string{"publish.run", "queue.enqueue", "workflow.run", "tikhub.rank.daily", "studio.clip.execute", "browser.start", "browser.stop", "fleet.run", "meta.login"} {
		if strings.Contains(compact, name) {
			t.Fatalf("compact schema must exclude %s:\n%s", name, compact)
		}
	}
}

func TestPlannerEgoSpaceActionsNeedSeparateUserConfirmation(t *testing.T) {
	for _, name := range []string{"ego.space.ensure", "ego.space.open", "ego.space.login", "ego.space.resume"} {
		def, ok := tools.Default.Get(name)
		if !ok {
			t.Fatalf("missing tool definition %s", name)
		}
		if !plannerActionNeedsSeparateConfirmation(name, def.SideEffect) {
			t.Fatalf("model-planned browser action %s (%s) was not blocked", name, def.SideEffect)
		}
	}
}

func TestLLMToolsAgentRejectsUnavailableGatewayWithoutCallingTools(t *testing.T) {
	dir := configuredToolPlannerDir(t, agentcfg.DoubaoModelID)
	setToolPlannerCompleter(t, func(model string) (toolPlannerCompleter, error) {
		return nil, errors.New("offline")
	})
	var events bytes.Buffer
	result, err := LLMToolsAgent(context.Background(), SchemeRunRequest{
		Goal:    "list local accounts",
		DryRun:  false,
		DataDir: dir,
		Runtime: &tools.Runtime{DataDir: dir, Events: observability.NewJSONEmitter(&events, observability.Options{Service: "test"})},
	}, "trace-gateway-down")
	if err == nil {
		t.Fatal("expected gateway failure")
	}
	if result == nil || result.OK || result.Error != "LLM_GATEWAY_UNAVAILABLE" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Steps) != 0 || events.Len() != 0 {
		t.Fatalf("gateway failure must not call tools: steps=%v events=%s", result.Steps, events.String())
	}
}

func TestLLMToolsAgentRejectsInvalidPlanWithoutCallingTools(t *testing.T) {
	dir := configuredToolPlannerDir(t, agentcfg.DoubaoModelID)
	setToolPlannerCompleter(t, func(model string) (toolPlannerCompleter, error) {
		return &fakeToolPlannerCompleter{output: "I cannot produce JSON today."}, nil
	})
	var events bytes.Buffer
	result, err := LLMToolsAgent(context.Background(), SchemeRunRequest{
		Goal:    "list local accounts",
		DryRun:  false,
		DataDir: dir,
		Runtime: &tools.Runtime{DataDir: dir, Events: observability.NewJSONEmitter(&events, observability.Options{Service: "test"})},
	}, "trace-invalid-plan")
	if err == nil {
		t.Fatal("expected invalid plan failure")
	}
	if result == nil || result.OK || result.Error != "LLM_TOOL_PLAN_INVALID" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Steps) != 0 || events.Len() != 0 {
		t.Fatalf("invalid plan must not call tools: steps=%v events=%s", result.Steps, events.String())
	}
}

func TestLLMToolsAgentBlocksAutomaticPublishWithoutCallingTools(t *testing.T) {
	dir := configuredToolPlannerDir(t, agentcfg.DoubaoModelID)
	setToolPlannerCompleter(t, func(model string) (toolPlannerCompleter, error) {
		return &fakeToolPlannerCompleter{output: `{"steps":[{"tool":"publish.run","args":{"platform":"facebook","content_id":1,"target_id":1,"dry_run":false,"confirm":true,"idempotency_key":"model-generated"}}],"reason":"publish"}`}, nil
	})
	var events bytes.Buffer
	result, err := LLMToolsAgent(context.Background(), SchemeRunRequest{
		Goal:    "publish this now",
		DryRun:  false,
		DataDir: dir,
		Runtime: &tools.Runtime{DataDir: dir, Events: observability.NewJSONEmitter(&events, observability.Options{Service: "test"})},
	}, "trace-block-publish")
	if err != nil {
		t.Fatalf("blocking response should be a stable result, got %v", err)
	}
	if result == nil || result.OK || result.Error != "AUTOMATED_EXTERNAL_ACTION_REQUIRES_USER_CONFIRMATION" {
		t.Fatalf("result=%+v", result)
	}
	if len(result.Steps) != 1 || result.Steps[0]["tool"] != "publish.run" || result.Steps[0]["executed"] != false || events.Len() != 0 {
		t.Fatalf("publish must be blocked before Registry: steps=%+v events=%s", result.Steps, events.String())
	}
}

func TestLLMToolsAgentRunsSafeStepsWhileBlockingExternalActions(t *testing.T) {
	dir := configuredToolPlannerDir(t, agentcfg.DoubaoModelID)
	setToolPlannerCompleter(t, func(model string) (toolPlannerCompleter, error) {
		return &fakeToolPlannerCompleter{output: `{"steps":[{"tool":"account.list","args":{}},{"tool":"publish.run","args":{"platform":"facebook","content_id":1,"target_id":1,"dry_run":false,"confirm":true,"idempotency_key":"model-generated"}}],"reason":"inspect then publish"}`}, nil
	})
	var events bytes.Buffer
	result, err := LLMToolsAgent(context.Background(), SchemeRunRequest{
		Goal:    "inspect then publish",
		DryRun:  false,
		DataDir: dir,
		Runtime: &tools.Runtime{DataDir: dir, Events: observability.NewJSONEmitter(&events, observability.Options{Service: "test"})},
	}, "trace-safe-then-block")
	if err != nil || result == nil || result.OK || result.Error != "AUTOMATED_EXTERNAL_ACTION_REQUIRES_USER_CONFIRMATION" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Steps) != 2 || result.Steps[0]["tool"] != "account.list" || result.Steps[0]["ok"] != true || result.Steps[1]["tool"] != "publish.run" || result.Steps[1]["executed"] != false {
		t.Fatalf("unexpected guarded steps=%+v", result.Steps)
	}
	if !strings.Contains(events.String(), "account.list") || strings.Contains(events.String(), "publish.run") {
		t.Fatalf("only safe tool should reach Registry: %s", events.String())
	}
}

func TestLLMToolsAgentRunsReviewedLocalMaterialsSkill(t *testing.T) {
	dir := configuredToolPlannerDir(t, agentcfg.DoubaoModelID)
	setToolPlannerCompleter(t, func(model string) (toolPlannerCompleter, error) {
		return &fakeToolPlannerCompleter{output: `{"steps":[{"tool":"skill.run","args":{"skill_id":"social.materials-list"}}],"reason":"list local materials"}`}, nil
	})
	result, err := LLMToolsAgent(context.Background(), SchemeRunRequest{
		Goal:    "列出本地素材",
		DryRun:  false,
		DataDir: dir,
		Runtime: &tools.Runtime{DataDir: dir},
	}, "trace-reviewed-materials")
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Steps) != 1 || result.Steps[0]["tool"] != "skill.run" || result.Steps[0]["ok"] != true {
		t.Fatalf("skill.run was not executed: %+v", result.Steps)
	}
}

func TestLLMToolsAgentBlocksMediaImportOutsideCurrentSession(t *testing.T) {
	dir := configuredToolPlannerDir(t, agentcfg.DoubaoModelID)
	setToolPlannerCompleter(t, func(model string) (toolPlannerCompleter, error) {
		return &fakeToolPlannerCompleter{output: `{"steps":[{"tool":"media.import","args":{"path":"/not-a-session/clip.mp4"}}],"reason":"import"}`}, nil
	})
	var events bytes.Buffer
	result, err := LLMToolsAgent(context.Background(), SchemeRunRequest{
		Goal:                    "导入当前附件",
		DryRun:                  false,
		DataDir:                 dir,
		RestrictMediaImport:     true,
		AllowedMediaImportPaths: []string{"/session/attaches/clip.mp4"},
		Runtime:                 &tools.Runtime{DataDir: dir, Events: observability.NewJSONEmitter(&events, observability.Options{Service: "test"})},
	}, "trace-block-media-path")
	if err != nil || result == nil || result.OK || result.Error != errMediaImportPath {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Steps) != 1 || result.Steps[0]["tool"] != "media.import" || result.Steps[0]["executed"] != false || events.Len() != 0 {
		t.Fatalf("outside media import must be blocked before Registry: steps=%+v events=%s", result.Steps, events.String())
	}
}

func TestMediaImportPathsAllowedRequiresExactSessionAttachment(t *testing.T) {
	steps := []ToolAgentStep{{Tool: "media.import", Args: map[string]interface{}{"path": "/session/attaches/clip.mp4"}}}
	if !mediaImportPathsAllowed(steps, []string{"/session/attaches/clip.mp4"}) {
		t.Fatal("current-session attachment should be allowed")
	}
	if mediaImportPathsAllowed(steps, []string{"/session/attaches/other.mp4"}) {
		t.Fatal("a different path must be rejected")
	}
	if mediaImportPathsAllowed([]ToolAgentStep{{Tool: "media.import", Args: map[string]interface{}{}}}, []string{"/session/attaches/clip.mp4"}) {
		t.Fatal("missing media path must be rejected")
	}
}

func TestLLMToolsAgentReturnsValidatedDryPlan(t *testing.T) {
	dir := configuredToolPlannerDir(t, agentcfg.DoubaoModelID)
	fake := &fakeToolPlannerCompleter{output: `{"steps":[{"tool":"account.list","args":{}}],"reason":"read local accounts"}`}
	setToolPlannerCompleter(t, func(model string) (toolPlannerCompleter, error) {
		return fake, nil
	})
	result, err := LLMToolsAgent(context.Background(), SchemeRunRequest{
		Goal: "list local accounts", DryRun: true, DataDir: dir,
	}, "trace-valid-plan")
	if err != nil || result == nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(result.Steps) != 1 || result.Steps[0]["tool"] != "account.list" || result.Steps[0]["executed"] != false {
		t.Fatalf("unexpected dry plan: %+v", result.Steps)
	}
	if len(fake.inputs) != 1 ||
		!strings.Contains(fake.inputs[0], "[system]\nYou are Alpha-Nexus Tool Planner.") ||
		!strings.Contains(fake.inputs[0], "[goal]\nlist local accounts\n[/goal]") ||
		!strings.Contains(fake.inputs[0], "[tools]\n") ||
		!strings.Contains(fake.inputs[0], "- account.list ") {
		t.Fatalf("Ark Responses input did not explicitly contain system, goal and tools: %q", fake.inputs)
	}
}

func TestLLMToolsAgentUsesRequestedVerifiedArkModel(t *testing.T) {
	dir := configuredToolPlannerDir(
		t,
		agentcfg.DoubaoModelID,
		agentcfg.DoubaoSeed21TurboModelID,
	)
	var createdModel string
	setToolPlannerCompleter(t, func(model string) (toolPlannerCompleter, error) {
		createdModel = model
		return &fakeToolPlannerCompleter{output: `{"steps":[{"tool":"account.list","args":{}}]}`}, nil
	})
	result, err := LLMToolsAgent(context.Background(), SchemeRunRequest{
		Goal: "list", DryRun: true, DataDir: dir, Model: agentcfg.DoubaoSeed21TurboModelID,
	}, "trace-verified-model")
	if err != nil || result == nil || !result.OK ||
		createdModel != agentcfg.DoubaoSeed21TurboModelID ||
		result.Data["model"] != agentcfg.DoubaoSeed21TurboModelID {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestLLMToolsAgentRejectsUnverifiedRequestedModelBeforeCompleter(t *testing.T) {
	dir := configuredToolPlannerDir(t, agentcfg.DoubaoModelID)
	called := false
	setToolPlannerCompleter(t, func(model string) (toolPlannerCompleter, error) {
		called = true
		return &fakeToolPlannerCompleter{}, nil
	})
	result, err := LLMToolsAgent(context.Background(), SchemeRunRequest{
		Goal: "list", DryRun: true, DataDir: dir, Model: agentcfg.DoubaoSeed21TurboModelID,
	}, "trace-unverified-model")
	if err == nil || result == nil || result.OK || result.Error != errLLMModelNotVerified || called {
		t.Fatalf("result=%+v err=%v called=%v", result, err, called)
	}
}

func TestLLMToolsAgentRejectsMissingVerifiedDefaultBeforeCompleter(t *testing.T) {
	called := false
	setToolPlannerCompleter(t, func(model string) (toolPlannerCompleter, error) {
		called = true
		return &fakeToolPlannerCompleter{}, nil
	})
	result, err := LLMToolsAgent(context.Background(), SchemeRunRequest{
		Goal: "list", DryRun: true, DataDir: t.TempDir(),
	}, "trace-no-verified-model")
	if err == nil || result == nil || result.OK || result.Error != errLLMModelNotVerified || called {
		t.Fatalf("result=%+v err=%v called=%v", result, err, called)
	}
}

type fakeToolPlannerCompleter struct {
	output string
	err    error
	inputs []string
}

func (f *fakeToolPlannerCompleter) Complete(_ context.Context, input string) (string, error) {
	f.inputs = append(f.inputs, input)
	return f.output, f.err
}

func configuredToolPlannerDir(t *testing.T, defaultModel string, models ...string) string {
	t.Helper()
	dir := t.TempDir()
	allModels := append([]string{defaultModel}, models...)
	entries := make([]agentcfg.ModelEntry, 0, len(allModels))
	for _, model := range allModels {
		entries = append(entries, agentcfg.ModelEntry{
			ID:          model,
			Enabled:     true,
			ValidatedAt: time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		})
	}
	if err := agentcfg.Save(dir, &agentcfg.Config{
		DefaultModel: defaultModel,
		Models:       entries,
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func setToolPlannerCompleter(
	t *testing.T,
	factory func(model string) (toolPlannerCompleter, error),
) {
	t.Helper()
	previous := newToolPlannerCompleter
	newToolPlannerCompleter = factory
	t.Cleanup(func() { newToolPlannerCompleter = previous })
}
