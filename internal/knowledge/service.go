package knowledge

import (
	"context"
	"fmt"
	"path"
	"strings"
	"unicode"

	"alpha-nexus-egolite/internal/db"
)

type SearchRequest struct {
	BaseIDs []int64 `json:"base_ids"`
	Scopes  []Scope `json:"scopes,omitempty"`
	Query   string  `json:"query"`
	TopK    int     `json:"top_k"`
}

type Scope struct {
	BaseID       int64  `json:"base_id"`
	SourcePrefix string `json:"source_prefix,omitempty"`
}

type ScopeOption struct {
	BaseID       int64  `json:"base_id"`
	BaseName     string `json:"base_name"`
	SourcePrefix string `json:"source_prefix,omitempty"`
	Label        string `json:"label"`
}

type Citation struct {
	ChunkID     int64  `json:"chunk_id"`
	BaseName    string `json:"base_name"`
	SourceName  string `json:"source_name"`
	HeadingPath string `json:"heading_path"`
	DocumentSHA string `json:"document_sha,omitempty"`
}

type Service struct {
	db      *db.DB
	backend SearchBackend
}

func NewService(database *db.DB) *Service {
	return NewServiceWithBackend(database, nil)
}

func NewServiceWithBackend(database *db.DB, backend SearchBackend) *Service {
	if backend == nil {
		backend = newFTS5SearchBackend(database)
	}
	return &Service{db: database, backend: backend}
}

func (s *Service) Search(ctx context.Context, request SearchRequest) ([]db.KnowledgeSearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("knowledge service unavailable")
	}
	request.Query = strings.TrimSpace(request.Query)
	if request.Query == "" {
		return []db.KnowledgeSearchResult{}, nil
	}
	if request.TopK <= 0 {
		request.TopK = 6
	}
	if request.TopK > 12 {
		request.TopK = 12
	}
	candidateLimit := request.TopK * 4
	if candidateLimit > 12 {
		candidateLimit = 12
	}
	scopes := request.Scopes
	if len(scopes) == 0 && len(request.BaseIDs) > 0 {
		scopes = make([]Scope, 0, len(request.BaseIDs))
		for _, baseID := range request.BaseIDs {
			scopes = append(scopes, Scope{BaseID: baseID})
		}
	}
	normalizedScopes, err := s.ValidateScopes(ctx, scopes)
	if err != nil {
		return nil, err
	}
	results, err := s.backend.Search(ctx, BackendSearchRequest{
		Scopes: normalizedScopes, Query: request.Query, CandidateLimit: candidateLimit,
	})
	if err != nil {
		return nil, err
	}
	for index := range results {
		parts := strings.Split(results[index].HeadingPath, " / ")
		for partIndex := range parts {
			parts[partIndex] = cleanMarkdownHeading(parts[partIndex])
		}
		results[index].HeadingPath = strings.Join(parts, " / ")
	}
	return diversifyKnowledgeResults(results, request.TopK), nil
}

func (s *Service) ValidateScopes(ctx context.Context, scopes []Scope) ([]Scope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(scopes) > 16 {
		return nil, fmt.Errorf("too many knowledge scopes")
	}
	normalized := make([]Scope, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope.BaseID <= 0 {
			return nil, fmt.Errorf("invalid knowledge base scope")
		}
		if _, err := s.db.GetKnowledgeBase(scope.BaseID); err != nil {
			return nil, fmt.Errorf("knowledge base scope not found")
		}
		prefix, err := normalizeSourcePrefix(scope.SourcePrefix)
		if err != nil {
			return nil, err
		}
		if prefix != "" {
			documents, err := s.db.ListKnowledgeDocuments(scope.BaseID)
			if err != nil {
				return nil, err
			}
			found := false
			for _, document := range documents {
				if document.Status == "ready" && sourceInPrefix(document.SourceName, prefix) {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("knowledge folder scope not found")
			}
		}
		key := fmt.Sprintf("%d:%s", scope.BaseID, prefix)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, Scope{BaseID: scope.BaseID, SourcePrefix: prefix})
	}
	return normalized, nil
}

func (s *Service) ListScopes(ctx context.Context) ([]ScopeOption, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bases, err := s.db.ListKnowledgeBases()
	if err != nil {
		return nil, err
	}
	options := make([]ScopeOption, 0)
	for _, base := range bases {
		options = append(options, ScopeOption{BaseID: base.ID, BaseName: base.Name, Label: base.Name})
		documents, err := s.db.ListKnowledgeDocuments(base.ID)
		if err != nil {
			return nil, err
		}
		seen := make(map[string]struct{})
		for _, document := range documents {
			if document.Status != "ready" {
				continue
			}
			parts := strings.Split(strings.Trim(strings.ReplaceAll(document.SourceName, "\\", "/"), "/"), "/")
			for index := 1; index < len(parts); index++ {
				prefix := strings.Join(parts[:index], "/")
				if _, exists := seen[prefix]; exists {
					continue
				}
				seen[prefix] = struct{}{}
				options = append(options, ScopeOption{BaseID: base.ID, BaseName: base.Name, SourcePrefix: prefix, Label: base.Name + " / " + prefix})
			}
		}
	}
	return options, nil
}

func normalizeSourcePrefix(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" {
		return "", nil
	}
	clean := path.Clean(value)
	if clean == "." || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid knowledge folder scope")
	}
	return strings.Trim(clean, "/"), nil
}

func sourceInPrefix(sourceName, prefix string) bool {
	sourceName = strings.Trim(strings.ReplaceAll(sourceName, "\\", "/"), "/")
	return sourceName == prefix || strings.HasPrefix(sourceName, prefix+"/")
}

// Prefer distinct source documents before filling remaining slots with a
// second chunk from the same document. This keeps the lightweight lexical RAG
// useful for broad creative prompts without adding an embedding dependency.
func diversifyKnowledgeResults(results []db.KnowledgeSearchResult, limit int) []db.KnowledgeSearchResult {
	if limit <= 0 || len(results) <= 1 {
		return results
	}
	unique := make([]db.KnowledgeSearchResult, 0, limit)
	repeated := make([]db.KnowledgeSearchResult, 0, len(results))
	seenDocuments := make(map[int64]struct{}, limit)
	seenContent := make(map[string]int64, limit)
	seenChunks := make(map[string]struct{}, limit)
	acceptedChunkContent := make([]string, 0, limit)
	for _, result := range results {
		chunkKey := strings.Join(strings.Fields(result.Content), " ")
		if chunkKey != "" {
			if _, seen := seenChunks[chunkKey]; seen {
				continue
			}
			nearDuplicate := false
			for _, accepted := range acceptedChunkContent {
				if knowledgeChunksNearDuplicate(chunkKey, accepted) {
					nearDuplicate = true
					break
				}
			}
			if nearDuplicate {
				continue
			}
			seenChunks[chunkKey] = struct{}{}
			acceptedChunkContent = append(acceptedChunkContent, chunkKey)
		}
		contentKey := strings.TrimSpace(result.DocumentSHA)
		if contentKey != "" {
			if documentID, seen := seenContent[contentKey]; seen && documentID != result.DocumentID {
				continue
			}
			seenContent[contentKey] = result.DocumentID
		}
		if _, seen := seenDocuments[result.DocumentID]; seen {
			repeated = append(repeated, result)
			continue
		}
		seenDocuments[result.DocumentID] = struct{}{}
		unique = append(unique, result)
		if len(unique) == limit {
			return unique
		}
	}
	for _, result := range repeated {
		unique = append(unique, result)
		if len(unique) == limit {
			break
		}
	}
	return unique
}

// Five-rune shingles remove copied excerpts that differ only by a short title,
// URL, or export wrapper. The comparison is bounded to the at-most-12 search
// candidates and keeps short generic snippets distinct.
func knowledgeChunksNearDuplicate(left, right string) bool {
	leftShingles := knowledgeChunkShingles(left, 5)
	rightShingles := knowledgeChunkShingles(right, 5)
	if len(leftShingles) < 20 || len(rightShingles) < 20 {
		return false
	}
	intersection := 0
	for shingle := range leftShingles {
		if _, exists := rightShingles[shingle]; exists {
			intersection++
		}
	}
	denominator := len(leftShingles)
	if len(rightShingles) < denominator {
		denominator = len(rightShingles)
	}
	return float64(intersection)/float64(denominator) >= 0.65
}

func knowledgeChunkShingles(content string, width int) map[string]struct{} {
	normalized := make([]rune, 0, len([]rune(content)))
	for _, character := range strings.ToLower(content) {
		if unicode.IsLetter(character) || unicode.IsNumber(character) {
			normalized = append(normalized, character)
		}
	}
	if width <= 0 || len(normalized) < width {
		return nil
	}
	shingles := make(map[string]struct{}, len(normalized)-width+1)
	for index := 0; index+width <= len(normalized); index++ {
		shingles[string(normalized[index:index+width])] = struct{}{}
	}
	return shingles
}

func BuildCitedContext(results []db.KnowledgeSearchResult, maxChars int) (string, []Citation) {
	if maxChars <= 0 || maxChars > 12000 {
		maxChars = 12000
	}
	var builder strings.Builder
	citations := make([]Citation, 0, len(results))
	for _, result := range results {
		header := fmt.Sprintf("[KB:%d] %s / %s", result.ChunkID, result.BaseName, result.SourceName)
		if result.HeadingPath != "" {
			header += " / " + result.HeadingPath
		}
		separator := ""
		if builder.Len() > 0 {
			separator = "\n\n"
		}
		remaining := maxChars - len([]rune(builder.String())) - len([]rune(separator+header+"\n"))
		if remaining <= 0 {
			break
		}
		content := []rune(strings.TrimSpace(result.Content))
		truncated := len(content) > remaining
		if truncated {
			if builder.Len() > 0 {
				break
			}
			content = content[:remaining]
		}
		builder.WriteString(separator + header + "\n" + string(content))
		citations = append(citations, Citation{
			ChunkID: result.ChunkID, BaseName: result.BaseName,
			SourceName: result.SourceName, HeadingPath: result.HeadingPath, DocumentSHA: result.DocumentSHA,
		})
		if truncated {
			break
		}
	}
	return builder.String(), citations
}

// CitationsUsedInOutput keeps only retrieved sources that the generated text
// explicitly attributes with the reviewed [KB:id] marker. Retrieval alone is
// not evidence that a draft used a source.
func CitationsUsedInOutput(output string, retrieved []Citation) []Citation {
	used := make([]Citation, 0, len(retrieved))
	seen := make(map[int64]struct{}, len(retrieved))
	for _, citation := range retrieved {
		if citation.ChunkID <= 0 || !strings.Contains(output, fmt.Sprintf("[KB:%d]", citation.ChunkID)) {
			continue
		}
		if _, exists := seen[citation.ChunkID]; exists {
			continue
		}
		seen[citation.ChunkID] = struct{}{}
		used = append(used, citation)
	}
	return used
}
