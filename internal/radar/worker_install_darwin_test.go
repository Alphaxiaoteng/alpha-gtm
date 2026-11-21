//go:build darwin

package radar

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 已安装的 worker 必须是当前这份可执行文件，否则界面上的「自动更新」开关
// 会指向一个不含该代码的旧二进制。
func TestWorkerBinaryMatchesDetectsStaleBuild(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(source, []byte("new-binary"), 0o700); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o700); err != nil {
		t.Fatalf("write target: %v", err)
	}
	// 目标更旧且更小 —— 必须判定为需要更新。
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(target, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if workerBinaryMatches(source, target) {
		t.Error("a smaller, older worker must be treated as stale")
	}
}

func TestWorkerBinaryMatchesAcceptsSameBuild(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	payload := []byte("same-binary")
	for _, path := range []string{source, target} {
		if err := os.WriteFile(path, payload, 0o700); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(source, now, now); err != nil {
		t.Fatalf("chtimes source: %v", err)
	}
	if err := os.Chtimes(target, now.Add(time.Minute), now.Add(time.Minute)); err != nil {
		t.Fatalf("chtimes target: %v", err)
	}
	if !workerBinaryMatches(source, target) {
		t.Error("same size with target newer must be considered up to date")
	}
}

func TestWorkerBinaryMatchesMissingTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("x"), 0o700); err != nil {
		t.Fatalf("write: %v", err)
	}
	if workerBinaryMatches(source, filepath.Join(dir, "absent")) {
		t.Error("a missing target must never match")
	}
}

// 临时数据目录不得改写用户真实的 LaunchAgent。实测过：跑一次带 NewServer 的
// 测试，宿主机的 com.alpha-nexus.radar-worker.plist 就被指向了 t.TempDir()
// 下已删除的二进制，用户的定时抓取当场失效。
func TestIsEphemeralDataDirRejectsTemp(t *testing.T) {
	cases := map[string]bool{
		t.TempDir():                          true,
		"/tmp/whatever":                      true,
		"/var/folders/ab/cd/T/test-123":      true,
		"":                                   true,
		"/Users/albert/.alpha-nexus-egolite": false,
		"/Users/someone/Projects/data":       false,
	}
	for dir, wantEphemeral := range cases {
		if got := isEphemeralDataDir(dir); got != wantEphemeral {
			t.Errorf("isEphemeralDataDir(%q) = %v, want %v", dir, got, wantEphemeral)
		}
	}
}

// 默认数据目录必须沿用历史 label，否则升级会留下一个孤儿任务继续跑。
func TestWorkerLabelKeepsDefaultAndSeparatesOthers(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := workerLabel(filepath.Join(home, ".alpha-nexus-egolite")); got != defaultWorkerLabel {
		t.Fatalf("default data dir must keep the historical label, got %q", got)
	}
	first := workerLabel("/Users/someone/data-a")
	second := workerLabel("/Users/someone/data-b")
	if first == defaultWorkerLabel || second == defaultWorkerLabel {
		t.Fatal("a custom data dir must not reuse the default label")
	}
	if first == second {
		t.Fatalf("different data dirs must get different labels: %q", first)
	}
	if again := workerLabel("/Users/someone/data-a"); again != first {
		t.Fatalf("label must be stable for the same dir: %q vs %q", again, first)
	}
}
