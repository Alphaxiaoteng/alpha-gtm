package ui

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/media"
)

func (s *Server) handleAPIContentMedia(w http.ResponseWriter, r *http.Request) {
	contentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || contentID <= 0 {
		http.Error(w, "content id required", http.StatusBadRequest)
		return
	}
	content, err := s.db.GetContent(contentID)
	if err != nil {
		http.Error(w, "content not found", http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		assets, err := s.db.ListMediaByContent(contentID)
		if err != nil {
			http.Error(w, "list media failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, assets)
	case http.MethodPost:
		var body struct {
			LibraryID int64  `json:"library_id"`
			Role      string `json:"role"`
			Position  int    `json:"position"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			http.Error(w, "invalid media body", http.StatusBadRequest)
			return
		}
		if body.LibraryID <= 0 {
			http.Error(w, "media library id is required", http.StatusBadRequest)
			return
		}
		body.Role = strings.TrimSpace(body.Role)
		if body.Role == "" {
			body.Role = "content"
		}
		if (body.Role != "content" && body.Role != "cover") || body.Position < 0 || body.Position > 10_000 {
			http.Error(w, "media role or position is invalid", http.StatusBadRequest)
			return
		}
		item, err := s.db.GetMediaLibraryItem(body.LibraryID)
		if err != nil {
			http.Error(w, "media library item not found", http.StatusNotFound)
			return
		}
		_, err = media.ValidateContentAttachment(content.Type, item.Purpose, item.FilePath, item.SHA256)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "changed since import") {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		asset, err := s.db.CreateMediaAssetFromLibraryWithRole(contentID, body.LibraryID, body.Role, body.Position)
		if err != nil {
			http.Error(w, "attach media failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, asset)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIMedia(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "media asset id required", http.StatusBadRequest)
		return
	}
	if err := s.db.DeleteMediaAsset(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, "media asset not found", http.StatusNotFound)
			return
		}
		http.Error(w, "delete media failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"deleted": true})
}

func (s *Server) handleAPIMediaContent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "media asset id required", http.StatusBadRequest)
		return
	}
	asset, err := s.db.GetMediaAsset(id)
	if err != nil {
		http.Error(w, "media asset not found", http.StatusNotFound)
		return
	}
	path, err := validateLocalMediaPath(asset.FilePath)
	if err != nil {
		http.Error(w, "media file unavailable", http.StatusNotFound)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "media file unavailable", http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "media file unavailable", http.StatusNotFound)
		return
	}
	mimeType := strings.ToLower(strings.TrimSpace(asset.MimeType))
	if mimeType == "" {
		mimeType = media.GuessMime(path)
	}
	if !strings.HasPrefix(mimeType, "image/") && !strings.HasPrefix(mimeType, "video/") && !strings.HasPrefix(mimeType, "audio/") {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func validateLocalMediaPath(value string) (string, error) {
	return media.ValidateLocalPath(value)
}

func mediaKind(mime string) string {
	if strings.HasPrefix(mime, "video/") {
		return "video"
	}
	if strings.HasPrefix(mime, "audio/") {
		return "audio"
	}
	return "image"
}
