package publishguard

import (
	"errors"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestSingleMediaPathRejectsMoreThanOne(t *testing.T) {
	if _, err := SingleMediaPath([]db.MediaAsset{{FilePath: "/one.png"}, {FilePath: "/two.png"}}, nil); !errors.Is(err, ErrMultipleMediaUnsupported) {
		t.Fatalf("multiple media err=%v", err)
	}
	if got, err := SingleMediaPath(nil, nil); err != nil || got != "" {
		t.Fatalf("zero media got=%q err=%v", got, err)
	}
	if got, err := SingleMediaPath([]db.MediaAsset{{OriginalPath: "/one.png"}}, nil); err != nil || got != "/one.png" {
		t.Fatalf("one media got=%q err=%v", got, err)
	}
}

func TestPublishMediaPayloadKeepsVideoAndCoverRoles(t *testing.T) {
	primary, media, err := PublishMediaPayload([]db.MediaAsset{
		{Kind: "video", FilePath: "/video.mp4", SHA256: "ABC"},
		{Kind: "image", FilePath: "/cover.jpg", SHA256: "DEF"},
	}, nil, "video")
	if err != nil || primary != "/video.mp4" || len(media) != 2 {
		t.Fatalf("primary=%q media=%+v err=%v", primary, media, err)
	}
	if media[0].Role != "video" || media[0].Position != 0 || media[0].SHA256 != "abc" {
		t.Fatalf("video=%+v", media[0])
	}
	if media[1].Role != "cover" || media[1].Position != 1 || media[1].SHA256 != "def" {
		t.Fatalf("cover=%+v", media[1])
	}
}

func TestPublishMediaPayloadKeepsGraphTextImagesInOrder(t *testing.T) {
	primary, media, err := PublishMediaPayload([]db.MediaAsset{
		{Kind: "image", OriginalPath: "/one.png"},
		{Kind: "image", FilePath: "/two.png"},
	}, nil, "graph_text")
	if err != nil || primary != "/one.png" || len(media) != 2 || media[0].Position != 0 || media[1].Position != 1 {
		t.Fatalf("primary=%q media=%+v err=%v", primary, media, err)
	}
}

func TestValidateTargetPlatformRejectsCrossPlatformAsset(t *testing.T) {
	asset := &db.AccountAsset{PlatformCode: "xhs"}
	if err := ValidateTargetPlatform(asset, "douyin"); !errors.Is(err, ErrTargetPlatformMismatch) {
		t.Fatalf("cross-platform target err=%v", err)
	}
	if err := ValidateTargetPlatform(asset, "xhs"); err != nil {
		t.Fatalf("matching target err=%v", err)
	}
}

func TestValidateTargetPlatformRejectsMetaBrowserContainer(t *testing.T) {
	asset := &db.AccountAsset{ID: 1, PlatformCode: "facebook", AssetType: "browser_account", Enabled: true}
	if err := ValidateTargetPlatform(asset, "facebook"); !errors.Is(err, ErrTargetAssetTypeInvalid) {
		t.Fatalf("err=%v, want %v", err, ErrTargetAssetTypeInvalid)
	}
	asset.AssetType = "facebook_page"
	if err := ValidateTargetPlatform(asset, "facebook"); err != nil {
		t.Fatalf("valid Facebook Page target rejected: %v", err)
	}
}

func TestValidateTargetOwnerRejectsCrossPlatformAccount(t *testing.T) {
	asset := &db.AccountAsset{ID: 1, AccountID: 2, PlatformCode: "facebook", AssetType: "facebook_page", Enabled: true}
	wrongOwner := &db.Account{ID: 2, PlatformCode: "xhs", Enabled: true}
	if err := ValidateTargetOwner(wrongOwner, asset, "facebook"); !errors.Is(err, ErrTargetOwnerPlatformMismatch) {
		t.Fatalf("cross-platform owner err=%v", err)
	}
	if err := ValidateTargetOwner(&db.Account{ID: 2, PlatformCode: "facebook", Enabled: true}, asset, "facebook"); err != nil {
		t.Fatalf("matching owner err=%v", err)
	}
	if err := ValidateTargetOwner(&db.Account{ID: 2, PlatformCode: "meta", Enabled: true}, asset, "facebook"); err != nil {
		t.Fatalf("combined meta owner err=%v", err)
	}
	if err := ValidateTargetOwner(&db.Account{ID: 2, PlatformCode: "instagram", Enabled: true}, asset, "facebook"); err != nil {
		t.Fatalf("Meta Business login owner err=%v", err)
	}
}

func TestValidateTargetOwnershipAllowsPersistedMetaBusinessOwner(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	owner, err := database.CreateAccount("instagram", "instagram owner", "")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := database.CreateAccountAsset(owner.ID, "facebook", "facebook_page", "wrong target", "page-1", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTargetOwnership(database, asset, "facebook"); err != nil {
		t.Fatalf("persisted Meta Business owner err=%v", err)
	}
}
