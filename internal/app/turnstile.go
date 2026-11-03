package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const turnstileOrigin = "https://challenges.cloudflare.com"

// Turnstile is Cloudflare's bot check on the new ticket form. Without keys the
// form has no check.
type Turnstile struct {
	SiteKey, Secret string
	VerifyURL       string // the siteverify endpoint; tests point it elsewhere
	Client          *http.Client
}

func (t *Turnstile) Enabled() bool { return t != nil && t.SiteKey != "" && t.Secret != "" }

// Verify reports whether the token passed the check for this site's ticket
// form. An unreachable Cloudflare counts as a failure.
func (t *Turnstile) Verify(ctx context.Context, token, remoteIP, hostname string) bool {
	if token == "" || len(token) > 2048 {
		return false
	}
	form := url.Values{"secret": {t.Secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	endpoint, client := t.VerifyURL, t.Client
	if endpoint == "" {
		endpoint = turnstileOrigin + "/turnstile/v0/siteverify"
	}
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var result struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
		Action   string `json:"action"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&result) != nil {
		return false
	}
	return result.Success && result.Action == "ticket" && strings.EqualFold(result.Hostname, hostname)
}

// allowTurnstile lets this page load Cloudflare's check script and frame; the
// rest of the site keeps the stricter policy.
func allowTurnstile(w http.ResponseWriter) {
	csp := w.Header().Get("Content-Security-Policy")
	csp = strings.Replace(csp, "script-src 'self'", "script-src 'self' "+turnstileOrigin, 1)
	w.Header().Set("Content-Security-Policy", csp+"; frame-src "+turnstileOrigin)
}
