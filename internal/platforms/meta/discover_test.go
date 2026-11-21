//go:build legacy_chromium

package meta

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestChooseMetaBusinessTargetUsesExistingBusinessSuitePage(t *testing.T) {
	got, err := chooseMetaBusinessTarget([]devtoolsTarget{
		{ID: "worker", Type: "service_worker", URL: "https://business.facebook.com/sw"},
		{ID: "other", Type: "page", URL: "https://example.com"},
		{ID: "meta-page", Type: "page", URL: "https://business.facebook.com/latest/home?asset_id=123"},
	})
	if err != nil || got != "meta-page" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestChooseMetaBusinessTargetPrefersAlreadyOpenUnifiedComposer(t *testing.T) {
	got, err := chooseMetaBusinessTarget([]devtoolsTarget{
		{ID: "meta-home", Type: "page", URL: "https://business.facebook.com/latest/home?asset_id=123"},
		{ID: "instagram", Type: "page", URL: "https://www.instagram.com/"},
		{ID: "meta-composer", Type: "page", URL: "https://business.facebook.com/latest/composer/?asset_id=123"},
	})
	if err != nil || got != "meta-composer" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestMetaBusinessTargetIDRejectsNonLoopbackCDPBeforeLookup(t *testing.T) {
	if _, err := metaBusinessTargetID(context.Background(), "http://example.com:9222"); err == nil || err.Error() != "META_DISCOVER_CDP_INVALID" {
		t.Fatalf("err=%v", err)
	}
}

func TestValidateDiscoverCDPURLAllowsOnlyLoopbackEndpoint(t *testing.T) {
	for _, raw := range []string{
		"", "https://127.0.0.1:9222", "http://example.com:9222",
		"http://user:pass@127.0.0.1:9222", "http://127.0.0.1:9222/json",
	} {
		if _, err := validateDiscoverCDPURL(raw); err == nil {
			t.Fatalf("expected invalid CDP URL: %q", raw)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:9222", "http://localhost:9222", "http://[::1]:9222"} {
		if got, err := validateDiscoverCDPURL(raw); err != nil || got != raw {
			t.Fatalf("raw=%q got=%q err=%v", raw, got, err)
		}
	}
}

func TestNormalizeDiscoveredNamesDeduplicatesAndRejectsNoise(t *testing.T) {
	got := normalizeDiscoveredNames([]string{" Page A ", "Page A", "", "Instagram", "A\nB"})
	if len(got) != 1 || got[0] != "Page A" {
		t.Fatalf("got=%v", got)
	}
}

func TestDiscoveredTargetsShareOneMetaBusinessLogin(t *testing.T) {
	pages := []string{"Page A"}
	instagram := []string{"IG A"}
	for _, platform := range []string{"facebook", "instagram", "meta"} {
		gotPages, gotInstagram := discoveredTargetsForAccountPlatform(platform, pages, instagram)
		if len(gotPages) != 1 || len(gotInstagram) != 1 {
			t.Fatalf("%s targets pages=%v instagram=%v", platform, gotPages, gotInstagram)
		}
	}
}

func TestDiscoverTargetsFromBusinessHomeExtractsLinkedMetaAssets(t *testing.T) {
	pageURL := "https://business.facebook.com/latest/home?business_id=1708507717147261&asset_id=1241410172381837"
	html := `<script type="application/json">{
		"selected_bizlink_instagram":{
			"username":"chenpaiteng",
			"linked_page":{"id":"1241410172381837"},
			"__isNode":"InstagramUserV2",
			"id":"17841416733781085"
		},
		"asset_type":"PAGE",
		"asset_id":"1241410172381837",
		"name":"AlphaIntelligence"
	}</script>`

	pages, instagram := discoverTargetsFromBusinessHome(pageURL, html)
	if len(pages) != 1 || pages[0].Name != "AlphaIntelligence" || pages[0].ExternalID != "1241410172381837" {
		t.Fatalf("pages=%+v", pages)
	}
	if len(instagram) != 1 || instagram[0].Name != "chenpaiteng" || instagram[0].ExternalID != "17841416733781085" {
		t.Fatalf("instagram=%+v", instagram)
	}
	if instagram[0].ConnectedExternalID != "1241410172381837" {
		t.Fatalf("connected page id=%q", instagram[0].ConnectedExternalID)
	}
}

func TestSaveDiscoveredAssetsLinksVerifiedFacebookAndInstagramTargets(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	account, err := database.CreateAccount("instagram", "Meta login", "")
	if err != nil {
		t.Fatal(err)
	}

	err = saveDiscoveredAssets(database, account.ID,
		[]discoveredTarget{{Name: "AlphaIntelligence", ExternalID: "1241410172381837"}},
		[]discoveredTarget{{Name: "chenpaiteng", ExternalID: "17841416733781085", ConnectedExternalID: "1241410172381837"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := database.ListAccountAssetsByAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	byPlatform := map[string]db.AccountAsset{}
	for _, asset := range assets {
		if asset.AssetType == "facebook_page" || asset.AssetType == "instagram_account" {
			byPlatform[asset.PlatformCode] = asset
		}
	}
	if len(byPlatform) != 2 {
		t.Fatalf("publish assets=%+v all=%+v", byPlatform, assets)
	}
	facebook := byPlatform["facebook"]
	instagram := byPlatform["instagram"]
	if facebook.ConnectedAssetID != instagram.ID || facebook.ConnectedAssetName != instagram.Name {
		t.Fatalf("facebook relation=%+v instagram=%+v", facebook, instagram)
	}
	if instagram.ConnectedAssetID != facebook.ID || instagram.ConnectedAssetName != facebook.Name {
		t.Fatalf("instagram relation=%+v facebook=%+v", instagram, facebook)
	}
}

func TestEnsureMetaPageTargetCreatesBusinessSuiteTabWhenBrowserHasNoPage(t *testing.T) {
	var newCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/list":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case "/json/new":
			if r.Method != http.MethodPut {
				t.Errorf("method=%s", r.Method)
			}
			if r.URL.Query().Encode() != "https%3A%2F%2Fbusiness.facebook.com%2F=" {
				t.Errorf("query=%q", r.URL.RawQuery)
			}
			newCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"meta-page","type":"page","url":"https://business.facebook.com/"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := ensureMetaPageTarget(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	if newCalls.Load() != 1 {
		t.Fatalf("new page calls=%d", newCalls.Load())
	}
}
