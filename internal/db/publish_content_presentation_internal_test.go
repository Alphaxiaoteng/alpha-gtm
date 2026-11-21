package db

import "testing"

func TestPublishContentPresentationUsesManagerMarker(t *testing.T) {
	for _, platformCode := range []string{"kuaishou", "wechat_video", "facebook"} {
		t.Run(platformCode, func(t *testing.T) {
			snapshot := `{"targets":[{"account_id":7,"platform_code":"` + platformCode + `","title":"页面标题","caption":"  后台匹配首行  \n第二行","body":"正文"}]}`
			title, cover := publishContentPresentation(snapshot, "回退标题", 7, platformCode)
			if title != "后台匹配首行" || cover != "" {
				t.Fatalf("title=%q cover=%q", title, cover)
			}
		})
	}
}
