package handlers

import (
	"io/fs"
	"net/http"
	"strings"
)

// SPAHandler serves the Vite-built SPA. Requests for existing files (hashed
// asset bundles, favicon, etc.) are served directly; everything else falls
// back to index.html so the client-side router can take over.
func SPAHandler(spa fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(spa))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			serveIndex(w, r, spa)
			return
		}
		if _, err := fs.Stat(spa, clean); err != nil {
			serveIndex(w, r, spa)
			return
		}
		// Long-cache hashed asset bundles; everything else short-cache.
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, spa fs.FS) {
	b, err := fs.ReadFile(spa, "index.html")
	if err != nil {
		http.Error(w, "SPA not built — run `npm run build` in web/", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
	_ = r // silence unused-param lint
}
