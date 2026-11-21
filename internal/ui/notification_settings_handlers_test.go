package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/notify"
)

type notificationTestSecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (store *notificationTestSecrets) Configured(kind string) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.values[kind] != "", nil
}

func (store *notificationTestSecrets) Load(kind string) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.values[kind], nil
}

func (store *notificationTestSecrets) Save(kind, value string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.values[kind] = value
	return nil
}

type notificationTestSender struct{ sends int }

func (*notificationTestSender) Validate(context.Context, db.NotificationChannel, string) error {
	return nil
}

func (sender *notificationTestSender) Send(context.Context, db.NotificationChannel, string, notify.Message) error {
	sender.sends++
	return nil
}

func TestNotificationSettingsAPIStoresSecretsWithoutReturningThem(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	store := &notificationTestSecrets{values: map[string]string{}}
	sender := &notificationTestSender{}
	server.notifier = notify.NewService(server.db, store, sender)
	const secret = "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-handler-secret"

	request := httptest.NewRequest(http.MethodPut, "/api/notification-settings/feishu", strings.NewReader(`{"enabled":true,"webhook_url":"`+secret+`"}`))
	trustNotificationRequest(request)
	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "synthetic-handler-secret") || strings.Contains(response.Body.String(), "webhook_url") {
		t.Fatalf("PUT status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/notification-settings", nil)
	response = httptest.NewRecorder()
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "synthetic-handler-secret") || !strings.Contains(response.Body.String(), `"configured":true`) {
		t.Fatalf("GET status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/notification-settings/feishu/test", nil)
	trustNotificationRequest(request)
	response = httptest.NewRecorder()
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK || sender.sends != 1 {
		t.Fatalf("test status=%d sends=%d body=%s", response.Code, sender.sends, response.Body.String())
	}
	var payload struct {
		OK         bool  `json:"ok"`
		DeliveryID int64 `json:"delivery_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || !payload.OK || payload.DeliveryID <= 0 {
		t.Fatalf("payload=%+v err=%v", payload, err)
	}
}

func TestNotificationSettingsAPIRejectsUnknownFieldsAndPrivateWebhook(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	store := &notificationTestSecrets{values: map[string]string{}}
	server.notifier = notify.NewService(server.db, store, notify.NewDefaultSender(0))

	request := httptest.NewRequest(http.MethodPut, "/api/notification-settings/feishu", bytes.NewBufferString(`{"enabled":true,"unknown":true}`))
	trustNotificationRequest(request)
	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), string(notify.CodeInvalidConfig)) {
		t.Fatalf("unknown field status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPut, "/api/notification-settings/webhook", bytes.NewBufferString(`{"enabled":true,"webhook_url":"https://127.0.0.1/hook"}`))
	trustNotificationRequest(request)
	response = httptest.NewRecorder()
	server.mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), string(notify.CodeEndpointRejected)) {
		t.Fatalf("private webhook status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestNotificationReminderIntervalAPIOnlyAllowsOnceOrTwiceDaily(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	server.notifier = notify.NewService(server.db, &notificationTestSecrets{values: map[string]string{}}, &notificationTestSender{})
	for _, test := range []struct {
		hours  int
		status int
	}{{12, http.StatusOK}, {24, http.StatusOK}, {1, http.StatusBadRequest}} {
		request := httptest.NewRequest(http.MethodPut, "/api/notification-settings", strings.NewReader(`{"reminder_interval_hours":`+strconv.Itoa(test.hours)+`}`))
		trustNotificationRequest(request)
		response := httptest.NewRecorder()
		server.mux.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("hours=%d status=%d body=%s", test.hours, response.Code, response.Body.String())
		}
	}
}

func TestNotificationTestEndpointRejectsCrossSiteAndNonJSONRequests(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	sender := &notificationTestSender{}
	server.notifier = notify.NewService(server.db, &notificationTestSecrets{values: map[string]string{"feishu": "synthetic-secret"}}, sender)

	foreign := httptest.NewRequest(http.MethodPost, "/api/notification-settings/feishu/test", strings.NewReader(`{}`))
	foreign.RemoteAddr = "127.0.0.1:41000"
	foreign.Host = "127.0.0.1:17321"
	foreign.Header.Set("Origin", "https://attacker.invalid")
	foreign.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.mux.ServeHTTP(response, foreign)
	if response.Code != http.StatusForbidden || sender.sends != 0 {
		t.Fatalf("foreign status=%d sends=%d body=%s", response.Code, sender.sends, response.Body.String())
	}

	plain := httptest.NewRequest(http.MethodPost, "/api/notification-settings/feishu/test", strings.NewReader(`{}`))
	trustNotificationRequest(plain)
	plain.Header.Set("Content-Type", "text/plain")
	response = httptest.NewRecorder()
	server.mux.ServeHTTP(response, plain)
	if response.Code != http.StatusUnsupportedMediaType || sender.sends != 0 {
		t.Fatalf("plain status=%d sends=%d body=%s", response.Code, sender.sends, response.Body.String())
	}

	remote := httptest.NewRequest(http.MethodPost, "/api/notification-settings/feishu/test", strings.NewReader(`{}`))
	remote.RemoteAddr = "198.51.100.20:41000"
	remote.Host = "127.0.0.1:17321"
	remote.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	server.mux.ServeHTTP(response, remote)
	if response.Code != http.StatusForbidden || sender.sends != 0 {
		t.Fatalf("remote status=%d sends=%d body=%s", response.Code, sender.sends, response.Body.String())
	}
}

func trustNotificationRequest(request *http.Request) {
	request.RemoteAddr = "127.0.0.1:41000"
	request.Host = "127.0.0.1:17321"
	request.Header.Set("Origin", "http://127.0.0.1:17321")
	request.Header.Set("Content-Type", "application/json")
}
