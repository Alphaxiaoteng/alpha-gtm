package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 签名必须对文件新增、改写敏感，否则缓存会让新抓取的内容一直不显示。
func TestRadarSnapshotSignatureReactsToChanges(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "20261120")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	empty := radarSnapshotSignature(dir)

	file := filepath.Join(nested, "call-1.json")
	if err := os.WriteFile(file, []byte("{\"a\":1}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	added := radarSnapshotSignature(dir)
	if added == empty {
		t.Error("adding a snapshot must change the signature")
	}

	if err := os.WriteFile(file, []byte("{\"a\":1234567}"), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	grown := radarSnapshotSignature(dir)
	if grown == added {
		t.Error("growing a snapshot must change the signature")
	}

	stamp := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(file, stamp, stamp); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	touched := radarSnapshotSignature(dir)
	if touched == grown {
		t.Error("rewriting a snapshot in place must change the signature")
	}
}

func TestRadarSnapshotSignatureMissingDirectoryIsStable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	first := radarSnapshotSignature(dir)
	second := radarSnapshotSignature(dir)
	if first != second {
		t.Errorf("signature must be stable for a missing directory: %q vs %q", first, second)
	}
}

// 同一签名下第二次读取必须来自缓存，否则界面每帧都要重解析数百 MB。
func TestCachedTrendRadarSummaryReusesResult(t *testing.T) {
	server := &Server{}
	signature := radarSnapshotSignature(t.TempDir())
	server.radarSummary = &radarSummaryCacheEntry{
		signature: signature,
		summary:   &trendRadarSummary{SnapshotState: "ready", GeneratedAt: "cached"},
		builtAt:   time.Now(),
	}

	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "tikhub", "calls"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	first, err := server.cachedTrendRadarSummary(dataDir)
	if err != nil {
		t.Fatalf("cachedTrendRadarSummary: %v", err)
	}
	second, err := server.cachedTrendRadarSummary(dataDir)
	if err != nil {
		t.Fatalf("cachedTrendRadarSummary: %v", err)
	}
	if first != second {
		t.Error("second read should return the same cached pointer")
	}
	if first.GeneratedAt != "cached" {
		t.Errorf("cache was not used, generated_at = %q", first.GeneratedAt)
	}
}
