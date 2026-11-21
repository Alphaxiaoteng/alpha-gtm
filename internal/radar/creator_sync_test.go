package radar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"alpha-nexus-egolite/internal/tikhub"
)

func creatorPostsPage(ids ...string) map[string]interface{} {
	list := make([]interface{}, 0, len(ids))
	for index, id := range ids {
		list = append(list, map[string]interface{}{
			"aweme_id":    id,
			"desc":        "作品 " + id,
			"create_time": 1793000000 - index,
			"video": map[string]interface{}{
				"cover": map[string]interface{}{
					"url_list": []interface{}{"https://p3-sign.douyinpic.com/" + id + ".jpeg"},
				},
			},
		})
	}
	return map[string]interface{}{"data": map[string]interface{}{"aweme_list": list}}
}

func TestCreatorPostsPagingReadsDouyinCursorShapes(t *testing.T) {
	// 抖音把 has_more 返回成数字，max_cursor 是超大整数。
	cases := []struct {
		name     string
		hasMore  interface{}
		cursor   interface{}
		wantMore bool
		wantNext string
	}{
		{"numeric more", float64(1), float64(1789638008000), true, "1789638008000"},
		{"no more", float64(0), float64(1789638008000), false, ""},
		{"string more", "1", "1789638008000", true, "1789638008000"},
		{"bool more", true, float64(1789638008000), true, "1789638008000"},
		{"zero cursor cannot page", float64(1), float64(0), false, ""},
		{"missing cursor cannot page", float64(1), nil, false, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := map[string]interface{}{"data": map[string]interface{}{
				"has_more": testCase.hasMore, "max_cursor": testCase.cursor,
			}}
			next, more := tikhub.CreatorPostsPaging(response)
			if more != testCase.wantMore {
				t.Fatalf("more=%v want %v", more, testCase.wantMore)
			}
			if next != testCase.wantNext {
				t.Fatalf("next=%q want %q", next, testCase.wantNext)
			}
		})
	}
}

func TestSaveAndLoadCreatorIndexRoundTrips(t *testing.T) {
	dataDir := t.TempDir()
	posts := []tikhub.CreatorPostMedia{
		{AwemeID: "b", Title: "较新", CreateTime: 200, CoverPath: "media/covers/b.jpg"},
		{AwemeID: "a", Title: "较旧", CreateTime: 100},
	}
	path, err := saveCreatorIndex(dataDir, "douyin", "user-1", "测试博主", posts)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "index.json" {
		t.Fatalf("index path=%q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("index perm=%v want 0600", info.Mode().Perm())
	}

	loaded, err := LoadCreatorIndex(dataDir, "douyin", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Posts) != 2 || loaded.Posts[0].AwemeID != "b" {
		t.Fatalf("loaded=%#v", loaded.Posts)
	}
	if loaded.Name != "测试博主" {
		t.Fatalf("name=%q", loaded.Name)
	}
	if loaded.Posts[0].CoverPath != "media/covers/b.jpg" {
		t.Fatalf("cover path lost: %q", loaded.Posts[0].CoverPath)
	}
}

func TestLoadCreatorIndexToleratesMissingAndCorruptManifests(t *testing.T) {
	dataDir := t.TempDir()
	// 从未同步过：必须返回空索引而不是错误。
	fresh, err := LoadCreatorIndex(dataDir, "douyin", "never")
	if err != nil {
		t.Fatalf("missing manifest must not error: %v", err)
	}
	if len(fresh.Posts) != 0 {
		t.Fatalf("fresh index must be empty, got %#v", fresh.Posts)
	}
	if fresh.Posts == nil {
		t.Fatal("fresh index posts must be an empty slice so JSON encodes []")
	}

	// 损坏的清单不能阻塞重新同步。
	root := tikhub.CreatorFolder(dataDir, "douyin", "broken")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovered, err := LoadCreatorIndex(dataDir, "douyin", "broken")
	if err != nil {
		t.Fatalf("corrupt manifest must not error: %v", err)
	}
	if len(recovered.Posts) != 0 {
		t.Fatalf("corrupt manifest must recover empty, got %#v", recovered.Posts)
	}
}

func TestCreatorIndexJSONUsesStableEmptyArray(t *testing.T) {
	dataDir := t.TempDir()
	path, err := saveCreatorIndex(dataDir, "douyin", "user-2", "", []tikhub.CreatorPostMedia{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	posts, ok := decoded["posts"].([]interface{})
	if !ok {
		t.Fatalf("posts must encode as an array, got %#v", decoded["posts"])
	}
	if len(posts) != 0 {
		t.Fatalf("posts=%#v", posts)
	}
}

func TestIsRetryableCreatorError(t *testing.T) {
	// 本地取消和存储错误必须立即失败，不能靠重试掩盖。
	if isRetryableCreatorError(nil) {
		t.Fatal("nil error is not retryable")
	}
	if isRetryableCreatorError(context.Canceled) {
		t.Fatal("cancellation must not be retried")
	}
	if isRetryableCreatorError(context.DeadlineExceeded) {
		t.Fatal("deadline must not be retried")
	}
	if isRetryableCreatorError(errors.New("snapshot_write_failed")) {
		t.Fatal("local storage failures must not be retried")
	}
	if isRetryableCreatorError(errors.New("empty API key")) {
		t.Fatal("misconfiguration must not be retried")
	}
	// TikHub 对同一游标会偶发 400 "Request failed. Please retry."，必须重试。
	// 这里用真实的客户端错误构造方式验证分类。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":400,"message":"Request failed. Please retry."}`))
	}))
	defer server.Close()

	client := tikhub.New("test-key")
	client.BaseURL = server.URL
	if _, err := client.GetTikHubUserInfo(context.Background()); err == nil {
		t.Fatal("expected a provider failure from the stub server")
	} else if !isRetryableCreatorError(err) {
		t.Fatalf("provider 400 must be retryable, got %v", err)
	}

	code, ok := tikhub.ProviderFailureCode(errors.New("snapshot_write_failed"))
	if ok || code != "" {
		t.Fatalf("local errors must not be reported as provider failures: %q %v", code, ok)
	}
}

// 受限抓取（--max-pages / cursor_stalled）只看到列表的一头。它不能把没见到的
// 旧作品从索引里删掉——那些文件还在 media/ 里，删了记录就变成孤儿。
func TestMergeCreatorIndexPostsKeepsUnseenHistory(t *testing.T) {
	fresh := []tikhub.CreatorPostMedia{
		{AwemeID: "a", Title: "新-a", Likes: 10},
		{AwemeID: "b", Title: "新-b", Likes: 20},
	}
	previous := []tikhub.CreatorPostMedia{
		{AwemeID: "a", Title: "旧-a", Likes: 1},
		{AwemeID: "c", Title: "旧-c", Likes: 3},
	}

	merged := mergeCreatorIndexPosts(fresh, previous)
	if len(merged) != 3 {
		t.Fatalf("merged=%d want 3 (%#v)", len(merged), merged)
	}
	byID := map[string]tikhub.CreatorPostMedia{}
	for _, post := range merged {
		byID[post.AwemeID] = post
	}
	// 本次抓到的用新数据，不能回退成旧指标
	if byID["a"].Likes != 10 || byID["a"].Title != "新-a" {
		t.Fatalf("fresh post was overwritten by history: %#v", byID["a"])
	}
	// 没见到的旧作品必须留下
	if _, ok := byID["c"]; !ok {
		t.Fatalf("history was dropped: %#v", merged)
	}
	// 本次新增的作品照常加入
	if _, ok := byID["b"]; !ok {
		t.Fatalf("fresh post is missing: %#v", merged)
	}
}

func TestMergeCreatorIndexPostsWithoutHistoryIsJustTheFetch(t *testing.T) {
	fresh := []tikhub.CreatorPostMedia{{AwemeID: "a"}, {AwemeID: "b"}}
	if merged := mergeCreatorIndexPosts(fresh, nil); len(merged) != 2 {
		t.Fatalf("merged=%d want 2", len(merged))
	}
}

// 空抓取（全部重试耗尽）不能把索引清空。
func TestMergeCreatorIndexPostsKeepsEverythingWhenNothingWasFetched(t *testing.T) {
	previous := []tikhub.CreatorPostMedia{{AwemeID: "a"}, {AwemeID: "b"}}
	if merged := mergeCreatorIndexPosts(nil, previous); len(merged) != 2 {
		t.Fatalf("merged=%d want the untouched history", len(merged))
	}
}
