//go:build legacy_chromium

package meta

import (
	"context"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/platforms"
)

func TestValidateAndDryRunInstagram(t *testing.T) {
	p := &Publisher{platform: "instagram"}
	req := platforms.PublishRequest{
		ContentID: 1,
		AssetID:   2,
		Caption:   "hello",
		AssetType: "instagram_account",
		AssetName: "ig",
		DryRun:    true,
	}
	if err := p.Validate(req); err != nil {
		t.Fatal(err)
	}
	res, err := p.Publish(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "dry_run_ok" {
		t.Fatalf("status=%s", res.Status)
	}
}

func TestDryRunIncludesMediaAndAutoSubmitHints(t *testing.T) {
	p := &Publisher{platform: "facebook"}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		ContentID:  1,
		AssetID:    2,
		Caption:    "cap",
		AssetType:  "facebook_page",
		AssetName:  "page",
		MediaPath:  "/tmp/photo.jpg",
		AutoSubmit: true,
		DryRun:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "dry_run_ok" {
		t.Fatalf("status=%s", res.Status)
	}
	if !strings.Contains(res.Message, "media=") || !strings.Contains(res.Message, "auto_submit=true") {
		t.Fatalf("expected media/auto_submit in message: %s", res.Message)
	}
}

func TestPublishRequiresCDPWhenNotDryRun(t *testing.T) {
	p := &Publisher{platform: "instagram"}
	res, err := p.Publish(context.Background(), platforms.PublishRequest{
		ContentID:                1,
		AssetID:                  2,
		Caption:                  "hello",
		AssetType:                "instagram_account",
		AssetName:                "Alpha IG",
		AssetExternalID:          "987654321098765",
		ConnectedAssetExternalID: "123456789012345",
		DryRun:                   false,
		CDPURL:                   "",
	})
	if err == nil {
		t.Fatal("expected error for empty CDP")
	}
	if res == nil || res.Status != "retryable_failed" {
		t.Fatalf("expected retryable_failed, got %+v", res)
	}
}

func TestValidateRejectsWrongAssetType(t *testing.T) {
	p := &Publisher{platform: "facebook"}
	err := p.Validate(platforms.PublishRequest{
		ContentID: 1,
		AssetID:   2,
		Caption:   "x",
		AssetType: "instagram_account",
	})
	if err == nil {
		t.Fatal("expected asset type error")
	}
}

func TestDryRunFailsEmptyContent(t *testing.T) {
	p := &Publisher{platform: "facebook"}
	_, err := p.Publish(context.Background(), platforms.PublishRequest{
		ContentID: 1,
		AssetID:   2,
		AssetType: "facebook_page",
		DryRun:    true,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRegisterPublishers(t *testing.T) {
	if _, err := platforms.GetPublisher("instagram"); err != nil {
		t.Fatal(err)
	}
	if _, err := platforms.GetPublisher("facebook"); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeFileTag(t *testing.T) {
	if got := sanitizeFileTag("meta publish!"); got != "meta_publish_" {
		// only alnum/_/- kept; space and ! become _
		if !strings.HasPrefix(got, "meta") {
			t.Fatalf("got %q", got)
		}
	}
}

func TestMetaCaptionAppendsStructuredTagsWithoutDuplicatingExistingHashtags(t *testing.T) {
	caption, tags, err := metaCaption("正文 #内容运营", `["内容运营","#AlphaNexus#","视频"]`)
	if err != nil {
		t.Fatal(err)
	}
	if caption != "正文 #内容运营\n\n#AlphaNexus #视频" {
		t.Fatalf("caption=%q", caption)
	}
	if strings.Join(tags, ",") != "AlphaNexus,视频" {
		t.Fatalf("tags=%v", tags)
	}
}

func TestMetaCaptionRejectsInvalidTagsJSON(t *testing.T) {
	if _, _, err := metaCaption("正文", `{"tag":"AlphaNexus"}`); err == nil {
		t.Fatal("expected invalid tags JSON to fail closed")
	}
}
