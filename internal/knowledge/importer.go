package knowledge

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"alpha-nexus-egolite/internal/db"
	"github.com/giraffesyo/downmark"
	"github.com/giraffesyo/downmark/convert/docx"
	"github.com/giraffesyo/downmark/convert/html"
	"github.com/giraffesyo/downmark/convert/pdf"
)

const (
	maxInputBytes        = int64(64 << 20)
	maxExpandedFileBytes = int64(16 << 20)
	maxExpandedTotal     = int64(128 << 20)
	maxArchiveEntries    = 1024
)

type InputFile struct {
	Name string
	Path string
}

type ImportRequest struct {
	BaseID int64
	Files  []InputFile
	OnItem func(ImportItem)
}

type ImportItem struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DocumentID int64  `json:"document_id,omitempty"`
	ErrorCode  string `json:"error_code,omitempty"`
	Message    string `json:"message,omitempty"`
}

type ImportResult struct {
	RunID    int64        `json:"run_id"`
	Total    int          `json:"total"`
	Imported int          `json:"imported"`
	Skipped  int          `json:"skipped"`
	Failed   int          `json:"failed"`
	Items    []ImportItem `json:"items"`
}

type Importer struct {
	db     *db.DB
	engine *downmark.Engine
}

func NewImporter(database *db.DB) *Importer {
	engine := newConversionEngine()
	return &Importer{db: database, engine: engine}
}

func newConversionEngine() *downmark.Engine {
	engine := downmark.New(downmark.WithoutBuiltins())
	pdf.Register(engine)
	docx.Register(engine, docx.Options{})
	html.Register(engine, html.Options{})
	return engine
}

// ConvertFileToMarkdown exposes the same reviewed local parser for additional
// knowledge domains. It performs no persistence and makes no network calls.
func ConvertFileToMarkdown(ctx context.Context, name, filePath string) (string, error) {
	return (&Importer{engine: newConversionEngine()}).convert(ctx, name, filePath)
}

func (i *Importer) Import(ctx context.Context, request ImportRequest) (ImportResult, error) {
	if i == nil || i.db == nil || request.BaseID <= 0 || len(request.Files) == 0 {
		return ImportResult{}, fmt.Errorf("invalid knowledge import")
	}
	if _, err := i.db.GetKnowledgeBase(request.BaseID); err != nil {
		return ImportResult{}, err
	}
	run, err := i.db.CreateKnowledgeImportRun(request.BaseID)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{RunID: run.ID}
	add := func(item ImportItem) error {
		result.add(item)
		if request.OnItem != nil {
			request.OnItem(item)
		}
		return i.db.UpdateKnowledgeImportRunProgress(run.ID, int64(result.Total), int64(result.Imported), int64(result.Skipped), int64(result.Failed))
	}
	for _, input := range request.Files {
		if err := ctx.Err(); err != nil {
			_ = i.db.InterruptKnowledgeImportRun(run.ID, int64(result.Total), int64(result.Imported), int64(result.Skipped), int64(result.Failed))
			return result, err
		}
		if sensitiveKnowledgeName(input.Name) {
			if err := add(ImportItem{Name: input.Name, Status: "skipped", ErrorCode: "sensitive_source_skipped"}); err != nil {
				return result, err
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(input.Name), ".zip") {
			items := i.importZip(ctx, request.BaseID, run.ID, input)
			for _, item := range items {
				if err := add(item); err != nil {
					return result, err
				}
			}
			continue
		}
		item := i.importPath(ctx, request.BaseID, run.ID, input.Name, input.Path, maxInputBytes)
		if err := add(item); err != nil {
			return result, err
		}
	}
	if err := i.db.CompleteKnowledgeImportRun(run.ID, int64(result.Total), int64(result.Imported), int64(result.Skipped), int64(result.Failed)); err != nil {
		return result, err
	}
	return result, nil
}

func (r *ImportResult) add(item ImportItem) {
	r.Total++
	r.Items = append(r.Items, item)
	switch item.Status {
	case "imported":
		r.Imported++
	case "skipped":
		r.Skipped++
	default:
		r.Failed++
	}
}

func (i *Importer) importZip(ctx context.Context, baseID, runID int64, input InputFile) []ImportItem {
	info, err := os.Stat(input.Path)
	if err != nil || info.Size() > maxInputBytes {
		return []ImportItem{{Name: input.Name, Status: "failed", ErrorCode: "input_too_large", Message: safeError(err)}}
	}
	reader, err := zip.OpenReader(input.Path)
	if err != nil {
		return []ImportItem{{Name: input.Name, Status: "failed", ErrorCode: "invalid_zip", Message: safeError(err)}}
	}
	defer reader.Close()
	if len(reader.File) > maxArchiveEntries {
		return []ImportItem{{Name: input.Name, Status: "failed", ErrorCode: "archive_entry_limit"}}
	}
	var total int64
	items := make([]ImportItem, 0, len(reader.File))
	for _, entry := range reader.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		clean := filepath.Clean(filepath.ToSlash(entry.Name))
		if clean == "." || strings.HasPrefix(clean, "../") || filepath.IsAbs(clean) {
			items = append(items, ImportItem{Name: entry.Name, Status: "failed", ErrorCode: "archive_path_traversal"})
			continue
		}
		if strings.EqualFold(filepath.Ext(clean), ".zip") {
			items = append(items, ImportItem{Name: clean, Status: "failed", ErrorCode: "nested_archive_unsupported"})
			continue
		}
		size := int64(entry.UncompressedSize64)
		total += size
		if size > maxExpandedFileBytes || total > maxExpandedTotal {
			items = append(items, ImportItem{Name: clean, Status: "failed", ErrorCode: "archive_expansion_limit"})
			continue
		}
		file, err := entry.Open()
		if err != nil {
			items = append(items, ImportItem{Name: clean, Status: "failed", ErrorCode: "archive_read_failed", Message: safeError(err)})
			continue
		}
		temp, err := os.CreateTemp("", "alpha-nexus-knowledge-*"+filepath.Ext(clean))
		if err == nil {
			_, err = io.Copy(temp, io.LimitReader(file, maxExpandedFileBytes+1))
		}
		_ = file.Close()
		if temp != nil {
			_ = temp.Close()
		}
		if err != nil {
			if temp != nil {
				_ = os.Remove(temp.Name())
			}
			items = append(items, ImportItem{Name: clean, Status: "failed", ErrorCode: "archive_read_failed", Message: safeError(err)})
			continue
		}
		item := i.importPath(ctx, baseID, runID, clean, temp.Name(), maxExpandedFileBytes)
		_ = os.Remove(temp.Name())
		items = append(items, item)
	}
	if len(items) == 0 {
		items = append(items, ImportItem{Name: input.Name, Status: "failed", ErrorCode: "archive_empty"})
	}
	return items
}

func (i *Importer) importPath(ctx context.Context, baseID, runID int64, name, path string, maxBytes int64) ImportItem {
	name = strings.TrimSpace(filepath.ToSlash(name))
	if name == "" || path == "" {
		return ImportItem{Name: name, Status: "failed", ErrorCode: "invalid_input"}
	}
	if sensitiveKnowledgeName(name) {
		return ImportItem{Name: name, Status: "skipped", ErrorCode: "sensitive_source_skipped"}
	}
	if _, supported := supportedKnowledgeExtensions[strings.ToLower(filepath.Ext(name))]; !supported {
		return ImportItem{Name: name, Status: "skipped", ErrorCode: "unsupported_type"}
	}
	info, err := os.Stat(path)
	if err != nil {
		return ImportItem{Name: name, Status: "failed", ErrorCode: "read_failed", Message: safeError(err)}
	}
	if info.Size() <= 0 || info.Size() > maxBytes {
		return ImportItem{Name: name, Status: "failed", ErrorCode: "input_too_large"}
	}
	sourceModifiedNS := info.ModTime().UnixNano()
	if cached, cacheErr := i.db.FindKnowledgeDocumentBySourceMetadata(baseID, name, info.Size(), sourceModifiedNS); cacheErr != nil {
		return ImportItem{Name: name, Status: "failed", ErrorCode: "database_failed", Message: safeError(cacheErr)}
	} else if cached != nil {
		if cached.Status == "ready" {
			return ImportItem{Name: name, Status: "skipped", DocumentID: cached.ID}
		}
		if cached.Status == "superseded" {
			return ImportItem{Name: name, Status: "skipped", DocumentID: cached.ID, ErrorCode: "stale_source_version"}
		}
		if cached.Status == "failed" && cached.ErrorCode == "no_extractable_text" {
			return ImportItem{Name: name, Status: "skipped", DocumentID: cached.ID, ErrorCode: "unchanged_no_extractable_text"}
		}
	}
	if cached, cacheErr := i.bootstrapLegacySourceMetadata(baseID, runID, name, info); cacheErr != nil {
		return ImportItem{Name: name, Status: "failed", ErrorCode: "database_failed", Message: safeError(cacheErr)}
	} else if cached != nil {
		return ImportItem{Name: name, Status: "skipped", DocumentID: cached.ID}
	}
	file, err := os.Open(path)
	if err != nil {
		return ImportItem{Name: name, Status: "failed", ErrorCode: "read_failed", Message: safeError(err)}
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(file, maxBytes+1)); err != nil {
		_ = file.Close()
		return ImportItem{Name: name, Status: "failed", ErrorCode: "read_failed", Message: safeError(err)}
	}
	_ = file.Close()
	digest := hex.EncodeToString(hash.Sum(nil))
	pending := db.KnowledgeDocumentInput{
		BaseID: baseID, ImportRunID: runID, SourceName: name, SourceType: sourceType(name), SHA256: digest, Status: "pending",
		SourceSizeBytes: info.Size(), SourceModifiedNS: sourceModifiedNS,
	}
	existing, created, err := i.db.UpsertKnowledgeDocument(pending)
	if err != nil {
		return ImportItem{Name: name, Status: "failed", ErrorCode: "database_failed", Message: safeError(err)}
	}
	if !created && existing.Status == "ready" {
		if existing.SourceName == name {
			if err := i.db.UpdateKnowledgeDocumentSourceMetadata(existing.ID, runID, name, info.Size(), sourceModifiedNS); err != nil {
				return ImportItem{Name: name, Status: "failed", ErrorCode: "database_failed", Message: safeError(err)}
			}
		}
		return ImportItem{Name: name, Status: "skipped", DocumentID: existing.ID}
	}
	if !created {
		if _, _, err := i.replacePendingDocument(pending); err != nil {
			return ImportItem{Name: name, Status: "failed", ErrorCode: "database_failed", Message: safeError(err)}
		}
	}

	markdown, convertErr := i.convert(ctx, name, path)
	if convertErr != nil || strings.TrimSpace(markdown) == "" {
		code := "parse_failed"
		if convertErr == nil || strings.Contains(strings.ToLower(safeError(convertErr)), "no extractable text") {
			code = "no_extractable_text"
		}
		return i.persistFailure(baseID, name, digest, code, convertErr, info.Size(), sourceModifiedNS)
	}
	chunks := ChunkMarkdown(markdown)
	if len(chunks) == 0 {
		return i.persistFailure(baseID, name, digest, "no_extractable_text", nil, info.Size(), sourceModifiedNS)
	}
	relativeOriginal, relativeNormalized, err := i.persistFiles(digest, filepath.Ext(name), path, markdown)
	if err != nil {
		return i.persistFailure(baseID, name, digest, "storage_failed", err, info.Size(), sourceModifiedNS)
	}
	document, _, err := i.replacePendingDocument(db.KnowledgeDocumentInput{
		BaseID: baseID, ImportRunID: runID, SourceName: name, SourceType: sourceType(name), SHA256: digest, Status: "pending",
		OriginalPath: relativeOriginal, NormalizedPath: relativeNormalized,
		SourceSizeBytes: info.Size(), SourceModifiedNS: sourceModifiedNS,
	})
	if err != nil {
		return ImportItem{Name: name, Status: "failed", ErrorCode: "storage_failed", Message: safeError(err)}
	}
	current, err := i.db.FinalizeKnowledgeDocument(document.ID, chunks)
	if err != nil {
		return ImportItem{Name: name, Status: "failed", DocumentID: document.ID, ErrorCode: "index_failed", Message: safeError(err)}
	}
	if !current {
		return ImportItem{Name: name, Status: "skipped", DocumentID: document.ID, ErrorCode: "stale_source_version"}
	}
	return ImportItem{Name: name, Status: "imported", DocumentID: document.ID}
}

func (i *Importer) bootstrapLegacySourceMetadata(baseID, runID int64, name string, info os.FileInfo) (*db.KnowledgeDocument, error) {
	document, err := i.db.FindReadyKnowledgeDocumentBySourceName(baseID, name)
	if err != nil || document == nil || document.SourceSizeBytes > 0 || document.SourceModifiedNS > 0 || document.OriginalPath == "" {
		return nil, err
	}
	indexedAt, ok := parseKnowledgeTimestamp(document.UpdatedAt)
	if !ok || info.ModTime().After(indexedAt) {
		return nil, nil
	}
	originalRoot := filepath.Join(i.db.DataDir, "knowledge", "originals")
	originalPath := filepath.Join(i.db.DataDir, filepath.FromSlash(document.OriginalPath))
	relative, err := filepath.Rel(originalRoot, originalPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, nil
	}
	stored, err := os.Stat(originalPath)
	if err != nil || !stored.Mode().IsRegular() || stored.Size() != info.Size() {
		return nil, nil
	}
	if err := i.db.UpdateKnowledgeDocumentSourceMetadata(document.ID, runID, name, info.Size(), info.ModTime().UnixNano()); err != nil {
		return nil, err
	}
	document.SourceSizeBytes = info.Size()
	document.SourceModifiedNS = info.ModTime().UnixNano()
	return document, nil
}

func parseKnowledgeTimestamp(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func (i *Importer) convert(ctx context.Context, name, path string) (string, error) {
	extension := strings.ToLower(filepath.Ext(name))
	if extension == ".md" || extension == ".markdown" || extension == ".txt" {
		body, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if !utf8.Valid(body) {
			return "", fmt.Errorf("text is not valid UTF-8")
		}
		return string(body), nil
	}
	source, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer source.Close()
	converted, err := i.engine.Convert(ctx, source, downmark.StreamInfo{
		Filename: filepath.Base(name), Extension: extension, LocalPath: path,
	})
	if err != nil {
		return "", err
	}
	if converted == nil {
		return "", fmt.Errorf("empty conversion result")
	}
	return converted.Markdown, nil
}

func (i *Importer) replacePendingDocument(input db.KnowledgeDocumentInput) (*db.KnowledgeDocument, bool, error) {
	var id int64
	err := i.db.QueryRow(`SELECT id FROM knowledge_documents WHERE base_id=? AND sha256=?`, input.BaseID, input.SHA256).Scan(&id)
	if err != nil {
		return i.db.UpsertKnowledgeDocument(input)
	}
	var runID any
	if input.ImportRunID > 0 {
		runID = input.ImportRunID
	}
	_, err = i.db.Exec(`UPDATE knowledge_documents SET import_run_id=COALESCE(?,import_run_id),
		source_name=?,source_type=?,status=?,error_code=?,error_message=?,original_path=?,
		normalized_path=?,source_size_bytes=?,source_modified_ns=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		runID, input.SourceName, input.SourceType, input.Status, strings.TrimSpace(input.ErrorCode),
		strings.TrimSpace(input.ErrorMessage), input.OriginalPath, input.NormalizedPath,
		input.SourceSizeBytes, input.SourceModifiedNS, id)
	if err != nil {
		return nil, false, err
	}
	item, err := i.db.GetKnowledgeDocument(id)
	return item, false, err
}

func (i *Importer) persistFailure(baseID int64, name, digest, code string, cause error, sourceSizeBytes, sourceModifiedNS int64) ImportItem {
	input := db.KnowledgeDocumentInput{
		BaseID: baseID, SourceName: name, SourceType: sourceType(name), SHA256: digest,
		Status: "failed", ErrorCode: code, ErrorMessage: safeError(cause),
		SourceSizeBytes: sourceSizeBytes, SourceModifiedNS: sourceModifiedNS,
	}
	document, _, err := i.replacePendingDocument(input)
	item := ImportItem{Name: name, Status: "failed", ErrorCode: code, Message: safeError(cause)}
	if err == nil {
		item.DocumentID = document.ID
	} else {
		item.Message = safeError(err)
	}
	return item
}

func (i *Importer) persistFiles(digest, extension, sourcePath, markdown string) (string, string, error) {
	knowledgeRoot := filepath.Join(i.db.DataDir, "knowledge")
	originals := filepath.Join(knowledgeRoot, "originals")
	normalized := filepath.Join(knowledgeRoot, "normalized")
	if err := os.MkdirAll(originals, 0700); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(normalized, 0700); err != nil {
		return "", "", err
	}
	extension = strings.ToLower(extension)
	if len(extension) > 10 {
		extension = ""
	}
	originalPath := filepath.Join(originals, digest+extension)
	if _, err := os.Stat(originalPath); os.IsNotExist(err) {
		input, openErr := os.Open(sourcePath)
		if openErr != nil {
			return "", "", openErr
		}
		output, createErr := os.OpenFile(originalPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr == nil {
			_, createErr = io.Copy(output, input)
			_ = output.Close()
		}
		_ = input.Close()
		if createErr != nil && !os.IsExist(createErr) {
			return "", "", createErr
		}
	}
	normalizedPath := filepath.Join(normalized, digest+".md")
	if err := os.WriteFile(normalizedPath, []byte(markdown), 0600); err != nil {
		return "", "", err
	}
	originalRel, _ := filepath.Rel(i.db.DataDir, originalPath)
	normalizedRel, _ := filepath.Rel(i.db.DataDir, normalizedPath)
	return filepath.ToSlash(originalRel), filepath.ToSlash(normalizedRel), nil
}

func sourceType(name string) string {
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if extension == "md" || extension == "markdown" {
		return "markdown"
	}
	if extension == "" {
		return "text"
	}
	return extension
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 300 {
		message = message[:300]
	}
	return message
}
