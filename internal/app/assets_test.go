package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"supportcenter/web"
)

func TestAssetsCacheByContentVersion(t *testing.T) {
	h := SecurityHeaders(AssetHandler())
	get := func(path, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", path, nil)
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	version, ok := web.AssetVersion("app.css")
	if !ok {
		t.Fatal("app.css has no version")
	}
	// Page links carry the content hash, so they may be cached for a year.
	if res := get("/assets/app.css?v="+version, ""); res.Code != 200 || res.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || res.Header().Get("ETag") != `"`+version+`"` {
		t.Fatalf("versioned asset: %d %q %q", res.Code, res.Header().Get("Cache-Control"), res.Header().Get("ETag"))
	}
	// A stale or guessed version must not be pinned for a year.
	if res := get("/assets/app.css?v=000000000000", ""); res.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Fatal("mismatched version cached long term", res.Header().Get("Cache-Control"))
	}
	// Unversioned files loaded by the image worker revalidate without a download.
	wasm, _ := web.AssetVersion("oxipng.wasm")
	if res := get("/assets/oxipng.wasm", `"`+wasm+`"`); res.Code != http.StatusNotModified || res.Body.Len() != 0 {
		t.Fatal("unchanged asset was downloaded again", res.Code)
	}
	if res := get("/assets/oxipng.wasm", `"stale"`); res.Code != 200 || res.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Fatal("changed asset not served", res.Code, res.Header().Get("Cache-Control"))
	}
}
