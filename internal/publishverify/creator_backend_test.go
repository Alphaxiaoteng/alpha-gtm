//go:build legacy_chromium

package publishverify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type creatorBackendMatrixFixture struct {
	ID           string
	PlatformCode string
	ContentType  string
	ManagerURL   string
	PostURL      string
}

func alphaGamerCreatorBackendMatrix() []creatorBackendMatrixFixture {
	return []creatorBackendMatrixFixture{
		{ID: "AG-01", PlatformCode: "instagram", ContentType: "graph_text", ManagerURL: "https://business.facebook.com", PostURL: "https://www.instagram.com/p/AG-01/"},
		{ID: "AG-02", PlatformCode: "instagram", ContentType: "video", ManagerURL: "https://business.facebook.com", PostURL: "https://www.instagram.com/reel/AG-02/"},
		{ID: "AG-03", PlatformCode: "xhs", ContentType: "graph_text", ManagerURL: "https://creator.xiaohongshu.com", PostURL: "https://www.xiaohongshu.com/explore/AG-03"},
		{ID: "AG-04", PlatformCode: "xhs", ContentType: "video", ManagerURL: "https://creator.xiaohongshu.com", PostURL: "https://www.xiaohongshu.com/explore/AG-04"},
		{ID: "AG-05", PlatformCode: "douyin", ContentType: "graph_text", ManagerURL: "https://creator.douyin.com", PostURL: "https://www.douyin.com/note/AG-05"},
		{ID: "AG-06", PlatformCode: "douyin", ContentType: "video", ManagerURL: "https://creator.douyin.com", PostURL: "https://www.douyin.com/video/AG-06"},
		{ID: "AG-07", PlatformCode: "kuaishou", ContentType: "graph_text", ManagerURL: "https://cp.kuaishou.com", PostURL: "https://www.kuaishou.com/short-video/AG-07"},
		{ID: "AG-08", PlatformCode: "kuaishou", ContentType: "video", ManagerURL: "https://cp.kuaishou.com", PostURL: "https://www.kuaishou.com/short-video/AG-08"},
		{ID: "AG-09", PlatformCode: "wechat_video", ContentType: "video", ManagerURL: "https://channels.weixin.qq.com", PostURL: "https://channels.weixin.qq.com/post/AG-09"},
		{ID: "AG-10", PlatformCode: "wechat_mp", ContentType: "article", ManagerURL: "https://mp.weixin.qq.com/", PostURL: "https://mp.weixin.qq.com/s/AG-10"},
		{ID: "AG-11", PlatformCode: "csdn", ContentType: "article", ManagerURL: "https://mp.csdn.net", PostURL: "https://blog.csdn.net/alphagamer/article/details/AG-11"},
		{ID: "AG-12", PlatformCode: "facebook", ContentType: "graph_text", ManagerURL: "https://business.facebook.com", PostURL: "https://www.facebook.com/alphagamer/posts/AG-12"},
		{ID: "AG-13", PlatformCode: "facebook", ContentType: "video", ManagerURL: "https://business.facebook.com", PostURL: "https://www.facebook.com/alphagamer/videos/AG-13"},
		{ID: "AG-14", PlatformCode: "youtube", ContentType: "video", ManagerURL: "https://studio.youtube.com", PostURL: "https://youtu.be/AG-14"},
		{ID: "AG-15", PlatformCode: "tiktok", ContentType: "graph_text", ManagerURL: "https://www.tiktok.com/tiktokstudio/content", PostURL: "https://www.tiktok.com/@alphagamer/photo/AG-15"},
		{ID: "AG-16", PlatformCode: "tiktok", ContentType: "video", ManagerURL: "https://www.tiktok.com/tiktokstudio/content", PostURL: "https://www.tiktok.com/@alphagamer/video/AG-16"},
	}
}

func TestCreatorBackendMatrixFixturesCoverTenPlatformsAndSixteenForms(t *testing.T) {
	fixtures := alphaGamerCreatorBackendMatrix()
	if len(fixtures) != 16 {
		t.Fatalf("fixture count=%d, want 16", len(fixtures))
	}
	platforms := map[string]int{}
	forms := map[string]bool{}
	submittedAt := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	for index, fixture := range fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			platforms[fixture.PlatformCode]++
			forms[fixture.PlatformCode+"/"+fixture.ContentType] = true
			expected, snapshot := creatorBackendFixturePair(fixture, index, submittedAt)

			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseCreatorBackendSnapshot(fixture.PlatformCode, raw, pngFixture())
			if err != nil {
				t.Fatal(err)
			}
			item, matched := MatchCreatorBackend(expected, parsed)
			if !matched || item.ExternalPostID != fixture.ID {
				t.Fatalf("matched=%v item=%+v", matched, item)
			}
		})
	}
	if len(platforms) != 10 {
		t.Fatalf("platform count=%d platforms=%v", len(platforms), platforms)
	}
	wantForms := map[string]bool{
		"instagram/graph_text": true, "instagram/video": true,
		"xhs/graph_text": true, "xhs/video": true,
		"douyin/graph_text": true, "douyin/video": true,
		"kuaishou/graph_text": true, "kuaishou/video": true,
		"wechat_video/video": true, "wechat_mp/article": true, "csdn/article": true,
		"facebook/graph_text": true, "facebook/video": true, "youtube/video": true,
		"tiktok/graph_text": true, "tiktok/video": true,
	}
	if len(forms) != len(wantForms) {
		t.Fatalf("form count=%d forms=%v", len(forms), forms)
	}
	for form := range wantForms {
		if !forms[form] {
			t.Fatalf("missing fixture form %q", form)
		}
	}
}

func TestMatchCreatorBackendFailsClosedForEveryRequiredEvidenceDimension(t *testing.T) {
	submittedAt := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	for index, fixture := range alphaGamerCreatorBackendMatrix() {
		t.Run(fixture.ID, func(t *testing.T) {
			expected, snapshot := creatorBackendFixturePair(fixture, index, submittedAt)
			cases := map[string]func(*CreatorBackendExpectation, *CreatorBackendSnapshot){
				"unknown platform": func(expected *CreatorBackendExpectation, _ *CreatorBackendSnapshot) {
					expected.PlatformCode = "unknown"
				},
				"wrong snapshot platform": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.PlatformCode = "unknown"
				},
				"unsupported content type": func(expected *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					expected.ContentType = "unsupported"
					snapshot.Items[0].ContentType = "unsupported"
				},
				"wrong manager": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.ManagerURL = "https://example.com/content"
				},
				"wrong target name": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.TargetName = "other"
				},
				"wrong target external id": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.TargetExternalID = "other"
				},
				"non-published status": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.Items[0].Status = "Processing"
				},
				"wrong content": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.Items[0].Title = "other"
					snapshot.Items[0].Caption = "other"
					snapshot.Items[0].Body = "other"
					snapshot.Items[0].ContentSHA256 = strings.Repeat("a", 64)
				},
				"wrong media": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.Items[0].MediaSHA256 = strings.Repeat("a", 64)
					snapshot.Items[0].ThumbnailSHA256 = strings.Repeat("b", 64)
				},
				"outside publish window": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.Items[0].PublishedAt = submittedAt.Add(-3 * time.Minute).Format(time.RFC3339)
				},
				"wrong public host": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.Items[0].PostURL = "https://example.com/" + fixture.ID
				},
				"public url id mismatch": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.Items[0].PostURL = strings.Replace(fixture.PostURL, fixture.ID, "other", 1)
				},
				"missing external id": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.Items[0].ExternalPostID = ""
				},
				"non-png screenshot": func(_ *CreatorBackendExpectation, snapshot *CreatorBackendSnapshot) {
					snapshot.ScreenshotPNG = []byte("not png")
				},
			}
			for name, mutate := range cases {
				t.Run(name, func(t *testing.T) {
					expectedCopy := expected
					snapshotCopy := snapshot
					snapshotCopy.Items = append([]CreatorBackendItem(nil), snapshot.Items...)
					snapshotCopy.ScreenshotPNG = append([]byte(nil), snapshot.ScreenshotPNG...)
					mutate(&expectedCopy, &snapshotCopy)
					if item, matched := MatchCreatorBackend(expectedCopy, snapshotCopy); matched {
						t.Fatalf("unexpected match: %+v", item)
					}
				})
			}
		})
	}
}

func TestMatchCreatorBackendAcceptsExactBodyFingerprintWhenManagerOmitsText(t *testing.T) {
	fixture := alphaGamerCreatorBackendMatrix()[10]
	submittedAt := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	expected, snapshot := creatorBackendFixturePair(fixture, 10, submittedAt)
	snapshot.Items[0].Title = ""
	snapshot.Items[0].Caption = ""
	snapshot.Items[0].Body = ""
	snapshot.Items[0].ContentSHA256 = normalizedTextSHA256(expected.Body)

	if item, matched := MatchCreatorBackend(expected, snapshot); !matched || item.ExternalPostID != fixture.ID {
		t.Fatalf("matched=%v item=%+v", matched, item)
	}
}

func TestParseCreatorBackendSnapshotRequiresExplicitMatchingPlatformAndPNG(t *testing.T) {
	fixture := alphaGamerCreatorBackendMatrix()[0]
	expected, snapshot := creatorBackendFixturePair(
		fixture, 0, time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC),
	)
	_ = expected
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseCreatorBackendSnapshot("facebook", raw, pngFixture()); err == nil {
		t.Fatal("cross-platform snapshot was accepted")
	}
	snapshot.PlatformCode = ""
	raw, _ = json.Marshal(snapshot)
	if _, err := ParseCreatorBackendSnapshot("instagram", raw, pngFixture()); err == nil {
		t.Fatal("snapshot without explicit platform was accepted")
	}
	snapshot.PlatformCode = "instagram"
	raw, _ = json.Marshal(snapshot)
	if _, err := ParseCreatorBackendSnapshot("instagram", raw, []byte("not png")); err == nil {
		t.Fatal("snapshot without PNG evidence was accepted")
	}
	if _, err := ParseCreatorBackendSnapshot("instagram", append(raw, []byte(` {}`)...), pngFixture()); err == nil {
		t.Fatal("snapshot with trailing JSON was accepted")
	}
	unknownField := append([]byte(nil), raw[:len(raw)-1]...)
	unknownField = append(unknownField, []byte(`,"broad_page_text":"Published"}`)...)
	if _, err := ParseCreatorBackendSnapshot("instagram", unknownField, pngFixture()); err == nil {
		t.Fatal("snapshot with unknown broad page text was accepted")
	}
}

func TestMatchCreatorBackendAcceptsOnlyExplicitPublishedStatuses(t *testing.T) {
	fixture := alphaGamerCreatorBackendMatrix()[0]
	expected, snapshot := creatorBackendFixturePair(
		fixture, 0, time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC),
	)
	for _, status := range []string{"Published", "Public", "已发布", "已发表"} {
		t.Run(status, func(t *testing.T) {
			snapshotCopy := snapshot
			snapshotCopy.Items = append([]CreatorBackendItem(nil), snapshot.Items...)
			snapshotCopy.Items[0].Status = status
			if _, matched := MatchCreatorBackend(expected, snapshotCopy); !matched {
				t.Fatalf("explicit status %q did not match", status)
			}
		})
	}
	for _, status := range []string{"published", "公开", "Checks complete", "审核中", "Draft"} {
		t.Run("reject-"+status, func(t *testing.T) {
			snapshotCopy := snapshot
			snapshotCopy.Items = append([]CreatorBackendItem(nil), snapshot.Items...)
			snapshotCopy.Items[0].Status = status
			if item, matched := MatchCreatorBackend(expected, snapshotCopy); matched {
				t.Fatalf("non-explicit status %q matched: %+v", status, item)
			}
		})
	}
}

func TestUnverifiedCreatorBackendObserverFailsClosedForAllTenPlatforms(t *testing.T) {
	for platformCode := range creatorBackendPlatformContracts() {
		t.Run(platformCode, func(t *testing.T) {
			observer := UnverifiedCreatorBackendObserver{PlatformCode: platformCode}
			_, err := observer.Observe(context.Background(), "http://127.0.0.1:15101", CreatorBackendExpectation{
				PlatformCode: platformCode,
			})
			if !errors.Is(err, ErrCreatorBackendDOMUnverified) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func creatorBackendFixturePair(fixture creatorBackendMatrixFixture, index int, submittedAt time.Time) (CreatorBackendExpectation, CreatorBackendSnapshot) {
	title := "αgamer｜阿尔法玩家，正式入驻"
	caption := "你好，这里是 αgamer｜阿尔法玩家。\n本条封面与动态视觉由 AI 生成。"
	body := "只把能够核对的信息写成确定事实；本条视觉由 AI 生成。"
	mediaHash := fixtureSHA256(fmt.Sprintf("%s/%s/%d", fixture.PlatformCode, fixture.ContentType, index))
	coverHash := fixtureSHA256("cover/" + fixture.ID)
	targetName := "αgamer-" + fixture.PlatformCode
	targetExternalID := "target-" + fixture.PlatformCode
	expected := CreatorBackendExpectation{
		PlatformCode:     fixture.PlatformCode,
		ContentType:      fixture.ContentType,
		TargetName:       targetName,
		TargetExternalID: targetExternalID,
		Title:            title,
		Caption:          caption,
		Body:             body,
		MediaSHA256:      []string{mediaHash, coverHash},
		SubmittedAt:      submittedAt,
		PublishedBefore:  submittedAt.Add(10 * time.Minute),
	}
	snapshot := CreatorBackendSnapshot{
		PlatformCode:     fixture.PlatformCode,
		ManagerURL:       fixture.ManagerURL,
		TargetName:       targetName,
		TargetExternalID: targetExternalID,
		Items: []CreatorBackendItem{{
			ContentType:     fixture.ContentType,
			Status:          "Published",
			Title:           title,
			Caption:         caption,
			Body:            body,
			ContentSHA256:   normalizedTextSHA256(body),
			MediaSHA256:     mediaHash,
			ThumbnailSHA256: coverHash,
			PublishedAt:     submittedAt.Add(time.Duration(index+1) * time.Second).Format(time.RFC3339),
			PostURL:         fixture.PostURL,
			ExternalPostID:  fixture.ID,
		}},
		ScreenshotPNG: pngFixture(),
	}
	return expected, snapshot
}

func fixtureSHA256(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
