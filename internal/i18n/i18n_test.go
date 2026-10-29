package i18n

import (
	"net/http/httptest"
	"testing"
)

func TestDetectLanguagePreference(t *testing.T) {
	for _, tc := range []struct{ header, cookie, want string }{
		{"en-US,en;q=0.9,zh-TW;q=0.8", "", "en"},
		{"zh-TW, en;q=0.8", "", "zh-Hant"},
		{"en;q=0,zh-TW;q=0.6", "", "zh-Hant"},
		{"en-US", "zh-Hant", "zh-Hant"},
		{"zh-TW", "en", "en"},
		{"fr-FR", "", "zh-Hant"},
	} {
		r := httptest.NewRequest("GET", "https://support.example.test/tickets", nil)
		r.Header.Set("Accept-Language", tc.header)
		if tc.cookie != "" {
			r.Header.Set("Cookie", CookieName+"="+tc.cookie)
		}
		if got := Detect(r); got != tc.want {
			t.Errorf("%q with cookie %q: got %q, want %q", tc.header, tc.cookie, got, tc.want)
		}
	}
}

func TestPreviewCookieOnlyOnLoopback(t *testing.T) {
	for _, host := range []string{"https://support.example.test", "http://127.0.0.1:8081"} {
		r := httptest.NewRequest("GET", host+"/tickets", nil)
		r.Header.Set("Cookie", PreviewCookieName+"=en")
		want := "zh-Hant"
		if r.URL.Hostname() == "127.0.0.1" {
			want = "en"
		}
		if got := Detect(r); got != want {
			t.Errorf("%s: got %q, want %q", host, got, want)
		}
	}
}
