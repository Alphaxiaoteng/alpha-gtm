package tikhub

import (
	"encoding/json"
	"strings"
	"testing"
)

// 抖音路径取自真实响应 tikhub/calls/2026-11-12/187_douyin_creator_profile.json
// （data.user.follower_count=74555），小红书取自 196_xhs_creator_profile.json
// （data.data.fans=3068）。客户端用 UseNumber 解码，所以 json.Number 是主路径。
func TestCreatorFollowerCountReadsRealPlatformShapes(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		response string
		want     int64
		wantOK   bool
	}{
		{
			name: "抖音 JSON 数字", platform: "douyin",
			response: `{"code":200,"data":{"user":{"follower_count":74555,"mplatform_followers_count":274572}}}`,
			want:     74555, wantOK: true,
		},
		{
			name: "抖音真实 0 是合法值", platform: "douyin",
			response: `{"code":200,"data":{"user":{"follower_count":0}}}`,
			want:     0, wantOK: true,
		},
		{
			name: "抖音字符串数字", platform: "douyin",
			response: `{"code":200,"data":{"user":{"follower_count":"12345"}}}`,
			want:     12345, wantOK: true,
		},
		{
			name: "抖音缺字段", platform: "douyin",
			response: `{"code":200,"data":{"user":{"nickname":"没有粉丝字段"}}}`,
			wantOK:   false,
		},
		{
			name: "抖音带单位字符串不猜单位", platform: "douyin",
			response: `{"code":200,"data":{"user":{"follower_count":"1.2万"}}}`,
			wantOK:   false,
		},
		{
			name: "抖音负值不接受", platform: "douyin",
			response: `{"code":200,"data":{"user":{"follower_count":-5}}}`,
			wantOK:   false,
		},
		{
			name: "小红书 JSON 数字", platform: "xhs",
			response: `{"code":200,"data":{"code":0,"success":true,"data":{"fans":3068,"follows":170}}}`,
			want:     3068, wantOK: true,
		},
		{
			name: "小红书缺字段", platform: "xhs",
			response: `{"code":200,"data":{"code":0,"success":true,"data":{"follows":170}}}`,
			wantOK:   false,
		},
		{
			name: "小红书 follows 不能当粉丝", platform: "xhs",
			response: `{"code":200,"data":{"code":0,"data":{"follows":170}}}`,
			wantOK:   false,
		},
		{
			name: "未支持平台", platform: "kuaishou",
			response: `{"code":200,"data":{"data":{"fans":100}}}`,
			wantOK:   false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := decodeFakeResponse(t, testCase.response)
			got, ok := CreatorFollowerCount(response, testCase.platform)
			if ok != testCase.wantOK {
				t.Fatalf("ok=%v want %v (value=%d)", ok, testCase.wantOK, got)
			}
			if ok && got != testCase.want {
				t.Fatalf("followers=%d want %d", got, testCase.want)
			}
		})
	}
}

func TestCreatorPostsHasMoreReadsPlatformShapes(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		response string
		want     bool
	}{
		{name: "抖音数字 1", platform: "douyin", response: `{"data":{"has_more":1}}`, want: true},
		{name: "抖音数字 0", platform: "douyin", response: `{"data":{"has_more":0}}`, want: false},
		{name: "小红书布尔 true", platform: "xhs", response: `{"data":{"data":{"has_more":true}}}`, want: true},
		{name: "小红书布尔 false", platform: "xhs", response: `{"data":{"data":{"has_more":false}}}`},
		{name: "小红书缺字段", platform: "xhs", response: `{"data":{"data":{"notes":[]}}}`},
		{name: "空响应", platform: "xhs", response: `{}`},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := decodeFakeResponse(t, testCase.response)
			if got := CreatorPostsHasMore(response, testCase.platform); got != testCase.want {
				t.Fatalf("has_more=%v want %v", got, testCase.want)
			}
		})
	}
}

// decodeFakeResponse mimics the provider client: JSON decoded with UseNumber.
func decodeFakeResponse(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&out); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return out
}
