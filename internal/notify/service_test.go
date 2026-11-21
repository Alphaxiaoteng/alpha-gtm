package notify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
)

type memorySecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (store *memorySecrets) Configured(kind string) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.values[kind] != "", nil
}

func (store *memorySecrets) Load(kind string) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value := store.values[kind]
	if value == "" {
		return "", newError(CodeNotConfigured)
	}
	return value, nil
}

func (store *memorySecrets) Save(kind, value string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.values[kind] = value
	return nil
}

type recordingSender struct {
	mu       sync.Mutex
	messages []Message
	fail     bool
}

func (*recordingSender) Validate(context.Context, db.NotificationChannel, string) error { return nil }

func (sender *recordingSender) Send(_ context.Context, _ db.NotificationChannel, _ string, message Message) error {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	sender.messages = append(sender.messages, message)
	if sender.fail {
		return newError(CodeRequestFailed)
	}
	return nil
}

func (sender *recordingSender) count() int {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return len(sender.messages)
}

func (sender *recordingSender) snapshot() []Message {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return append([]Message(nil), sender.messages...)
}

func testNotificationDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	return database
}

func testEmailChannel() db.NotificationChannel {
	return db.NotificationChannel{
		Kind: "email", SMTPHost: "smtp.example.test", SMTPPort: 465, SMTPSecurity: "tls",
		SMTPUsername: "sender@example.test", EmailFrom: "sender@example.test", EmailTo: []string{"receiver@example.test"},
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met")
}

func startService(t *testing.T, service *Service) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var waitGroup sync.WaitGroup
	service.Start(ctx, &waitGroup)
	return func() {
		cancel()
		waitGroup.Wait()
	}
}

func TestSettingsNeverReturnStoredWebhookSecret(t *testing.T) {
	database := testNotificationDB(t)
	store := &memorySecrets{values: map[string]string{}}
	service := NewService(database, store, &recordingSender{})
	secret := "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-secret-value"
	status, err := service.UpdateChannel(context.Background(), "feishu", ChannelUpdate{Enabled: true, WebhookURL: secret})
	if err != nil || !status.Enabled || !status.Configured {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	settings, err := service.Settings()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(settings)
	if strings.Contains(string(encoded), "synthetic-secret-value") || strings.Contains(string(encoded), "webhook_url") {
		t.Fatalf("settings leaked webhook secret: %s", encoded)
	}
	for _, name := range []string{"app.db", "app.db-wal", "app.db-shm"} {
		payload, err := os.ReadFile(filepath.Join(database.DataDir, name))
		if err == nil && strings.Contains(string(payload), "synthetic-secret-value") {
			t.Fatalf("webhook secret leaked into SQLite file %s", name)
		}
	}
}

func TestOfflineReminderRateLimitsFailedAttemptsAcrossRestart(t *testing.T) {
	database := testNotificationDB(t)
	account, err := database.CreateAccount("kuaishou", "快手测试账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET login_state='logged_out' WHERE id=?`, account.ID); err != nil {
		t.Fatal(err)
	}
	channel, err := database.GetNotificationChannel("feishu")
	if err != nil {
		t.Fatal(err)
	}
	channel.Enabled = true
	if _, err := database.SaveNotificationChannel(*channel); err != nil {
		t.Fatal(err)
	}
	store := &memorySecrets{values: map[string]string{"feishu": "synthetic-secret"}}
	sender := &recordingSender{fail: true}

	first := NewService(database, store, sender)
	stopFirst := startService(t, first)
	first.enqueueOfflineReminders()
	waitFor(t, func() bool { return sender.count() == 1 })
	stopFirst()

	second := NewService(database, store, sender)
	stopSecond := startService(t, second)
	second.enqueueOfflineReminders()
	time.Sleep(100 * time.Millisecond)
	stopSecond()
	if sender.count() != 1 {
		t.Fatalf("failed reminder retried immediately; sends=%d", sender.count())
	}
	old := time.Now().UTC().Add(-13 * time.Hour).Format(time.RFC3339)
	if _, err := database.Exec(`UPDATE notification_deliveries SET created_at=? WHERE account_id=?`, old, account.ID); err != nil {
		t.Fatal(err)
	}
	third := NewService(database, store, sender)
	stopThird := startService(t, third)
	third.enqueueOfflineReminders()
	waitFor(t, func() bool { return sender.count() == 2 })
	stopThird()
	var failed int
	if err := database.QueryRow(`SELECT COUNT(*) FROM notification_deliveries WHERE account_id=? AND status='failed'`, account.ID).Scan(&failed); err != nil || failed != 2 {
		t.Fatalf("failed deliveries=%d err=%v", failed, err)
	}
	items, err := database.ListNotifications(10, false)
	if err != nil || len(items) != 0 {
		t.Fatalf("reminders should not create local inbox noise: items=%+v err=%v", items, err)
	}
}

func TestAccountTransitionsCreateHumanLocalNotificationsAndExternalMessages(t *testing.T) {
	database := testNotificationDB(t)
	account, err := database.CreateAccount("kuaishou", "快手测试账号", "")
	if err != nil {
		t.Fatal(err)
	}
	channel, err := database.GetNotificationChannel("feishu")
	if err != nil {
		t.Fatal(err)
	}
	channel.Enabled = true
	if _, err := database.SaveNotificationChannel(*channel); err != nil {
		t.Fatal(err)
	}
	sender := &recordingSender{}
	service := NewService(database, &memorySecrets{values: map[string]string{"feishu": "synthetic-secret"}}, sender)
	stop := startService(t, service)
	defer stop()
	if err := service.NotifyAccountChange(AccountStateChange{
		AccountID: account.ID, PlatformCode: "kuaishou", AccountName: "快手测试账号",
		Previous: db.AccountAuthLoggedIn, Current: db.AccountAuthLoggedOut, ReasonCode: db.LoginReasonSessionExpiredPage,
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		items, _ := database.ListNotifications(10, false)
		return len(items) == 1 && sender.count() == 1
	})
	if err := service.NotifyAccountChange(AccountStateChange{
		AccountID: account.ID, PlatformCode: "kuaishou", AccountName: "快手测试账号",
		Previous: db.AccountAuthLoggedOut, Current: db.AccountAuthLoggedIn,
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		items, _ := database.ListNotifications(10, false)
		return len(items) == 2 && sender.count() == 2
	})
	items, err := database.ListNotifications(10, false)
	if err != nil {
		t.Fatal(err)
	}
	joined := items[0].Title + items[0].Body + items[1].Title + items[1].Body
	for _, forbidden := range []string{"SESSION_EXPIRED_PAGE", "CDP", "Cookie", "profile", "secret"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("local notification leaked technical detail %q: %s", forbidden, joined)
		}
	}
	if !strings.Contains(joined, "账号需要重新登录") || !strings.Contains(joined, "账号已恢复登录") {
		t.Fatalf("notifications are not human-readable: %s", joined)
	}
	messages := sender.snapshot()
	if len(messages) != 2 || messages[0].EventType != "account_offline" || messages[1].EventType != "account_recovered" {
		t.Fatalf("external transition messages=%+v", messages)
	}
	_ = service.NotifyAccountChange(AccountStateChange{AccountID: account.ID, Previous: db.AccountAuthUnknown, Current: db.AccountAuthLoggedOut})
	_ = service.NotifyAccountChange(AccountStateChange{AccountID: account.ID, Previous: db.AccountAuthLoggedOut, Current: db.AccountAuthLoggedOut})
	time.Sleep(50 * time.Millisecond)
	if sender.count() != 2 {
		t.Fatalf("non-transition state sent external notification; sends=%d", sender.count())
	}
}

func TestServiceStartDoesNotSendStaleOfflineStateBeforeObserverRuns(t *testing.T) {
	database := testNotificationDB(t)
	account, err := database.CreateAccount("kuaishou", "陈旧离线状态", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET login_state='logged_out' WHERE id=?`, account.ID); err != nil {
		t.Fatal(err)
	}
	channel, err := database.GetNotificationChannel("feishu")
	if err != nil {
		t.Fatal(err)
	}
	channel.Enabled = true
	if _, err := database.SaveNotificationChannel(*channel); err != nil {
		t.Fatal(err)
	}
	sender := &recordingSender{}
	service := NewService(database, &memorySecrets{values: map[string]string{"feishu": "synthetic-secret"}}, sender)
	stop := startService(t, service)
	time.Sleep(100 * time.Millisecond)
	stop()
	if sender.count() != 0 {
		t.Fatalf("stale startup state sent before observation; sends=%d", sender.count())
	}
}

func TestEnablingChannelExplicitlyWakesCurrentOfflineReminder(t *testing.T) {
	database := testNotificationDB(t)
	account, err := database.CreateAccount("kuaishou", "已确认离线", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE accounts SET login_state='logged_out' WHERE id=?`, account.ID); err != nil {
		t.Fatal(err)
	}
	store := &memorySecrets{values: map[string]string{}}
	sender := &recordingSender{}
	service := NewService(database, store, sender)
	stop := startService(t, service)
	defer stop()
	if _, err := service.UpdateChannel(context.Background(), "feishu", ChannelUpdate{Enabled: true, WebhookURL: "synthetic-secret"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return sender.count() == 1 })
}

func TestTestChannelPersistsProviderFailure(t *testing.T) {
	database := testNotificationDB(t)
	store := &memorySecrets{values: map[string]string{"feishu": "synthetic-secret"}}
	service := NewService(database, store, &recordingSender{fail: true})
	id, err := service.TestChannel(context.Background(), "feishu")
	if CodeOf(err) != CodeRequestFailed || id <= 0 {
		t.Fatalf("id=%d code=%s", id, CodeOf(err))
	}
	delivery, err := database.GetNotificationDelivery(id)
	if err != nil || delivery.Status != "failed" || delivery.ErrorCode != string(CodeRequestFailed) {
		t.Fatalf("delivery=%+v err=%v", delivery, err)
	}
}

func TestNormalizeSecretRejectsControlCharacters(t *testing.T) {
	for _, value := range []string{"", "line\nbreak", strings.Repeat("x", 4097)} {
		if _, err := normalizeSecret("feishu", value); !errors.Is(err, codedError{code: CodeInvalidConfig}) && CodeOf(err) != CodeInvalidConfig {
			t.Fatalf("value was accepted: length=%d code=%s", len(value), CodeOf(err))
		}
	}
}
