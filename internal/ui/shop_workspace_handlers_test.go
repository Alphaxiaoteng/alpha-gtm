package ui

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"alpha-nexus-egolite/internal/db"
)

func TestShopContentAndKnowledgeHandlersUseIsolatedTables(t *testing.T) {
	database, _ := newObserverTestDB(t)
	server := &Server{db: database}
	if _, err := database.CreateContent("article", "Media only", "", "media", "[]", "draft"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateKnowledgeBase("Media KB", ""); err != nil {
		t.Fatal(err)
	}

	create := httptest.NewRecorder()
	server.handleAPIShopContents(create, httptest.NewRequest(http.MethodPost, "/api/shop/contents", bytes.NewBufferString(
		`{"type":"article","title":"Shop only","body":"shop","tags_json":"[]","status":"draft"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var content db.Content
	if err := json.NewDecoder(create.Body).Decode(&content); err != nil || content.ID != 1 || content.Title != "Shop only" {
		t.Fatalf("content=%+v err=%v", content, err)
	}

	baseResponse := httptest.NewRecorder()
	server.handleAPIShopKnowledgeBases(baseResponse, httptest.NewRequest(http.MethodPost, "/api/shop/knowledge/bases", bytes.NewBufferString(
		`{"name":"Shop KB","description":"商品与客服资料"}`)))
	if baseResponse.Code != http.StatusCreated {
		t.Fatalf("base status=%d body=%s", baseResponse.Code, baseResponse.Body.String())
	}
	var base db.KnowledgeBase
	if err := json.NewDecoder(baseResponse.Body).Decode(&base); err != nil || base.ID != 1 {
		t.Fatalf("base=%+v err=%v", base, err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("base_id", "1")
	file, err := writer.CreateFormFile("files", "products.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("# 商品规则\n\n售后承诺必须与店铺真实政策一致。"))
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/shop/knowledge/imports", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	importResponse := httptest.NewRecorder()
	server.handleAPIShopKnowledgeImports(importResponse, request)
	if importResponse.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", importResponse.Code, importResponse.Body.String())
	}

	search := httptest.NewRecorder()
	server.handleAPIShopKnowledgeSearch(search, httptest.NewRequest(http.MethodPost, "/api/shop/knowledge/search", bytes.NewBufferString(
		`{"base_ids":[1],"query":"售后承诺","top_k":6}`)))
	if search.Code != http.StatusOK || !bytes.Contains(search.Body.Bytes(), []byte("店铺真实政策")) {
		t.Fatalf("search status=%d body=%s", search.Code, search.Body.String())
	}

	var mediaContents, shopContents, mediaBases, shopBases int
	for query, target := range map[string]*int{
		`SELECT COUNT(*) FROM contents`:             &mediaContents,
		`SELECT COUNT(*) FROM shop_contents`:        &shopContents,
		`SELECT COUNT(*) FROM knowledge_bases`:      &mediaBases,
		`SELECT COUNT(*) FROM shop_knowledge_bases`: &shopBases,
	} {
		if err := database.QueryRow(query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if mediaContents != 1 || shopContents != 1 || mediaBases != 1 || shopBases != 1 {
		t.Fatalf("mediaContents=%d shopContents=%d mediaBases=%d shopBases=%d", mediaContents, shopContents, mediaBases, shopBases)
	}
}
