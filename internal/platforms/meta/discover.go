//go:build legacy_chromium

package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"alpha-nexus-egolite/internal/db"
)

// Discover scans the currently open Meta Business Suite page for connected
// Facebook Pages and Instagram accounts, saving them as account_assets.
func Discover(ctx context.Context, cdpURL string, accountID int64, database *db.DB) (*DiscoverResult, error) {
	result := &DiscoverResult{}
	if database == nil || accountID <= 0 {
		return result, fmt.Errorf("database and account_id are required")
	}
	account, err := database.GetAccount(accountID)
	if err != nil {
		return result, err
	}
	if account.PlatformCode != "meta" && account.PlatformCode != "facebook" && account.PlatformCode != "instagram" {
		return result, fmt.Errorf("account %d is not a Meta account", accountID)
	}
	if !account.Enabled {
		return result, fmt.Errorf("ACCOUNT_DISABLED: account %d is disabled", accountID)
	}
	endpoint, err := validateDiscoverCDPURL(cdpURL)
	if err != nil {
		return result, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	targetID, err := metaBusinessTargetID(ctx, endpoint)
	if err != nil {
		return result, err
	}
	// Discovery observes a BrowserFacade-owned tab. Keep its ownership
	// independent from request cancellation and detach without closing it.
	ownershipCtx, ownershipCancel := context.WithCancel(context.Background())
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(ownershipCtx, endpoint)
	tabCtx, tabCancel := chromedp.NewContext(allocCtx, chromedp.WithTargetID(targetID))
	browserCtx, actionCancel := context.WithTimeout(tabCtx, 30*time.Second)
	stopCaller := context.AfterFunc(ctx, actionCancel)
	defer func() {
		stopCaller()
		actionCancel()
		if chromedpContext := chromedp.FromContext(tabCtx); chromedpContext != nil && chromedpContext.Target != nil {
			chromedpContext.Target.TargetID = ""
		}
		tabCancel()
		allocCancel()
		ownershipCancel()
	}()

	// Meta's current Business Suite home already carries the verified Page and
	// linked Instagram IDs in its bootstrap data. Prefer it over settings URLs,
	// which change often and previously left us with unpublishable name-only rows.
	var currentURL, pageHTML string
	_ = chromedp.Run(browserCtx,
		chromedp.Location(&currentURL),
		chromedp.OuterHTML("html", &pageHTML, chromedp.ByQuery),
	)
	pageTargets, instagramTargets := discoverTargetsFromBusinessHome(currentURL, pageHTML)

	if len(pageTargets) == 0 {
		err = chromedp.Run(browserCtx,
			chromedp.Navigate("https://business.facebook.com/settings/pages"),
			chromedp.Sleep(5*time.Second),
			chromedp.WaitReady("body", chromedp.ByQuery),
		)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("facebook settings: %v", err))
		} else {
			var pageNames []string
			err = chromedp.Run(browserCtx,
				chromedp.Evaluate(`
			(() => {
				const pages = [];
				// Keep extraction narrow: broad list-item selectors can turn menu
				// labels into fake account assets.
				const selectors = [
					'[data-testid="page-name"]',
					'a[href*="/settings/pages/"]',
					'a[href*="/pages/"]',
				];
				for (const sel of selectors) {
					document.querySelectorAll(sel).forEach(el => {
						const text = el.textContent.trim();
						if (text && text.length > 0 && text.length < 100) {
							pages.push(text);
						}
					});
				}
				// Fallback: look for links that might be page names
				if (pages.length === 0) {
					document.querySelectorAll('a').forEach(el => {
						const href = el.getAttribute('href') || '';
						const text = el.textContent.trim();
						if (href.includes('/pages/') && text.length > 0 && text.length < 100) {
							pages.push(text);
						}
					});
				}
				return Array.from(new Set(pages));
			})()
		`, &pageNames),
			)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("extract pages: %v", err))
			} else {
				pageTargets = targetsFromNames(normalizeDiscoveredNames(pageNames))
			}
		}
	}

	if len(instagramTargets) == 0 {
		err = chromedp.Run(browserCtx,
			chromedp.Navigate("https://business.facebook.com/settings/instagram-accounts"),
			chromedp.Sleep(5*time.Second),
			chromedp.WaitReady("body", chromedp.ByQuery),
		)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("instagram settings: %v", err))
		} else {
			var igNames []string
			err = chromedp.Run(browserCtx,
				chromedp.Evaluate(`
				(() => {
					const accounts = [];
					document.querySelectorAll('[data-testid="instagram-account-name"], a[href*="instagram.com/"]').forEach(el => {
						const text = el.textContent.trim();
						if (text && text.length > 0 && text.length < 100 && !text.includes('Instagram')) {
							accounts.push(text);
						}
					});
					return Array.from(new Set(accounts));
				})()
			`, &igNames),
			)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("extract instagram accounts: %v", err))
			} else {
				instagramTargets = targetsFromNames(normalizeDiscoveredNames(igNames))
			}
		}
	}

	pageNames := discoveredTargetNames(pageTargets)
	igNames := discoveredTargetNames(instagramTargets)
	pageNames, igNames = discoveredTargetsForAccountPlatform(account.PlatformCode, pageNames, igNames)

	if err := saveDiscoveredAssets(database, accountID, pageTargets, instagramTargets); err != nil {
		return result, err
	}
	result.FacebookPages = pageNames
	result.InstagramAccounts = igNames

	result.Success = len(result.Errors) == 0
	return result, nil
}

type devtoolsTarget struct {
	ID   target.ID `json:"id"`
	Type string    `json:"type"`
	URL  string    `json:"url"`
}

func ensureMetaPageTarget(ctx context.Context, endpoint string) error {
	endpoint, err := validateDiscoverCDPURL(endpoint)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	listRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/json/list", nil)
	if err != nil {
		return fmt.Errorf("META_PAGE_TARGET_MISSING")
	}
	response, err := client.Do(listRequest)
	if err != nil {
		return fmt.Errorf("META_PAGE_TARGET_MISSING")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("META_PAGE_TARGET_MISSING")
	}
	var targets []devtoolsTarget
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&targets); err != nil {
		return fmt.Errorf("META_PAGE_TARGET_MISSING")
	}
	for _, candidate := range targets {
		if candidate.Type == "page" && candidate.ID != "" {
			return nil
		}
	}

	// A running account browser may legitimately have no open tab. Create one
	// inside that same BrowserFacade-owned session; never restart the browser or
	// touch its profile.
	newURL := strings.TrimRight(endpoint, "/") + "/json/new?" + url.QueryEscape("https://business.facebook.com/")
	newRequest, err := http.NewRequestWithContext(ctx, http.MethodPut, newURL, nil)
	if err != nil {
		return fmt.Errorf("META_PAGE_TARGET_MISSING")
	}
	newResponse, err := client.Do(newRequest)
	if err != nil {
		return fmt.Errorf("META_PAGE_TARGET_MISSING")
	}
	defer newResponse.Body.Close()
	if newResponse.StatusCode < http.StatusOK || newResponse.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("META_PAGE_TARGET_MISSING")
	}
	var created devtoolsTarget
	if err := json.NewDecoder(io.LimitReader(newResponse.Body, 1<<20)).Decode(&created); err != nil ||
		created.Type != "page" || created.ID == "" {
		return fmt.Errorf("META_PAGE_TARGET_MISSING")
	}
	return nil
}

func metaBusinessTargetID(ctx context.Context, endpoint string) (target.ID, error) {
	endpoint, err := validateDiscoverCDPURL(endpoint)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/json/list", nil)
	if err != nil {
		return "", fmt.Errorf("META_DISCOVER_TARGET_MISSING")
	}
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		return "", fmt.Errorf("META_DISCOVER_TARGET_MISSING")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("META_DISCOVER_TARGET_MISSING")
	}
	var targets []devtoolsTarget
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&targets); err != nil {
		return "", fmt.Errorf("META_DISCOVER_TARGET_MISSING")
	}
	return chooseMetaBusinessTarget(targets)
}

func chooseMetaBusinessTarget(targets []devtoolsTarget) (target.ID, error) {
	for _, candidate := range targets {
		if candidate.Type == "page" &&
			strings.HasPrefix(candidate.URL, "https://business.facebook.com/latest/composer") &&
			candidate.ID != "" {
			return candidate.ID, nil
		}
	}
	for _, candidate := range targets {
		if candidate.Type == "page" && strings.HasPrefix(candidate.URL, "https://business.facebook.com/") && candidate.ID != "" {
			return candidate.ID, nil
		}
	}
	return "", fmt.Errorf("META_DISCOVER_TARGET_MISSING")
}

type discoveredTarget struct {
	Name                string
	ExternalID          string
	ConnectedExternalID string
}

var (
	metaPageAssetPattern  = regexp.MustCompile(`"asset_type"\s*:\s*"PAGE"\s*,\s*"asset_id"\s*:\s*"([0-9]+)"\s*,\s*"name"\s*:\s*"((?:\\.|[^"])*)"`)
	metaIGNamePattern     = regexp.MustCompile(`"username"\s*:\s*"((?:\\.|[^"])*)"`)
	metaIGIDPattern       = regexp.MustCompile(`"__isNode"\s*:\s*"InstagramUserV2"\s*,\s*"id"\s*:\s*"([0-9]+)"`)
	metaLinkedPagePattern = regexp.MustCompile(`"linked_page"\s*:\s*\{[^{}]*"id"\s*:\s*"([0-9]+)"`)
)

func discoverTargetsFromBusinessHome(rawURL, html string) ([]discoveredTarget, []discoveredTarget) {
	html = strings.ReplaceAll(html, "&quot;", `"`)
	currentPageID := ""
	if parsed, err := url.Parse(strings.TrimSpace(rawURL)); err == nil {
		currentPageID = strings.TrimSpace(parsed.Query().Get("asset_id"))
	}

	var pages []discoveredTarget
	for _, match := range metaPageAssetPattern.FindAllStringSubmatch(html, -1) {
		if len(match) != 3 || !validMetaAssetID(match[1]) || (currentPageID != "" && currentPageID != match[1]) {
			continue
		}
		name := decodeJSONString(match[2])
		if len(normalizeDiscoveredNames([]string{name})) == 0 {
			continue
		}
		pages = appendUniqueDiscoveredTarget(pages, discoveredTarget{Name: name, ExternalID: match[1]})
	}

	var instagram []discoveredTarget
	const marker = `"selected_bizlink_instagram":`
	if start := strings.Index(html, marker); start >= 0 {
		segment := html[start:]
		if end := strings.Index(segment, `"selected_whatsapp"`); end >= 0 {
			segment = segment[:end]
		}
		nameMatch := metaIGNamePattern.FindStringSubmatch(segment)
		idMatch := metaIGIDPattern.FindStringSubmatch(segment)
		linkedPageMatch := metaLinkedPagePattern.FindStringSubmatch(segment)
		if len(nameMatch) == 2 && len(idMatch) == 2 && validMetaAssetID(idMatch[1]) {
			name := decodeJSONString(nameMatch[1])
			if len(normalizeDiscoveredNames([]string{name})) > 0 {
				connectedExternalID := ""
				if len(linkedPageMatch) == 2 && validMetaAssetID(linkedPageMatch[1]) {
					connectedExternalID = linkedPageMatch[1]
				}
				instagram = appendUniqueDiscoveredTarget(instagram, discoveredTarget{
					Name:                name,
					ExternalID:          idMatch[1],
					ConnectedExternalID: connectedExternalID,
				})
			}
		}
	}
	return pages, instagram
}

func decodeJSONString(raw string) string {
	decoded, err := strconv.Unquote(`"` + raw + `"`)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	return strings.TrimSpace(decoded)
}

func validMetaAssetID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func appendUniqueDiscoveredTarget(targets []discoveredTarget, candidate discoveredTarget) []discoveredTarget {
	for _, target := range targets {
		if target.ExternalID == candidate.ExternalID || strings.EqualFold(target.Name, candidate.Name) {
			return targets
		}
	}
	return append(targets, candidate)
}

func targetsFromNames(names []string) []discoveredTarget {
	targets := make([]discoveredTarget, 0, len(names))
	for _, name := range names {
		targets = append(targets, discoveredTarget{Name: name})
	}
	return targets
}

func discoveredTargetNames(targets []discoveredTarget) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	return names
}

// discoveredTargetsForAccountPlatform treats every Meta Business login as a
// shared container while preserving the two publish targets as separate rows.
func discoveredTargetsForAccountPlatform(accountPlatform string, facebookPages, instagramAccounts []string) ([]string, []string) {
	switch strings.ToLower(strings.TrimSpace(accountPlatform)) {
	case "facebook", "instagram", "meta":
		return facebookPages, instagramAccounts
	default:
		return nil, nil
	}
}

func validateDiscoverCDPURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Hostname() == "" || parsed.Port() == "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("META_DISCOVER_CDP_INVALID")
	}
	host := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("META_DISCOVER_CDP_INVALID")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 || port > 65535 {
		return "", fmt.Errorf("META_DISCOVER_CDP_INVALID")
	}
	return raw, nil
}

func normalizeDiscoveredNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		lower := strings.ToLower(name)
		if name == "" || len([]rune(name)) > 100 || strings.ContainsAny(name, "\r\n\t") ||
			lower == "instagram" || lower == "facebook" || lower == "pages" {
			continue
		}
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		result = append(result, name)
	}
	return result
}

func saveDiscoveredAssets(database *db.DB, accountID int64, facebookPages, instagramAccounts []discoveredTarget) error {
	existing, err := database.ListAccountAssetsByAccount(accountID)
	if err != nil {
		return fmt.Errorf("list existing Meta assets: %w", err)
	}
	seen := make(map[string]db.AccountAsset, len(existing))
	for _, asset := range existing {
		seen[asset.PlatformCode+"\x00"+strings.ToLower(strings.TrimSpace(asset.Name))] = asset
	}
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, group := range []struct {
		platform, assetType string
		targets             []discoveredTarget
	}{
		{platform: "facebook", assetType: "facebook_page", targets: facebookPages},
		{platform: "instagram", assetType: "instagram_account", targets: instagramAccounts},
	} {
		for _, target := range group.targets {
			key := group.platform + "\x00" + strings.ToLower(target.Name)
			if asset, exists := seen[key]; exists {
				if asset.ExternalID != "" && target.ExternalID != "" && asset.ExternalID != target.ExternalID {
					return fmt.Errorf("%s target identity changed; manual review required", group.platform)
				}
				if asset.ExternalID == "" && target.ExternalID != "" {
					if _, err := tx.Exec(`UPDATE account_assets SET external_id = ?, last_seen_at = ?, updated_at = ? WHERE id = ?`,
						target.ExternalID, now, now, asset.ID); err != nil {
						return fmt.Errorf("confirm discovered %s asset: %w", group.platform, err)
					}
					asset.ExternalID = target.ExternalID
				}
				continue
			}
			if _, err := tx.Exec(`INSERT INTO account_assets (account_id, platform_code, asset_type, name, external_id, avatar, enabled, last_seen_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, '', 1, ?, ?, ?)`,
				accountID, group.platform, group.assetType, target.Name, target.ExternalID, now, now, now); err != nil {
				return fmt.Errorf("save %s asset: %w", group.platform, err)
			}
			seen[key] = db.AccountAsset{
				AccountID:    accountID,
				PlatformCode: group.platform,
				AssetType:    group.assetType,
				Name:         target.Name,
				ExternalID:   target.ExternalID,
			}
		}
	}
	pageNamesByExternalID := make(map[string]string, len(facebookPages))
	for _, page := range facebookPages {
		if page.ExternalID != "" {
			pageNamesByExternalID[page.ExternalID] = page.Name
		}
	}
	for _, instagram := range instagramAccounts {
		pageName, linked := pageNamesByExternalID[instagram.ConnectedExternalID]
		if !linked || instagram.ExternalID == "" {
			continue
		}
		result, err := tx.Exec(`UPDATE account_assets
			SET connected_asset_id = (
				SELECT id FROM account_assets
				WHERE account_id = ? AND platform_code = 'facebook' AND external_id = ?
				LIMIT 1
			), connected_asset_name = ?, updated_at = ?
			WHERE account_id = ? AND platform_code = 'instagram' AND external_id = ?`,
			accountID, instagram.ConnectedExternalID, pageName, now,
			accountID, instagram.ExternalID,
		)
		if err != nil {
			return fmt.Errorf("link discovered Instagram target: %w", err)
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return fmt.Errorf("link discovered Instagram target: verified asset missing")
		}
		result, err = tx.Exec(`UPDATE account_assets
			SET connected_asset_id = (
				SELECT id FROM account_assets
				WHERE account_id = ? AND platform_code = 'instagram' AND external_id = ?
				LIMIT 1
			), connected_asset_name = ?, updated_at = ?
			WHERE account_id = ? AND platform_code = 'facebook' AND external_id = ?`,
			accountID, instagram.ExternalID, instagram.Name, now,
			accountID, instagram.ConnectedExternalID,
		)
		if err != nil {
			return fmt.Errorf("link discovered Facebook target: %w", err)
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return fmt.Errorf("link discovered Facebook target: verified asset missing")
		}
	}
	return tx.Commit()
}

type DiscoverResult struct {
	Success           bool
	FacebookPages     []string
	InstagramAccounts []string
	Errors            []string
}
