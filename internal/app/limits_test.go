package app

import (
	"fmt"
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

func TestLoginLimiterGroupsIPv6Prefixes(t *testing.T) {
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
	// Rotating addresses inside one /64 does not earn extra attempts.
	for i := 1; i <= 5; i++ {
		if code := status(fmt.Sprintf("2001:db8:1:2::%x", i)); code != 204 {
			t.Fatal("login rejected under the per-prefix limit", code)
		}
	}
	if code := status("2001:db8:1:2:ffff::1"); code != 429 {
		t.Fatal("another address in the same /64 bypassed the limit", code)
	}
	if code := status("2001:db8:1:3::1"); code != 204 {
		t.Fatal("a different /64 was limited", code)
	}
}
