package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/publishplan"
)

func TestAuthorizeAgentToolBindsPublishActionToCurrentSession(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContent("graph_text", "title", "caption", "body", `[]`, "draft")
	if err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("xhs", "creator", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 1 {
		t.Fatalf("account assets=%+v err=%v", assets, err)
	}
	path := filepath.Join(t.TempDir(), "image.png")
	contents := []byte("agent-domain-approval-image")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	media, err := database.CreateMediaAsset(content.ID, "image", path, path, "image/png", int64(len(contents)), hex.EncodeToString(digest[:]), 100, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (&publishplan.Service{DB: database}).Freeze(publishplan.FreezeInput{
		ContentID: content.ID, MediaMode: "graph_text", MediaIDs: []int64{media.ID},
		Targets: []publishplan.TargetInput{{AccountID: account.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database, health: healthState{Nonce: "agent-session-current"}}

	for _, toolName := range []string{"publish.plan.prepare", "publish.plan.submit"} {
		got, err := server.AuthorizeAgentTool(context.Background(), toolName, map[string]interface{}{
			"plan_id": int64(plan.ID), "snapshot_hash": plan.SnapshotHash,
		})
		if err != nil {
			t.Fatalf("%s authorization: %v", toolName, err)
		}
		if len(got) != 4 || got["plan_id"] != int64(plan.ID) || got["snapshot_hash"] != plan.SnapshotHash ||
			got["approval_id"] == "" || got["nonce"] == "" || got["confirm"] != nil {
			t.Fatalf("%s returned unexpected flat args: %+v", toolName, got)
		}
		approvalID, _ := got["approval_id"].(string)
		var sessionID, storedTool, target, expiresAt string
		if err := database.QueryRow(`SELECT session_id,tool_name,target_resource,expires_at FROM approvals WHERE id=?`, approvalID).
			Scan(&sessionID, &storedTool, &target, &expiresAt); err != nil {
			t.Fatal(err)
		}
		if sessionID != "agent-session-current" || storedTool != toolName || target != "publish_plan:"+jsonNumber(plan.ID) {
			t.Fatalf("approval binding session=%q tool=%q target=%q", sessionID, storedTool, target)
		}
		expires, err := time.Parse(time.RFC3339Nano, expiresAt)
		if err != nil || expires.Before(time.Now().Add(4*time.Minute)) || expires.After(time.Now().Add(6*time.Minute)) {
			t.Fatalf("approval TTL=%q parsed=%v", expiresAt, expires)
		}
	}

	if _, err := server.AuthorizeAgentTool(context.Background(), "publish.plan.prepare", map[string]interface{}{
		"plan_id": "not-an-integer", "snapshot_hash": plan.SnapshotHash,
	}); err == nil || !strings.Contains(err.Error(), "AGENT_DOMAIN_APPROVAL_INVALID") {
		t.Fatalf("string plan_id was accepted: %v", err)
	}
	if _, err := server.AuthorizeAgentTool(context.Background(), "publish.plan.freeze", map[string]interface{}{
		"plan_id": int64(plan.ID), "snapshot_hash": plan.SnapshotHash,
	}); err == nil || !strings.Contains(err.Error(), "NOT_REQUIRED") {
		t.Fatalf("freeze unexpectedly requested domain approval: %v", err)
	}
}

func TestAuthorizeAgentToolBindsShopApprovalToNestedInputAndSession(t *testing.T) {
	server, database, plan, _ := newShopListingTestServerForPlatform(t, &shopListingTestRunner{}, "shopify")
	defer database.Close()
	server.health.Nonce = "shop-agent-session"

	got, err := server.AuthorizeAgentTool(context.Background(), "shop.listing.prepare", map[string]interface{}{
		"input": map[string]interface{}{"plan_id": int64(plan.ID), "snapshot_hash": plan.SnapshotHash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["confirm"] != nil {
		t.Fatalf("unexpected shop top-level args: %+v", got)
	}
	input, ok := got["input"].(map[string]interface{})
	if !ok || len(input) != 4 || input["plan_id"] != int64(plan.ID) || input["snapshot_hash"] != plan.SnapshotHash ||
		input["approval_id"] == "" || input["nonce"] == "" || input["confirm"] != nil {
		t.Fatalf("unexpected shop nested args: %+v", got)
	}
	approvalID, _ := input["approval_id"].(string)
	var sessionID, storedTool, target, expiresAt string
	if err := database.QueryRow(`SELECT session_id,tool_name,target_resource,expires_at FROM approvals WHERE id=?`, approvalID).
		Scan(&sessionID, &storedTool, &target, &expiresAt); err != nil {
		t.Fatal(err)
	}
	if sessionID != "shop-agent-session" || storedTool != "shop.listing.prepare" || target != "shop_listing_plan:"+jsonNumber(plan.ID) {
		t.Fatalf("shop approval binding session=%q tool=%q target=%q", sessionID, storedTool, target)
	}
	if _, err := server.AuthorizeAgentTool(context.Background(), "shop.listing.prepare", map[string]interface{}{
		"input": map[string]interface{}{"plan_id": int64(plan.ID), "snapshot_hash": plan.SnapshotHash, "approval_id": "model"},
	}); err == nil || !strings.Contains(err.Error(), "AGENT_DOMAIN_APPROVAL_INVALID") {
		t.Fatalf("model domain credential was accepted by UI callback: %v", err)
	}
	if expires, err := time.Parse(time.RFC3339Nano, expiresAt); err != nil || expires.Before(time.Now().Add(4*time.Minute)) || expires.After(time.Now().Add(6*time.Minute)) {
		t.Fatalf("shop approval TTL=%q parsed=%v", expiresAt, expires)
	}
}
