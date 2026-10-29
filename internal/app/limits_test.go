package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginLimiterIsPerClient(t *testing.T) {
	a := &App{}
	h := a.loginLimiter(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	status := func(ip string) int {
		r := httptest.NewRequest("GET", "https://support.example.test/login", nil)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("CF-Connecting-IP", ip)
		w := httptest.NewRecorder()
		h(w, r)
		return w.Code
	}
	for range 5 {
		if code := status("203.0.113.1"); code != 204 {
			t.Fatal("login rejected under the per-client limit", code)
		}
	}
	if code := status("203.0.113.1"); code != 429 {
		t.Fatal("per-client login limit not enforced", code)
	}
	if code := status("203.0.113.2"); code != 204 {
		t.Fatal("one client exhausted another client's login allowance", code)
	}
}
