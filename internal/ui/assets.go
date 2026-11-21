package ui

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed all:dist
var uiAssets embed.FS

// AssetsHandler returns an http.Handler that serves the embedded web frontend.
// It supports SPA fallback so sub-routes like /trend, /accounts, etc. serve index.html.
func AssetsHandler() http.Handler {
	distFS, err := fs.Sub(uiAssets, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(filepath.Clean(r.URL.Path), "/")
		if cleanPath == "" {
			// The shell HTML must be revalidated: it names the hashed bundle.
			w.Header().Set("Cache-Control", "no-cache")
			fileServer.ServeHTTP(w, r)
			return
		}

		// If it exists in distFS (e.g. assets/*.js, favicon.ico), serve directly
		f, err := distFS.Open(cleanPath)
		if err == nil {
			f.Close()
			// Vite names every bundle with a content hash, so a year-long
			// immutable cache can never serve stale code and repeat visits skip
			// the network entirely.
			if strings.HasPrefix(cleanPath, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// Do not serve index.html for API requests
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}

		// Fallback to index.html for SPA routes like /trend, /accounts, /content, etc.
		indexFile, err := distFS.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer indexFile.Close()

		stat, err := indexFile.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "index.html", stat.ModTime(), indexFile.(io.ReadSeeker))
	})
}
