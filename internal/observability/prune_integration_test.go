package observability

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPruneEventFilesRealisticDirectory 验证清扫器在接近真实的数据目录上
// 只回收已退出进程的文件，且能报告可回收体积。
func TestPruneEventFilesRealisticDirectory(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	// 大量已退出进程的文件，模拟长时间运行后累积的 6000+ 个文件。
	for i := 0; i < 200; i++ {
		name := filepath.Join(dir, "events-"+itoa(900000+i)+".jsonl")
		if err := os.WriteFile(name, make([]byte, 256), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		stamp := now.Add(-48 * time.Hour)
		if err := os.Chtimes(name, stamp, stamp); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
	}
	// 当前进程的文件必须保留。
	live := filepath.Join(dir, "events-"+itoa(os.Getpid())+".jsonl")
	if err := os.WriteFile(live, []byte("live"), 0o600); err != nil {
		t.Fatalf("write live: %v", err)
	}
	stale := now.Add(-48 * time.Hour)
	if err := os.Chtimes(live, stale, stale); err != nil {
		t.Fatalf("chtimes live: %v", err)
	}
	// 非事件文件必须原样保留。
	other := filepath.Join(dir, "ui-serve.stdout.log")
	if err := os.WriteFile(other, []byte("stdout"), 0o600); err != nil {
		t.Fatalf("write other: %v", err)
	}

	result, err := PruneEventFiles(dir, PruneEventFileOptions{
		KeepPIDs: map[int]struct{}{os.Getpid(): {}},
	})
	if err != nil {
		t.Fatalf("PruneEventFiles: %v", err)
	}
	if result.Removed != 200 {
		t.Errorf("Removed = %d, want 200", result.Removed)
	}
	if result.Kept != 1 {
		t.Errorf("Kept = %d, want 1 (only this process)", result.Kept)
	}
	if _, statErr := os.Stat(live); statErr != nil {
		t.Errorf("live file must survive: %v", statErr)
	}
	if _, statErr := os.Stat(other); statErr != nil {
		t.Errorf("non-event file must survive: %v", statErr)
	}
	for i := 0; i < 200; i += 50 {
		name := filepath.Join(dir, "events-"+itoa(900000+i)+".jsonl")
		if _, statErr := os.Stat(name); !os.IsNotExist(statErr) {
			t.Errorf("%s should be pruned", filepath.Base(name))
		}
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
