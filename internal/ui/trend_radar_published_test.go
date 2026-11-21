package ui

import (
	"encoding/json"
	"testing"
)

// Published time decides whether a work belongs to "近 7 天". Every platform
// spells it differently, and a wrong unit silently shifts dates by years.
func TestRadarPublishedAtFor(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		item     map[string]interface{}
		want     string
	}{
		{
			name: "douyin epoch seconds", platform: "douyin",
			item: map[string]interface{}{"aweme_info": map[string]interface{}{"create_time": float64(1795078532)}},
			want: "2026-11-19T08:55:32Z",
		},
		{
			name: "douyin milliseconds", platform: "douyin",
			item: map[string]interface{}{"aweme_info": map[string]interface{}{"create_time": float64(1795078532000)}},
			want: "2026-11-19T08:55:32Z",
		},
		{
			name: "xhs note timestamp", platform: "xhs",
			item: map[string]interface{}{"note": map[string]interface{}{"timestamp": json.Number("1793106043")}},
			want: "2026-10-27T13:00:43Z",
		},
		{
			name: "youtube iso", platform: "youtube",
			item: map[string]interface{}{"snippet": map[string]interface{}{"publishedAt": "2026-11-18T03:04:05Z"}},
			want: "2026-11-18T03:04:05Z",
		},
		{
			name: "x ruby date", platform: "x",
			item: map[string]interface{}{"legacy": map[string]interface{}{"created_at": "Wed Nov 19 03:04:05 +0000 2026"}},
			want: "2026-11-19T03:04:05Z",
		},
		{
			name: "xhs milliseconds as json number", platform: "xhs",
			item: map[string]interface{}{"note": map[string]interface{}{"timestamp": json.Number("1793106043000")}},
			want: "2026-10-27T13:00:43Z",
		},
		{
			// An edited note carries update_time; that is not a publish time.
			name: "xhs edit time is not publish time", platform: "xhs",
			item: map[string]interface{}{"note": map[string]interface{}{"update_time": float64(1793185713000)}},
			want: "",
		},
		{
			name: "missing stays empty", platform: "xhs",
			item: map[string]interface{}{"note": map[string]interface{}{"title": "无时间"}},
			want: "",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := radarPublishedAtFor(testCase.platform, testCase.item, ""); got != testCase.want {
				t.Fatalf("published_at = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestRadarPublishedAtFallsBackOnlyWhenMissing(t *testing.T) {
	fetched := "2026-11-20T01:00:00Z"
	item := map[string]interface{}{"note": map[string]interface{}{"timestamp": float64(1793106043)}}
	if got := radarPublishedAtFor("xhs", item, fetched); got == fetched {
		t.Fatalf("a real publish time must win over the fetch fallback: %q", got)
	}
	empty := map[string]interface{}{"note": map[string]interface{}{}}
	if got := radarPublishedAtFor("xhs", empty, fetched); got != fetched {
		t.Fatalf("missing publish time should fall back: %q", got)
	}
}
