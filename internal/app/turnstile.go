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
// form, and if not, why, for the log; the reason never holds the token or the
// secret. An unreachable Cloudflare counts as a failure.
func (t *Turnstile) Verify(ctx context.Context, token, remoteIP, hostname string) (ok bool, reason string) {
	if token == "" || len(token) > 2048 {
		return false, "missing-token"
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
		return false, "request"
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client.Do(req)
	if err != nil {
		return false, "unreachable"
	}
	defer res.Body.Close()
	var result struct {
		Success    bool     `json:"success"`
		Hostname   string   `json:"hostname"`
		Action     string   `json:"action"`
		ErrorCodes []string `json:"error-codes"`
	}
	if res.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&result) != nil {
		return false, "bad-response"
	}
	switch {
	case !result.Success:
		// Cloudflare's codes, e.g. timeout-or-duplicate; kept short and plain for the log.
		codes := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == ',' {
				return r
			}
			return -1
		}, strings.Join(result.ErrorCodes, ","))
		return false, "rejected:" + codes[:min(len(codes), 100)]
	case result.Action != "ticket":
		return false, "action-mismatch"
	case !strings.EqualFold(result.Hostname, hostname):
		return false, "hostname-mismatch"
	}
	return true, ""
}

// allowTurnstile lets this page load Cloudflare's check script and frame; the
// rest of the site keeps the stricter policy.
func allowTurnstile(w http.ResponseWriter) {
	csp := w.Header().Get("Content-Security-Policy")
	csp = strings.Replace(csp, "script-src 'self'", "script-src 'self' "+turnstileOrigin, 1)
	w.Header().Set("Content-Security-Policy", csp+"; frame-src "+turnstileOrigin)
}
