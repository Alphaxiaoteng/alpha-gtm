package tools

import (
	"context"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestAccountTagToolsCreateBulkUpdateAndResolve(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	first, _ := database.CreateAccount("wechat_mp", "mp", "")
	second, _ := database.CreateAccount("csdn", "csdn", "")
	database.Close()
	runtime := &Runtime{DataDir: dir}
	created, err := Default.Call(context.Background(), runtime, "account.tag.create", map[string]interface{}{
		"name": "文章矩阵", "color": "#0EA5E9",
	})
	if err != nil || !created.OK {
		t.Fatalf("create result=%+v err=%v", created, err)
	}
	tagID := int64(created.Data["tag"].(map[string]interface{})["id"].(int64))
	secondTag, err := Default.Call(context.Background(), runtime, "account.tag.create", map[string]interface{}{
		"name": "优先账号", "color": "#F97316",
	})
	if err != nil || !secondTag.OK {
		t.Fatalf("create second result=%+v err=%v", secondTag, err)
	}
	secondTagID := int64(secondTag.Data["tag"].(map[string]interface{})["id"].(int64))
	reordered, err := Default.Call(context.Background(), runtime, "account.tag.reorder", map[string]interface{}{
		"tag_ids": []interface{}{float64(secondTagID), float64(tagID)},
	})
	if err != nil || !reordered.OK {
		t.Fatalf("reorder result=%+v err=%v", reordered, err)
	}
	updated, err := Default.Call(context.Background(), runtime, "account.tag.update", map[string]interface{}{
		"account_ids": []interface{}{float64(first.ID), float64(second.ID)},
		"tag_ids":     []interface{}{float64(tagID)},
		"mode":        "append",
	})
	if err != nil || !updated.OK {
		t.Fatalf("update result=%+v err=%v", updated, err)
	}
	resolved, err := Default.Call(context.Background(), runtime, "account.tag.resolve", map[string]interface{}{
		"tag_ids": []interface{}{float64(tagID)},
	})
	if err != nil || !resolved.OK || resolved.Data["count"] != 2 {
		t.Fatalf("resolve result=%+v err=%v", resolved, err)
	}
	listed, err := Default.Call(context.Background(), runtime, "account.list", nil)
	if err != nil || !listed.OK {
		t.Fatalf("list result=%+v err=%v", listed, err)
	}
	rows := listed.Data["accounts"].([]map[string]interface{})
	if len(rows) != 2 || len(rows[0]["tags"].([]db.AccountTag)) != 1 || len(rows[1]["tags"].([]db.AccountTag)) != 1 {
		t.Fatalf("account.list tags=%+v", rows)
	}
}
