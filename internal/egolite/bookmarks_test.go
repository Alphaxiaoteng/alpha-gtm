package egolite_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/egolite"
)

type fakeBookmarkImporter struct {
	calls int
	mode  string
}

type spaceBookmarkRunner struct {
	programs []string
}

func (runner *spaceBookmarkRunner) Run(_ context.Context, program string, result any) error {
	runner.programs = append(runner.programs, program)
	return json.Unmarshal([]byte(`{"importedCount":1,"completed":true}`), result)
}

func (importer *fakeBookmarkImporter) Import(_ context.Context, request egolite.BookmarkImportRequest) (string, error) {
	importer.calls++
	if _, err := os.Stat(request.Path); err != nil {
		return "", err
	}
	return importer.mode, nil
}

func TestPlatformBookmarkHTMLUsesRequiredDomesticFirstOrder(t *testing.T) {
	html := string(egolite.BuildPlatformBookmarksHTML())
	names := []string{
		"抖音", "视频号", "快手", "小红书", "TikTok", "YouTube", "Meta", "微信公众号", "CSDN",
		"抖音电商", "淘宝", "TikTok Shop", "Shopee", "Shopify", "Amazon Seller",
	}
	previous := -1
	for _, name := range names {
		index := strings.Index(html, ">"+name+"</A>")
		if index < 0 || index <= previous {
			t.Fatalf("bookmark %q is missing or out of order", name)
		}
		previous = index
	}
	if strings.Count(html, "<DT><A HREF=") != 15 || !strings.Contains(html, ">Alpha-Nexus 平台</H3>") {
		t.Fatalf("unexpected bookmarks html: %s", html)
	}
	if !strings.Contains(html, `HREF="https://business.facebook.com/latest/home">Meta</A>`) {
		t.Fatalf("Meta bookmark must open Business Suite home: %s", html)
	}
	if !strings.Contains(html, `HREF="https://myseller.taobao.com/home.htm">淘宝</A>`) ||
		!strings.Contains(html, `HREF="https://sellercentral.amazon.com/">Amazon Seller</A>`) {
		t.Fatalf("e-commerce bookmarks must use reviewed seller-console routes: %s", html)
	}
}

func TestBookmarkImportIsIdempotentAndNeverUsesBrowserProfileFiles(t *testing.T) {
	database := openServiceDB(t)
	loggedIn, err := database.CreateAccountForIdentity(1, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(loggedIn.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	importer := &fakeBookmarkImporter{mode: "automatic"}
	service := egolite.NewBookmarkService(database, t.TempDir(), importer)

	first, err := service.Import(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Import(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "imported" || second.Status != "imported" || importer.calls != 2 {
		t.Fatalf("first=%+v second=%+v calls=%d", first, second, importer.calls)
	}
	if strings.Contains(first.Path, "/Default/") || strings.Contains(first.Path, "/Profile ") {
		t.Fatalf("bookmark file touched browser profile path: %s", first.Path)
	}
}

func TestBookmarkImportIncludesOnlyBrowserOneSelectedPlatforms(t *testing.T) {
	database := openServiceDB(t)
	loggedIn, err := database.CreateAccountForIdentity(1, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(loggedIn.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	loggedOut, err := database.CreateAccountForIdentity(1, "kuaishou", "快手", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(loggedOut.ID, db.AccountAuthLoggedOut); err != nil {
		t.Fatal(err)
	}
	otherBrowser, err := database.CreateEgoIdentity("浏览器 2")
	if err != nil {
		t.Fatal(err)
	}
	otherLoggedIn, err := database.CreateAccountForIdentity(otherBrowser.ID, "youtube", "YouTube", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(otherLoggedIn.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}

	service := egolite.NewBookmarkService(database, t.TempDir(), &fakeBookmarkImporter{mode: "automatic"})
	result, err := service.Import(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	if !strings.Contains(html, ">Alpha-Nexus 浏览器 1 已登录平台</H3>") ||
		!strings.Contains(html, `HREF="https://creator.douyin.com">抖音</A>`) {
		t.Fatalf("Browser 1 bookmark content=%s", html)
	}
	if !strings.Contains(html, `HREF="https://cp.kuaishou.com">快手</A>`) {
		t.Fatalf("selected media platform missing: %s", html)
	}
	for _, absent := range []string{">YouTube</A>"} {
		if strings.Contains(html, absent) {
			t.Fatalf("bookmark included an unlogged or another-browser platform %q: %s", absent, html)
		}
	}
}

func TestBookmarkImportNeverMarksUnsupportedSpaceImportAsComplete(t *testing.T) {
	database := openServiceDB(t)
	account, err := database.CreateAccountForIdentity(1, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	service := egolite.NewBookmarkService(database, t.TempDir(), nil)
	if _, err := service.Import(context.Background()); err == nil || err.Error() != "EGO_BOOKMARK_SPACE_IMPORT_UNSUPPORTED" {
		t.Fatalf("unexpected import error: %v", err)
	}
	var status, mode string
	if err := database.QueryRow(`SELECT status,mode FROM ego_bookmark_imports WHERE version=7`).Scan(&status, &mode); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || mode != "unsupported" {
		t.Fatalf("unsupported import was recorded as status=%q mode=%q", status, mode)
	}
}

func TestBookmarkImportUsesOnlyTheBoundBrowserOneSpace(t *testing.T) {
	database := openServiceDB(t)
	account, err := database.CreateAccountForIdentity(1, "douyin", "抖音", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateAccountLoginState(account.ID, db.AccountAuthLoggedIn); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateEgoSpaceForIdentity(1, 7, "alpha-nexus/matrix-1", "alpha-nexus/matrix-1"); err != nil {
		t.Fatal(err)
	}
	runner := &spaceBookmarkRunner{}
	spaces := egolite.NewSpaceService(database, runner)
	service := egolite.NewBookmarkService(database, t.TempDir(), egolite.NewSpaceBookmarkImporter(spaces))
	result, err := service.Import(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "automatic" || len(runner.programs) != 2 {
		t.Fatalf("result=%+v programs=%d", result, len(runner.programs))
	}
	program := runner.programs[0]
	for _, required := range []string{"useOrCreateTaskSpace(7)", "chrome.bookmarks.create", "chrome.bookmarks.update"} {
		if !strings.Contains(program, required) {
			t.Fatalf("bookmark program missing %q: %s", required, program)
		}
	}
	if strings.Contains(program, "takeOverTaskSpace") || strings.Contains(program, "osascript") || strings.Contains(program, "activate") {
		t.Fatalf("bookmark program escaped Browser 1 Space: %s", program)
	}
	if !strings.Contains(runner.programs[1], "completeTaskSpace(7, { keep: false })") {
		t.Fatalf("bookmark cleanup was not isolated: %s", runner.programs[1])
	}
}
