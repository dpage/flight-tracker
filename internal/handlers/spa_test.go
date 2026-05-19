package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestSPAHandlerServesIndexAtRoot(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>app</title>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
		"favicon.ico":   {Data: []byte("ico")},
	}
	h := SPAHandler(fsys)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("root = %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("index should be no-cache, got %q", w.Header().Get("Cache-Control"))
	}

	// Unknown client-side route → falls back to index.html.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/some/spa/route", nil))
	if w.Code != 200 || w.Body.String() == "" {
		t.Errorf("spa fallback = %d", w.Code)
	}

	// Hashed asset → long-cache header + served directly.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/assets/app.js", nil))
	if w.Code != 200 {
		t.Fatalf("asset = %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("asset cache header = %q", cc)
	}

	// Existing non-asset file served without the long-cache header.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/favicon.ico", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") == "public, max-age=31536000, immutable" {
		t.Errorf("favicon served wrong: %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestSPAHandlerMissingIndex(t *testing.T) {
	// No index.html → serveIndex returns 500.
	h := SPAHandler(fstest.MapFS{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("missing index = %d, want 500", w.Code)
	}
}
