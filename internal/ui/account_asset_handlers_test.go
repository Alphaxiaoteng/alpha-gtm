package ui

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/config"
	"alpha-nexus-egolite/internal/db"
)

func TestAccountAssetListAndAvatarServeOnlyManagedSnapshot(t *testing.T) {
	dataDir := t.TempDir()
	server, err := NewServer(config.Config{DataDir: dataDir, UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("meta", "Meta", "")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := server.db.CreateAccountAsset(account.ID, "instagram", "instagram_account", "Alpha", "", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(dataDir, "account-asset-avatars")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	reference := filepath.ToSlash(filepath.Join("account-asset-avatars", "asset-"+strconv.FormatInt(asset.ID, 10)+"-fixture.png"))
	file, err := os.OpenFile(filepath.Join(dataDir, filepath.FromSlash(reference)), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			canvas.Set(x, y, color.RGBA{R: uint8(20 + x*3), G: uint8(40 + y*4), B: 180, A: 255})
		}
	}
	if err := png.Encode(file, canvas); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.db.UpdateAccountAssetAvatar(asset.ID, reference); err != nil {
		t.Fatal(err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/account-assets", nil)
	listRecorder := httptest.NewRecorder()
	server.ServeHTTP(listRecorder, listReq)
	var assets []db.AccountAsset
	if err := json.NewDecoder(listRecorder.Body).Decode(&assets); err != nil {
		t.Fatal(err)
	}
	wantAvatarPath := "/api/account-assets/" + strconv.FormatInt(asset.ID, 10) + "/avatar"
	listedAvatar := ""
	for _, listed := range assets {
		if listed.ID == asset.ID {
			listedAvatar = listed.Avatar
		}
	}
	if !strings.HasPrefix(listedAvatar, wantAvatarPath+"?v=") {
		t.Fatalf("managed avatar URL missing: %+v", assets)
	}

	avatarReq := httptest.NewRequest(http.MethodGet, wantAvatarPath, nil)
	avatarReq.SetPathValue("id", strconv.FormatInt(asset.ID, 10))
	avatarRecorder := httptest.NewRecorder()
	server.ServeHTTP(avatarRecorder, avatarReq)
	if avatarRecorder.Code != http.StatusOK || avatarRecorder.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("avatar status=%d content-type=%s body=%s", avatarRecorder.Code, avatarRecorder.Header().Get("Content-Type"), avatarRecorder.Body.String())
	}
}

func TestMetaAccountAssetCreateAPI(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("facebook", "brand page manager", "")
	if err != nil {
		t.Fatal(err)
	}

	create := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/assets", bytes.NewBufferString(`{"name":"Alpha Nexus Page","externalId":"page-123"}`))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		return recorder
	}

	created := create()
	if created.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", created.Code, created.Body.String())
	}
	var asset db.AccountAsset
	if err := json.NewDecoder(created.Body).Decode(&asset); err != nil {
		t.Fatal(err)
	}
	if asset.AccountID != account.ID || asset.PlatformCode != "facebook" || asset.AssetType != "facebook_page" || asset.ExternalID != "page-123" {
		t.Fatalf("asset=%+v", asset)
	}
	assets, err := server.db.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 2 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
	events, err := server.db.ListAuditEvents(10)
	if err != nil || len(events) == 0 || events[0].RelatedType != "account_asset" || events[0].RelatedID != asset.ID {
		t.Fatalf("audit events=%+v err=%v", events, err)
	}

	if duplicate := create(); duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
}

func TestCombinedMetaAccountCreatesBothTargetTypesWithoutSplittingLogin(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("meta", "combined meta login", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"platform":"facebook","name":"Alpha Page","externalId":"page-123"}`,
		`{"platform":"instagram","name":"Alpha IG","externalId":"ig-123"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/assets", bytes.NewBufferString(body))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("body=%s status=%d response=%s", body, recorder.Code, recorder.Body.String())
		}
	}
	assets, err := server.db.ListAccountAssetsByAccount(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	var facebook, instagram bool
	for _, asset := range assets {
		facebook = facebook || asset.PlatformCode == "facebook" && asset.AssetType == "facebook_page"
		instagram = instagram || asset.PlatformCode == "instagram" && asset.AssetType == "instagram_account"
	}
	if !facebook || !instagram {
		t.Fatalf("assets=%+v", assets)
	}
}

func TestLegacyInstagramLoginCanConfigureBothMetaTargetTypes(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("instagram", "existing Meta login", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"platform":"facebook","name":"Alpha Page","externalId":"page-123"}`,
		`{"platform":"instagram","name":"Alpha IG","externalId":"ig-123"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/assets", bytes.NewBufferString(body))
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("body=%s status=%d response=%s", body, recorder.Code, recorder.Body.String())
		}
	}
}

func TestMetaAccountAssetRejectsUnsupportedAccount(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("xhs", "not meta", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/assets", bytes.NewBufferString(`{"name":"not allowed","externalId":"xhs-1"}`))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAccountAssetListDoesNotExposeRemoteAvatar(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("facebook", "remote avatar fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	asset, err := server.db.CreateAccountAsset(
		account.ID,
		"facebook",
		"facebook_page",
		"Alpha Page",
		"page-public-id",
		"https://cdn.example/avatar.png?signature=synthetic",
		0,
		"",
	)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/account-assets", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	var assets []db.AccountAsset
	if err := json.NewDecoder(recorder.Body).Decode(&assets); err != nil {
		t.Fatal(err)
	}
	for _, listed := range assets {
		if listed.ID == asset.ID && listed.Avatar != "" {
			t.Fatal("account asset API exposed a provider avatar URL")
		}
	}
}

func TestMetaAccountAssetConfirmDiscoveredTarget(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("facebook", "discovery account", "")
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := server.db.CreateAccountAsset(account.ID, "facebook", "facebook_page", "Alpha Page", "", "https://cdn.example/avatar.png?signature=synthetic", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/assets", bytes.NewBufferString(`{"name":"alpha page","externalId":"page-456"}`))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var confirmed db.AccountAsset
	if err := json.NewDecoder(recorder.Body).Decode(&confirmed); err != nil {
		t.Fatal(err)
	}
	if confirmed.ID != discovered.ID || confirmed.ExternalID != "page-456" || confirmed.Name != "alpha page" || confirmed.Avatar != "" {
		t.Fatalf("confirmed=%+v discovered=%+v", confirmed, discovered)
	}
	assets, err := server.db.ListAccountAssetsByAccount(account.ID)
	if err != nil || len(assets) != 2 {
		t.Fatalf("assets=%+v err=%v", assets, err)
	}
}

func TestLegacyMetaAccountAssetDiscoverRouteIsUnavailable(t *testing.T) {
	server, err := NewServer(config.Config{DataDir: t.TempDir(), UIPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	account, err := server.db.CreateAccount("facebook", "discovery account", "")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/"+strconv.FormatInt(account.ID, 10)+"/assets/discover", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
