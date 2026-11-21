package ui

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/knowledge"
)

const (
	maxKnowledgeHTTPBody = int64(129 << 20)
	maxKnowledgeFileBody = int64(64 << 20)
)

const alphaNotesKnowledgeRelativePath = "Library/Mobile Documents/com~apple~CloudDocs/Alpha-Notes/02_AI工程与自动化/2.2_AI内容与短视频"

func (s *Server) handleAPIAlphaNotesKnowledgeImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	defer r.Body.Close()
	var input struct {
		BaseID int64 `json:"base_id"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid Alpha-Notes import body", http.StatusBadRequest)
		return
	}
	base, err := s.alphaNotesKnowledgeBase(input.BaseID)
	if err != nil {
		http.Error(w, "invalid knowledge base", http.StatusBadRequest)
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "Alpha-Notes directory unavailable", http.StatusNotFound)
		return
	}
	files, err := knowledge.DiscoverDirectory(filepath.Join(home, alphaNotesKnowledgeRelativePath))
	if err != nil || len(files) == 0 {
		http.Error(w, "Alpha-Notes directory has no available knowledge files", http.StatusNotFound)
		return
	}
	result, err := knowledge.NewImporter(s.db).Import(r.Context(), knowledge.ImportRequest{BaseID: base.ID, Files: files})
	if err != nil {
		if err == context.Canceled || err == context.DeadlineExceeded {
			http.Error(w, "Alpha-Notes import cancelled", http.StatusRequestTimeout)
			return
		}
		http.Error(w, "Alpha-Notes import failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"base": base, "import": result})
}

func (s *Server) alphaNotesKnowledgeBase(requestedID int64) (*db.KnowledgeBase, error) {
	if requestedID > 0 {
		return s.db.GetKnowledgeBase(requestedID)
	}
	items, err := s.db.ListKnowledgeBases()
	if err != nil {
		return nil, err
	}
	for index := range items {
		if items[index].Name == "Alpha-Notes 编导与短视频" {
			return &items[index], nil
		}
	}
	return s.db.CreateKnowledgeBase("Alpha-Notes 编导与短视频", "本机 Alpha-Notes 的 AI 内容、短视频、编导与运营资料；敏感目录和生成依赖已排除。")
}

func (s *Server) handleAPIKnowledgeBases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.db.ListKnowledgeBases()
		if err != nil {
			http.Error(w, "list knowledge bases failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"items": items, "count": len(items)})
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		defer r.Body.Close()
		var input struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "invalid knowledge base body", http.StatusBadRequest)
			return
		}
		item, err := s.db.CreateKnowledgeBase(input.Name, input.Description)
		if err != nil {
			http.Error(w, "invalid knowledge base", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, item)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIKnowledgeBase(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid knowledge base id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		defer r.Body.Close()
		var input struct {
			Name            string `json:"name"`
			Description     string `json:"description"`
			TopK            int    `json:"top_k"`
			MaxContextChars int    `json:"max_context_chars"`
		}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&input); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "invalid knowledge base body", http.StatusBadRequest)
			return
		}
		item, err := s.db.UpdateKnowledgeBase(id, input.Name, input.Description, input.TopK, input.MaxContextChars)
		if err != nil {
			http.Error(w, "invalid knowledge base", http.StatusBadRequest)
			return
		}
		writeJSON(w, item)
	case http.MethodDelete:
		if err := s.db.DeleteKnowledgeBase(id); err != nil {
			http.Error(w, "knowledge base not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPIKnowledgeImports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxKnowledgeHTTPBody)
	defer r.Body.Close()
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "invalid multipart import", http.StatusBadRequest)
		return
	}
	incomingDir := filepath.Join(s.db.DataDir, "knowledge", "incoming")
	if err := os.MkdirAll(incomingDir, 0700); err != nil {
		http.Error(w, "prepare import failed", http.StatusInternalServerError)
		return
	}
	var baseID int64
	var files []knowledge.InputFile
	var sourceNames []string
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
			http.Error(w, "read multipart import failed", http.StatusBadRequest)
			return
		}
		if part.FormName() == "base_id" && part.FileName() == "" {
			value, readErr := io.ReadAll(io.LimitReader(part, 64))
			_ = part.Close()
			if readErr != nil {
				http.Error(w, "read base id failed", http.StatusBadRequest)
				return
			}
			baseID, _ = strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
			continue
		}
		if part.FormName() == "source_name" && part.FileName() == "" {
			value, readErr := io.ReadAll(io.LimitReader(part, 4096))
			_ = part.Close()
			if readErr != nil {
				http.Error(w, "read source name failed", http.StatusBadRequest)
				return
			}
			sourceNames = append(sourceNames, strings.TrimSpace(string(value)))
			continue
		}
		if part.FormName() != "files" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		name := filepath.Base(strings.TrimSpace(part.FileName()))
		if name == "." || name == "" {
			_ = part.Close()
			continue
		}
		temp, createErr := os.CreateTemp(incomingDir, "upload-*"+filepath.Ext(name))
		if createErr != nil {
			_ = part.Close()
			http.Error(w, "prepare upload failed", http.StatusInternalServerError)
			return
		}
		written, copyErr := io.Copy(temp, io.LimitReader(part, maxKnowledgeFileBody+1))
		_ = part.Close()
		closeErr := temp.Close()
		temporary = append(temporary, temp.Name())
		if copyErr != nil || closeErr != nil {
			http.Error(w, "save upload failed", http.StatusInternalServerError)
			return
		}
		if written > maxKnowledgeFileBody {
			http.Error(w, "knowledge file too large", http.StatusRequestEntityTooLarge)
			return
		}
		sourceName := name
		if index := len(files); index < len(sourceNames) {
			sourceName = safeKnowledgeSourceName(sourceNames[index], name)
		}
		files = append(files, knowledge.InputFile{Name: sourceName, Path: temp.Name()})
	}
	if baseID <= 0 || len(files) == 0 {
		http.Error(w, "base_id and files are required", http.StatusBadRequest)
		return
	}
	result, err := knowledge.NewImporter(s.db).Import(r.Context(), knowledge.ImportRequest{BaseID: baseID, Files: files})
	if err != nil {
		http.Error(w, "knowledge import failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, result)
}

func safeKnowledgeSourceName(requested, uploaded string) string {
	fallback := filepath.Base(strings.TrimSpace(uploaded))
	requested = strings.TrimSpace(strings.ReplaceAll(requested, "\\", "/"))
	clean := pathpkg.Clean(requested)
	if clean == "." || pathpkg.IsAbs(clean) || strings.HasPrefix(clean, "../") || len(clean) > 1024 {
		return fallback
	}
	if pathpkg.Base(clean) != fallback {
		return fallback
	}
	return clean
}

func (s *Server) handleAPIKnowledgeSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	defer r.Body.Close()
	var request knowledge.SearchRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid knowledge search body", http.StatusBadRequest)
		return
	}
	results, err := knowledge.NewService(s.db).Search(r.Context(), request)
	if err != nil {
		http.Error(w, "knowledge search failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"results": results, "count": len(results)})
}

func (s *Server) handleAPIKnowledgeScopes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	service := s.knowledgeService
	if service == nil {
		service = knowledge.NewService(s.db)
	}
	items, err := service.ListScopes(r.Context())
	if err != nil {
		http.Error(w, "list knowledge scopes failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) handleAPIKnowledgeImportRun(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid knowledge import id", http.StatusBadRequest)
		return
	}
	item, err := s.db.GetKnowledgeImportRun(id)
	if err != nil {
		http.Error(w, "knowledge import not found", http.StatusNotFound)
		return
	}
	writeJSON(w, item)
}

func (s *Server) handleAPIKnowledgeDocuments(w http.ResponseWriter, r *http.Request) {
	baseID, err := strconv.ParseInt(r.URL.Query().Get("base_id"), 10, 64)
	if err != nil || baseID <= 0 {
		http.Error(w, "base_id is required", http.StatusBadRequest)
		return
	}
	items, err := s.db.ListKnowledgeDocuments(baseID)
	if err != nil {
		http.Error(w, "list knowledge documents failed", http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{"items": items, "count": len(items)})
}
