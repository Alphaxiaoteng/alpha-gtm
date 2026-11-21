package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestPublishTargetNotificationsUseAllowedKindsAndDeduplicate(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	server := &Server{db: database}
	targets := []egoPublishTargetResult{
		{Platform: "douyin", Status: "failed", ErrorCode: "LOGIN_REQUIRED", JobID: 11},
		{Platform: "facebook", Status: "failed", ErrorCode: "EGO_META_SUBMIT_FAILED", JobID: 12},
		{Platform: "youtube", Status: "result_unknown", ErrorCode: "EGO_SUBMIT_RESULT_UNKNOWN", JobID: 13},
		{Platform: "csdn", Status: "published_verified", JobID: 14},
	}
	for _, target := range targets {
		server.notifyPublishPlanTarget(7, target)
		server.notifyPublishPlanTarget(7, target)
	}
	items, err := database.ListNotifications(20, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("notifications=%+v, want four deduplicated events", items)
	}
	want := map[string]bool{
		"publish.login_required": false,
		"publish.failed":         false,
		"publish.result_unknown": false,
		"publish.completed":      false,
	}
	for _, item := range items {
		if _, ok := want[item.Type]; !ok || item.RelatedType != "publish_job" || item.RelatedID <= 0 {
			t.Fatalf("unexpected notification=%+v", item)
		}
		want[item.Type] = true
	}
	for kind, observed := range want {
		if !observed {
			t.Fatalf("missing %s", kind)
		}
	}
}

func TestNotificationByIDReturnsOnlyPersistedContent(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	item, err := database.CreateNotification("publish.failed", "error", "平台异常", "douyin", "publish_job", 21)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database}
	request := httptest.NewRequest(http.MethodGet, "/api/notifications/1", nil)
	request.SetPathValue("id", "1")
	recorder := httptest.NewRecorder()
	server.handleAPINotification(recorder, request)
	if recorder.Code != http.StatusOK || item.ID != 1 || recorder.Body.String() == "" {
		t.Fatalf("status=%d body=%s item=%+v", recorder.Code, recorder.Body.String(), item)
	}
}
