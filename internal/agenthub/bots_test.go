package agenthub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	return NewStore(database)
}

func TestBotCRUDRoundTrip(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	created, err := store.CreateBot(ctx, Bot{
		Name:         "  资料管家  ",
		Role:         "研究助理",
		Instructions: "只回答被证据支持的内容。",
		Model:        "codex/gpt-5.6-sol",
		Avatar:       "📚",
		Color:        "#4F7CFF",
		Effort:       "high",
		Kickoff:      "读一下这份资料",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "资料管家" {
		t.Fatalf("unexpected created bot: %+v", created)
	}
	if len(created.Capabilities) != 0 || created.Capabilities == nil {
		t.Fatalf("capabilities must default to an empty array: %+v", created.Capabilities)
	}

	record, err := store.GetBot(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Instructions != "只回答被证据支持的内容。" || record.Model != "codex/gpt-5.6-sol" {
		t.Fatalf("round trip lost fields: %+v", record)
	}

	bots, err := store.ListBots(ctx)
	if err != nil || len(bots) != 1 {
		t.Fatalf("list bots=%v err=%v", bots, err)
	}

	role := "内容策划"
	capabilities := []string{"wechat.qa"}
	updated, err := store.UpdateBot(ctx, created.ID, BotPatch{Role: &role, Capabilities: &capabilities})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Role != role || len(updated.Capabilities) != 1 || updated.Capabilities[0] != "wechat.qa" {
		t.Fatalf("partial update lost fields: %+v", updated)
	}
	if updated.Instructions != record.Instructions || updated.Name != record.Name {
		t.Fatalf("partial update overwrote untouched fields: %+v", updated)
	}

	if err := store.DeleteBot(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetBot(ctx, created.ID); !errors.Is(err, ErrBotNotFound) {
		t.Fatalf("deleted bot still resolves: %v", err)
	}
	if err := store.DeleteBot(ctx, created.ID); !errors.Is(err, ErrBotNotFound) {
		t.Fatalf("second delete must report not found: %v", err)
	}
}

func TestBotRejectsInvalidFields(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	cases := map[string]Bot{
		"name required":         {Name: "   "},
		"model not slash":       {Name: "A", Model: "gpt-5.6-sol"},
		"model bad chars":       {Name: "A", Model: "codex/gpt 5"},
		"model two parts":       {Name: "A", Model: "codex/openrouter/gpt"},
		"role with path":        {Name: "A", Role: "/Users/albert/secret"},
		"avatar token":          {Name: "A", Avatar: "token"},
		"capability junk":       {Name: "A", Capabilities: []string{"not a token"}},
		"instructions too long": {Name: "A", Instructions: strings.Repeat("x", botInstructionsMax+1)},
	}
	for name, bot := range cases {
		if _, err := store.CreateBot(ctx, bot); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}

	if _, err := store.CreateBot(ctx, Bot{Name: "A", Model: "codex/gpt-5.6-sol"}); err != nil {
		t.Fatalf("valid provider/model was rejected: %v", err)
	}
}

func TestConversationWithoutBotKeepsLegacyShape(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	conversation, err := store.CreateConversation(ctx, ProviderCodex, "老会话", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.BotID != "" {
		t.Fatalf("legacy conversation gained a bot id: %q", record.BotID)
	}

	// A row written before the column existed (INSERT omits bot_id) must read back.
	if _, err := store.db.ExecContext(ctx, `INSERT INTO agent_conversations(id,title,provider_id,model_id,workspace_mode,context_json,created_at,updated_at)
		VALUES('conv_legacy','旧行','codex','','media','{}','2024-01-01T00:00:00Z','2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.GetConversation(ctx, "conv_legacy")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.BotID != "" {
		t.Fatalf("old row bot id = %q", legacy.BotID)
	}

	// Old JSON without bot_id must still decode, and an empty value must not be
	// written back into new JSON.
	var decoded Conversation
	if err := json.Unmarshal([]byte(`{"id":"conv_x","title":"t","provider_id":"codex","workspace_mode":"media","context":{}}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.BotID != "" {
		t.Fatalf("legacy JSON decoded bot id %q", decoded.BotID)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "bot_id") {
		t.Fatalf("empty bot id leaked into JSON: %s", encoded)
	}
}

func TestDeleteBotDetachesConversations(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	bot, err := store.CreateBot(ctx, Bot{Name: "人格", Instructions: "简短回答"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(ctx, ProviderCodex, "有 bot 的会话", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := store.SetConversationBot(ctx, conversation.ID, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bound.BotID != bot.ID {
		t.Fatalf("bind failed: %+v", bound)
	}

	if err := store.DeleteBot(ctx, bot.ID); err != nil {
		t.Fatal(err)
	}
	detached, err := store.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("conversation was deleted with the bot: %v", err)
	}
	if detached.BotID != "" {
		t.Fatalf("conversation still references deleted bot: %q", detached.BotID)
	}
	if _, err := store.SetConversationBot(ctx, conversation.ID, "bot_missing"); !errors.Is(err, ErrBotNotFound) {
		t.Fatalf("binding a missing bot must fail: %v", err)
	}
}

func TestStartRunInjectsBotInstructions(t *testing.T) {
	requests := make(chan RunRequest, 1)
	service := setupService(t, captureRunner{request: requests})
	ctx := context.Background()

	bot, err := service.CreateBot(ctx, Bot{Name: "资料管家", Role: "研究助理", Instructions: "只能引用带 [KB:id] 的证据。"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := service.CreateConversation(ctx, ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetConversationBot(ctx, conversation.ID, bot.ID); err != nil {
		t.Fatal(err)
	}

	run, err := service.StartRunWithPromptContext(ctx, conversation.ID, ProviderCodex, testVerifiedModel, "你好", ContextSnapshot{}, "证据包正文")
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if !strings.Contains(request.Instructions, "只能引用带 [KB:id] 的证据。") {
		t.Fatalf("run request instructions missing bot persona: %q", request.Instructions)
	}
	if !strings.Contains(request.Message, "只能引用带 [KB:id] 的证据。") {
		t.Fatalf("provider message missing bot persona: %q", request.Message)
	}
	if !strings.Contains(request.Message, "证据包正文") {
		t.Fatalf("provider message lost the evidence packet: %q", request.Message)
	}
	if strings.Index(request.Message, "只能引用") > strings.Index(request.Message, "证据包正文") {
		t.Fatalf("persona must precede evidence: %q", request.Message)
	}
	waitForRun(t, service, run.ID, "completed")
}

func TestStartRunWithoutBotAddsNoPersona(t *testing.T) {
	requests := make(chan RunRequest, 1)
	service := setupService(t, captureRunner{request: requests})
	ctx := context.Background()

	conversation, err := service.CreateConversation(ctx, ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(ctx, conversation.ID, ProviderCodex, testVerifiedModel, "你好", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	request := <-requests
	if request.Instructions != "" || request.Message != "你好" {
		t.Fatalf("bot-less run received a persona: %+v", request)
	}
	waitForRun(t, service, run.ID, "completed")
}

func TestBotInstructionsSurviveDeletedBotAtRunTime(t *testing.T) {
	requests := make(chan RunRequest, 1)
	service := setupService(t, captureRunner{request: requests})
	ctx := context.Background()

	bot, err := service.CreateBot(ctx, Bot{Name: "临时人格", Instructions: "旧人格指令"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := service.CreateConversation(ctx, ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetConversationBot(ctx, conversation.ID, bot.ID); err != nil {
		t.Fatal(err)
	}
	// Simulate a stale bot_id whose roster row is already gone.
	if _, err := service.store.db.ExecContext(ctx, `DELETE FROM agent_bots WHERE id=?`, bot.ID); err != nil {
		t.Fatal(err)
	}
	run, err := service.StartRun(ctx, conversation.ID, ProviderCodex, testVerifiedModel, "你好", ContextSnapshot{})
	if err != nil {
		t.Fatalf("stale bot id must not block the run: %v", err)
	}
	request := <-requests
	if request.Instructions != "" || strings.Contains(request.Message, "旧人格指令") {
		t.Fatalf("deleted bot instructions leaked into the run: %+v", request)
	}
	waitForRun(t, service, run.ID, "completed")
}

// Generated avatars are stored as /api/ai-image/assets/<sha256>.<ext>. That
// string starts with "/" but is not a local path, so it needs its own rule
// instead of the generic path guard.
func TestBotAvatarAcceptsManagedAssetURL(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	hex := strings.Repeat("a", 64)
	managed := "/api/ai-image/assets/" + hex + ".png"

	bot, err := store.CreateBot(ctx, Bot{Name: "A", Avatar: managed})
	if err != nil {
		t.Fatalf("managed avatar rejected: %v", err)
	}
	if bot.Avatar != managed {
		t.Fatalf("avatar round-trip = %q", bot.Avatar)
	}

	for name, avatar := range map[string]string{
		"traversal":        "/api/ai-image/assets/../etc/passwd",
		"short hex":        "/api/ai-image/assets/abc.png",
		"bad extension":    "/api/ai-image/assets/" + hex + ".exe",
		"absolute path":    "/Users/albert/avatar.png",
		"local file URL":   "file:///Users/albert/avatar.png",
		"nested asset":     "/api/ai-image/assets/sub/" + hex + ".png",
		"long glyph label": strings.Repeat("x", 33),
	} {
		if _, err := store.CreateBot(ctx, Bot{Name: "B", Avatar: avatar}); err == nil {
			t.Fatalf("%s: expected rejection for %q", name, avatar)
		}
	}
}

// The bundled chibi avatar set is served statically; its path must survive the
// roster guard without opening a general path escape hatch.
func TestBotAvatarAcceptsBundledPreset(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	preset := "/bot-avatars/sakura.svg"

	bot, err := store.CreateBot(ctx, Bot{Name: "P", Avatar: preset})
	if err != nil {
		t.Fatalf("bundled preset rejected: %v", err)
	}
	if bot.Avatar != preset {
		t.Fatalf("preset round-trip = %q", bot.Avatar)
	}

	for name, avatar := range map[string]string{
		"traversal":           "/bot-avatars/../etc/passwd",
		"uppercase":           "/bot-avatars/Sakura.svg",
		"space":               "/bot-avatars/a b.svg",
		"wrong ext":           "/bot-avatars/sakura.png",
		"nested":              "/bot-avatars/sub/sakura.svg",
		"other folder":        "/avatars/sakura.svg",
		"relative traversal":  "../../etc/passwd",
		"plain relative path": "assets/avatar.png",
	} {
		if _, err := store.CreateBot(ctx, Bot{Name: "Q", Avatar: avatar}); err == nil {
			t.Fatalf("%s: expected rejection for %q", name, avatar)
		}
	}
}

func TestBotProviderRoutesPinnedModel(t *testing.T) {
	cases := map[string]ProviderID{
		"agy/gemini-3.8-flash":         ProviderDsh,
		"openainext/grok-4.7":          ProviderDsh,
		"alpha-nexus-llm/grok-4.7":     ProviderCustom,
		"":                             DefaultProvider,
		"plain-model-without-provider": DefaultProvider,
	}
	for model, want := range cases {
		if got := BotProvider(model); got != want {
			t.Fatalf("BotProvider(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestListBotsNewestFirst(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if _, err := store.CreateBot(ctx, Bot{Name: "旧"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	newest, err := store.CreateBot(ctx, Bot{Name: "新"})
	if err != nil {
		t.Fatal(err)
	}
	bots, err := store.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(bots) != 2 || bots[0].ID != newest.ID {
		t.Fatalf("list order = %v, want newest first", []string{bots[0].Name, bots[1].Name})
	}
}

// The pin/primary columns are new, so they must survive a write, a read and a
// partial update that does not mention them.
func TestBotPinAndPrimaryRoundTrip(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	created, err := store.CreateBot(ctx, Bot{Name: "置顶的", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if !created.Pinned || created.IsPrimary {
		t.Fatalf("created flags = pinned:%v primary:%v", created.Pinned, created.IsPrimary)
	}
	record, err := store.GetBot(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !record.Pinned || record.IsPrimary {
		t.Fatalf("read back flags = pinned:%v primary:%v", record.Pinned, record.IsPrimary)
	}

	unpinned := false
	updated, err := store.UpdateBot(ctx, created.ID, BotPatch{Pinned: &unpinned})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Pinned {
		t.Fatal("pin patch was not applied")
	}

	promoted, err := store.SetPrimaryBot(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !promoted.IsPrimary || promoted.Name != "置顶的" {
		t.Fatalf("set primary = %+v", promoted)
	}
}

func TestSetPrimaryBotDemotesThePreviousOne(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	first, err := store.CreateBot(ctx, Bot{Name: "一号"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateBot(ctx, Bot{Name: "二号"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPrimaryBot(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPrimaryBot(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	var primaries int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_bots WHERE is_primary<>0`).Scan(&primaries); err != nil {
		t.Fatal(err)
	}
	if primaries != 1 {
		t.Fatalf("primary count = %d, want exactly 1", primaries)
	}
	stored, err := store.GetBot(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.IsPrimary {
		t.Fatal("previous primary kept the flag")
	}
	if _, err := store.SetPrimaryBot(ctx, "bot_missing"); !errors.Is(err, ErrBotNotFound) {
		t.Fatalf("missing bot primary = %v", err)
	}
}

// Ordering contract: primary first, then pinned, then newest. The rail relies
// on this so a marked persona is never buried.
func TestListBotsOrdersPrimaryThenPinnedThenNewest(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	oldest, err := store.CreateBot(ctx, Bot{Name: "最旧"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	pinnedBot, err := store.CreateBot(ctx, Bot{Name: "置顶", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	primaryBot, err := store.CreateBot(ctx, Bot{Name: "主控"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPrimaryBot(ctx, primaryBot.ID); err != nil {
		t.Fatal(err)
	}

	bots, err := store.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(bots))
	for _, bot := range bots {
		got = append(got, bot.ID)
	}
	want := []string{primaryBot.ID, pinnedBot.ID, oldest.ID}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("list order = %v, want %v", got, want)
	}
}

// A new conversation for a persona opens with its kickoff line, and the insert
// is idempotent so a retried creation cannot duplicate it.
func TestKickoffMessageSeededOnce(t *testing.T) {
	service := setupService(t, captureRunner{})
	ctx := context.Background()

	bot, err := service.CreateBot(ctx, Bot{Name: "选题助手", Kickoff: "今天想聊什么选题？"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := service.CreateConversation(ctx, ProviderCodex, bot.Name, testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetConversationBot(ctx, conversation.ID, bot.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.SeedKickoffMessage(ctx, conversation.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := service.store.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Messages) != 1 {
		t.Fatalf("messages = %d, want the kickoff only", len(stored.Messages))
	}
	if stored.Messages[0].Role != "assistant" || stored.Messages[0].Content != bot.Kickoff {
		t.Fatalf("first message = %+v", stored.Messages[0])
	}

	// Second call is a no-op: the greeting stays single.
	if err := service.SeedKickoffMessage(ctx, conversation.ID); err != nil {
		t.Fatal(err)
	}
	stored, err = service.store.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Messages) != 1 {
		t.Fatalf("kickoff duplicated: %d messages", len(stored.Messages))
	}
}

// A user message that already exists must not be displaced by a late kickoff.
func TestKickoffMessageDoesNotPrependToUsedConversation(t *testing.T) {
	service := setupService(t, captureRunner{})
	ctx := context.Background()

	bot, err := service.CreateBot(ctx, Bot{Name: "欢迎语", Kickoff: "你好呀"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := service.CreateConversation(ctx, ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetConversationBot(ctx, conversation.ID, bot.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartRun(ctx, conversation.ID, ProviderCodex, testVerifiedModel, "先说一句", ContextSnapshot{}); err != nil {
		t.Fatal(err)
	}
	if err := service.SeedKickoffMessage(ctx, conversation.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := service.store.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range stored.Messages {
		if message.Content == bot.Kickoff {
			t.Fatalf("kickoff injected into a used conversation: %+v", stored.Messages)
		}
	}
}

// A persona without a kickoff must not add an empty message.
func TestKickoffMessageSkippedWhenUnset(t *testing.T) {
	service := setupService(t, captureRunner{})
	ctx := context.Background()

	bot, err := service.CreateBot(ctx, Bot{Name: "无声"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := service.CreateConversation(ctx, ProviderCodex, "", testVerifiedModel, ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetConversationBot(ctx, conversation.ID, bot.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.SeedKickoffMessage(ctx, conversation.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := service.store.GetConversation(ctx, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Messages) != 0 {
		t.Fatalf("kickoff-less bot wrote messages: %+v", stored.Messages)
	}
}

func TestBotRejectsControlCharacters(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	cases := map[string]Bot{
		"name nul":         {Name: "a\x00b"},
		"role nul":         {Name: "A", Role: "a\x00b"},
		"instructions nul": {Name: "A", Instructions: "a\x00b"},
		"avatar nul":       {Name: "A", Avatar: "\x00"},
		"kickoff nul":      {Name: "A", Kickoff: "a\x00b"},
		"name newline":     {Name: "a\nb"},
		"name del":         {Name: "a\x7fb"},
	}
	for label, bot := range cases {
		if _, err := store.CreateBot(ctx, bot); err == nil {
			t.Fatalf("%s: expected rejection", label)
		}
	}
	// Multi-line instructions keep newlines and tabs.
	if _, err := store.CreateBot(ctx, Bot{Name: "多行", Instructions: "第一行\n第二行\t带制表"}); err != nil {
		t.Fatalf("multi-line instructions rejected: %v", err)
	}
}

func TestListConversationsIsCapped(t *testing.T) {
	store := testStore(t)
	// The cap exists so an old install cannot scan unbounded history; verify the
	// query stays valid with the LIMIT clause in place.
	if _, err := store.ListConversations(context.Background()); err != nil {
		t.Fatalf("list conversations: %v", err)
	}
}

func TestBotAvatarAcceptsPlatformLogo(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	logo := "/platforms/xhs.png"
	bot, err := store.CreateBot(ctx, Bot{Name: "小红书博主", Avatar: logo})
	if err != nil {
		t.Fatalf("platform logo rejected: %v", err)
	}
	if bot.Avatar != logo {
		t.Fatalf("avatar = %q", bot.Avatar)
	}
	for _, bad := range []string{
		"/platforms/../etc/passwd.png",
		"/platforms/XHS.png",
		"/platforms/xhs.exe",
		"/platforms/sub/xhs.png",
	} {
		if _, err := store.CreateBot(ctx, Bot{Name: "B", Avatar: bad}); err == nil {
			t.Fatalf("expected rejection for %q", bad)
		}
	}
}

// 打开 Bot 只会打开它最新的那条会话，所以角标也只能数那条会话的未读。数所有历史
// 会话会造出一个「怎么点都清不掉」的蓝点。

// insertUnreadMessage writes a row the way a provider run would: it does not
// touch last_read_at, so the rail can legitimately report it as unread.
func insertUnreadMessage(t *testing.T, store *Store, conversationID, role, content string) {
	t.Helper()
	now := nowText(store.now())
	if _, err := store.db.ExecContext(context.Background(), `INSERT INTO agent_messages(id,conversation_id,provider_id,run_id,role,content,context_json,created_at) VALUES(?,?,?,'',?,?,'{}',?)`,
		newID("msg"), conversationID, ProviderCodex, role, content, now); err != nil {
		t.Fatal(err)
	}
}

func TestRailUnreadTracksTheThreadTheRowOpens(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	bot, err := store.CreateBot(ctx, Bot{Name: "开发"})
	if err != nil {
		t.Fatal(err)
	}

	older, err := store.CreateConversation(ctx, ProviderCodex, "旧线程", "codex/gpt-5.6-sol", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetConversationBot(ctx, older.ID, bot.ID); err != nil {
		t.Fatal(err)
	}
	insertUnreadMessage(t, store, older.ID, "user", "旧线程的未读消息")

	latest, err := store.CreateConversation(ctx, ProviderCodex, "最新线程", "codex/gpt-5.6-sol", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetConversationBot(ctx, latest.ID, bot.ID); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"最新线程第一条", "最新线程第二条"} {
		insertUnreadMessage(t, store, latest.ID, "user", text)
	}

	items, err := store.RailBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var unread int
	for _, item := range items {
		if item.ID == bot.ID {
			unread = item.Unread
		}
	}
	if unread != 2 {
		t.Fatalf("rail unread=%d, want 2 (only the opened thread)", unread)
	}

	// 打开之后清掉水印，角标必须归零——旧线程的未读不能把它顶回来。
	if _, err := store.MarkConversationRead(ctx, latest.ID); err != nil {
		t.Fatal(err)
	}
	items, err = store.RailBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == bot.ID && item.Unread != 0 {
			t.Fatalf("after opening the bot, rail unread=%d, want 0", item.Unread)
		}
	}
}

func TestRailMainAgentUnreadTracksItsLatestThread(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	older, err := store.CreateConversation(ctx, ProviderCodex, "旧主线程", "codex/gpt-5.6-sol", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	insertUnreadMessage(t, store, older.ID, "assistant", "旧主线程的未读回复")
	latest, err := store.CreateConversation(ctx, ProviderCodex, "最新主线程", "codex/gpt-5.6-sol", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	insertUnreadMessage(t, store, latest.ID, "assistant", "最新主线程的未读回复")

	rail, err := store.RailMainAgent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rail.Unread != 1 {
		t.Fatalf("main agent unread=%d, want 1 (latest thread only)", rail.Unread)
	}
	if _, err := store.MarkConversationRead(ctx, latest.ID); err != nil {
		t.Fatal(err)
	}
	rail, err = store.RailMainAgent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rail.Unread != 0 {
		t.Fatalf("after opening, main agent unread=%d, want 0", rail.Unread)
	}
}

func TestDelegationWritesDoNotRaiseRailUnread(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	bot, err := store.CreateBot(ctx, Bot{Name: "开发"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(ctx, ProviderCodex, "开发", "codex/gpt-5.6-sol", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetConversationBot(ctx, conversation.ID, bot.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendUserMessage(ctx, conversation.ID, ProviderCodex, "@开发 检查登录流程"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendDelegationResultMessage(ctx, conversation.ID, ProviderCodex, "@开发 交回结果 没有问题"); err != nil {
		t.Fatal(err)
	}
	items, err := store.RailBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == bot.ID && item.Unread != 0 {
			t.Fatalf("delegation writes raised unread=%d; they are already visible to the user", item.Unread)
		}
	}
}

func TestRailPreviewStripsMarkdownSyntax(t *testing.T) {
	cases := map[string]string{
		"**结论：优先补「可见性」和「可写入」两类缺口。**":             "结论：优先补「可见性」和「可写入」两类缺口。",
		"## 抓取任务模板\n\n**1. 输入清单字段**":             "抓取任务模板 1. 输入清单字段",
		"- 第一项\n- 第二项":                           "第一项 第二项",
		"> 引用一句\n正文":                             "引用一句 正文",
		"看这个 [链接](https://example.com) 和 `code`": "看这个 链接 和 code",
		"| # | Bot |\n|---|---|\n| 1 | 开发 |":     "# Bot 1 开发",
		// snake_case 不能被当成强调符号吃掉。
		"看 file_name 与 snake_case": "看 file_name 与 snake_case",
		"**粗体** 与 `code`":          "粗体 与 code",
	}
	for input, want := range cases {
		if got := truncateRailMessage(input); got != want {
			t.Fatalf("truncateRailMessage(%q) = %q, want %q", input, got, want)
		}
	}
}

// insertRunMessage writes a message owned by a provider run: unlike the
// system-written rows, its unread state must never be cleared by delegation.
func insertRunMessage(t *testing.T, store *Store, conversationID, role, content, runID string) {
	t.Helper()
	now := nowText(store.now())
	if _, err := store.db.ExecContext(context.Background(), `INSERT INTO agent_messages(id,conversation_id,provider_id,run_id,role,content,context_json,created_at) VALUES(?,?,?,?,?,?,'{}',?)`,
		newID("msg"), conversationID, ProviderCodex, runID, role, content, now); err != nil {
		t.Fatal(err)
	}
}

func TestDelegationWriteKeepsRealUnread(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	bot, err := store.CreateBot(ctx, Bot{Name: "开发"})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.CreateConversation(ctx, ProviderCodex, "开发", "codex/gpt-5.6-sol", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetConversationBot(ctx, conversation.ID, bot.ID); err != nil {
		t.Fatal(err)
	}
	// 用户还没打开的一条真实模型回复。
	insertRunMessage(t, store, conversation.ID, "assistant", "真正的未读回复", "run_real")

	if err := store.AppendDelegationResultMessage(ctx, conversation.ID, ProviderCodex, "@开发 交回结果 系统写入"); err != nil {
		t.Fatal(err)
	}
	items, err := store.RailBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == bot.ID {
			if item.Unread == 0 {
				t.Fatal("delegation write cleared a real unread message")
			}
			return
		}
	}
	t.Fatal("bot missing from rail")
}

// 内置主 Agent 是产品固定入口，删掉会让 /bot 顶部大卡失去落点，
// 下次启动又被 EnsureBuiltinAlphaAgent 插回，期间的会话被解绑。
func TestDeleteBotRefusesBuiltinAgent(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	store := NewStore(database)
	ctx := context.Background()
	if err := store.EnsureBuiltinAlphaAgent(ctx); err != nil {
		t.Fatalf("ensure builtin: %v", err)
	}
	if err := store.DeleteBot(ctx, BuiltinAlphaAgentID); !errors.Is(err, ErrBuiltinBotImmutable) {
		t.Fatalf("err = %v, want ErrBuiltinBotImmutable", err)
	}
	if _, err := store.GetBot(ctx, BuiltinAlphaAgentID); err != nil {
		t.Errorf("builtin bot must still exist: %v", err)
	}
}
