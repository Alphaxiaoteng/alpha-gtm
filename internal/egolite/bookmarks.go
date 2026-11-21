package egolite

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alpha-nexus-egolite/internal/db"
)

const platformBookmarksVersion = 7

type PlatformBookmark struct {
	Name         string `json:"name"`
	URL          string `json:"url"`
	ToolbarTitle string `json:"toolbarTitle,omitempty"`
}

var platformBookmarks = []PlatformBookmark{
	{Name: "抖音", URL: "https://creator.douyin.com/"},
	{Name: "视频号", URL: "https://channels.weixin.qq.com/platform"},
	{Name: "快手", URL: "https://cp.kuaishou.com/"},
	{Name: "小红书", URL: "https://creator.xiaohongshu.com/"},
	{Name: "TikTok", URL: "https://www.tiktok.com/tiktokstudio"},
	{Name: "YouTube", URL: "https://studio.youtube.com/"},
	{Name: "Meta", URL: "https://business.facebook.com/latest/home"},
	{Name: "微信公众号", URL: "https://mp.weixin.qq.com/"},
	{Name: "CSDN", URL: "https://mp.csdn.net/"},
	{Name: "抖音电商", URL: "https://fxg.jinritemai.com/"},
	{Name: "淘宝", URL: "https://myseller.taobao.com/home.htm"},
	{Name: "TikTok Shop", URL: "https://seller.tiktokglobalshop.com/account/login"},
	{Name: "Shopee", URL: "https://seller.shopee.cn/"},
	{Name: "Shopify", URL: "https://www.shopify.com/login"},
	{Name: "Amazon Seller", URL: "https://sellercentral.amazon.com/"},
}

func BuildPlatformBookmarksHTML() []byte {
	return buildPlatformBookmarksHTML("Alpha-Nexus 平台", platformBookmarks)
}

// BuildLoggedInPlatformBookmarksHTML is the Browser 1 bookmark payload. Its
// input is resolved from the local account truth before import, so it cannot
// expose URLs for a platform that is not currently marked logged in.
func BuildLoggedInPlatformBookmarksHTML(bookmarks []PlatformBookmark) []byte {
	return buildPlatformBookmarksHTML("Alpha-Nexus 浏览器 1 已登录平台", bookmarks)
}

func buildPlatformBookmarksHTML(folderName string, bookmarks []PlatformBookmark) []byte {
	var output strings.Builder
	output.WriteString("<!DOCTYPE NETSCAPE-Bookmark-file-1>\n")
	output.WriteString(`<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">` + "\n")
	folderName = strings.TrimSpace(folderName)
	if folderName == "" {
		folderName = "Alpha-Nexus 平台"
	}
	fmt.Fprintf(&output, "<TITLE>%s</TITLE>\n<H1>%s</H1>\n<DL><p>\n", html.EscapeString(folderName), html.EscapeString(folderName))
	fmt.Fprintf(&output, "  <DT><H3>%s</H3>\n  <DL><p>\n", html.EscapeString(folderName))
	for _, bookmark := range bookmarks {
		fmt.Fprintf(&output, "    <DT><A HREF=\"%s\">%s</A>\n", html.EscapeString(bookmark.URL), html.EscapeString(bookmark.Name))
	}
	output.WriteString("  </DL><p>\n</DL><p>\n")
	return []byte(output.String())
}

type BookmarkImporter interface {
	Import(context.Context, BookmarkImportRequest) (string, error)
}

type BookmarkImportRequest struct {
	IdentityID int64
	Bookmarks  []PlatformBookmark
	Path       string
}

// SpaceBookmarkImporter adds bookmarks through the identity-bound EgoLite
// Space. It never activates the globally focused desktop window or touches a
// browser profile file.
type SpaceBookmarkImporter struct {
	spaces *SpaceService
}

func NewSpaceBookmarkImporter(spaces *SpaceService) SpaceBookmarkImporter {
	return SpaceBookmarkImporter{spaces: spaces}
}

func (importer SpaceBookmarkImporter) Import(ctx context.Context, request BookmarkImportRequest) (string, error) {
	if importer.spaces == nil || request.IdentityID <= 0 || len(request.Bookmarks) == 0 {
		return "", fmt.Errorf("EGO_BOOKMARK_SPACE_IMPORT_UNAVAILABLE")
	}
	space, err := importer.spaces.db.GetEgoSpaceByIdentity(request.IdentityID)
	if err != nil {
		return "", fmt.Errorf("EGO_SPACE_BINDING_MISSING")
	}
	bookmarksJSON, err := json.Marshal(request.Bookmarks)
	if err != nil {
		return "", fmt.Errorf("EGO_BOOKMARK_INPUT_INVALID")
	}
	mutationScriptJSON, err := json.Marshal(string(bookmarkMutationScript(bookmarksJSON)))
	if err != nil {
		return "", fmt.Errorf("EGO_BOOKMARK_INPUT_INVALID")
	}
	program := fmt.Sprintf(`const spaces = await listTaskSpaces()
const currentSpace = spaces.find((item) => Number(item.id) === %d)
if (!currentSpace) throw new Error("EGO_SPACE_BINDING_MISSING")
if (currentSpace.ownership === "agentDelegatedToUser" || currentSpace.ownership === "user") {
  throw new Error("EGO_SPACE_USER_CONTROLLED")
}
await useOrCreateTaskSpace(%d)
const bookmarks = %s
await openOrReuseTab("chrome://bookmarks", { wait: true, timeout: 30 })
const bookmarkMutationScript = %s
const rawMutation = await js(bookmarkMutationScript)
const mutation = typeof rawMutation === "string" ? JSON.parse(rawMutation) : rawMutation
if (!mutation || Number(mutation.verified) !== bookmarks.length) {
  throw new Error("EGO_BOOKMARK_SPACE_IMPORT_INCOMPLETE")
}

cliLog(%q + JSON.stringify({ importedCount: Number(mutation.verified) }))
`, space.SpaceID, space.SpaceID, string(bookmarksJSON), string(mutationScriptJSON), resultPrefix)
	var result struct {
		ImportedCount int `json:"importedCount"`
	}
	if err := importer.spaces.runtime.Run(ctx, program, &result); err != nil {
		return "", err
	}
	if result.ImportedCount != len(request.Bookmarks) {
		return "", fmt.Errorf("EGO_BOOKMARK_SPACE_IMPORT_INCOMPLETE")
	}
	cleanupProgram := fmt.Sprintf(`await useOrCreateTaskSpace(%d)
const completed = await completeTaskSpace(%d, { keep: false })
cliLog(%q + JSON.stringify({ completed: Boolean(completed?.done) }))
`, space.SpaceID, space.SpaceID, resultPrefix)
	var cleanup struct {
		Completed bool `json:"completed"`
	}
	_ = importer.spaces.runtime.Run(ctx, cleanupProgram, &cleanup)
	return "automatic", nil
}

type BookmarkImportResult struct {
	Status  string `json:"status"`
	Mode    string `json:"mode"`
	Path    string `json:"path"`
	Version int    `json:"version"`
}

type BookmarkService struct {
	db       *db.DB
	dataDir  string
	importer BookmarkImporter
}

func NewBookmarkService(database *db.DB, dataDir string, importer BookmarkImporter) *BookmarkService {
	return &BookmarkService{db: database, dataDir: dataDir, importer: importer}
}

// Import synchronizes Browser 1 with the selected platform management URLs.
// It deliberately reads only account metadata; browser profiles and
// authentication storage remain outside this flow.
func (service *BookmarkService) Import(ctx context.Context) (*BookmarkImportResult, error) {
	return service.ImportForIdentity(ctx, 1)
}

// ImportForIdentity mirrors selected platform accounts into the requested
// matrix's native bookmark bar. It is safe to invoke after a user changes the
// platform selection because owned bookmark titles are updated in place.
func (service *BookmarkService) ImportForIdentity(ctx context.Context, identityID int64) (*BookmarkImportResult, error) {
	if service == nil || service.db == nil || strings.TrimSpace(service.dataDir) == "" {
		return nil, fmt.Errorf("EGO_BOOKMARK_INPUT_INVALID")
	}
	if identityID <= 0 {
		return nil, fmt.Errorf("EGO_BOOKMARK_INPUT_INVALID")
	}
	bookmarks, err := service.selectedPlatformBookmarks(identityID)
	if err != nil {
		return nil, err
	}
	if len(bookmarks) == 0 {
		return nil, fmt.Errorf("EGO_BOOKMARK_NO_SELECTED_PLATFORM")
	}
	path := filepath.Join(service.dataDir, "bookmarks", fmt.Sprintf("alpha-nexus-browser-%d-platforms.html", identityID))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "bookmarks-*.html")
	if err != nil {
		return nil, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return nil, err
	}
	if _, err := temp.Write(BuildLoggedInPlatformBookmarksHTML(bookmarks)); err != nil {
		_ = temp.Close()
		return nil, err
	}
	if err := temp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return nil, err
	}
	mode := "unsupported"
	if service.importer == nil {
		service.recordImportAttempt("failed", mode, path)
		return nil, fmt.Errorf("EGO_BOOKMARK_SPACE_IMPORT_UNSUPPORTED")
	}
	importedMode, err := service.importer.Import(ctx, BookmarkImportRequest{IdentityID: identityID, Bookmarks: bookmarks, Path: path})
	if strings.TrimSpace(importedMode) != "" {
		mode = importedMode
	}
	if err != nil || mode != "automatic" {
		service.recordImportAttempt("failed", mode, path)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("EGO_BOOKMARK_SPACE_IMPORT_UNSUPPORTED")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := service.db.Exec(`INSERT INTO ego_bookmark_imports(version,status,mode,path,created_at,updated_at)
		VALUES (?,'imported',?,?,?,?)
		ON CONFLICT(version) DO UPDATE SET status=excluded.status,mode=excluded.mode,path=excluded.path,updated_at=excluded.updated_at`, platformBookmarksVersion, mode, path, now, now); err != nil {
		return nil, err
	}
	return &BookmarkImportResult{Status: "imported", Mode: mode, Path: path, Version: platformBookmarksVersion}, nil
}

func (service *BookmarkService) recordImportAttempt(status, mode, path string) {
	if service == nil || service.db == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = service.db.Exec(`INSERT INTO ego_bookmark_imports(version,status,mode,path,created_at,updated_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(version) DO UPDATE SET status=excluded.status,mode=excluded.mode,path=excluded.path,updated_at=excluded.updated_at`,
		platformBookmarksVersion, status, mode, path, now, now)
}

func (service *BookmarkService) selectedPlatformBookmarks(identityID int64) ([]PlatformBookmark, error) {
	rows, err := service.db.Query(`SELECT p.code,p.name,p.manager_url
		FROM accounts AS a
		JOIN platforms AS p ON p.code=a.platform_code
		WHERE a.identity_id=? AND a.enabled=1 AND p.enabled=1
		  AND TRIM(COALESCE(p.manager_url,''))<>''
		ORDER BY p.sort, a.id`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bookmarks := make([]PlatformBookmark, 0)
	seen := make(map[string]struct{})
	for rows.Next() {
		var code string
		var bookmark PlatformBookmark
		if err := rows.Scan(&code, &bookmark.Name, &bookmark.URL); err != nil {
			return nil, err
		}
		bookmark.Name = strings.TrimSpace(bookmark.Name)
		bookmark.URL = strings.TrimSpace(bookmark.URL)
		if bookmark.Name == "" || bookmark.URL == "" {
			continue
		}
		bookmark.ToolbarTitle = bookmarkToolbarTitle(code, bookmark.Name)
		if _, exists := seen[bookmark.URL]; exists {
			continue
		}
		seen[bookmark.URL] = struct{}{}
		bookmarks = append(bookmarks, bookmark)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return bookmarks, nil
}

func bookmarkToolbarTitle(_ string, name string) string {
	return strings.TrimSpace(name)
}

func bookmarkMutationScript(bookmarksJSON []byte) []byte {
	return []byte(fmt.Sprintf(`(async () => {
  const desired = %s;
  const normalizeURL = (value) => {
    try { return new URL(String(value)).href; } catch { return String(value ?? ""); }
  };
  const roots = await chrome.bookmarks.getTree();
  const walk = (nodes) => {
    for (const node of nodes || []) {
      if (node.folderType === "bookmarks-bar") return node;
      const nested = walk(node.children);
      if (nested) return nested;
    }
    return null;
  };
  const bar = walk(roots);
  if (!bar?.id) throw new Error("EGO_BOOKMARK_BAR_MISSING");
  const existing = await chrome.bookmarks.getChildren(bar.id);
  const managed = existing.filter((item) => item.url);
  for (const bookmark of desired) {
    const title = String(bookmark.toolbarTitle || bookmark.name || "").trim();
    const url = String(bookmark.url || "").trim();
    if (!title || !url) continue;
    const legacyTitles = new Set([title, "媒体 · " + title, "商城 · " + title]);
    const prior = managed.find((item) => legacyTitles.has(String(item.title || "")) || normalizeURL(item.url) === normalizeURL(url));
    if (prior) await chrome.bookmarks.update(prior.id, { title, url });
    else await chrome.bookmarks.create({ parentId: bar.id, title, url });
  }
  const after = await chrome.bookmarks.getChildren(bar.id);
  const verified = desired.filter((bookmark) => {
    const title = String(bookmark.toolbarTitle || bookmark.name || "").trim();
    const url = normalizeURL(bookmark.url);
    return after.some((item) => item.title === title && normalizeURL(item.url) === url);
  }).length;
  return JSON.stringify({ verified });
})()`, string(bookmarksJSON)))
}
