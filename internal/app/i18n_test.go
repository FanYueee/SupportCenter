package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"supportcenter/internal/i18n"
	"supportcenter/internal/model"
)

func TestTicketCompletionBuckets(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	_, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "active-only", Category: "other", Priority: "low"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	doneID, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "done-only", Category: "other", Priority: "low"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	done, err := a.Store.Ticket(ctx, alice, doneID, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Update(ctx, staff, doneID, true, "solved", "low", nil, done.Version); err != nil {
		t.Fatal(err)
	}
	closedID, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "closed-only", Category: "other", Priority: "low"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := a.Store.Ticket(ctx, alice, closedID, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Update(ctx, staff, closedID, true, "closed", "low", nil, closed.Version); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, want, absent string }{
		{"/tickets", "active-only", "done-only"},
		{"/tickets?phase=done", "done-only", "active-only"},
		{"/tickets?status=solved", "done-only", "active-only"},
	} {
		got := request(h, "GET", tc.path, "alice", nil)
		if got.Code != 200 || !strings.Contains(got.Body.String(), tc.want) || strings.Contains(got.Body.String(), tc.absent) {
			t.Fatalf("%s: %d %s", tc.path, got.Code, got.Body.String())
		}
	}
	if got := request(h, "GET", "/tickets?phase=done&status=new", "alice", nil); got.Code != 400 {
		t.Fatalf("mismatched phase/status: %d", got.Code)
	}
	if got := request(h, "GET", "/tickets?phase=done", "alice", nil); !strings.Contains(got.Body.String(), "closed-only") {
		t.Fatal("closed ticket missing from completed bucket")
	}
	if err := a.Store.Reply(ctx, alice, doneID, false, "more information", false, nil); err != nil {
		t.Fatal(err)
	}
	if active := request(h, "GET", "/tickets", "alice", nil); !strings.Contains(active.Body.String(), "done-only") {
		t.Fatal("reopened ticket did not return to unfinished bucket")
	}
	if completed := request(h, "GET", "/tickets?phase=done", "alice", nil); strings.Contains(completed.Body.String(), "done-only") {
		t.Fatal("reopened ticket remained in completed bucket")
	}
}

func TestLanguageChoiceAndCSRF(t *testing.T) {
	_, h, _, _, _ := testApp(t)
	page := httptest.NewRequest("GET", "https://support.example.test/account", nil)
	page.Header.Set("Accept-Language", "en-US,en;q=0.9")
	page.AddCookie(cookie("__Host-support-session", "alice", 3600))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, page)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `<html lang="en"`) || !strings.Contains(w.Body.String(), `id="language-heading">Language`) || !strings.Contains(w.Body.String(), `name="language" value="en" aria-pressed="true"`) || strings.Contains(w.Body.String(), `class="language-form"`) {
		t.Fatalf("English page: %d", w.Code)
	}
	bad := request(h, "POST", "/language", "alice", url.Values{"language": {"zh-Hant"}})
	if bad.Code != 403 {
		t.Fatalf("missing CSRF: %d", bad.Code)
	}
	good := request(h, "POST", "/language", "alice", url.Values{"csrf": {"csrf-alice"}, "language": {"zh-Hant"}})
	if good.Code != 303 || good.Header().Get("Location") != "/account" {
		t.Fatalf("language redirect: %d %s", good.Code, good.Header().Get("Location"))
	}
	cookies := good.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != i18n.CookieName || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("language cookie: %+v", cookies)
	}
	selected := httptest.NewRequest("GET", "https://support.example.test/account", nil)
	selected.Header.Set("Accept-Language", "en-US")
	selected.AddCookie(cookie("__Host-support-session", "alice", 3600))
	selected.AddCookie(cookies[0])
	localized := httptest.NewRecorder()
	h.ServeHTTP(localized, selected)
	if localized.Code != 200 || !strings.Contains(localized.Body.String(), `<html lang="zh-Hant"`) || !strings.Contains(localized.Body.String(), `name="language" value="zh-Hant" aria-pressed="true"`) {
		t.Fatal("saved language did not override browser language")
	}
	if invalid := request(h, "POST", "/language", "alice", url.Values{"csrf": {"csrf-alice"}, "language": {"invalid"}}); invalid.Code != 422 {
		t.Fatalf("invalid language accepted: %d", invalid.Code)
	}
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("GET", "https://support.example.test/", nil))
	if login.Code != 200 || strings.Contains(login.Body.String(), `action="/language"`) {
		t.Fatalf("language control leaked into sign-in page: %d", login.Code)
	}
	unauthenticated := request(h, "POST", "/language", "", url.Values{"language": {"en"}})
	if unauthenticated.Code != 303 || unauthenticated.Header().Get("Location") != "/" || len(unauthenticated.Result().Cookies()) != 0 {
		t.Fatalf("anonymous language switch accepted: %d", unauthenticated.Code)
	}
}
