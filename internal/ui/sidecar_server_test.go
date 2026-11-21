package ui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
)

func TestSidecarServeDynamicPortTransportAuthAndShutdown(t *testing.T) {
	srv, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if port == 0 {
		t.Fatal("dynamic listener returned port 0")
	}

	serveDone := make(chan error, 1)
	transportToken := strings.Repeat("a", 64)
	instanceNonce := strings.Repeat("b", 64)
	go func() {
		serveDone <- srv.Serve(listener, ServeOptions{
			TransportToken: transportToken,
			InstanceNonce:  instanceNonce,
			Version:        "test-version",
		})
	}()

	baseURL := "http://" + listener.Addr().String()
	health := getEventually(t, baseURL+"/healthz", "")
	if health.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", health.StatusCode)
	}
	var healthBody map[string]any
	if err := json.NewDecoder(health.Body).Decode(&healthBody); err != nil {
		t.Fatal(err)
	}
	_ = health.Body.Close()
	if healthBody["ready"] != true || healthBody["nonce"] != instanceNonce {
		t.Fatalf("unexpected health: %#v", healthBody)
	}
	if _, leaked := healthBody["data_dir"]; leaked {
		t.Fatalf("health leaked business data: %#v", healthBody)
	}

	missing := getEventually(t, baseURL+"/api/overview", "")
	if missing.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", missing.StatusCode)
	}
	_ = missing.Body.Close()

	query := getEventually(t, baseURL+"/api/overview?transport_token="+transportToken, "")
	if query.StatusCode != http.StatusBadRequest {
		t.Fatalf("query token status = %d, want 400", query.StatusCode)
	}
	_ = query.Body.Close()

	queryWithHeader := getEventually(t, baseURL+"/api/overview?transport_token=forbidden", transportToken)
	if queryWithHeader.StatusCode != http.StatusBadRequest {
		t.Fatalf("header plus query status = %d, want 400", queryWithHeader.StatusCode)
	}
	_ = queryWithHeader.Body.Close()

	wrong := getEventually(t, baseURL+"/api/overview", "wrong-transport-token")
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong header token status = %d, want 401", wrong.StatusCode)
	}
	_ = wrong.Body.Close()

	authed := getEventually(t, baseURL+"/api/overview", transportToken)
	if authed.StatusCode != http.StatusOK {
		t.Fatalf("header token status = %d, want 200", authed.StatusCode)
	}
	_ = authed.Body.Close()

	if _, err := srv.db.CreateNotification("publish.completed", "info", "发布完成", "publish completed", "publish_job", 1); err != nil {
		t.Fatal(err)
	}
	notificationsReq, err := http.NewRequest(http.MethodGet, baseURL+"/api/notifications", nil)
	if err != nil {
		t.Fatal(err)
	}
	notificationsReq.Header.Set(TransportTokenHeader, transportToken)
	notificationsResp, err := http.DefaultClient.Do(notificationsReq)
	if err != nil {
		t.Fatal(err)
	}
	var notifications []db.Notification
	if notificationsResp.StatusCode != http.StatusOK || json.NewDecoder(notificationsResp.Body).Decode(&notifications) != nil || len(notifications) != 1 || notifications[0].Read {
		_ = notificationsResp.Body.Close()
		t.Fatalf("notifications response status=%d items=%+v", notificationsResp.StatusCode, notifications)
	}
	_ = notificationsResp.Body.Close()
	markAllReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/notifications/read-all", nil)
	if err != nil {
		t.Fatal(err)
	}
	markAllReq.Header.Set(TransportTokenHeader, transportToken)
	markAllResp, err := http.DefaultClient.Do(markAllReq)
	if err != nil || markAllResp.StatusCode != http.StatusOK {
		if markAllResp != nil {
			_ = markAllResp.Body.Close()
		}
		t.Fatalf("mark all notifications err=%v", err)
	}
	_ = markAllResp.Body.Close()

	createContentReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/contents", bytes.NewBufferString(`{"type":"article","title":"API content","caption":"summary","body":"body","tags_json":"[]","status":"draft"}`))
	if err != nil {
		t.Fatal(err)
	}
	createContentReq.Header.Set("Content-Type", "application/json")
	createContentReq.Header.Set(TransportTokenHeader, transportToken)
	createContentResp, err := http.DefaultClient.Do(createContentReq)
	if err != nil {
		t.Fatal(err)
	}
	var createdContent db.Content
	if createContentResp.StatusCode != http.StatusCreated || json.NewDecoder(createContentResp.Body).Decode(&createdContent) != nil || createdContent.ID == 0 || createdContent.RawPublishData != "" {
		_ = createContentResp.Body.Close()
		t.Fatalf("create content status=%d content=%+v", createContentResp.StatusCode, createdContent)
	}
	_ = createContentResp.Body.Close()
	cloneReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/contents/%d/clone", baseURL, createdContent.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	cloneReq.Header.Set(TransportTokenHeader, transportToken)
	cloneResp, err := http.DefaultClient.Do(cloneReq)
	if err != nil {
		t.Fatal(err)
	}
	var clonedContent db.Content
	if cloneResp.StatusCode != http.StatusCreated || json.NewDecoder(cloneResp.Body).Decode(&clonedContent) != nil || clonedContent.ID == createdContent.ID || clonedContent.Status != "draft" {
		_ = cloneResp.Body.Close()
		t.Fatalf("clone content status=%d content=%+v", cloneResp.StatusCode, clonedContent)
	}
	_ = cloneResp.Body.Close()

	mediaPath := filepath.Join(t.TempDir(), "asset.jpg")
	if err := os.WriteFile(mediaPath, []byte("safe local media"), 0600); err != nil {
		t.Fatal(err)
	}
	importMediaReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/media/library", bytes.NewBufferString(fmt.Sprintf(`{"path":%q,"purpose":"article"}`, mediaPath)))
	if err != nil {
		t.Fatal(err)
	}
	importMediaReq.Header.Set("Content-Type", "application/json")
	importMediaReq.Header.Set(TransportTokenHeader, transportToken)
	importMediaResp, err := http.DefaultClient.Do(importMediaReq)
	if err != nil {
		t.Fatal(err)
	}
	var libraryMedia db.MediaLibraryItem
	if importMediaResp.StatusCode != http.StatusCreated || json.NewDecoder(importMediaResp.Body).Decode(&libraryMedia) != nil || libraryMedia.ID == 0 {
		_ = importMediaResp.Body.Close()
		t.Fatalf("import media status=%d media=%+v", importMediaResp.StatusCode, libraryMedia)
	}
	_ = importMediaResp.Body.Close()
	attachMediaReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/contents/%d/media", baseURL, createdContent.ID), bytes.NewBufferString(fmt.Sprintf(`{"library_id":%d}`, libraryMedia.ID)))
	if err != nil {
		t.Fatal(err)
	}
	attachMediaReq.Header.Set("Content-Type", "application/json")
	attachMediaReq.Header.Set(TransportTokenHeader, transportToken)
	attachMediaResp, err := http.DefaultClient.Do(attachMediaReq)
	if err != nil {
		t.Fatal(err)
	}
	var attachedMedia db.MediaAsset
	if attachMediaResp.StatusCode != http.StatusCreated || json.NewDecoder(attachMediaResp.Body).Decode(&attachedMedia) != nil || attachedMedia.ID == 0 || attachedMedia.LibraryID != libraryMedia.ID || attachedMedia.SHA256 == "" {
		_ = attachMediaResp.Body.Close()
		t.Fatalf("attach media status=%d media=%+v", attachMediaResp.StatusCode, attachedMedia)
	}
	_ = attachMediaResp.Body.Close()
	contentMediaReq, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/contents/%d/media", baseURL, createdContent.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	contentMediaReq.Header.Set(TransportTokenHeader, transportToken)
	contentMediaResp, err := http.DefaultClient.Do(contentMediaReq)
	if err != nil {
		t.Fatal(err)
	}
	var contentMedia []db.MediaAsset
	if contentMediaResp.StatusCode != http.StatusOK || json.NewDecoder(contentMediaResp.Body).Decode(&contentMedia) != nil || len(contentMedia) != 1 || contentMedia[0].ID != attachedMedia.ID {
		_ = contentMediaResp.Body.Close()
		t.Fatalf("list media status=%d media=%+v", contentMediaResp.StatusCode, contentMedia)
	}
	_ = contentMediaResp.Body.Close()
	detachMediaReq, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/media/%d", baseURL, attachedMedia.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	detachMediaReq.Header.Set(TransportTokenHeader, transportToken)
	detachMediaResp, err := http.DefaultClient.Do(detachMediaReq)
	if err != nil || detachMediaResp.StatusCode != http.StatusOK {
		if detachMediaResp != nil {
			_ = detachMediaResp.Body.Close()
		}
		t.Fatalf("detach media err=%v", err)
	}
	_ = detachMediaResp.Body.Close()

	createTemplateReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/templates", bytes.NewBufferString(`{"platform_code":"xhs","name":"API template","description":"safe","settings_json":"{\"visibility\":\"public\"}","enabled":true,"is_default":true}`))
	if err != nil {
		t.Fatal(err)
	}
	createTemplateReq.Header.Set("Content-Type", "application/json")
	createTemplateReq.Header.Set(TransportTokenHeader, transportToken)
	createTemplateResp, err := http.DefaultClient.Do(createTemplateReq)
	if err != nil {
		t.Fatal(err)
	}
	var createdTemplate db.PlatformSetting
	if createTemplateResp.StatusCode != http.StatusCreated || json.NewDecoder(createTemplateResp.Body).Decode(&createdTemplate) != nil || createdTemplate.ID == 0 || createdTemplate.SettingsJSON != `{"visibility":"public","comment_enabled":true,"declare_original":false,"location":"","collection":"","group":"","content_source":"none","related_media":"","allow_duet":false,"allow_copy":false}` {
		_ = createTemplateResp.Body.Close()
		t.Fatalf("create template status=%d template=%+v", createTemplateResp.StatusCode, createdTemplate)
	}
	_ = createTemplateResp.Body.Close()
	unsafeTemplateReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/templates", bytes.NewBufferString(`{"platform_code":"xhs","name":"unsafe","settings_json":"{\"token\":\"redacted\"}","enabled":true,"is_default":false}`))
	if err != nil {
		t.Fatal(err)
	}
	unsafeTemplateReq.Header.Set("Content-Type", "application/json")
	unsafeTemplateReq.Header.Set(TransportTokenHeader, transportToken)
	unsafeTemplateResp, err := http.DefaultClient.Do(unsafeTemplateReq)
	if err != nil {
		t.Fatal(err)
	}
	if unsafeTemplateResp.StatusCode != http.StatusBadRequest {
		_ = unsafeTemplateResp.Body.Close()
		t.Fatalf("unsafe template status=%d", unsafeTemplateResp.StatusCode)
	}
	_ = unsafeTemplateResp.Body.Close()

	schemaReq, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/tools/schema", nil)
	if err != nil {
		t.Fatal(err)
	}
	schemaReq.Header.Set(TransportTokenHeader, transportToken)
	schemaResp, err := http.DefaultClient.Do(schemaReq)
	if err != nil {
		t.Fatal(err)
	}
	if schemaResp.StatusCode != http.StatusOK {
		t.Fatalf("tools schema status=%d", schemaResp.StatusCode)
	}
	var schema []map[string]any
	if err := json.NewDecoder(schemaResp.Body).Decode(&schema); err != nil {
		t.Fatal(err)
	}
	_ = schemaResp.Body.Close()
	if len(schema) == 0 {
		t.Fatal("tools schema was empty")
	}

	callReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/tools/call", bytes.NewBufferString(`{"name":"tools.list","args":{"category":"meta"}}`))
	if err != nil {
		t.Fatal(err)
	}
	callReq.Header.Set("Content-Type", "application/json")
	callReq.Header.Set(TransportTokenHeader, transportToken)
	callResp, err := http.DefaultClient.Do(callReq)
	if err != nil {
		t.Fatal(err)
	}
	if callResp.StatusCode != http.StatusOK {
		t.Fatalf("tools call status=%d", callResp.StatusCode)
	}
	var callBody struct {
		RequestID string `json:"request_id"`
		Result    struct {
			OK   bool   `json:"ok"`
			Tool string `json:"tool"`
		} `json:"result"`
	}
	if err := json.NewDecoder(callResp.Body).Decode(&callBody); err != nil {
		t.Fatal(err)
	}
	_ = callResp.Body.Close()
	if callBody.RequestID == "" || !callBody.Result.OK || callBody.Result.Tool != "tools.list" {
		t.Fatalf("unexpected tools response: %+v", callBody)
	}

	badCallReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/tools/call", bytes.NewBufferString(`{"args":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	badCallReq.Header.Set(TransportTokenHeader, transportToken)
	badCallResp, err := http.DefaultClient.Do(badCallReq)
	if err != nil {
		t.Fatal(err)
	}
	if badCallResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing name status=%d", badCallResp.StatusCode)
	}
	_ = badCallResp.Body.Close()

	account, err := srv.db.CreateAccount("xhs", "sse-test", "")
	if err != nil {
		t.Fatal(err)
	}
	assetsReq, err := http.NewRequest(http.MethodGet, baseURL+"/api/account-assets", nil)
	if err != nil {
		t.Fatal(err)
	}
	assetsReq.Header.Set(TransportTokenHeader, transportToken)
	assetsResp, err := http.DefaultClient.Do(assetsReq)
	if err != nil {
		t.Fatal(err)
	}
	var assets []db.AccountAsset
	if assetsResp.StatusCode != http.StatusOK || json.NewDecoder(assetsResp.Body).Decode(&assets) != nil || len(assets) == 0 || assets[len(assets)-1].AccountID != account.ID {
		_ = assetsResp.Body.Close()
		t.Fatalf("account assets status=%d assets=%+v", assetsResp.StatusCode, assets)
	}
	_ = assetsResp.Body.Close()
	content, err := srv.db.CreateContent("graph_text", "", "", "", "", "draft")
	if err != nil {
		t.Fatal(err)
	}
	job, err := srv.db.CreatePublishJob(0, content.ID, account.ID, 0, "xhs", content.Type)
	if err != nil {
		t.Fatal(err)
	}
	createdEvent, err := srv.db.CreateJobEventDetailed(db.JobEventInput{JobID: job.ID, Level: "info", Type: "job.test", Message: "safe", RawJSON: `{"private":"not-in-sse"}`})
	if err != nil {
		t.Fatal(err)
	}
	streamCtx, streamCancel := context.WithTimeout(context.Background(), time.Second)
	defer streamCancel()
	streamReq, err := http.NewRequestWithContext(streamCtx, http.MethodGet, baseURL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamReq.Header.Set(TransportTokenHeader, transportToken)
	streamResp, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	if streamResp.StatusCode != http.StatusOK || !strings.HasPrefix(streamResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("SSE response status=%d type=%q", streamResp.StatusCode, streamResp.Header.Get("Content-Type"))
	}
	scanner := bufio.NewScanner(streamResp.Body)
	var streamedID int64
	var streamedData string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id: ") {
			if _, err := fmt.Sscan(strings.TrimPrefix(line, "id: "), &streamedID); err != nil {
				t.Fatal(err)
			}
		}
		if strings.HasPrefix(line, "data: ") {
			streamedData = strings.TrimPrefix(line, "data: ")
			break
		}
	}
	_ = streamResp.Body.Close()
	if streamedID != createdEvent.ID || strings.Contains(streamedData, "raw_json") || !strings.Contains(streamedData, "job.test") {
		t.Fatalf("SSE event mismatch id=%d data=%s", streamedID, streamedData)
	}

	invalidCursorReq, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	invalidCursorReq.Header.Set(TransportTokenHeader, transportToken)
	invalidCursorReq.Header.Set("Last-Event-ID", "not-a-number")
	invalidCursorResp, err := http.DefaultClient.Do(invalidCursorReq)
	if err != nil {
		t.Fatal(err)
	}
	if invalidCursorResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid SSE cursor status=%d", invalidCursorResp.StatusCode)
	}
	_ = invalidCursorResp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve returned error after shutdown: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop after Shutdown")
	}
}

func getEventually(t *testing.T, url, token string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set(TransportTokenHeader, token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			return resp
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s: %v", url, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
