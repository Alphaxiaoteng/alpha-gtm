package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/knowledge"
	"alpha-nexus-egolite/internal/media"
)

func (s *Server) handleAPIShopContents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.db.ListShopContents()
		if err != nil {
			http.Error(w, "list shop contents failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, items)
	case http.MethodPost:
		body, ok := decodeContentBody(w, r)
		if !ok {
			return
		}
		item, err := s.db.CreateShopContent(body.Type, body.Title, body.Caption, body.Body, body.TagsJSON, body.Status, body.PlannedAt)
		if err != nil {
			http.Error(w, "create shop content failed", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIShopContent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "shop content id required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		item, err := s.db.GetShopContent(id)
		if err != nil {
			http.Error(w, "shop content not found", http.StatusNotFound)
			return
		}
		writeJSON(w, item)
	case http.MethodPut:
		body, ok := decodeContentBody(w, r)
		if !ok {
			return
		}
		if err := s.db.ReplaceShopContent(id, body.Type, body.Title, body.Caption, body.Body, body.TagsJSON, body.Status, body.PlannedAt); err != nil {
			http.Error(w, "update shop content failed", http.StatusBadRequest)
			return
		}
		item, _ := s.db.GetShopContent(id)
		writeJSON(w, item)
	case http.MethodDelete:
		if err := s.db.DeleteShopContent(id); err != nil {
			http.Error(w, "shop content not found", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]bool{"deleted": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIShopContentMedia(w http.ResponseWriter, r *http.Request) {
	contentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || contentID <= 0 {
		http.Error(w, "shop content id required", http.StatusBadRequest)
		return
	}
	content, err := s.db.GetShopContent(contentID)
	if err != nil {
		http.Error(w, "shop content not found", http.StatusNotFound)
		return
	}
	if r.Method == http.MethodGet {
		items, err := s.db.ListShopContentMedia(contentID)
		if err != nil {
			http.Error(w, "list shop content media failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, items)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		LibraryID int64  `json:"library_id"`
		Role      string `json:"role"`
		Position  int    `json:"position"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || body.LibraryID <= 0 {
		http.Error(w, "invalid shop content media", http.StatusBadRequest)
		return
	}
	if body.Role == "" {
		body.Role = "content"
	}
	library, err := s.db.GetMediaLibraryItem(body.LibraryID)
	if err != nil {
		http.Error(w, "media library item not found", http.StatusNotFound)
		return
	}
	if _, err := media.ValidateContentAttachment(content.Type, library.Purpose, library.FilePath, library.SHA256); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	item, err := s.db.AttachShopContentMedia(contentID, body.LibraryID, strings.TrimSpace(body.Role), body.Position)
	if err != nil {
		http.Error(w, "attach shop content media failed", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, item)
}

func (s *Server) handleAPIShopMediaContent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "shop media id required", http.StatusBadRequest)
		return
	}
	item, err := s.db.GetShopContentMedia(id)
	if err != nil {
		http.Error(w, "shop media not found", http.StatusNotFound)
		return
	}
	path, err := validateLocalMediaPath(item.FilePath)
	if err != nil {
		http.Error(w, "shop media unavailable", http.StatusNotFound)
		return
	}
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "shop media unavailable", http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, "shop media unavailable", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", item.MimeType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) handleAPIShopKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.db.ListShopKnowledgeBases()
		if err != nil {
			http.Error(w, "list shop knowledge bases failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"items": items, "count": len(items)})
	case http.MethodPost:
		var input struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		if decoder.Decode(&input) != nil {
			http.Error(w, "invalid shop knowledge base", http.StatusBadRequest)
			return
		}
		item, err := s.db.CreateShopKnowledgeBase(input.Name, input.Description)
		if err != nil {
			http.Error(w, "invalid shop knowledge base", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIShopKnowledgeDocuments(w http.ResponseWriter, r *http.Request) {
	baseID, err := strconv.ParseInt(r.URL.Query().Get("base_id"), 10, 64)
	if err != nil || baseID <= 0 {
		http.Error(w, "base_id is required", http.StatusBadRequest)
		return
	}
	items, err := s.db.ListShopKnowledgeDocuments(baseID)
	if err != nil {
		http.Error(w, "list shop knowledge documents failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleAPIShopKnowledgeSearch(w http.ResponseWriter, r *http.Request) {
	var request knowledge.SearchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	if decoder.Decode(&request) != nil {
		http.Error(w, "invalid shop knowledge search", http.StatusBadRequest)
		return
	}
	items, err := s.db.SearchShopKnowledgeChunks(request.BaseIDs, request.Query, request.TopK)
	if err != nil {
		http.Error(w, "shop knowledge search failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"results": items, "count": len(items)})
}

func (s *Server) handleAPIShopKnowledgeImports(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxKnowledgeHTTPBody)
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid shop knowledge import", http.StatusBadRequest)
		return
	}
	incoming := filepath.Join(s.db.DataDir, "shop-knowledge", "incoming")
	if err := os.MkdirAll(incoming, 0700); err != nil {
		http.Error(w, "prepare shop knowledge import failed", http.StatusInternalServerError)
		return
	}
	var baseID int64
	var files []knowledge.InputFile
	var temporary []string
	defer func() {
		for _, path := range temporary {
			_ = os.Remove(path)
		}
	}()
	for {
		part, nextErr := reader.NextPart()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			http.Error(w, "read shop knowledge import failed", http.StatusBadRequest)
			return
		}
		if part.FormName() == "base_id" && part.FileName() == "" {
			value, _ := io.ReadAll(io.LimitReader(part, 64))
			baseID, _ = strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
			_ = part.Close()
			continue
		}
		if part.FormName() != "files" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		name := filepath.Base(part.FileName())
		temp, createErr := os.CreateTemp(incoming, "upload-*"+filepath.Ext(name))
		if createErr != nil {
			http.Error(w, "prepare shop knowledge file failed", http.StatusInternalServerError)
			return
		}
		written, copyErr := io.Copy(temp, io.LimitReader(part, maxKnowledgeFileBody+1))
		_ = part.Close()
		_ = temp.Close()
		temporary = append(temporary, temp.Name())
		if copyErr != nil || written > maxKnowledgeFileBody {
			http.Error(w, "shop knowledge file too large", http.StatusRequestEntityTooLarge)
			return
		}
		files = append(files, knowledge.InputFile{Name: name, Path: temp.Name()})
	}
	if baseID <= 0 || len(files) == 0 {
		http.Error(w, "base_id and files are required", http.StatusBadRequest)
		return
	}
	run, err := s.db.CreateShopKnowledgeImportRun(baseID)
	if err != nil {
		http.Error(w, "shop knowledge base not found", http.StatusBadRequest)
		return
	}
	result := knowledge.ImportResult{RunID: run.ID, Items: make([]knowledge.ImportItem, 0, len(files))}
	for _, input := range files {
		item := s.importShopKnowledgeFile(r.Context(), baseID, input)
		result.Total++
		result.Items = append(result.Items, item)
		switch item.Status {
		case "imported":
			result.Imported++
		case "skipped":
			result.Skipped++
		default:
			result.Failed++
		}
	}
	_ = s.db.CompleteShopKnowledgeImportRun(run.ID, result.Total, result.Imported, result.Skipped, result.Failed)
	writeJSON(w, result)
}

func (s *Server) importShopKnowledgeFile(ctx context.Context, baseID int64, input knowledge.InputFile) knowledge.ImportItem {
	markdown, err := knowledge.ConvertFileToMarkdown(ctx, input.Name, input.Path)
	if err != nil || strings.TrimSpace(markdown) == "" {
		return knowledge.ImportItem{Name: input.Name, Status: "failed", ErrorCode: "parse_failed"}
	}
	chunks := knowledge.ChunkMarkdown(markdown)
	if len(chunks) == 0 {
		return knowledge.ImportItem{Name: input.Name, Status: "failed", ErrorCode: "no_extractable_text"}
	}
	file, err := os.Open(input.Path)
	if err != nil {
		return knowledge.ImportItem{Name: input.Name, Status: "failed", ErrorCode: "read_failed"}
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	_ = file.Close()
	if err != nil {
		return knowledge.ImportItem{Name: input.Name, Status: "failed", ErrorCode: "read_failed"}
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	root := filepath.Join(s.db.DataDir, "shop-knowledge")
	originalDir, normalizedDir := filepath.Join(root, "originals"), filepath.Join(root, "normalized")
	if os.MkdirAll(originalDir, 0700) != nil || os.MkdirAll(normalizedDir, 0700) != nil {
		return knowledge.ImportItem{Name: input.Name, Status: "failed", ErrorCode: "storage_failed"}
	}
	originalPath := filepath.Join(originalDir, digest+filepath.Ext(input.Name))
	if _, err := os.Stat(originalPath); os.IsNotExist(err) {
		if err := copyShopKnowledgeFile(input.Path, originalPath); err != nil {
			return knowledge.ImportItem{Name: input.Name, Status: "failed", ErrorCode: "storage_failed"}
		}
	}
	normalizedPath := filepath.Join(normalizedDir, digest+".md")
	if err := os.WriteFile(normalizedPath, []byte(markdown), 0600); err != nil {
		return knowledge.ImportItem{Name: input.Name, Status: "failed", ErrorCode: "storage_failed"}
	}
	originalRel, _ := filepath.Rel(s.db.DataDir, originalPath)
	normalizedRel, _ := filepath.Rel(s.db.DataDir, normalizedPath)
	document, created, err := s.db.ImportShopKnowledgeDocument(baseID, input.Name, shopKnowledgeSourceType(input.Name), digest,
		filepath.ToSlash(originalRel), filepath.ToSlash(normalizedRel), chunks)
	if err != nil {
		return knowledge.ImportItem{Name: input.Name, Status: "failed", ErrorCode: "index_failed"}
	}
	if !created {
		return knowledge.ImportItem{Name: input.Name, Status: "skipped", DocumentID: document.ID}
	}
	return knowledge.ImportItem{Name: input.Name, Status: "imported", DocumentID: document.ID}
}

func copyShopKnowledgeFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func shopKnowledgeSourceType(name string) string {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if extension == "md" || extension == "markdown" {
		return "markdown"
	}
	if extension == "" {
		return "text"
	}
	return extension
}
