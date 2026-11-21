package agenthub

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// waitDelegation polls the durable record the same way the UI does, so the test
// covers the real async path instead of calling the watcher directly.
func waitDelegation(t *testing.T, service *Service, id string) *Delegation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		delegation, err := service.GetDelegation(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if delegation.Status != DelegationRunning {
			return delegation
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("delegation %s did not finish", id)
	return nil
}

func TestDelegateRunsAnotherBotAndWritesTheResultBack(t *testing.T) {
	service := setupService(t, scriptedRunner{events: []ProviderEvent{
		{Type: "message.delta", Payload: map[string]any{"text": "登录流程检查完成，没有发现阻塞问题。"}},
	}})
	ctx := context.Background()
	parent, err := service.CreateConversation(ctx, ProviderCodex, "主线程", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := service.CreateBot(ctx, Bot{Name: "开发", Role: "工程"})
	if err != nil {
		t.Fatal(err)
	}

	delegation, err := service.Delegate(ctx, DelegateRequest{
		ParentConversationID: parent.ID,
		ToBotID:              worker.ID,
		Task:                 "检查登录流程",
	})
	if err != nil {
		t.Fatal(err)
	}
	if delegation.Status != DelegationRunning {
		t.Fatalf("new delegation status=%q, want running", delegation.Status)
	}
	if delegation.ChildConversationID == parent.ID {
		t.Fatal("delegation must run in the child bot's own conversation")
	}

	finished := waitDelegation(t, service, delegation.ID)
	if finished.Status != DelegationCompleted {
		t.Fatalf("delegation status=%q error=%q, want completed", finished.Status, finished.ErrorCode)
	}
	if !strings.Contains(finished.Result, "登录流程检查完成") {
		t.Fatalf("delegation result=%q", finished.Result)
	}

	child, err := service.GetConversation(ctx, delegation.ChildConversationID)
	if err != nil {
		t.Fatal(err)
	}
	if child.BotID != worker.ID {
		t.Fatalf("child conversation bot=%q, want %q", child.BotID, worker.ID)
	}

	// The parent transcript must show both halves of the handoff: who got the
	// task and what came back.
	updated, err := service.GetConversation(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var asked, answered bool
	for _, message := range updated.Messages {
		if message.Role == "user" && strings.HasPrefix(message.Content, "@开发 ") {
			asked = true
		}
		if message.Role == "assistant" && strings.Contains(message.Content, "@开发 交回结果") && strings.Contains(message.Content, "登录流程检查完成") {
			answered = true
		}
	}
	if !asked || !answered {
		t.Fatalf("parent transcript asked=%v answered=%v messages=%+v", asked, answered, updated.Messages)
	}

	// A second delegation continues the same child thread instead of creating a
	// sibling conversation.
	second, err := service.Delegate(ctx, DelegateRequest{
		ParentConversationID: parent.ID,
		ToBotID:              worker.ID,
		Task:                 "再确认一次",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ChildConversationID != delegation.ChildConversationID {
		t.Fatalf("second delegation conversation=%q, want reuse of %q", second.ChildConversationID, delegation.ChildConversationID)
	}
	waitDelegation(t, service, second.ID)
}

func TestDelegateValidatesItsInputs(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	ctx := context.Background()
	parent, err := service.CreateConversation(ctx, ProviderCodex, "主线程", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := service.CreateBot(ctx, Bot{Name: "开发"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.Delegate(ctx, DelegateRequest{ToBotID: worker.ID, Task: "x"}); err == nil {
		t.Fatal("delegation without a parent conversation must fail")
	}
	if _, err := service.Delegate(ctx, DelegateRequest{ParentConversationID: parent.ID, Task: "x"}); err == nil {
		t.Fatal("delegation without a target bot must fail")
	}
	if _, err := service.Delegate(ctx, DelegateRequest{ParentConversationID: parent.ID, ToBotID: worker.ID, Task: "   "}); err == nil {
		t.Fatal("delegation without a task must fail")
	}
	if _, err := service.Delegate(ctx, DelegateRequest{ParentConversationID: parent.ID, FromBotID: worker.ID, ToBotID: worker.ID, Task: "x"}); !errors.Is(err, ErrDelegationSelf) {
		t.Fatalf("self delegation error=%v, want ErrDelegationSelf", err)
	}
	if _, err := service.Delegate(ctx, DelegateRequest{ParentConversationID: parent.ID, ToBotID: "bot_missing", Task: "x"}); !errors.Is(err, ErrBotNotFound) {
		t.Fatalf("missing bot error=%v, want ErrBotNotFound", err)
	}
}

func TestCancelDelegationRecordsCancelledHandoff(t *testing.T) {
	service := setupService(t, scriptedRunner{block: true})
	ctx := context.Background()
	parent, err := service.CreateConversation(ctx, ProviderCodex, "主线程", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := service.CreateBot(ctx, Bot{Name: "开发"})
	if err != nil {
		t.Fatal(err)
	}
	delegation, err := service.Delegate(ctx, DelegateRequest{
		ParentConversationID: parent.ID,
		ToBotID:              worker.ID,
		Task:                 "长时间任务",
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.CancelDelegation(ctx, delegation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != DelegationCancelled {
		t.Fatalf("cancel status=%q, want cancelled", cancelled.Status)
	}
}

func TestDelegateRejectsSecondHandoffToTheSameBot(t *testing.T) {
	service := setupService(t, scriptedRunner{block: true})
	ctx := context.Background()
	parent, err := service.CreateConversation(ctx, ProviderCodex, "主线程", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := service.CreateBot(ctx, Bot{Name: "开发"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Delegate(ctx, DelegateRequest{ParentConversationID: parent.ID, ToBotID: worker.ID, Task: "第一件"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Delegate(ctx, DelegateRequest{ParentConversationID: parent.ID, ToBotID: worker.ID, Task: "第二件"}); !errors.Is(err, ErrDelegationBusy) {
		t.Fatalf("duplicate handoff error=%v, want ErrDelegationBusy", err)
	}
}

func TestDelegateCapsRunningHandoffsPerConversation(t *testing.T) {
	service := setupService(t, scriptedRunner{block: true})
	ctx := context.Background()
	parent, err := service.CreateConversation(ctx, ProviderCodex, "主线程", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < MaxConcurrentDelegations; index++ {
		worker, createErr := service.CreateBot(ctx, Bot{Name: "机器人" + string(rune('A'+index))})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, delegateErr := service.Delegate(ctx, DelegateRequest{ParentConversationID: parent.ID, ToBotID: worker.ID, Task: "任务"}); delegateErr != nil {
			t.Fatalf("delegation %d: %v", index, delegateErr)
		}
	}
	extra, err := service.CreateBot(ctx, Bot{Name: "多余的 Bot"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Delegate(ctx, DelegateRequest{ParentConversationID: parent.ID, ToBotID: extra.ID, Task: "任务"}); !errors.Is(err, ErrDelegationLimit) {
		t.Fatalf("over-cap error=%v, want ErrDelegationLimit", err)
	}
}

func TestRecoverInterruptedDelegationsClosesTheHandoff(t *testing.T) {
	service := setupService(t, scriptedRunner{})
	ctx := context.Background()
	parent, err := service.CreateConversation(ctx, ProviderCodex, "主线程", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := service.CreateBot(ctx, Bot{Name: "开发"})
	if err != nil {
		t.Fatal(err)
	}
	// 直接落一条「运行中」的交接，模拟进程在 watcher 还在轮询时被杀。
	stale, err := service.store.CreateDelegation(ctx, Delegation{
		ParentConversationID: parent.ID,
		ToBotID:              worker.ID,
		ChildConversationID:  parent.ID,
		ChildRunID:           "run_gone",
		Task:                 "重启前没跑完的任务",
		Status:               DelegationRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.recoverDelegations(); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.GetDelegation(ctx, stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != DelegationFailed || recovered.ErrorCode != "AGENT_DELEGATION_INTERRUPTED" {
		t.Fatalf("recovered=%+v", recovered)
	}
	updated, err := service.GetConversation(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var noted bool
	for _, message := range updated.Messages {
		if message.Role == "assistant" && strings.Contains(message.Content, "上次交接在应用重启时中断") {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("parent transcript missing interruption note: %+v", updated.Messages)
	}
}
