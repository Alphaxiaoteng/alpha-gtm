package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestContentHandlerPersistsCanonicalKnowledgeCitationSnapshots(t *testing.T) {
	database, _ := newObserverTestDB(t)
	base, err := database.CreateKnowledgeBase("API 引用库", "")
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "来源.md", SourceType: "markdown", SHA256: strings.Repeat("c", 64), Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{
		{Ordinal: 0, HeadingPath: "章节一", Content: "证据一"},
		{Ordinal: 1, HeadingPath: "章节二", Content: "证据二"},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query(`SELECT id FROM knowledge_chunks WHERE document_id=? ORDER BY ordinal`, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	var chunkIDs []int64
	for rows.Next() {
		var chunkID int64
		if err := rows.Scan(&chunkID); err != nil {
			t.Fatal(err)
		}
		chunkIDs = append(chunkIDs, chunkID)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/contents", bytes.NewBufferString(
		`{"type":"article","title":"引用文章","body":"正文","tags_json":"[]","status":"draft","knowledge_citation_chunk_ids":[`+
			strconv.FormatInt(chunkIDs[1], 10)+`,`+strconv.FormatInt(chunkIDs[0], 10)+`,`+strconv.FormatInt(chunkIDs[1], 10)+`]}`))
	server.handleAPIContents(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var content db.Content
	if err := json.NewDecoder(recorder.Body).Decode(&content); err != nil {
		t.Fatal(err)
	}
	read := httptest.NewRecorder()
	server.handleAPIContentKnowledgeCitations(read, httptest.NewRequest(http.MethodGet, "/api/contents/1/knowledge-citations", nil), content.ID)
	var citations []db.ContentKnowledgeCitation
	if err := json.NewDecoder(read.Body).Decode(&citations); err != nil {
		t.Fatal(err)
	}
	if read.Code != http.StatusOK || len(citations) != 2 || citations[0].ChunkID != chunkIDs[1] || citations[0].Ordinal != 0 || citations[1].ChunkID != chunkIDs[0] || citations[1].Ordinal != 1 ||
		citations[0].BaseID != base.ID || citations[0].DocumentSHA256 != strings.Repeat("c", 64) || citations[0].SourceName != "来源.md" || citations[0].HeadingPath != "章节二" {
		t.Fatalf("status=%d body=%s", read.Code, read.Body.String())
	}
	if _, err := database.ExecContext(context.Background(), `DELETE FROM knowledge_bases WHERE id=?`, base.ID); err != nil {
		t.Fatal(err)
	}
	afterDelete := httptest.NewRecorder()
	server.handleAPIContentKnowledgeCitations(afterDelete, httptest.NewRequest(http.MethodGet, "/api/contents/1/knowledge-citations", nil), content.ID)
	var durable []db.ContentKnowledgeCitation
	if err := json.NewDecoder(afterDelete.Body).Decode(&durable); err != nil {
		t.Fatal(err)
	}
	if afterDelete.Code != http.StatusOK || len(durable) != 2 || durable[0].ChunkID != chunkIDs[1] || durable[0].Ordinal != 0 || durable[1].ChunkID != chunkIDs[0] || durable[1].Ordinal != 1 ||
		durable[0].BaseID != base.ID || durable[0].DocumentSHA256 != strings.Repeat("c", 64) || durable[0].SourceName != "来源.md" || durable[0].HeadingPath != "章节二" {
		t.Fatalf("after=%+v", durable)
	}
}

func TestContentKnowledgeCitationJSONPresenceSemantics(t *testing.T) {
	for _, raw := range []string{"null", `{}`, `["1"]`, `[1.5]`} {
		if _, _, err := parseKnowledgeCitationChunkIDs(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid raw value accepted: %s", raw)
		}
	}
	if values, present, err := parseKnowledgeCitationChunkIDs(nil); err != nil || present || values != nil {
		t.Fatalf("omitted values=%v present=%v err=%v", values, present, err)
	}
	if values, present, err := parseKnowledgeCitationChunkIDs(json.RawMessage(`[]`)); err != nil || !present || len(values) != 0 {
		t.Fatalf("empty values=%v present=%v err=%v", values, present, err)
	}
}

func TestContentHandlerRejectsInvalidCitationJSONWithoutMutation(t *testing.T) {
	database, _ := newObserverTestDB(t)
	server := &Server{db: database}
	invalidValues := []string{"null", `{}`, `["1"]`, `[1.5]`}
	for _, raw := range invalidValues {
		recorder := httptest.NewRecorder()
		server.handleAPIContents(recorder, httptest.NewRequest(http.MethodPost, "/api/contents", bytes.NewBufferString(
			`{"type":"article","title":"不得创建","body":"正文","tags_json":"[]","status":"draft","knowledge_citation_chunk_ids":`+raw+`}`)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("raw=%s status=%d body=%s", raw, recorder.Code, recorder.Body.String())
		}
	}
	var created int
	if err := database.QueryRow(`SELECT COUNT(*) FROM contents`).Scan(&created); err != nil || created != 0 {
		t.Fatalf("created=%d err=%v", created, err)
	}
	base, err := database.CreateKnowledgeBase("拒绝更新引用库", "")
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "reject.md", SourceType: "markdown", SHA256: strings.Repeat("f", 64), Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{{Ordinal: 0, Content: "保留证据"}}); err != nil {
		t.Fatal(err)
	}
	var chunkID int64
	if err := database.QueryRow(`SELECT id FROM knowledge_chunks WHERE document_id=?`, document.ID).Scan(&chunkID); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContentWithPlannedAtAndKnowledgeCitations("article", "原题", "", "正文", "[]", "draft", "", []int64{chunkID})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range invalidValues {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/api/contents/"+strconv.FormatInt(content.ID, 10), bytes.NewBufferString(
			`{"type":"article","title":"不得更新","body":"正文","tags_json":"[]","status":"draft","knowledge_citation_chunk_ids":`+raw+`}`))
		request.SetPathValue("id", strconv.FormatInt(content.ID, 10))
		server.handleAPIContent(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("raw=%s status=%d body=%s", raw, recorder.Code, recorder.Body.String())
		}
		stored, err := database.GetContent(content.ID)
		if err != nil || stored.Title != "原题" {
			t.Fatalf("raw=%s stored=%+v err=%v", raw, stored, err)
		}
		citations, err := database.ListContentKnowledgeCitations(content.ID)
		if err != nil || len(citations) != 1 || citations[0].ChunkID != chunkID {
			t.Fatalf("raw=%s citations=%+v err=%v", raw, citations, err)
		}
	}
}

func TestContentUpdateOmittedCitationsPreservesAndEmptyClears(t *testing.T) {
	database, _ := newObserverTestDB(t)
	base, err := database.CreateKnowledgeBase("更新引用库", "")
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := database.UpsertKnowledgeDocument(db.KnowledgeDocumentInput{
		BaseID: base.ID, SourceName: "update.md", SourceType: "markdown", SHA256: strings.Repeat("d", 64), Status: "ready",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ReplaceKnowledgeChunks(document.ID, []db.KnowledgeChunkInput{
		{Ordinal: 0, Content: "更新证据一"}, {Ordinal: 1, Content: "更新证据二"},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query(`SELECT id FROM knowledge_chunks WHERE document_id=? ORDER BY ordinal`, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	var chunkIDs []int64
	for rows.Next() {
		var chunkID int64
		if err := rows.Scan(&chunkID); err != nil {
			t.Fatal(err)
		}
		chunkIDs = append(chunkIDs, chunkID)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	content, err := database.CreateContentWithPlannedAtAndKnowledgeCitations("article", "原题", "", "正文", "[]", "draft", "", []int64{chunkIDs[0]})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{db: database}
	update := func(payload string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/api/contents/"+strconv.FormatInt(content.ID, 10), bytes.NewBufferString(payload))
		request.SetPathValue("id", strconv.FormatInt(content.ID, 10))
		server.handleAPIContent(recorder, request)
		return recorder
	}
	omitted := update(`{"type":"article","title":"保留","body":"正文","tags_json":"[]","status":"draft"}`)
	if omitted.Code != http.StatusOK {
		t.Fatalf("omitted status=%d body=%s", omitted.Code, omitted.Body.String())
	}
	citations, err := database.ListContentKnowledgeCitations(content.ID)
	if err != nil || len(citations) != 1 || citations[0].ChunkID != chunkIDs[0] {
		t.Fatalf("citations=%+v err=%v", citations, err)
	}
	duplicate := update(`{"type":"article","title":"去重","body":"正文","tags_json":"[]","status":"draft","knowledge_citation_chunk_ids":[` +
		strconv.FormatInt(chunkIDs[1], 10) + `,` + strconv.FormatInt(chunkIDs[0], 10) + `,` + strconv.FormatInt(chunkIDs[1], 10) + `]}`)
	if duplicate.Code != http.StatusOK {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
	read := httptest.NewRecorder()
	server.handleAPIContentKnowledgeCitations(read, httptest.NewRequest(http.MethodGet, "/api/contents/"+strconv.FormatInt(content.ID, 10)+"/knowledge-citations", nil), content.ID)
	var duplicateCitations []db.ContentKnowledgeCitation
	if err := json.NewDecoder(read.Body).Decode(&duplicateCitations); err != nil {
		t.Fatal(err)
	}
	if read.Code != http.StatusOK || len(duplicateCitations) != 2 || duplicateCitations[0].ChunkID != chunkIDs[1] || duplicateCitations[0].Ordinal != 0 || duplicateCitations[1].ChunkID != chunkIDs[0] || duplicateCitations[1].Ordinal != 1 {
		t.Fatalf("duplicate citations=%+v status=%d", duplicateCitations, read.Code)
	}
	empty := update(`{"type":"article","title":"清空","body":"正文","tags_json":"[]","status":"draft","knowledge_citation_chunk_ids":[]}`)
	if empty.Code != http.StatusOK {
		t.Fatalf("empty status=%d body=%s", empty.Code, empty.Body.String())
	}
	citations, err = database.ListContentKnowledgeCitations(content.ID)
	if err != nil || len(citations) != 0 {
		t.Fatalf("citations=%+v err=%v", citations, err)
	}
}

func TestContentHandlerStoresPlannedAtWithoutCreatingPublishWork(t *testing.T) {
	database, _ := newObserverTestDB(t)
	server := &Server{db: database}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/contents", bytes.NewBufferString(`{"type":"article","title":"排期文章","body":"正文","tags_json":"[]","status":"draft","planned_at":"2026-08-01T09:30:00+08:00"}`))
	server.handleAPIContents(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var content db.Content
	if err := json.NewDecoder(recorder.Body).Decode(&content); err != nil {
		t.Fatal(err)
	}
	if content.PlannedAt != "2026-08-01T01:30:00Z" {
		t.Fatalf("plannedAt=%q", content.PlannedAt)
	}
	var jobs int
	if err := database.QueryRow(`SELECT COUNT(*) FROM publish_jobs WHERE content_id=?`, content.ID).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("publish jobs=%d err=%v", jobs, err)
	}
}
