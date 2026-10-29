package app

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func TestBrandHostsFromEnvironment(t *testing.T) {
	hosts, err := BrandHostsFromEnvironment(" support.vproxy.cloud=vproxy ,", "support.atomroute.com")
	if err != nil || len(hosts) != 1 || hosts["support.vproxy.cloud"] != "vproxy" {
		t.Fatal(hosts, err)
	}
	if hosts, err = BrandHostsFromEnvironment("", "support.atomroute.com"); err != nil || len(hosts) != 0 {
		t.Fatal(hosts, err)
	}
	for _, bad := range []string{"support.vproxy.cloud", "support.vproxy.cloud=unknown", "support.atomroute.com=vproxy", "https://support.vproxy.cloud=vproxy", "Support.Vproxy.Cloud=vproxy"} {
		if _, err := BrandHostsFromEnvironment(bad, "support.atomroute.com"); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestBrandHostSharesSignInWithItsOwnOrigin(t *testing.T) {
	a, _, _, _, _ := testApp(t)
	a.Auth = &Auth{OAuth: oauth2.Config{ClientID: "test-client", RedirectURL: a.Origin + "/auth/callback", Endpoint: oauth2.Endpoint{AuthURL: "https://sso.example.test/authorize"}, Scopes: []string{"openid"}}, Issuer: "https://sso.example.test/"}
	a.BrandHosts = map[string]string{"support.vproxy.test": "vproxy"}
	h := a.Handler()
	send := func(method, host, path, origin string, form url.Values) *httptest.ResponseRecorder {
		var body *strings.Reader
		if form != nil {
			body = strings.NewReader(form.Encode())
		} else {
			body = strings.NewReader("")
		}
		r := httptest.NewRequest(method, "https://"+host+path, body)
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
	for host, want := range map[string]string{
		"support.vproxy.test":  "https://support.vproxy.test/auth/callback",
		"support.example.test": a.Origin + "/auth/callback",
		"unlisted.example":     a.Origin + "/auth/callback",
	} {
		res := send("GET", host, "/login", "", nil)
		target, err := url.Parse(res.Header().Get("Location"))
		if res.Code != http.StatusFound || err != nil || target.Query().Get("redirect_uri") != want {
			t.Fatalf("%s: redirect_uri %q, want %q", host, target.Query().Get("redirect_uri"), want)
		}
	}
	for host, logo := range map[string]string{"support.vproxy.test": "vproxy-logo.png", "support.example.test": "atomroute-logo.png"} {
		body := send("GET", host, "/", "", nil).Body.String()
		if !strings.Contains(body, logo) || strings.Count(body, "-logo.png") != 1 {
			t.Fatalf("%s: expected only %s", host, logo)
		}
	}
	// Each hostname accepts form posts only from its own origin.
	form := url.Values{"csrf": {"csrf-alice"}, "language": {"en"}}
	if res := send("POST", "support.vproxy.test", "/language", "https://support.vproxy.test", form); res.Code != http.StatusSeeOther {
		t.Fatal("same-origin post on the brand host rejected", res.Code)
	}
	if res := send("POST", "support.vproxy.test", "/language", a.Origin, form); res.Code != http.StatusForbidden {
		t.Fatal("cross-host post accepted", res.Code)
	}
	res := send("POST", "support.vproxy.test", "/logout", "https://support.vproxy.test", url.Values{"csrf": {"csrf-alice"}})
	target, _ := url.Parse(res.Header().Get("Location"))
	if res.Code != http.StatusSeeOther || target.Query().Get("returnTo") != "https://support.vproxy.test" {
		t.Fatal("logout did not return to the brand host", res.Code, res.Header().Get("Location"))
	}
}
