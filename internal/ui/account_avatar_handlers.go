//go:build !legacy_chromium

package ui

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"strings"

	"alpha-nexus-egolite/internal/db"
)

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
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
