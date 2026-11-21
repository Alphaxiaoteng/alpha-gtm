package ui

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/config"
)

func TestAccountDeleteRejectsRunningPublishOwner(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("xhs", "delete-busy", "")
	if err != nil {
		t.Fatal(err)
	}
	assets, err := server.db.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	content, err := server.db.CreateContent("graph_text", "delete guard", "caption", "body", "[]", "draft")
	if err != nil {
		t.Fatal(err)
	}
	job, err := server.db.CreatePublishJobWithIntent(0, content.ID, account.ID, assets[0].ID, "xhs", content.Type, "", "prepare", "")
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := server.db.ClaimPublishJob(job.ID, "delete-busy"); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := server.db.Exec(`INSERT INTO browser_runtime_instances
		(profile_id, account_id, pid, cdp_port, cdp_url, state, active_job_id, started_at, last_seen_at, updated_at)
		VALUES (?, ?, 99999996, 15133, 'http://127.0.0.1:15133', 'ready', ?, ?, ?, ?)`,
		browser.ProfileIDForAccount(account.ID), account.ID, job.ID, now, now, now); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/delete", bytes.NewBufferString(fmt.Sprintf(`{"id":%d}`, account.ID)))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := server.db.GetAccount(account.ID); err != nil {
		t.Fatalf("busy account was deleted: %v", err)
	}
	var activeJobID int64
	if err := server.db.QueryRow(`SELECT active_job_id FROM browser_runtime_instances WHERE account_id=?`, account.ID).Scan(&activeJobID); err != nil {
		t.Fatal(err)
	}
	if activeJobID != job.ID {
		t.Fatalf("runtime owner changed: got=%d want=%d", activeJobID, job.ID)
	}
}
