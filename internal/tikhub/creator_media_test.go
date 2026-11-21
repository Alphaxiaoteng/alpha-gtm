package tikhub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// creatorPostsResponse mirrors the real shape returned by
// douyin.creator_posts: data.aweme_list[].video.cover.url_list holds the cover
// variants (HEIC first, JPEG second) and music.play_url holds the audio.
func creatorPostsResponse() map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"has_more":   1,
			"max_cursor": 1789638008000,
			"aweme_list": []interface{}{
				map[string]interface{}{
					"aweme_id":    "7678643711023369146",
					"desc":        "第一条作品",
					"create_time": 1793000000,
					"statistics": map[string]interface{}{
						"digg_count":    631,
						"comment_count": 17,
						"collect_count": 212,
						"share_count":   100,
						"play_count":    8000,
					},
					"video": map[string]interface{}{
						"duration": 83934,
						"cover": map[string]interface{}{
							"url_list": []interface{}{
								"https://p3-sign.douyinpic.com/cover.heic",
								"https://p3-sign.douyinpic.com/cover.jpeg",
							},
						},
						"origin_cover": map[string]interface{}{
							"url_list": []interface{}{"https://p3-sign.douyinpic.com/origin.heic"},
						},
					},
					"music": map[string]interface{}{
						"play_url": map[string]interface{}{
							"url_list": []interface{}{"https://lf9-music-east.douyinstatic.com/obj/ies-music/1.mp3"},
						},
					},
				},
				map[string]interface{}{
					"aweme_id":    "7659704534466612709",
					"desc":        "第二条作品",
					"create_time": 1792000000,
					"video": map[string]interface{}{
						"cover": map[string]interface{}{
							"url_list": []interface{}{"https://p3-sign.douyinpic.com/second.jpeg"},
						},
					},
					// 该作品没有原声（真实数据里约 4/23 如此）。
				},
				// 缺少 aweme_id 的脏数据必须被跳过，而不是产出无主文件。
				map[string]interface{}{"desc": "无 ID"},
			},
		},
	}
}

func TestExtractCreatorPostMediaPrefersViewableCoverAndReadsAudio(t *testing.T) {
	items := ExtractCreatorPostMedia(creatorPostsResponse())
	if len(items) != 2 {
		t.Fatalf("items=%d want 2 (entry without aweme_id must be skipped)", len(items))
	}
	first := items[0]
	if first.AwemeID != "7678643711023369146" {
		t.Fatalf("aweme_id=%q", first.AwemeID)
	}
	// HEIC 排在前面但不能被选中：多数浏览器和编辑器无法打开。
	if !strings.HasSuffix(first.CoverURL, "cover.jpeg") {
		t.Fatalf("cover=%q, want the jpeg variant", first.CoverURL)
	}
	if !strings.HasSuffix(first.AudioURL, "1.mp3") {
		t.Fatalf("audio=%q", first.AudioURL)
	}
	if first.Title != "第一条作品" {
		t.Fatalf("title=%q", first.Title)
	}
	if first.Likes != 631 || first.Comments != 17 || first.Collects != 212 || first.Shares != 100 || first.Play != 8000 {
		t.Fatalf("unexpected statistics: likes=%d comments=%d collects=%d shares=%d play=%d",
			first.Likes, first.Comments, first.Collects, first.Shares, first.Play)
	}
	if first.DurationMS != 83934 {
		t.Fatalf("duration_ms=%d want 83934", first.DurationMS)
	}
	if items[1].AudioURL != "" {
		t.Fatalf("second post has no original sound, got %q", items[1].AudioURL)
	}
}

func TestExtractCreatorPostMediaFallsBackWhenOnlyHEICExists(t *testing.T) {
	items := ExtractCreatorPostMedia(creatorPostsResponse())
	if !strings.HasSuffix(items[1].CoverURL, "second.jpeg") {
		t.Fatalf("cover=%q", items[1].CoverURL)
	}
	// 只有 HEIC 时仍要给出可下载地址，而不是丢弃封面。
	response := map[string]interface{}{
		"data": map[string]interface{}{
			"aweme_list": []interface{}{
				map[string]interface{}{
					"aweme_id": "1",
					"video": map[string]interface{}{
						"cover": map[string]interface{}{"url_list": []interface{}{"https://p3.douyinpic.com/only.heic"}},
					},
				},
			},
		},
	}
	got := ExtractCreatorPostMedia(response)
	if len(got) != 1 || !strings.HasSuffix(got[0].CoverURL, "only.heic") {
		t.Fatalf("got=%#v", got)
	}
}

func TestAllowedMediaURLAcceptsAudioCDNsButNotUnknownHosts(t *testing.T) {
	for _, raw := range []string{
		"https://lf9-music-east.douyinstatic.com/obj/ies-music/1.mp3",
		"https://api-play.amemv.com/aweme/v1/play/",
		"https://p3-sign.douyinpic.com/cover.jpeg",
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !allowedMediaURL(parsed) {
			t.Fatalf("expected %s to be allowed", raw)
		}
	}
	for _, raw := range []string{
		"https://evil.example.com/a.mp3",
		"http://lf9-music-east.douyinstatic.com/1.mp3", // 明文拒绝
		"https://notdouyinstatic.com/1.mp3",            // 后缀必须落在点上
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if allowedMediaURL(parsed) {
			t.Fatalf("expected %s to be rejected", raw)
		}
	}
}

func TestDownloadCreatorMediaWritesCoversAndAudioIntoCreatorFolder(t *testing.T) {
	image := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	audio := []byte("ID3\x03\x00\x00\x00\x00\x00\x00")
	var hits int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		if strings.HasSuffix(r.URL.Path, ".mp3") {
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write(audio)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(image)
	}))
	defer server.Close()

	items := []CreatorPostMedia{
		{AwemeID: "111", CoverURL: server.URL + "/a.jpg", AudioURL: server.URL + "/a.mp3"},
		{AwemeID: "222", CoverURL: server.URL + "/b.jpg"},
	}
	dataDir := t.TempDir()
	result, err := downloadCreatorMedia(context.Background(), dataDir, "douyin", "user-1", items,
		CreatorMediaOptions{Workers: 4}, server.Client(), func(*url.URL) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if result.Covers != 2 || result.Audio != 1 || result.Failed != 0 {
		t.Fatalf("result=%#v", result)
	}

	root := CreatorFolder(dataDir, "douyin", "user-1")
	// 媒体必须落在博主文件夹内，且带本地相对路径。
	for _, relative := range []string{"media/covers/111.jpg", "media/covers/222.jpg", "media/audio/111.mp3"} {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatalf("missing %s: %v", relative, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s is empty", relative)
		}
	}
	if items[0].CoverPath != "media/covers/111.jpg" {
		t.Fatalf("cover path=%q", items[0].CoverPath)
	}
	if items[0].AudioPath != "media/audio/111.mp3" {
		t.Fatalf("audio path=%q", items[0].AudioPath)
	}
	if items[1].AudioPath != "" {
		t.Fatalf("post without audio must not claim a path, got %q", items[1].AudioPath)
	}

	// 二次运行必须复用已有文件，不再打网络。
	before := atomic.LoadInt64(&hits)
	again, err := downloadCreatorMedia(context.Background(), dataDir, "douyin", "user-1", items,
		CreatorMediaOptions{Workers: 4}, server.Client(), func(*url.URL) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if again.Skipped != 3 || again.Covers != 0 || again.Audio != 0 {
		t.Fatalf("rerun=%#v want all skipped", again)
	}
	if atomic.LoadInt64(&hits) != before {
		t.Fatalf("rerun hit the network: %d -> %d", before, atomic.LoadInt64(&hits))
	}
}

func TestDownloadCreatorMediaRejectsNonMediaPayloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>blocked</html>"))
	}))
	defer server.Close()

	dataDir := t.TempDir()
	items := []CreatorPostMedia{{AwemeID: "333", CoverURL: server.URL + "/cover.jpg"}}
	result, err := downloadCreatorMedia(context.Background(), dataDir, "douyin", "user-2", items,
		CreatorMediaOptions{Workers: 2}, server.Client(), func(*url.URL) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 || result.Covers != 0 {
		t.Fatalf("result=%#v, an HTML error page must not be archived as a cover", result)
	}
	if items[0].CoverPath != "" {
		t.Fatalf("failed download must not report a path, got %q", items[0].CoverPath)
	}
}

// client.do 用 json.Decoder.UseNumber 解码，真实响应里的数值字段到达时是
// json.Number。旧的 toInt64 没有这个分支，所有指标都会落 0，看着像真实数据。
func TestExtractCreatorPostMediaReadsJSONNumberCounters(t *testing.T) {
	body := `{"data":{"aweme_list":[{"aweme_id":"7300000000000000001","desc":"示例作品",` +
		`"create_time":1735689600,` +
		`"statistics":{"play_count":1234567,"digg_count":88,"comment_count":12,` +
		`"collect_count":9007199254740993,"share_count":3},` +
		`"video":{"duration":15000,` +
		`"cover":{"url_list":["https://p3-sign.douyinpic.com/number.jpeg"]}}}]}}`
	var response map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	items := ExtractCreatorPostMedia(response)
	if len(items) != 1 {
		t.Fatalf("items=%d want 1", len(items))
	}
	item := items[0]
	// collect_count 超过 2^53：只有走 Int64() 才能保住精度。
	if item.CreateTime != 1735689600 || item.Play != 1234567 || item.Likes != 88 ||
		item.Comments != 12 || item.Collects != 9007199254740993 || item.Shares != 3 ||
		item.DurationMS != 15000 {
		t.Fatalf("json.Number counters were dropped: %+v", item)
	}
}

func TestToInt64ReadsEveryDecodedNumberShape(t *testing.T) {
	cases := []struct {
		name  string
		input interface{}
		want  int64
	}{
		{"json integer", json.Number("1234"), 1234},
		{"json integer beyond float64 precision", json.Number("9007199254740993"), 9007199254740993},
		{"json fractional falls back to float", json.Number("12.9"), 12},
		{"json garbage", json.Number("not-a-number"), 0},
		{"plain float64", float64(7), 7},
		{"plain int", 42, 42},
		{"numeric string", "314", 314},
		{"nil", nil, 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := toInt64(testCase.input); got != testCase.want {
				t.Fatalf("toInt64(%#v)=%d want %d", testCase.input, got, testCase.want)
			}
		})
	}
}

func TestSortCreatorMediaNewestFirstIsStable(t *testing.T) {
	items := []CreatorPostMedia{
		{AwemeID: "a", CreateTime: 100},
		{AwemeID: "c", CreateTime: 300},
		{AwemeID: "b", CreateTime: 200},
	}
	SortCreatorMediaNewestFirst(items)
	if items[0].AwemeID != "c" || items[2].AwemeID != "a" {
		t.Fatalf("order=%v", []string{items[0].AwemeID, items[1].AwemeID, items[2].AwemeID})
	}
}

func TestAllowedMediaURLFollowsOnlyVerifiedCDNRedirects(t *testing.T) {
	// 真实场景：lf9-music-east.douyinstatic.com 会 302 到 bsgslb.cn 负载均衡域名，
	// 并在 real_host 里用 base64 声明真实源站。
	realHost := base64.RawURLEncoding.EncodeToString([]byte("lf9-music-east.douyinstatic.com"))
	trusted := "https://v9-default.tt.x.bsgslb.cn/lf9-music-east.douyinstatic.com/obj/ies-music-hj/1.mp3?real_host=" + realHost
	parsed, err := url.Parse(trusted)
	if err != nil {
		t.Fatal(err)
	}
	if !allowedMediaURL(parsed) {
		t.Fatalf("verified CDN redirect must be allowed: %s", trusted)
	}

	// 同样的负载均衡域名，但声称的源站不可信时必须拒绝。
	evilHost := base64.RawURLEncoding.EncodeToString([]byte("evil.example.com"))
	for _, raw := range []string{
		"https://v9-default.tt.x.bsgslb.cn/1.mp3?real_host=" + evilHost,
		"https://v9-default.tt.x.bsgslb.cn/1.mp3", // 没有 real_host 声明
		"https://v9-default.tt.x.bsgslb.cn/1.mp3?real_host=!!!notbase64!!!",
		"https://v9-default.tt.x.bsgslb.cn/1.mp3?real_host=" + base64.RawURLEncoding.EncodeToString([]byte("../../etc/passwd")),
	} {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if allowedMediaURL(parsed) {
			t.Fatalf("unverified CDN redirect must be rejected: %s", raw)
		}
	}
}

func TestDownloadCreatorMediaReportsJobTotalsAndFailureReasons(t *testing.T) {
	image := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".mp3") {
			// 音频源站返回 HTML 错误页，必须被拒绝且记录原因。
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>nope</html>"))
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(image)
	}))
	defer server.Close()

	items := []CreatorPostMedia{
		{AwemeID: "aaa", CoverURL: server.URL + "/a.jpg", AudioURL: server.URL + "/a.mp3"},
		{AwemeID: "bbb", CoverURL: server.URL + "/b.jpg", AudioURL: server.URL + "/b.mp3"},
	}
	result, err := downloadCreatorMedia(context.Background(), t.TempDir(), "douyin", "user-3", items,
		CreatorMediaOptions{Workers: 3}, server.Client(), func(*url.URL) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	// Total 统计下载任务（封面 2 + 音频 2），而不是作品数。
	if result.Total != 4 {
		t.Fatalf("total=%d want 4 download jobs", result.Total)
	}
	if result.Covers != 2 || result.Audio != 0 || result.Failed != 2 {
		t.Fatalf("result=%#v", result)
	}
	if result.Covers+result.Audio+result.Skipped+result.Failed != result.Total {
		t.Fatalf("counters must add up to total: %#v", result)
	}
	if len(result.Failures) != 2 {
		t.Fatalf("failures=%#v, want one entry per failed asset", result.Failures)
	}
	for _, failure := range result.Failures {
		if failure.Kind != "audio" || failure.Reason == "" {
			t.Fatalf("failure=%#v must name the kind and reason", failure)
		}
	}
}

// 取消/超时只能说明这批任务没跑完，不能算成功：旧实现累计 Failed 之后仍然
// return nil，调用方会把半截归档当成完整归档。
func TestDownloadCreatorMediaReportsCancelledContextAsError(t *testing.T) {
	image := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(image)
	}))
	defer server.Close()

	items := []CreatorPostMedia{
		{AwemeID: "444", CoverURL: server.URL + "/a.jpg", AudioURL: server.URL + "/a.mp3"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := downloadCreatorMedia(ctx, t.TempDir(), "douyin", "user-4", items,
		CreatorMediaOptions{Workers: 2}, server.Client(), func(*url.URL) bool { return true })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation must be reported, result=%#v err=%v", result, err)
	}
	// 两个任务都没完成，计数必须如实，且不能伪装成封面/音频成功。
	if result.Failed != 2 || result.Covers != 0 || result.Audio != 0 {
		t.Fatalf("result=%#v", result)
	}
}
