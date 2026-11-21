//go:build legacy_chromium

package ui

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/browser"
	"alpha-nexus-egolite/internal/db"
)

func (s *Server) handleAPIAccountObserve(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	if s.accountObserver == nil {
		http.Error(w, "account observer is not initialized", http.StatusServiceUnavailable)
		return
	}
	result, err := s.accountObserver.ObserveAccount(r.Context(), accountID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if result.AuthState == db.AccountAuthLoggedIn {
		// A verified manual check is the only automatic backup trigger. The
		// one-second monitor deliberately never reads cookie values.
		if _, backupErr := s.backupAccountSession(r.Context(), accountID); backupErr == nil {
			result.SessionBackupState = "saved"
		} else {
			result.SessionBackupState = "unavailable"
		}
	}
	writeJSON(w, result)
}

func (s *Server) handleAPIAccountsObserve(w http.ResponseWriter, r *http.Request) {
	if s.accountObserver == nil {
		http.Error(w, "account observer is not initialized", http.StatusServiceUnavailable)
		return
	}
	results, err := s.accountObserver.ObserveRunning(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for index := range results {
		if results[index].AuthState != db.AccountAuthLoggedIn {
			continue
		}
		if _, backupErr := s.backupAccountSession(r.Context(), results[index].AccountID); backupErr == nil {
			results[index].SessionBackupState = "saved"
		} else {
			results[index].SessionBackupState = "unavailable"
		}
	}
	writeJSON(w, map[string]any{"observations": results})
}

func (s *Server) handleAPIAccountLoginEvents(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.db.ListAccountLoginEvents(accountID, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for index := range events {
		if events[index].AvatarSource != db.AvatarSourceObserved {
			events[index].AvatarSource = db.AvatarSourcePlatformFallback
			events[index].AvatarSHA256 = ""
		}
	}
	writeJSON(w, events)
}

func (s *Server) handleAPIAccountBrowserEvents(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	if _, err := s.db.GetAccount(accountID); err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.db.ListBrowserLifecycleEvents(browser.ProfileIDForAccount(accountID), limit)
	if err != nil {
		http.Error(w, "read browser lifecycle events failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, events)
}

func (s *Server) handleAPIAccountObserverStatus(w http.ResponseWriter, _ *http.Request) {
	if s.accountObserver == nil {
		writeJSON(w, AccountObserverStatus{IntervalSeconds: int(accountObservationInterval.Seconds())})
		return
	}
	writeJSON(w, s.accountObserver.Status())
}

func (s *Server) handleAPIAccountAvatar(w http.ResponseWriter, r *http.Request) {
	accountID, err := parseAccountIDParam(r)
	if err != nil {
		writeFacadeError(w, err)
		return
	}
	account, err := s.db.GetAccount(accountID)
	if err != nil {
		http.Error(w, "account not found", http.StatusNotFound)
		return
	}
	if account.AvatarSource != db.AvatarSourceObserved || account.AvatarSHA256 == "" {
		http.Error(w, "observed account avatar not found", http.StatusNotFound)
		return
	}
	dataDir, err := s.cfg.ResolvedDataDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	path, err := managedAvatarPath(dataDir, account.Avatar)
	if err != nil {
		http.Error(w, "observed account avatar not found", http.StatusNotFound)
		return
	}
	payload, err := os.ReadFile(path)
	if err != nil || len(payload) == 0 || len(payload) > 2<<20 {
		http.Error(w, "observed account avatar not found", http.StatusNotFound)
		return
	}
	digest := sha256.Sum256(payload)
	if fmt.Sprintf("%x", digest[:]) != account.AvatarSHA256 {
		http.Error(w, "observed account avatar not found", http.StatusNotFound)
		return
	}
	if normalized, normalizeErr := normalizeAvatarPNG(payload); normalizeErr == nil {
		payload = normalized
	}
	if !avatarSnapshotHasVisualContent(payload) {
		recoveredRef, recoveredSHA, recoverErr := recoverLatestValidAccountAvatarSnapshot(dataDir, account.ID)
		if recoverErr != nil {
			http.Error(w, "observed account avatar not found", http.StatusNotFound)
			return
		}
		recoveredPath, pathErr := managedAvatarPath(dataDir, recoveredRef)
		if pathErr != nil {
			http.Error(w, "observed account avatar not found", http.StatusNotFound)
			return
		}
		recoveredPayload, readErr := os.ReadFile(recoveredPath)
		if readErr != nil || len(recoveredPayload) == 0 || len(recoveredPayload) > 2<<20 {
			http.Error(w, "observed account avatar not found", http.StatusNotFound)
			return
		}
		recoveredDigest := sha256.Sum256(recoveredPayload)
		if fmt.Sprintf("%x", recoveredDigest[:]) != recoveredSHA {
			http.Error(w, "observed account avatar not found", http.StatusNotFound)
			return
		}
		payload = recoveredPayload
		if normalized, normalizeErr := normalizeAvatarPNG(payload); normalizeErr == nil {
			payload = normalized
		}
	}
	servedDigest := sha256.Sum256(payload)
	etag := `"` + fmt.Sprintf("%x", servedDigest[:]) + `"`
	if strings.TrimSpace(r.Header.Get("If-None-Match")) == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// Revalidate local avatar snapshots so a legacy padded image is replaced as
	// soon as this version of the app serves its normalized response.
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

// handleAPIAccountVerify is the legacy body-based route. It now delegates to
// the same passive observer and therefore cannot navigate the user's tab.
func (s *Server) handleAPIAccountVerifyPassive(w http.ResponseWriter, r *http.Request) {
	var request struct {
		AccountID int64 `json:"accountId"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request.AccountID <= 0 {
		http.Error(w, fmt.Sprintf("invalid accountId: %v", err), http.StatusBadRequest)
		return
	}
	if s.accountObserver == nil {
		http.Error(w, "account observer is not initialized", http.StatusServiceUnavailable)
		return
	}
	result, err := s.accountObserver.ObserveAccount(r.Context(), request.AccountID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"loggedIn":            result.AuthState == db.AccountAuthLoggedIn,
		"verificationState":   verificationStateForObservation(result.AuthState),
		"accountId":           result.AccountIdentifier,
		"name":                result.Name,
		"avatar":              result.Avatar,
		"pageUrl":             result.PageOrigin,
		"authState":           result.AuthState,
		"persistedLoginState": result.PersistedLoginState,
		"reasonCode":          result.ReasonCode,
		"evidenceCode":        result.EvidenceCode,
		"loginMode":           result.LoginMode,
		"authMethod":          result.AuthMethod,
	})
}

func verificationStateForObservation(authState string) string {
	switch authState {
	case db.AccountAuthLoggedIn:
		return "verified"
	case db.AccountAuthLoggedOut:
		return "login_required"
	default:
		return "not_verified"
	}
}
