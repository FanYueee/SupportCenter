package app

import (
	"bytes"
	"image"
	"image/gif"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func writeImage(t *testing.T, name string, w, h int, encode func(*bytes.Buffer, image.Image) error) string {
	t.Helper()
	var out bytes.Buffer
	if err := encode(&out, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func encodePNG(out *bytes.Buffer, img image.Image) error { return png.Encode(out, img) }

func TestSiteFromEnvironment(t *testing.T) {
	logo, icon := writeImage(t, "logo.png", 640, 149, encodePNG), writeImage(t, "icon.png", 192, 192, encodePNG)
	set := func(env map[string]string) {
		for _, key := range []string{"SITE_NAME", "SITE_OWNER", "SITE_WEBSITE", "SITE_LOGO", "SITE_ICON"} {
			t.Setenv(key, env[key])
		}
	}
	set(map[string]string{"SITE_NAME": " Example Cloud ", "SITE_OWNER": "Example Cloud Ltd.", "SITE_WEBSITE": "https://example.com/zh", "SITE_LOGO": logo, "SITE_ICON": icon})
	site, err := SiteFromEnvironment()
	if err != nil || site.Name != "Example Cloud" || site.Owner != "Example Cloud Ltd." || site.Website != "https://example.com/zh" {
		t.Fatal(site, err)
	}
	if site.Logo.File != "logo.png" || site.Logo.Type != "image/png" || site.Logo.Width != 640 || site.Icon.File != "icon.png" || site.Icon.Height != 192 {
		t.Fatalf("images: %+v %+v", site.Logo, site.Icon)
	}
	set(nil)
	if site, err = SiteFromEnvironment(); err != nil || site.Name != "" || site.Logo != nil || site.Icon != nil || site.Title() != "支援中心" {
		t.Fatal("an unconfigured site must still start", site, err)
	}
	notImage := filepath.Join(t.TempDir(), "logo.svg")
	os.WriteFile(notImage, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), 0o600)
	huge := filepath.Join(t.TempDir(), "huge.png")
	os.WriteFile(huge, make([]byte, 1<<20+1), 0o600)
	for name, env := range map[string]map[string]string{
		"control character": {"SITE_NAME": "Example\nCloud"},
		"long name":         {"SITE_NAME": strings.Repeat("長", 61)},
		"script website":    {"SITE_WEBSITE": "javascript:alert(1)"},
		"relative website":  {"SITE_WEBSITE": "/home"},
		"credentials":       {"SITE_WEBSITE": "https://user:pass@example.com"},
		"missing logo":      {"SITE_LOGO": filepath.Join(t.TempDir(), "none.png")},
		"svg logo":          {"SITE_LOGO": notImage},
		"gif icon":          {"SITE_ICON": writeImage(t, "icon.gif", 64, 64, func(out *bytes.Buffer, img image.Image) error { return gif.Encode(out, img, nil) })},
		"tiny icon":         {"SITE_ICON": writeImage(t, "tiny.png", 8, 8, encodePNG)},
		"large file":        {"SITE_LOGO": huge},
	} {
		set(env)
		if _, err := SiteFromEnvironment(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestPagesShowTheConfiguredSite(t *testing.T) {
	a, h, _, _, _ := testApp(t)
	t.Setenv("SITE_NAME", "Example Cloud")
	t.Setenv("SITE_OWNER", "Example Cloud Ltd.")
	t.Setenv("SITE_WEBSITE", "https://example.com")
	t.Setenv("SITE_LOGO", writeImage(t, "logo.png", 640, 149, encodePNG))
	t.Setenv("SITE_ICON", writeImage(t, "icon.png", 192, 192, encodePNG))
	site, err := SiteFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	a.Site = site
	year := strconv.Itoa(time.Now().Year())
	login := request(h, "GET", "/", "", nil).Body.String()
	for _, want := range []string{
		"<title>登入 · Example Cloud 支援中心</title>",
		`<link rel="icon" type="image/png" href="` + site.Icon.URL() + `">`,
		`aria-label="Example Cloud 支援中心"`,
		`<img src="` + site.Logo.URL() + `" alt="Example Cloud" width="206" height="48">`,
		`<source media="(max-width: 760px)" srcset="` + site.Icon.URL() + `"`,
		"<h1>登入 Example Cloud 支援中心</h1>",
		`<a href="https://example.com" aria-label="Example Cloud 官方網站">© ` + year + ` Example Cloud Ltd.</a></footer>`,
		"<span>登入</span>",
	} {
		if !strings.Contains(login, want) {
			t.Errorf("sign-in page missing %s", want)
		}
	}
	if page := request(h, "GET", "/tickets", "alice", nil).Body.String(); !strings.Contains(page, `<span class="brand-name" aria-hidden="true">Example Cloud</span>`) || !strings.Contains(page, "Example Cloud Ltd.</a></footer>") {
		t.Error("signed-in pages lost the site name or footer")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "en")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if english := w.Body.String(); !strings.Contains(english, "<h1>Sign in to Example Cloud Support Center</h1>") || !strings.Contains(english, `aria-label="Example Cloud website"`) {
		t.Error("English pages do not name the site")
	}
	// The logo is served from memory and cached for good under its versioned URL.
	res := request(h, "GET", site.Logo.URL(), "", nil)
	if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "image/png" || !strings.Contains(res.Header().Get("Cache-Control"), "immutable") || !bytes.Equal(res.Body.Bytes(), site.Logo.Data) {
		t.Fatal("logo not served", res.Code, res.Header())
	}
	if res = request(h, "GET", "/branding/logo.png", "", nil); res.Header().Get("Cache-Control") != "public, max-age=3600" {
		t.Fatal("unversioned logo cached for good", res.Header().Get("Cache-Control"))
	}
	for _, path := range []string{"/branding/other.png", "/branding/..%2f.env", "/branding/logo.jpg"} {
		if res = request(h, "GET", path, "", nil); res.Code != http.StatusNotFound {
			t.Error(path, res.Code)
		}
	}
}

func TestUnconfiguredSiteStaysNeutral(t *testing.T) {
	_, h, _, _, _ := testApp(t)
	page := request(h, "GET", "/", "", nil).Body.String()
	for _, want := range []string{"<title>登入 · 支援中心</title>", `type="image/svg+xml"`, `class="brand-icon"`, "<h1>登入支援中心</h1>"} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, unwanted := range []string{"<footer", "brand-divider", "brand-name", "/branding/"} {
		if strings.Contains(page, unwanted) {
			t.Errorf("unexpected %s", unwanted)
		}
	}
	if res := request(h, "GET", "/branding/logo.png", "", nil); res.Code != http.StatusNotFound {
		t.Fatal("logo served without one configured", res.Code)
	}
}

// Every hostname that reaches the site signs in, posts and signs out through APP_URL.
func TestOtherHostsUseTheConfiguredOrigin(t *testing.T) {
	a, _, _, _, _ := testApp(t)
	a.Auth = &Auth{OAuth: oauth2.Config{ClientID: "test-client", RedirectURL: a.Origin + "/auth/callback", Endpoint: oauth2.Endpoint{AuthURL: "https://sso.example.test/authorize"}, Scopes: []string{"openid"}}, Issuer: "https://sso.example.test/"}
	h := a.Handler()
	send := func(method, path, origin string, form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://other.example.test"+path, strings.NewReader(form.Encode()))
		if form != nil {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.AddCookie(cookie("__Host-support-session", "alice", 86400))
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	res := send("GET", "/login", "", nil)
	target, err := url.Parse(res.Header().Get("Location"))
	if res.Code != http.StatusFound || err != nil || target.Query().Get("redirect_uri") != a.Origin+"/auth/callback" {
		t.Fatal("sign-in left the configured origin", res.Header().Get("Location"))
	}
	form := url.Values{"csrf": {"csrf-alice"}, "language": {"en"}}
	if res = send("POST", "/language", "https://other.example.test", form); res.Code != http.StatusForbidden {
		t.Fatal("post from another origin accepted", res.Code)
	}
	if res = send("POST", "/language", a.Origin, form); res.Code != http.StatusSeeOther {
		t.Fatal("post from the configured origin rejected", res.Code)
	}
	res = send("POST", "/logout", a.Origin, url.Values{"csrf": {"csrf-alice"}})
	if target, _ = url.Parse(res.Header().Get("Location")); res.Code != http.StatusSeeOther || target.Query().Get("returnTo") != a.Origin {
		t.Fatal("sign-out left the configured origin", res.Header().Get("Location"))
	}
}
