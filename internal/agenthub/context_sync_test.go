package agenthub

import (
	"context"
	"strings"
	"testing"
)

func TestUnattributedMessagesAreChronologicalAndRunFree(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	conversation, err := store.CreateConversation(ctx, ProviderCodex, "t", "codex/gpt-5.6-sol", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendUserMessage(ctx, conversation.ID, ProviderCodex, "@开发 第一件"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendDelegationResultMessage(ctx, conversation.ID, ProviderCodex, "@开发 交回结果 第一件完成"); err != nil {
		t.Fatal(err)
	}
	// 一次正常 run 的消息不属于「模型会话看不到的记录」，不能混进来。
	run := &Run{ID: "run_existing", ConversationID: conversation.ID, ProviderID: ProviderCodex}
	if err := store.AppendAssistantMessage(ctx, run, "上一轮模型回复"); err != nil {
		t.Fatal(err)
	}

	messages, err := store.ListUnattributedMessages(ctx, conversation.ID, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("unattributed messages = %d, want 2", len(messages))
	}
	if !strings.HasPrefix(messages[0].Content, "@开发 第一件") || !strings.Contains(messages[1].Content, "第一件完成") {
		t.Fatalf("messages out of order: %+v", messages)
	}
}

func TestRunPromptCarriesDelegationResultsTheSessionNeverSaw(t *testing.T) {
	runner := captureRunner{request: make(chan RunRequest, 1)}
	service := setupService(t, runner)
	ctx := context.Background()
	conversation, err := service.CreateConversation(ctx, ProviderCodex, "主线程", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.store.AppendUserMessage(ctx, conversation.ID, ProviderCodex, "@开发 检查登录流程"); err != nil {
		t.Fatal(err)
	}
	if err := service.store.AppendDelegationResultMessage(ctx, conversation.ID, ProviderCodex, "@开发 交回结果 登录流程没有阻塞问题"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRun(ctx, conversation.ID, ProviderCodex, testVerifiedModel, "根据交接结果给结论", ContextSnapshot{}); err != nil {
		t.Fatal(err)
	}
	request := <-runner.request
	for _, want := range []string{"@开发 检查登录流程", "@开发 交回结果 登录流程没有阻塞问题", "不是新的操作指令"} {
		if !strings.Contains(request.Message, want) {
			t.Fatalf("run prompt missing %q:\n%s", want, request.Message)
		}
	}
}
