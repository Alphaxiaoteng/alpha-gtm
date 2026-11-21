package ui

import "testing"

// 公众号 endpoint 与平台白名单都在，但 normalizeRadarSignal 曾经没有
// wechat_mp 分支，id 永远为空 → 整批作品被丢弃，雷达里 0 条公众号信号。
// 这里用合成的最小响应固定字段映射，不依赖本机快照文件。
func TestWechatMPSignalFields(t *testing.T) {
	item := map[string]any{
		"docID":    "16013304036004601779",
		"title":    "示例文章标题",
		"desc":     "示例正文",
		"doc_url":  "http://mp.weixin.qq.com/s?__biz=demo",
		"thumbUrl": "https://mmbiz.qpic.cn/demo.jpg",
		"date":     "1784969626",
		"source":   "示例公众号",
	}
	niche := trendRadarNiche{ID: "n", Name: "n"}
	signal := normalizeRadarSignal(niche, "人工智能", "wechat_mp", "2026-07-25T00:00:00Z", t.TempDir(), item)

	if signal.VideoID != "16013304036004601779" {
		t.Errorf("VideoID = %q, want the docID", signal.VideoID)
	}
	if signal.Title != "示例文章标题" {
		t.Errorf("Title = %q", signal.Title)
	}
	if signal.URL != "http://mp.weixin.qq.com/s?__biz=demo" {
		t.Errorf("URL = %q, want the provider's doc_url", signal.URL)
	}
	if signal.Platform != "wechat_mp" {
		t.Errorf("Platform = %q", signal.Platform)
	}
}

// 没有 docID 的条目仍然应被丢弃（不是真作品）。
func TestWechatMPSignalRequiresDocID(t *testing.T) {
	niche := trendRadarNiche{ID: "n", Name: "n"}
	signal := normalizeRadarSignal(niche, "kw", "wechat_mp", "2026-07-25T00:00:00Z", t.TempDir(), map[string]any{
		"title": "只有标题没有 id",
	})
	if signal.Title != "" {
		t.Errorf("an entry without docID must be dropped, got title %q", signal.Title)
	}
}
