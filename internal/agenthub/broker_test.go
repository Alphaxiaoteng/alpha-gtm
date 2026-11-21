package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tools"
)

func newBrokerTest(t *testing.T, def tools.Def) (*ToolBroker, *Store, *Run) {
	return newBrokerTestWithMessage(t, def, "test")
}

func newBrokerTestWithMessage(t *testing.T, def tools.Def, message string) (*ToolBroker, *Store, *Run) {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	conversation, err := store.CreateConversation(context.Background(), ProviderCodex, "test", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(context.Background(), conversation.ID, ProviderCodex, "", message, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	registry := tools.NewRegistry()
	registry.Register(def)
	if err := registry.SetAgentMeta(def.Name, "test", def.AgentRisk, "test impact", "test-idempotency"); err != nil {
		t.Fatal(err)
	}
	broker, err := NewToolBroker(store, registry, &tools.Runtime{DataDir: database.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	return broker, store, run
}

func TestToolBrokerL0RedactsSensitiveOutput(t *testing.T) {
	broker, _, run := newBrokerTest(t, tools.Def{
		Name: "safe.read", Category: "test", SideEffect: "read", AgentRisk: tools.AgentRiskL0,
		Handler: func(context.Context, *tools.Runtime, map[string]interface{}) (*tools.Result, error) {
			return &tools.Result{OK: true, Data: map[string]interface{}{
				"id": 4, "profile_path": "/private/profile", "nested": map[string]interface{}{"transport_token": "secret"},
			}}, nil
		},
	})
	call, err := broker.Propose(context.Background(), run.ID, "safe.read", nil)
	if err != nil {
		t.Fatal(err)
	}
	if call.Status != "completed" {
		t.Fatalf("status=%s", call.Status)
	}
	encoded, _ := json.Marshal(call.Result)
	if strings.Contains(string(encoded), "profile") || strings.Contains(string(encoded), "secret") {
		t.Fatalf("sensitive result was not redacted: %s", encoded)
	}
}

func TestToolBrokerRejectsGenericSkillWrapperWhenItIsNotAgentVisible(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	conversation, err := store.CreateConversation(context.Background(), ProviderCodex, "test", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.CreateRun(context.Background(), conversation.ID, ProviderCodex, "", "run a publish skill", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	registry := tools.NewRegistry()
	registry.Register(tools.Def{Name: "skill.run", Category: "skill", SideEffect: "external_publish", Handler: func(context.Context, *tools.Runtime, map[string]interface{}) (*tools.Result, error) {
		called = true
		return &tools.Result{OK: true}, nil
	}})
	broker, err := NewToolBroker(store, registry, &tools.Runtime{DataDir: database.DataDir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Propose(context.Background(), run.ID, "skill.run", map[string]interface{}{"skill_id": "social.publish-prepare"}); err == nil || !strings.Contains(err.Error(), "AGENT_TOOL_NOT_ALLOWED") {
		t.Fatalf("generic publish skill wrapper must be unavailable to Agent, err=%v", err)
	}
	if called {
		t.Fatal("unexposed generic skill wrapper executed")
	}
}

func TestToolBrokerStripsModelApprovalFieldsAndRejectsReplay(t *testing.T) {
	var received map[string]interface{}
	broker, store, run := newBrokerTest(t, tools.Def{
		Name: "safe.write", Category: "test", SideEffect: "write", AgentRisk: tools.AgentRiskL1,
		Handler: func(_ context.Context, _ *tools.Runtime, args map[string]interface{}) (*tools.Result, error) {
			received = args
			return &tools.Result{OK: true}, nil
		},
	})
	call, err := broker.Propose(context.Background(), run.ID, "safe.write", map[string]interface{}{
		"object_id": 7, "confirm": true, "approval_id": "forged", "nonce": "forged",
	})
	if err != nil {
		t.Fatal(err)
	}
	var frozen string
	if err := store.db.QueryRow(`SELECT params_json FROM agent_tool_calls WHERE id=?`, call.ID).Scan(&frozen); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(frozen, "forged") || strings.Contains(frozen, "confirm") {
		t.Fatalf("model-controlled confirmation survived freezing: %s", frozen)
	}
	_, executed, err := broker.Resolve(context.Background(), call.ApprovalID, "approved", true)
	if err != nil {
		t.Fatal(err)
	}
	if executed.Status != "completed" || received["confirm"] != true {
		t.Fatalf("approved call did not receive server confirmation: %+v %+v", executed, received)
	}
	if _, _, err := broker.Resolve(context.Background(), call.ApprovalID, "approved", false); err == nil {
		t.Fatal("approval replay must fail")
	}
}

func TestToolBrokerRejectsFrozenParameterTampering(t *testing.T) {
	executed := false
	broker, store, run := newBrokerTest(t, tools.Def{
		Name: "safe.write", Category: "test", SideEffect: "write", AgentRisk: tools.AgentRiskL1,
		Handler: func(context.Context, *tools.Runtime, map[string]interface{}) (*tools.Result, error) {
			executed = true
			return &tools.Result{OK: true}, nil
		},
	})
	call, err := broker.Propose(context.Background(), run.ID, "safe.write", map[string]interface{}{"object_id": 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE agent_tool_calls SET params_json='{"object_id":8}' WHERE id=?`, call.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := broker.Resolve(context.Background(), call.ApprovalID, "approved", false); err == nil || !strings.Contains(err.Error(), "TAMPERED") {
		t.Fatalf("expected tamper rejection, got %v", err)
	}
	if executed {
		t.Fatal("tampered call executed")
	}
}

func TestToolBrokerRejectsPathAndPrivateApprovalForPublishPrepare(t *testing.T) {
	var received map[string]interface{}
	broker, _, run := newBrokerTest(t, tools.Def{
		Name: "publish.plan.prepare", Category: "publish", SideEffect: "external_publish", AgentRisk: tools.AgentRiskL3,
		Handler: func(_ context.Context, _ *tools.Runtime, args map[string]interface{}) (*tools.Result, error) {
			received = args
			return &tools.Result{OK: true}, nil
		},
	})
	broker.runtime.AuthorizeAgentTool = func(_ context.Context, toolName string, frozen map[string]interface{}) (map[string]interface{}, error) {
		if toolName != "publish.plan.prepare" || frozen["approval_id"] != nil || frozen["nonce"] != nil {
			t.Fatalf("unexpected frozen domain args: tool=%s args=%+v", toolName, frozen)
		}
		return map[string]interface{}{"plan_id": int64(4), "snapshot_hash": "hash", "approval_id": "server-approval", "nonce": "server-nonce"}, nil
	}
	if _, err := broker.Propose(context.Background(), run.ID, "publish.plan.prepare", map[string]interface{}{"path": "/tmp/source"}); err == nil {
		t.Fatal("absolute path must be rejected")
	}
	call, err := broker.Propose(context.Background(), run.ID, "publish.plan.prepare", map[string]interface{}{
		"plan_id": 4, "approval_id": "generic", "nonce": "generic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := broker.Resolve(context.Background(), call.ApprovalID, "approved", false); err != nil {
		t.Fatal(err)
	}
	if received["approval_id"] != "server-approval" {
		t.Fatalf("server domain approval was not passed to Registry: %+v", received)
	}
	if received["nonce"] != "server-nonce" {
		t.Fatalf("server domain nonce was not passed to Registry: %+v", received)
	}
	if received["confirm"] != nil {
		t.Fatalf("publish prepare received generic confirm: %+v", received)
	}
}

func TestToolBrokerBridgesPublishAndShopDomainApprovalsWithoutPersistingCredentials(t *testing.T) {
	tests := []struct {
		name       string
		input      map[string]interface{}
		wantTool   string
		wantNested bool
	}{
		{
			name:     "publish prepare",
			input:    map[string]interface{}{"plan_id": int64(7), "snapshot_hash": "hash", "confirm": true, "approval_id": "forged", "nonce": "forged"},
			wantTool: "publish.plan.prepare",
		},
		{
			name:     "publish submit",
			input:    map[string]interface{}{"plan_id": int64(7), "snapshot_hash": "hash", "confirm": true, "approval_id": "forged", "nonce": "forged"},
			wantTool: "publish.plan.submit",
		},
		{
			name: "shop prepare",
			input: map[string]interface{}{"input": map[string]interface{}{
				"plan_id": int64(7), "snapshot_hash": "hash", "confirm": true, "approval_id": "forged", "nonce": "forged",
			}},
			wantTool:   "shop.listing.prepare",
			wantNested: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var received map[string]interface{}
			broker, store, run := newBrokerTest(t, tools.Def{
				Name: test.wantTool, Category: "test", SideEffect: "external_publish", AgentRisk: tools.AgentRiskL3,
				Handler: func(_ context.Context, _ *tools.Runtime, args map[string]interface{}) (*tools.Result, error) {
					received = args
					return &tools.Result{OK: true, Data: map[string]interface{}{
						"approval_id": "server-approval", "nonce": "server-nonce", "confirm": true,
					}}, nil
				},
			})
			var frozen map[string]interface{}
			broker.runtime.AuthorizeAgentTool = func(_ context.Context, toolName string, args map[string]interface{}) (map[string]interface{}, error) {
				if toolName != test.wantTool {
					t.Fatalf("callback tool=%q", toolName)
				}
				frozen = args
				if test.wantNested {
					input, ok := args["input"].(map[string]interface{})
					if !ok || len(input) != 2 || input["approval_id"] != nil || input["nonce"] != nil || input["confirm"] != nil {
						t.Fatalf("frozen nested args=%+v", args)
					}
					return map[string]interface{}{"input": map[string]interface{}{
						"plan_id": int64(7), "snapshot_hash": "hash", "approval_id": "server-approval", "nonce": "server-nonce",
					}}, nil
				}
				if len(args) != 2 || args["approval_id"] != nil || args["nonce"] != nil || args["confirm"] != nil {
					t.Fatalf("frozen flat args=%+v", args)
				}
				return map[string]interface{}{"plan_id": int64(7), "snapshot_hash": "hash", "approval_id": "server-approval", "nonce": "server-nonce"}, nil
			}
			call, err := broker.Propose(context.Background(), run.ID, test.wantTool, test.input)
			if err != nil {
				t.Fatal(err)
			}
			if _, executed, err := broker.Resolve(context.Background(), call.ApprovalID, "approved", false); err != nil || executed == nil || executed.Status != "completed" {
				t.Fatalf("resolve=%+v err=%v", executed, err)
			}
			if frozen == nil || strings.Contains(mustJSON(frozen), "forged") {
				t.Fatalf("model credentials survived frozen callback args: %+v", frozen)
			}
			if test.wantNested {
				if _, ok := received["confirm"]; ok {
					t.Fatalf("shop prepare received top-level generic confirm: %+v", received)
				}
				if _, ok := received["input"].(map[string]interface{})["confirm"]; ok {
					t.Fatalf("shop prepare received nested generic confirm: %+v", received)
				}
			}
			storedCall, paramsJSON, err := store.GetToolCall(context.Background(), call.ID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(paramsJSON, "server-approval") || strings.Contains(paramsJSON, "forged") {
				t.Fatalf("domain credentials were persisted in ToolCall: %+v params=%s", storedCall, paramsJSON)
			}
			if strings.Contains(mustJSON(call.Result), "server-approval") || strings.Contains(mustJSON(call.Result), "server-nonce") {
				t.Fatalf("domain credentials leaked in Agent result: %+v", call.Result)
			}
			events, err := store.ListEventsAfter(context.Background(), run.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(mustJSON(events), "server-approval") || strings.Contains(mustJSON(events), "server-nonce") {
				t.Fatalf("domain credentials leaked in Agent events: %+v", events)
			}
		})
	}
}

func TestToolBrokerDomainApprovalFailurePersistsFailedWithoutExecution(t *testing.T) {
	for _, test := range []struct {
		name     string
		callback func(context.Context, string, map[string]interface{}) (map[string]interface{}, error)
	}{
		{name: "missing callback"},
		{name: "failed callback", callback: func(context.Context, string, map[string]interface{}) (map[string]interface{}, error) {
			return nil, errors.New("synthetic domain approval failure")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			handlerCalled := false
			broker, store, run := newBrokerTest(t, tools.Def{
				Name: "publish.plan.prepare", Category: "test", SideEffect: "external_publish", AgentRisk: tools.AgentRiskL3,
				Handler: func(context.Context, *tools.Runtime, map[string]interface{}) (*tools.Result, error) {
					handlerCalled = true
					return &tools.Result{OK: true}, nil
				},
			})
			broker.runtime.AuthorizeAgentTool = test.callback
			call, err := broker.Propose(context.Background(), run.ID, "publish.plan.prepare", map[string]interface{}{"plan_id": int64(7), "snapshot_hash": "hash"})
			if err != nil {
				t.Fatal(err)
			}
			_, executed, err := broker.Resolve(context.Background(), call.ApprovalID, "approved", false)
			if err == nil || executed == nil || executed.Status != "failed" {
				t.Fatalf("failed domain approval result=%+v err=%v", executed, err)
			}
			if handlerCalled {
				t.Fatal("Registry handler ran after domain approval failure")
			}
			stored, _, err := store.GetToolCall(context.Background(), call.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != "failed" || stored.ErrorCode == "" {
				t.Fatalf("failed ToolCall was not persisted: %+v", stored)
			}
			events, err := store.ListEventsAfter(context.Background(), run.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.Type == "tool.started" {
					t.Fatal("domain approval failure left a tool.started event")
				}
			}
		})
	}
}

func mustJSON(value interface{}) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func TestToolBrokerDoesNotInjectGenericConfirmationIntoPublishPlanFreeze(t *testing.T) {
	var received map[string]interface{}
	broker, _, run := newBrokerTest(t, tools.Def{
		Name: "publish.plan.freeze", Category: "publish", SideEffect: "external_publish", AgentRisk: tools.AgentRiskL3,
		Handler: func(_ context.Context, _ *tools.Runtime, args map[string]interface{}) (*tools.Result, error) {
			received = args
			return &tools.Result{OK: true}, nil
		},
	})
	call, err := broker.Propose(context.Background(), run.ID, "publish.plan.freeze", map[string]interface{}{"content_id": 7})
	if err != nil {
		t.Fatal(err)
	}
	if _, executed, err := broker.Resolve(context.Background(), call.ApprovalID, "approved", false); err != nil || executed.Status != "completed" {
		t.Fatalf("resolve=%+v err=%v", executed, err)
	}
	if received["confirm"] != nil {
		t.Fatalf("publish plan freeze received generic confirm: %+v", received)
	}
}

func TestToolBrokerAllowsOnlyCurrentMessageMediaImportPathAfterApproval(t *testing.T) {
	allowedPath := t.TempDir()
	var received map[string]interface{}
	def := tools.Def{
		Name: "media.import", Category: "media", SideEffect: "write", AgentRisk: tools.AgentRiskL2,
		Handler: func(_ context.Context, _ *tools.Runtime, args map[string]interface{}) (*tools.Result, error) {
			received = args
			return &tools.Result{OK: true, Data: map[string]interface{}{"imported_count": 2}}, nil
		},
	}
	broker, _, run := newBrokerTestWithMessage(t, def, "把这个文件夹导入内容库："+allowedPath)
	call, err := broker.Propose(context.Background(), run.ID, "media.import", map[string]interface{}{
		"path": allowedPath, "recursive": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if call.Status != "pending" || call.RiskLevel != string(tools.AgentRiskL2) || call.ApprovalID == "" {
		t.Fatalf("media import must require L2 approval: %+v", call)
	}
	if strings.Contains(call.ImpactPreview, allowedPath) || !strings.Contains(call.ImpactPreview, "路径") {
		t.Fatalf("approval preview must identify the object without exposing the absolute path: %q", call.ImpactPreview)
	}
	if _, executed, err := broker.Resolve(context.Background(), call.ApprovalID, "approved", true); err != nil {
		t.Fatal(err)
	} else if executed.Status != "completed" {
		t.Fatalf("status=%s", executed.Status)
	}
	if received["path"] != allowedPath || received["confirm"] != true {
		t.Fatalf("approved import args=%+v", received)
	}

	blockedBroker, _, blockedRun := newBrokerTestWithMessage(t, def, "导入我刚才说的文件夹")
	if _, err := blockedBroker.Propose(context.Background(), blockedRun.ID, "media.import", map[string]interface{}{"path": allowedPath}); err == nil || !strings.Contains(err.Error(), "PATH_NOT_AUTHORIZED") {
		t.Fatalf("path absent from current user message must be rejected, got %v", err)
	}

	prefixBroker, _, prefixRun := newBrokerTestWithMessage(t, def, "导入这个路径："+allowedPath+"-other")
	if _, err := prefixBroker.Propose(context.Background(), prefixRun.ID, "media.import", map[string]interface{}{"path": allowedPath}); err == nil || !strings.Contains(err.Error(), "PATH_NOT_AUTHORIZED") {
		t.Fatalf("path prefix must not authorize a different object, got %v", err)
	}
}
