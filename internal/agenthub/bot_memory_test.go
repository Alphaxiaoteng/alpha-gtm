package agenthub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupMemoryService(t *testing.T) *Service {
	t.Helper()
	registry, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(testStore(t), registry, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestBotMemoryAppendReadClearRoundTrip(t *testing.T) {
	service := setupMemoryService(t)
	ctx := context.Background()
	bot, err := service.CreateBot(ctx, Bot{Name: "资料管家"})
	if err != nil {
		t.Fatal(err)
	}

	memory, err := service.ReadBotMemory(ctx, bot.ID)
	if err != nil || memory != "" {
		t.Fatalf("missing memory must read as empty: %q err=%v", memory, err)
	}

	if err := service.AppendBotMemory(ctx, bot.ID, "用户偏好", "偏好短句，不要标题党"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(service.dataDir, botMemoryDirName, bot.ID+".md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != botMemoryFileMode {
		t.Fatalf("memory file mode = %v, want %v", info.Mode().Perm(), os.FileMode(botMemoryFileMode))
	}
	memory, err = service.ReadBotMemory(ctx, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(memory, "# Bot 记忆：资料管家") {
		t.Fatalf("missing bot name in header: %q", memory)
	}
	for _, section := range botMemorySections {
		if !strings.Contains(memory, "## "+section) {
			t.Fatalf("missing section %q in %q", section, memory)
		}
	}
	if !strings.Contains(memory, "- 偏好短句，不要标题党") {
		t.Fatalf("appended entry missing: %q", memory)
	}
	if strings.Count(memory, botMemoryEmptyEntry) != len(botMemorySections)-1 {
		t.Fatalf("placeholder of the target section was not removed: %q", memory)
	}

	if err := service.AppendBotMemory(ctx, bot.ID, "品牌语气", "克制的口语，禁用 yyds"); err != nil {
		t.Fatal(err)
	}
	memory, err = service.ReadBotMemory(ctx, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(memory, "- 克制的口语，禁用 yyds") || !strings.Contains(memory, "- 偏好短句，不要标题党") {
		t.Fatalf("second append lost data: %q", memory)
	}

	// Idempotent: the same sentence must not be stored twice.
	if err := service.AppendBotMemory(ctx, bot.ID, "品牌语气", "克制的口语，禁用 yyds"); err != nil {
		t.Fatal(err)
	}
	memory, _ = service.ReadBotMemory(ctx, bot.ID)
	if strings.Count(memory, "克制的口语，禁用 yyds") != 1 {
		t.Fatalf("duplicate entry stored: %q", memory)
	}

	if err := service.ClearBotMemory(ctx, bot.ID); err != nil {
		t.Fatal(err)
	}
	memory, err = service.ReadBotMemory(ctx, bot.ID)
	if err != nil || memory != "" {
		t.Fatalf("cleared memory = %q err=%v", memory, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("memory file still exists: %v", err)
	}
	if err := service.ClearBotMemory(ctx, bot.ID); err != nil {
		t.Fatalf("clearing an absent file must be a no-op: %v", err)
	}
}

func TestBotMemoryRejectsUnsafeIDAndUnknownSection(t *testing.T) {
	service := setupMemoryService(t)
	ctx := context.Background()
	bot, err := service.CreateBot(ctx, Bot{Name: "边界"})
	if err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"", "bot_../escape", "bot_a/b", "conv_1"} {
		if _, err := service.ReadBotMemory(ctx, id); err == nil {
			t.Fatalf("unsafe bot id %q was accepted", id)
		}
		if err := service.AppendBotMemory(ctx, id, "用户偏好", "x"); err == nil {
			t.Fatalf("append with unsafe bot id %q was accepted", id)
		}
	}
	if err := service.AppendBotMemory(ctx, bot.ID, "未知段落", "x"); !errors.Is(err, ErrBotMemorySection) {
		t.Fatalf("unknown section err = %v", err)
	}
}

func TestBotMemoryWriteReplacesDocument(t *testing.T) {
	service := setupMemoryService(t)
	ctx := context.Background()
	bot, err := service.CreateBot(ctx, Bot{Name: "手写"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AppendBotMemory(ctx, bot.ID, "关键事实", "公司叫阿尔法"); err != nil {
		t.Fatal(err)
	}
	replacement := "# Bot 记忆：手写\n\n## 关键事实\n- 只保留这一条\n"
	if err := service.WriteBotMemory(ctx, bot.ID, replacement); err != nil {
		t.Fatal(err)
	}
	memory, err := service.ReadBotMemory(ctx, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if memory != replacement {
		t.Fatalf("memory = %q, want %q", memory, replacement)
	}
	if err := service.WriteBotMemory(ctx, bot.ID, "   "); err != nil {
		t.Fatal(err)
	}
	if memory, _ := service.ReadBotMemory(ctx, bot.ID); memory != "" {
		t.Fatalf("empty write must clear the file: %q", memory)
	}
}

// memoryRunner captures the provider request and answers with fixed text so a
// run can prove both prompt injection and post-run extraction.
type memoryRunner struct {
	answer  string
	request chan RunRequest
}

func (runner memoryRunner) Run(_ context.Context, request RunRequest, emit func(ProviderEvent)) (string, error) {
	if runner.request != nil {
		runner.request <- request
	}
	emit(ProviderEvent{Type: "message.delta", Payload: map[string]any{"text": runner.answer}})
	return "thread_memory", nil
}

func TestRunInjectsBotMemoryAndExtractsLessons(t *testing.T) {
	requests := make(chan RunRequest, 1)
	service := setupService(t, memoryRunner{
		answer:  "以后请记住：品牌语气要克制，不要用感叹号。别的都是废话。",
		request: requests,
	})
	ctx := context.Background()
	bot, err := service.CreateBot(ctx, Bot{Name: "记忆验证", Instructions: "你是品牌编辑"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.AppendBotMemory(ctx, bot.ID, "品牌语气", "禁用词：yyds"); err != nil {
		t.Fatal(err)
	}
	conversation, err := service.CreateConversation(ctx, ProviderCodex, "t", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetConversationBot(ctx, conversation.ID, bot.ID); err != nil {
		t.Fatal(err)
	}

	run, err := service.StartRun(ctx, conversation.ID, ProviderCodex, testVerifiedModel, "聊聊语气", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if !strings.Contains(request.Message, "【Bot 记忆】") || !strings.Contains(request.Message, "禁用词：yyds") {
		t.Fatalf("bot memory missing from the provider prompt: %q", request.Message)
	}
	if strings.Index(request.Message, "你是品牌编辑") > strings.Index(request.Message, "【Bot 记忆】") {
		t.Fatalf("memory must follow the persona block: %q", request.Message)
	}
	waitForRun(t, service, run.ID, "completed")

	memory, err := service.ReadBotMemory(ctx, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(memory, "以后请记住：品牌语气要克制，不要用感叹号。") {
		t.Fatalf("assistant lesson was not extracted: %q", memory)
	}
	if strings.Contains(memory, "别的都是废话") {
		t.Fatalf("unrelated sentence must not be stored: %q", memory)
	}
	if !strings.Contains(memory, "- 禁用词：yyds") {
		t.Fatalf("pre-existing memory was lost: %q", memory)
	}
}

func TestConversationWithoutBotWritesNoMemory(t *testing.T) {
	service := setupService(t, memoryRunner{answer: "以后请记住：不要用感叹号。"})
	ctx := context.Background()
	conversation, err := service.CreateConversation(ctx, ProviderCodex, "t", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(ctx, conversation.ID, ProviderCodex, testVerifiedModel, "hi", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, service, run.ID, "completed")
	if entries, err := os.ReadDir(filepath.Join(service.dataDir, botMemoryDirName)); err == nil && len(entries) > 0 {
		t.Fatalf("memory written without a bot: %v", entries)
	}
}
