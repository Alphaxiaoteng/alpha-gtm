package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// 趋势雷达首屏要解析几百 MB 快照，这是本项目最重的一次读取。
// 这个基准用合成快照固定住这条路径的成本，避免优化回退。
func BenchmarkBuildTrendRadarSummary(b *testing.B) {
	dataDir := b.TempDir()
	callsDir := filepath.Join(dataDir, "tikhub", "calls", "20261120")
	if err := os.MkdirAll(callsDir, 0o700); err != nil {
		b.Fatalf("mkdir: %v", err)
	}
	// 每个文件模拟一次真实抓取：几十条作品，带标题、正文、指标。
	for i := 0; i < 8; i++ {
		payload := syntheticSnapshot(i)
		name := filepath.Join(callsDir, "call-"+itoaBench(i)+".json")
		if err := os.WriteFile(name, payload, 0o600); err != nil {
			b.Fatalf("write: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := buildTrendRadarSummary(dataDir, time.Now()); err != nil {
			b.Fatalf("buildTrendRadarSummary: %v", err)
		}
	}
	b.ReportAllocs()
}

func syntheticSnapshot(seed int) []byte {
	items := make([]byte, 0, 64*1024)
	items = append(items, '[', ']')
	_ = items
	var builder []byte
	builder = append(builder, `{"call_id":`...)
	builder = append(builder, itoaBench(seed+1)...)
	builder = append(builder, `,"platform":"douyin","fetched_at":"2026-11-20T10:00:00Z","response":{"data":[`...)
	for j := 0; j < 40; j++ {
		if j > 0 {
			builder = append(builder, ',')
		}
		builder = append(builder, `{"aweme_id":"`...)
		builder = append(builder, itoaBench(seed*100+j)...)
		builder = append(builder, `","desc":"这是第 `...)
		builder = append(builder, itoaBench(j)...)
		builder = append(builder, ` 条合成作品的标题与正文，用于压测解析路径","author":{"nickname":"创作者`...)
		builder = append(builder, itoaBench(seed)...)
		builder = append(builder, `"},"statistics":{"digg_count":1234,"collect_count":567,"share_count":89,"play_count":45678},"video":{"cover":{"url_list":["https://example.com/c.jpg"]}}}`...)
	}
	builder = append(builder, `]}}`...)
	return builder
}

func itoaBench(v int) string {
	return strconv.Itoa(v)
}
