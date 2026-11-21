package tikhub

import "testing"

func TestParseDouyinVideoInputSupportsSharedTextAndModalURL(t *testing.T) {
	fixtures := []struct {
		name, input, kind, endpoint, videoID, normalized string
	}{
		{
			name:  "copied share sentence",
			input: "8.76 02/18 d@n.Qx :4pm IVl:/ # 火种情感 # 火种文化 # 火种投资 # 认知 # 情感  https://v.douyin.com/eer9qWKqopE/ 复制此链接，打开Dou音搜索，直接观看视频！",
			kind:  "share_url", endpoint: DouyinVideoShareEndpoint,
			normalized: "https://v.douyin.com/eer9qWKqopE/",
		},
		{
			name:  "profile modal URL",
			input: "https://www.douyin.com/user/MS4wLjABAAAAVnUiQxjaK_sEf-4Mr20CpL_USrzAP13PpJELL_sBbCiBv5J1aYzfcCbk6jnN5AFY?from_tab_name=main&modal_id=7666283984318131889",
			kind:  "video_id", endpoint: DouyinVideoDetailEndpoint,
			videoID: "7666283984318131889", normalized: "https://www.douyin.com/video/7666283984318131889",
		},
		{
			name: "pure ID", input: "7666283984318131889",
			kind: "video_id", endpoint: DouyinVideoDetailEndpoint,
			videoID: "7666283984318131889", normalized: "https://www.douyin.com/video/7666283984318131889",
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			got, err := ParseDouyinVideoInput(fixture.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != fixture.kind || got.EndpointID != fixture.endpoint ||
				got.VideoID != fixture.videoID || got.NormalizedURL != fixture.normalized {
				t.Fatalf("parsed=%#v", got)
			}
		})
	}
}

func TestParseDouyinVideoInputRejectsUnsafeOrNonVideoInput(t *testing.T) {
	for _, input := range []string{
		"http://v.douyin.com/eer9qWKqopE/",
		"https://example.com/video/7666283984318131889",
		"https://user:pass@v.douyin.com/eer9qWKqopE/",
		"https://www.douyin.com/user/creator-only",
		"没有链接",
	} {
		if _, err := ParseDouyinVideoInput(input); err == nil {
			t.Fatalf("unsafe input accepted: %q", input)
		}
	}
}
