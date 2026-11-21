package radar

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"alpha-nexus-egolite/internal/tikhub"
)

func creatorFailure(awemeID, kind, reason string) CreatorArchiveFailure {
	return CreatorArchiveFailure{
		AwemeID: awemeID, Kind: kind, Reason: reason,
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func TestCreatorArchiveFailuresRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	seed := []CreatorArchiveFailure{
		creatorFailure("7678643711023369146", "cover", "http_403"),
		creatorFailure("7659704534466612709", "audio", "cancelled"),
	}
	if err := saveCreatorArchiveFailures(dataDir, "douyin", "user-1", seed); err != nil {
		t.Fatal(err)
	}

	loaded, ok := LoadCreatorArchiveFailures(dataDir, "douyin", "user-1")
	if !ok {
		t.Fatal("expected the ledger to load")
	}
	if len(loaded) != 2 {
		t.Fatalf("failures=%#v", loaded)
	}
	// 并发下载的完成顺序不固定，落盘必须排序，保证同一结果字节稳定。
	if loaded[0].AwemeID != "7659704534466612709" || loaded[1].AwemeID != "7678643711023369146" {
		t.Fatalf("order=%v", []string{loaded[0].AwemeID, loaded[1].AwemeID})
	}
	for _, failure := range loaded {
		if failure.Kind == "" || failure.Reason == "" || failure.AttemptedAt == "" {
			t.Fatalf("incomplete failure record: %#v", failure)
		}
		if _, err := time.Parse(time.RFC3339, failure.AttemptedAt); err != nil {
			t.Fatalf("attempted_at %q is not RFC3339: %v", failure.AttemptedAt, err)
		}
	}

	// 文件必须与 index.json 同级，且带上顶层 updated_at。
	target := filepath.Join(tikhub.CreatorFolder(dataDir, "douyin", "user-1"), "archive-failures.json")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("missing ledger file: %v", err)
	}
	ledger, err := loadLedgerFile(t, target)
	if err != nil {
		t.Fatal(err)
	}
	if ledger.SchemaVersion == 0 || ledger.Platform != "douyin" || ledger.UserID != "user-1" {
		t.Fatalf("ledger header=%#v", ledger)
	}
	if _, err := time.Parse(time.RFC3339, ledger.UpdatedAt); err != nil {
		t.Fatalf("updated_at %q is not RFC3339: %v", ledger.UpdatedAt, err)
	}
}

func loadLedgerFile(t *testing.T, path string) (CreatorArchiveFailureLedger, error) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return CreatorArchiveFailureLedger{}, err
	}
	var ledger CreatorArchiveFailureLedger
	if err := json.Unmarshal(raw, &ledger); err != nil {
		return CreatorArchiveFailureLedger{}, err
	}
	return ledger, nil
}

func TestCreatorArchiveFailuresReplaceOnNextMediaRun(t *testing.T) {
	dataDir := t.TempDir()
	seed := []CreatorArchiveFailure{
		creatorFailure("111", "cover", "http_403"),
		creatorFailure("222", "audio", "cancelled"),
	}
	if err := saveCreatorArchiveFailures(dataDir, "douyin", "user-2", seed); err != nil {
		t.Fatal(err)
	}

	// 这次 111 的封面成功、222 的音频仍失败：旧记录整体被替换。
	if err := persistCreatorArchiveFailures(dataDir, "douyin", "user-2", true, tikhub.CreatorMediaResult{
		Covers: 1, Failed: 1,
		Failures: []tikhub.CreatorMediaFailure{{AwemeID: "222", Kind: "audio", Reason: "cancelled"}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	loaded, ok := LoadCreatorArchiveFailures(dataDir, "douyin", "user-2")
	if !ok {
		t.Fatal("expected the ledger to load")
	}
	if len(loaded) != 1 || loaded[0].AwemeID != "222" || loaded[0].Kind != "audio" {
		t.Fatalf("succeeded asset must disappear from the ledger: %#v", loaded)
	}

	// 全部成功时必须删除文件，而不是留一个空数组。
	if err := persistCreatorArchiveFailures(dataDir, "douyin", "user-2", true, tikhub.CreatorMediaResult{Covers: 2}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCreatorArchiveFailures(dataDir, "douyin", "user-2"); ok {
		t.Fatal("a fully successful run must remove the ledger")
	}
	target := creatorArchiveFailuresPath(dataDir, "douyin", "user-2")
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, stat err=%v", target, err)
	}
}

func TestCreatorArchiveFailuresSurviveSkipMediaRuns(t *testing.T) {
	dataDir := t.TempDir()
	seed := []CreatorArchiveFailure{
		creatorFailure("111", "cover", "http_403"),
		creatorFailure("222", "audio", "cancelled"),
	}
	if err := saveCreatorArchiveFailures(dataDir, "douyin", "user-3", seed); err != nil {
		t.Fatal(err)
	}

	// skip_media 没跑媒体阶段，不能拿空结果覆盖历史失败明细。
	if err := persistCreatorArchiveFailures(dataDir, "douyin", "user-3", false, tikhub.CreatorMediaResult{}, nil); err != nil {
		t.Fatal(err)
	}
	loaded, ok := LoadCreatorArchiveFailures(dataDir, "douyin", "user-3")
	if !ok || len(loaded) != 2 {
		t.Fatalf("skip_media must keep history, got ok=%v failures=%#v", ok, loaded)
	}
	if loaded[0].Reason != "http_403" || loaded[1].Reason != "cancelled" {
		t.Fatalf("history rewritten: %#v", loaded)
	}
}

func TestLoadCreatorArchiveFailuresToleratesMissingAndCorruptFiles(t *testing.T) {
	dataDir := t.TempDir()
	if failures, ok := LoadCreatorArchiveFailures(dataDir, "douyin", "never"); ok || failures != nil {
		t.Fatalf("missing ledger must report absent, got ok=%v failures=%#v", ok, failures)
	}

	root := tikhub.CreatorFolder(dataDir, "douyin", "broken")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "archive-failures.json")
	for _, raw := range []string{"{not json", `{"failures": 42}`, `{"failures": [{"aweme_id": 7}]}`, ""} {
		if err := os.WriteFile(target, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := LoadCreatorArchiveFailures(dataDir, "douyin", "broken"); ok {
			t.Fatalf("corrupt ledger %q must report absent", raw)
		}
	}
}

// 失败项必须带回本次归档列表里的作品标题与封面路径，UI 才能直接显示是哪条作品。
func TestCreatorArchiveFailuresCarryPostIdentity(t *testing.T) {
	dataDir := t.TempDir()
	posts := []tikhub.CreatorPostMedia{
		{AwemeID: "333", Title: "凌晨四点的便利店", CoverPath: "media/covers/333.jpg"},
		{AwemeID: "444", Title: "第二个作品", CoverPath: "media/covers/444.jpg"},
	}
	if err := persistCreatorArchiveFailures(dataDir, "douyin", "user-4", true, tikhub.CreatorMediaResult{
		Covers: 1, Failed: 2,
		Failures: []tikhub.CreatorMediaFailure{
			{AwemeID: "333", Kind: "audio", Reason: "http_403"},
			{AwemeID: "444", Kind: "cover", Reason: "cancelled"},
		},
	}, posts); err != nil {
		t.Fatal(err)
	}

	loaded, ok := LoadCreatorArchiveFailures(dataDir, "douyin", "user-4")
	if !ok || len(loaded) != 2 {
		t.Fatalf("ok=%v failures=%#v", ok, loaded)
	}
	if loaded[0].AwemeID != "333" || loaded[0].Title != "凌晨四点的便利店" || loaded[0].CoverPath != "media/covers/333.jpg" {
		t.Fatalf("first failure lost its identity: %#v", loaded[0])
	}
	if loaded[1].AwemeID != "444" || loaded[1].Title != "第二个作品" || loaded[1].CoverPath != "media/covers/444.jpg" {
		t.Fatalf("second failure lost its identity: %#v", loaded[1])
	}

	// 落盘 JSON 必须真的带上这两个字段，而不是只在内存里补。
	raw, err := os.ReadFile(creatorArchiveFailuresPath(dataDir, "douyin", "user-4"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"title": "凌晨四点的便利店"`, `"cover_path": "media/covers/333.jpg"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("ledger JSON missing %s:\n%s", want, raw)
		}
	}
}

// 失败项不在本次抓取列表里（例如来自更早一次归档）时，身份字段留空且不报错。
func TestCreatorArchiveFailuresWithoutMatchingPostStayEmpty(t *testing.T) {
	dataDir := t.TempDir()
	posts := []tikhub.CreatorPostMedia{
		{AwemeID: "333", Title: "凌晨四点的便利店", CoverPath: "media/covers/333.jpg"},
	}
	if err := persistCreatorArchiveFailures(dataDir, "douyin", "user-5", true, tikhub.CreatorMediaResult{
		Failed: 2,
		Failures: []tikhub.CreatorMediaFailure{
			{AwemeID: "333", Kind: "audio", Reason: "http_403"},
			{AwemeID: "7660464546108533873", Kind: "cover", Reason: "cancelled"},
		},
	}, posts); err != nil {
		t.Fatal(err)
	}

	loaded, ok := LoadCreatorArchiveFailures(dataDir, "douyin", "user-5")
	if !ok || len(loaded) != 2 {
		t.Fatalf("ok=%v failures=%#v", ok, loaded)
	}
	// 排序按 aweme_id 升序，所以 "333" 在前。
	if loaded[0].AwemeID != "333" || loaded[0].Title != "凌晨四点的便利店" {
		t.Fatalf("known post must keep its identity: %#v", loaded[0])
	}
	unknown := loaded[1]
	if unknown.AwemeID != "7660464546108533873" {
		t.Fatalf("order=%v", []string{loaded[0].AwemeID, loaded[1].AwemeID})
	}
	if unknown.Title != "" || unknown.CoverPath != "" {
		t.Fatalf("unknown post must degrade to id only: %#v", unknown)
	}

	raw, err := os.ReadFile(creatorArchiveFailuresPath(dataDir, "douyin", "user-5"))
	if err != nil {
		t.Fatal(err)
	}
	// omitempty：查不到身份时不能写入空字符串字段污染台账。
	var fileLedger struct {
		Failures []map[string]json.RawMessage `json:"failures"`
	}
	if err := json.Unmarshal(raw, &fileLedger); err != nil {
		t.Fatal(err)
	}
	if len(fileLedger.Failures) != 2 {
		t.Fatalf("ledger failures=%#v", fileLedger.Failures)
	}
	for _, key := range []string{"title", "cover_path"} {
		if _, found := fileLedger.Failures[1][key]; found {
			t.Fatalf("unknown post must omit %q:\n%s", key, raw)
		}
	}
}

// 老台账（无 title / cover_path 字段）必须能直接读进来，字段退化为空字符串。
func TestLoadCreatorArchiveFailuresReadsLegacyLedger(t *testing.T) {
	dataDir := t.TempDir()
	root := tikhub.CreatorFolder(dataDir, "douyin", "legacy")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{
  "schema_version": 1,
  "platform": "douyin",
  "user_id": "legacy",
  "updated_at": "2026-01-02T03:04:05Z",
  "failures": [
    {"aweme_id": "7660464546108533873", "kind": "cover", "reason": "http_403", "attempted_at": "2026-01-02T03:04:05Z"}
  ]
}`
	target := creatorArchiveFailuresPath(dataDir, "douyin", "legacy")
	if err := os.WriteFile(target, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, ok := LoadCreatorArchiveFailures(dataDir, "douyin", "legacy")
	if !ok || len(loaded) != 1 {
		t.Fatalf("legacy ledger must load, ok=%v failures=%#v", ok, loaded)
	}
	if loaded[0].AwemeID != "7660464546108533873" || loaded[0].Reason != "http_403" {
		t.Fatalf("legacy record lost fields: %#v", loaded[0])
	}
	if loaded[0].Title != "" || loaded[0].CoverPath != "" {
		t.Fatalf("legacy record must have empty identity: %#v", loaded[0])
	}
	if CreatorArchiveFailuresSchemaVersion != 1 {
		t.Fatalf("schema_version must stay 1, got %d", CreatorArchiveFailuresSchemaVersion)
	}
}
