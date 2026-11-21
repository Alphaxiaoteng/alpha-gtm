package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"alpha-nexus-egolite/internal/platforms/meta"
	"alpha-nexus-egolite/internal/publishguard"
	"alpha-nexus-egolite/internal/trace"
)

const maxMetaAccountAssetBodyBytes = 8 << 10

func metaAccountAssetType(platformCode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(platformCode)) {
	case "facebook":
		return "facebook_page", true
	case "instagram":
		return "instagram_account", true
	default:
		return "", false
	}
}

func isMetaAccountPlatform(platformCode string) bool {
	switch strings.ToLower(strings.TrimSpace(platformCode)) {
	case "meta", "facebook", "instagram":
		return true
	default:
		return false
	}
}

// handleAPIMetaAccountAsset stores a named publish target after the user has
// completed local browser login. It never receives or stores platform credentials.
func (s *Server) handleAPIMetaAccountAsset(w http.ResponseWriter, r *http.Request) {
	accountID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || accountID <= 0 {
		http.Error(w, "account id required", http.StatusBadRequest)
		return
	}
	account, err := s.db.GetAccount(accountID)
	if err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}
	if !isMetaAccountPlatform(account.PlatformCode) {
		http.Error(w, "publish target setup supports Facebook and Instagram accounts only", http.StatusBadRequest)
		return
	}

	var body struct {
		Platform   string `json:"platform"`
		Name       string `json:"name"`
		ExternalID string `json:"externalId"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMetaAccountAssetBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&body); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	targetPlatform := strings.ToLower(strings.TrimSpace(body.Platform))
	if targetPlatform == "" {
		targetPlatform = strings.ToLower(strings.TrimSpace(account.PlatformCode))
	}
	assetType, supported := metaAccountAssetType(targetPlatform)
	if !supported {
		http.Error(w, "Meta target platform must be facebook or instagram", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	externalID := strings.TrimSpace(body.ExternalID)
	if utf8.RuneCountInString(name) == 0 || utf8.RuneCountInString(name) > 120 {
		http.Error(w, "target name must be between 1 and 120 characters", http.StatusBadRequest)
		return
	}
	if utf8.RuneCountInString(externalID) == 0 || utf8.RuneCountInString(externalID) > 256 {
		http.Error(w, "target id must be between 1 and 256 characters", http.StatusBadRequest)
		return
	}

	assets, err := s.db.ListAccountAssetsByAccount(accountID)
	if err != nil {
		http.Error(w, "list account assets failed", http.StatusInternalServerError)
		return
	}
	for _, asset := range assets {
		if asset.PlatformCode == targetPlatform && asset.AssetType == assetType && asset.ExternalID == externalID {
			http.Error(w, "publish target already exists", http.StatusConflict)
			return
		}
		if asset.PlatformCode == targetPlatform && asset.AssetType == assetType &&
			strings.EqualFold(strings.TrimSpace(asset.Name), name) && strings.TrimSpace(asset.ExternalID) == "" {
			updated, updateErr := s.db.ConfirmAccountAssetIdentity(asset.ID, accountID, name, externalID)
			if updateErr != nil {
				http.Error(w, "confirm publish target failed", http.StatusInternalServerError)
				return
			}
			_, _ = s.db.CreateAuditEvent(
				string(trace.New()), "info", "account_assets", fmt.Sprintf("manual %s publish target identity confirmed", targetPlatform), "account_asset", updated.ID, "",
			)
			updated.Avatar = ""
			writeJSON(w, updated)
			return
		}
	}

	asset, err := s.db.CreateAccountAsset(accountID, targetPlatform, assetType, name, externalID, "", 0, "")
	if err != nil {
		http.Error(w, "create publish target failed", http.StatusInternalServerError)
		return
	}
	_, _ = s.db.CreateAuditEvent(
		string(trace.New()), "info", "account_assets", fmt.Sprintf("manual %s publish target configured", targetPlatform), "account_asset", asset.ID, "",
	)
	asset.Avatar = ""
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, asset)
}

// handleAPIMetaAccountAssetDiscover scans the selected account's current Meta
// browser session only after an explicit UI request. It never starts a browser,
// accepts a remote CDP endpoint, or receives platform credentials.
func (s *Server) handleAPIMetaAccountAssetDiscover(w http.ResponseWriter, r *http.Request) {
	accountID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || accountID <= 0 {
		http.Error(w, "account id required", http.StatusBadRequest)
		return
	}
	account, err := s.db.GetAccount(accountID)
	if err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}
	if !isMetaAccountPlatform(account.PlatformCode) {
		http.Error(w, "publish target discovery supports Facebook and Instagram accounts only", http.StatusBadRequest)
		return
	}
	if !account.Enabled {
		http.Error(w, "ACCOUNT_DISABLED", http.StatusConflict)
		return
	}
	if s.facade == nil {
		http.Error(w, "browser facade is unavailable", http.StatusInternalServerError)
		return
	}
	release, err := s.acquireBrowserOperationLease(accountID, "meta_discover", uiBrowserCollectLeaseTTL)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	defer release()

	status, statusErr := s.facade.Status(accountID)
	cdpURL, cdpErr := publishguard.LiveCDPURL(accountID, status, statusErr)
	if cdpErr != nil {
		http.Error(w, cdpErr.Error(), http.StatusConflict)
		return
	}
	result, err := meta.Discover(r.Context(), cdpURL, accountID, s.db)
	if err != nil {
		// Discovery errors may include browser implementation details. Keep the
		// local API stable and avoid reflecting any browser/session information.
		http.Error(w, "META_DISCOVER_FAILED", http.StatusBadGateway)
		return
	}

	level := "info"
	message := fmt.Sprintf("Meta publish targets discovered: facebook_pages=%d instagram_accounts=%d", len(result.FacebookPages), len(result.InstagramAccounts))
	if !result.Success {
		level = "warn"
		message = "Meta publish target discovery completed with warnings"
	}
	// Discovery has already committed any observed assets. Match manual target
	// setup: audit is valuable but must not turn a completed local action into a
	// client-visible failure that invites a redundant retry.
	_, _ = s.db.CreateAuditEvent(string(trace.New()), level, "account_assets", message, "account", accountID, "")
	writeJSON(w, map[string]any{
		"accountId":         accountID,
		"facebookPages":     result.FacebookPages,
		"instagramAccounts": result.InstagramAccounts,
		"warnings":          result.Errors,
		"complete":          result.Success,
	})
}
