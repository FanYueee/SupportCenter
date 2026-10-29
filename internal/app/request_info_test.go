package app

import (
	"net/http/httptest"
	"testing"
)

func TestClientRequestInfoTrustsOnlyLocalProxy(t *testing.T) {
	for _, tc := range []struct {
		remote, forwarded, want string
	}{
		{"127.0.0.1:1234", "203.0.113.8", "203.0.113.8"},
		{"172.18.0.1:1234", "2001:db8::8", "2001:db8::8"},
		{"198.51.100.7:1234", "203.0.113.8", "198.51.100.7"},
		{"127.0.0.1:1234", "bad address", "127.0.0.1"},
	} {
		r := httptest.NewRequest("GET", "https://support.example.test/account", nil)
		r.RemoteAddr = tc.remote
		r.Header.Set("CF-Connecting-IP", tc.forwarded)
		r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0) Chrome/120.0\r\n")
		got := ClientRequestInfo(r)
		if got.IP != tc.want || got.UserAgent != "Mozilla/5.0 (Windows NT 10.0) Chrome/120.0" {
			t.Fatalf("%s via %s: %+v", tc.remote, tc.forwarded, got)
		}
	}
}
