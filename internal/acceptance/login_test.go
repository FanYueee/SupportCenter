package acceptance

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"supportcenter/internal/model"
	"testing"
)

func TestAcceptanceRejectsUnauthenticatedWrites(t *testing.T) {
	h := Handler(http.NotFoundHandler(), nil, "https://acceptance.test", "a-private-password-at-least-32-characters", map[string]model.User{"customer": {ID: 1, Role: "user"}})
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "https://acceptance.test/login", nil))
	cookies := page.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal("login CSRF cookie insecure")
	}
	english := httptest.NewRequest("GET", "https://acceptance.test/login", nil)
	english.Header.Set("Accept-Language", "en-US")
	translated := httptest.NewRecorder()
	h.ServeHTTP(translated, english)
	if translated.Code != 200 || translated.Header().Get("Content-Language") != "en" || !strings.Contains(translated.Body.String(), `<html lang="en"`) || !strings.Contains(translated.Body.String(), "Acceptance sign in") {
		t.Fatal("acceptance sign-in was not translated")
	}
	head := httptest.NewRecorder()
	h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "https://acceptance.test/login", nil))
	if head.Code != http.StatusOK || head.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("HEAD login health check did not reach acceptance login")
	}
	for _, tc := range []struct {
		origin, csrf, password string
		want                   int
	}{{"https://evil.test", cookies[0].Value, "wrong", 403}, {"https://acceptance.test", "wrong", "wrong", 403}, {"https://acceptance.test", cookies[0].Value, "wrong", 401}} {
		r := httptest.NewRequest("POST", "https://acceptance.test/acceptance/login", strings.NewReader(url.Values{"account": {"customer"}, "password": {tc.password}, "csrf": {tc.csrf}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", tc.origin)
		r.AddCookie(cookies[0])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("got %d, want %d", w.Code, tc.want)
		}
	}
}
