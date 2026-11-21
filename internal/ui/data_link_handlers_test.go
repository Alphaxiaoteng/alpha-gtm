package ui

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alpha-nexus-egolite/internal/db"
	"alpha-nexus-egolite/internal/tools"
	_ "modernc.org/sqlite"
)

func TestDataLinkUploadStreamsSQLiteIntoControlledStorage(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(t.TempDir(), "demo.sqlite")
	fixture, err := sql.Open("sqlite", fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(`CREATE TABLE orders(id INTEGER, gmv REAL); INSERT INTO orders VALUES(1,9.9)`); err != nil {
		t.Fatal(err)
	}
	_ = fixture.Close()
	body, contentType := sqliteUploadBody(t, fixturePath, "demo.sqlite")
	req := httptest.NewRequest(http.MethodPost, "/api/data/sqlite-sources", body)
	req.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()
	(&Server{db: database}).handleAPIDataLinkSQLiteSource(response, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	encoded := response.Body.String()
	if strings.Contains(encoded, dataDir) || strings.Contains(encoded, "file_path") {
		t.Fatalf("path leaked: %s", encoded)
	}
	var stored string
	if err := database.QueryRow(`SELECT file_path FROM data_link_sources`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, filepath.FromSlash(stored))); err != nil {
		t.Fatal(err)
	}
}

func TestDataLinkUploadRejectsUnsupportedFile(t *testing.T) {
	database, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "secret.txt")
	_, _ = part.Write([]byte("not sqlite"))
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/data/sqlite-sources", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	(&Server{db: database}).handleAPIDataLinkSQLiteSource(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDataLinkHTTPRegistryEndToEnd(t *testing.T) {
	dataDir := t.TempDir()
	database, err := db.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(); err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(t.TempDir(), "e2e.sqlite")
	fixture, err := sql.Open("sqlite", fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(`CREATE TABLE commerce(day TEXT, shop TEXT, orders INTEGER, gmv REAL); INSERT INTO commerce VALUES ('2026-08-01','demo',2,49.9)`); err != nil {
		t.Fatal(err)
	}
	_ = fixture.Close()
	server := &Server{db: database, toolRuntime: &tools.Runtime{DataDir: dataDir}}
	body, contentType := sqliteUploadBody(t, fixturePath, "e2e.sqlite")
	upload := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/data/sqlite-sources", body)
	request.Header.Set("Content-Type", contentType)
	server.handleAPIDataLinkSQLiteSource(upload, request)
	if upload.Code != http.StatusCreated {
		t.Fatalf("upload=%d %s", upload.Code, upload.Body.String())
	}
	var uploadResult struct {
		Source struct {
			ID int64 `json:"id"`
		} `json:"source"`
	}
	if err := json.Unmarshal(upload.Body.Bytes(), &uploadResult); err != nil {
		t.Fatal(err)
	}

	inspect := callDataLinkToolHTTP(t, server, `{"name":"data.sql.inspect","args":{"source_id":%d,"object_name":"commerce"}}`, uploadResult.Source.ID)
	if !inspect.Result.OK {
		t.Fatalf("inspect=%+v", inspect)
	}
	preflight := callDataLinkToolHTTP(t, server, `{"name":"data.sql.preflight","args":{"source_id":%d,"object_name":"commerce","mapping":{"date":"day","shop":"shop","orders":"orders","gmv":"gmv"}}}`, uploadResult.Source.ID)
	if !preflight.Result.OK {
		t.Fatalf("preflight=%+v", preflight)
	}
	collected := callDataLinkToolHTTP(t, server, `{"name":"data.sql.collect","args":{"source_id":%d,"object_name":"commerce","mapping":{"date":"day","shop":"shop","orders":"orders","gmv":"gmv"}}}`, uploadResult.Source.ID)
	if !collected.Result.OK {
		t.Fatalf("collect=%+v", collected)
	}
	snapshot := collected.Result.Data["snapshot"].(map[string]interface{})
	snapshotID := int64(snapshot["id"].(float64))
	base, err := database.CreateKnowledgeBase("E2E 复盘", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	confirmed := callDataLinkToolHTTP(t, server, `{"name":"data.sql.review.confirm","args":{"snapshot_id":%d,"base_id":%d,"confirm":true}}`, snapshotID, base.ID)
	if !confirmed.Result.OK {
		t.Fatalf("confirm=%+v", confirmed)
	}
	var documents int
	if err := database.QueryRow(`SELECT COUNT(*) FROM knowledge_documents WHERE base_id=? AND source_name LIKE 'sql-link/%' AND status='ready'`, base.ID).Scan(&documents); err != nil {
		t.Fatal(err)
	}
	if documents != 1 {
		t.Fatalf("knowledge documents=%d", documents)
	}
}

type dataLinkToolResponse struct {
	Result struct {
		OK    bool                   `json:"ok"`
		Error string                 `json:"error"`
		Data  map[string]interface{} `json:"data"`
	} `json:"result"`
}

func callDataLinkToolHTTP(t *testing.T, server *Server, format string, args ...interface{}) dataLinkToolResponse {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tools/call", strings.NewReader(fmt.Sprintf(format, args...)))
	response := httptest.NewRecorder()
	server.handleAPIToolsCall(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("tool status=%d body=%s", response.Code, response.Body.String())
	}
	var result dataLinkToolResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func sqliteUploadBody(t *testing.T, path, name string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := io.Copy(part, file); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, writer.FormDataContentType()
}
