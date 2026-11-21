package ui

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Server) handleAPIAccountAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	assets, err := s.db.ListAllAccountAssets()
	if err != nil {
		http.Error(w, "list account assets failed", http.StatusInternalServerError)
		return
	}
	// Provider avatar URLs can contain signed query material. Expose only local
	// snapshots captured from the bound Ego Space through a same-origin route.
	for index := range assets {
		if strings.HasPrefix(assets[index].Avatar, "account-asset-avatars/asset-") && !strings.Contains(assets[index].Avatar, "..") {
			assets[index].Avatar = fmt.Sprintf("/api/account-assets/%d/avatar?v=%s", assets[index].ID, url.QueryEscape(assets[index].LastSeenAt))
		} else {
			assets[index].Avatar = ""
		}
	}
	writeJSON(w, assets)
}

func (s *Server) handleAPIAccountAssetAvatar(w http.ResponseWriter, r *http.Request) {
	assetID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || assetID <= 0 {
		http.Error(w, "account asset id required", http.StatusBadRequest)
		return
	}
	asset, err := s.db.GetAccountAsset(assetID)
	if err != nil {
		http.Error(w, "account asset not found", http.StatusNotFound)
		return
	}
	path, err := managedAccountAssetAvatarPath(s.db.DataDir, asset.Avatar)
	if err != nil {
		http.Error(w, "observed account asset avatar not found", http.StatusNotFound)
		return
	}
	payload, err := os.ReadFile(path)
	if err != nil || len(payload) == 0 || len(payload) > 2<<20 || !avatarSnapshotHasVisualContent(payload) {
		http.Error(w, "observed account asset avatar not found", http.StatusNotFound)
		return
	}
	if normalized, normalizeErr := normalizeAvatarPNG(payload); normalizeErr == nil {
		payload = normalized
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func managedAccountAssetAvatarPath(dataDir, reference string) (string, error) {
	reference = filepath.ToSlash(strings.TrimSpace(reference))
	if filepath.IsAbs(reference) || !strings.HasPrefix(reference, "account-asset-avatars/asset-") || strings.Contains(reference, "../") {
		return "", fmt.Errorf("account asset avatar is not managed")
	}
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	directory := filepath.Join(root, "account-asset-avatars")
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("account asset avatar directory is unsafe")
	}
	resolved := filepath.Clean(filepath.Join(root, filepath.FromSlash(reference)))
	if !strings.HasPrefix(resolved, directory+string(os.PathSeparator)) {
		return "", fmt.Errorf("account asset avatar escapes managed directory")
	}
	info, err = os.Lstat(resolved)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("account asset avatar is not a regular file")
	}
	return resolved, nil
}
