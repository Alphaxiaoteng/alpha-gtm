package agenthub

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fixedClock returns a store whose "now" advances one second per call, so every
// created_at in a test is strictly ordered and reproducible.
func fixedClock(store *Store) *time.Time {
	clock := time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC)
	store.now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	return &clock
}

func TestBotListOrdersPrimaryPinnedThenGroup(t *testing.T) {
	store := testStore(t)
	fixedClock(store)
	ctx := context.Background()

	mustCreateBot := func(bot Bot) *Bot {
		t.Helper()
		created, err := store.CreateBot(ctx, bot)
		if err != nil {
			t.Fatalf("create %s: %v", bot.Name, err)
		}
		return created
	}
	// Creation order is deliberately the opposite of the expected order.
	mustCreateBot(Bot{Name: "older-ops", GroupName: "开发团队"})
	mustCreateBot(Bot{Name: "newer-ops", GroupName: "开发团队"})
	mustCreateBot(Bot{Name: "unfiled"})
	mustCreateBot(Bot{Name: "pinned-ops", GroupName: "开发团队", Pinned: true})
	mustCreateBot(Bot{Name: "boss", IsPrimary: true})

	bots, err := store.ListBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(bots))
	for _, bot := range bots {
		got = append(got, bot.Name)
	}
	// primary DESC, pinned DESC, group_name ASC ("" first), created_at DESC.
	want := []string{"boss", "pinned-ops", "unfiled", "newer-ops", "older-ops"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("bot order = %v, want %v", got, want)
	}
}

func TestBotGroupAndMutedRoundTrip(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	created, err := store.CreateBot(ctx, Bot{Name: "UI", GroupName: "  开发团队  ", Muted: true})
	if err != nil {
		t.Fatal(err)
	}
	if created.GroupName != "开发团队" || !created.Muted {
		t.Fatalf("create lost group/mute: %+v", created)
	}

	// A patch keeps the stored group unless the field is present.
	group := "新媒体团队"
	updated, err := store.UpdateBot(ctx, created.ID, BotPatch{GroupName: &group})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GroupName != "新媒体团队" || !updated.Muted {
		t.Fatalf("patch group/mute: %+v", updated)
	}
	muted := false
	updated, err = store.UpdateBot(ctx, created.ID, BotPatch{Muted: &muted})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GroupName != "新媒体团队" || updated.Muted {
		t.Fatalf("patch mute: %+v", updated)
	}
	// Clearing the group falls back to the rail's "其他" bucket.
	empty := ""
	updated, err = store.UpdateBot(ctx, created.ID, BotPatch{GroupName: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GroupName != "" {
		t.Fatalf("group = %q", updated.GroupName)
	}
	// A group name is display text, so it is length- and control-char checked.
	if _, err := store.CreateBot(ctx, Bot{Name: "B", GroupName: strings.Repeat("组", botGroupMax+1)}); !errors.Is(err, ErrBotInvalid) {
		t.Fatalf("over-long group must be rejected: %v", err)
	}
	if _, err := store.CreateBot(ctx, Bot{Name: "B", GroupName: "bad\x00group"}); !errors.Is(err, ErrBotInvalid) {
		t.Fatalf("control char in group must be rejected: %v", err)
	}
}

func TestBotRailUnreadAndLastMessage(t *testing.T) {
	store := testStore(t)
	fixedClock(store)
	ctx := context.Background()

	bot, err := store.CreateBot(ctx, Bot{Name: "UI", GroupName: "开发团队", Muted: true})
	if err != nil {
		t.Fatal(err)
	}
	unreadConversation, err := store.CreateConversation(ctx, ProviderCodex, "t", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetConversationBot(ctx, unreadConversation.ID, bot.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendKickoffMessage(ctx, unreadConversation.ID, ProviderCodex, "服务正忙。请稍后重试。"); err != nil {
		t.Fatal(err)
	}
	// A second thread of the same persona with no messages must not add unread.
	emptyConversation, err := store.CreateConversation(ctx, ProviderCodex, "空会话", "", ContextSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetConversationBot(ctx, emptyConversation.ID, bot.ID); err != nil {
		t.Fatal(err)
	}

	items, err := store.RailBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("rail items = %d", len(items))
	}
	item := items[0]
	if item.ID != bot.ID || item.GroupName != "开发团队" || !item.Muted {
		t.Fatalf("rail item lost bot fields: %+v", item)
	}
	if item.Unread != 1 {
		t.Fatalf("unread = %d, want 1", item.Unread)
	}
	if item.LastMessage != "服务正忙。请稍后重试。" || item.LastMessageAt == "" {
		t.Fatalf("last message = %q at %q", item.LastMessage, item.LastMessageAt)
	}

	// Reading the conversation clears the badge without touching updated_at.
	before, err := store.GetConversation(ctx, unreadConversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkConversationRead(ctx, unreadConversation.ID); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetConversation(ctx, unreadConversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.UpdatedAt != before.UpdatedAt {
		t.Fatalf("reading reordered the list: updated_at %q -> %q", before.UpdatedAt, after.UpdatedAt)
	}
	items, err = store.RailBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Unread != 0 {
		t.Fatalf("unread after read = %d", items[0].Unread)
	}

	// The preview is one line, capped at 80 runes.
	body := "第一行\n第二行 " + strings.Repeat("长", 120)
	if err := store.AppendKickoffMessage(ctx, emptyConversation.ID, ProviderCodex, body); err != nil {
		t.Fatal(err)
	}
	items, err = store.RailBots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	preview := items[0].LastMessage
	if strings.ContainsAny(preview, "\n\r") {
		t.Fatalf("preview kept a line break: %q", preview)
	}
	if runes := []rune(preview); len(runes) != botRailMessageMax {
		t.Fatalf("preview runes = %d, want %d (%q)", len(runes), botRailMessageMax, preview)
	}
}

func TestMarkConversationReadRejectsMissingConversation(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	if _, err := store.MarkConversationRead(ctx, "conv_missing"); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("missing conversation: %v", err)
	}
	if _, err := store.MarkConversationRead(ctx, "   "); !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("blank id: %v", err)
	}
}

func TestBotRailIsEmptyWithoutPersonas(t *testing.T) {
	store := testStore(t)
	items, err := store.RailBots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("rail = %#v", items)
	}
}
