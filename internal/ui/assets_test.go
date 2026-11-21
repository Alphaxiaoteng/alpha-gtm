package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func assetsRequest(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	AssetsHandler().ServeHTTP(recorder, request)
	return recorder
}

func TestHashedAssetsAreImmutable(t *testing.T) {
	// Find a real hashed asset name from the embedded build so this test does
	// not depend on a hard-coded bundle name.
	index := assetsRequest(t, "/")
	if index.Code != http.StatusOK {
		t.Fatalf("index status=%d", index.Code)
	}
	body := index.Body.String()
	start := strings.Index(body, "/assets/")
	if start < 0 {
		t.Fatal("index.html must reference a hashed asset")
	}
	end := strings.IndexAny(body[start:], "\"'")
	if end < 0 {
		t.Fatal("cannot read the hashed asset path")
	}
	assetPath := body[start : start+end]

	asset := assetsRequest(t, assetPath)
	if asset.Code != http.StatusOK {
		t.Fatalf("asset %s status=%d", assetPath, asset.Code)
	}
	if got := asset.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache-control=%q", got)
	}
}

func TestShellHTMLIsRevalidated(t *testing.T) {
	for _, path := range []string{"/", "/bot", "/dashboard"} {
		recorder := assetsRequest(t, path)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("%s cache-control=%q", path, got)
		}
	}
}
