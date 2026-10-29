package app

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

func signedToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	raw := head + "." + base64.RawURLEncoding.EncodeToString(body)
	sum := sha256.Sum256([]byte(raw))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return raw + "." + base64.RawURLEncoding.EncodeToString(sig)
}
func TestOIDCLoginAndSessionLifecycle(t *testing.T) {
	a, _, _, _, _ := testApp(t)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var nonce, challenge, mode, issuer string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != challenge {
			t.Error("PKCE verifier does not match challenge")
		}
		if r.Form.Get("redirect_uri") != a.Origin+"/auth/callback" {
			t.Error("redirect URI not fixed")
		}
		claims := map[string]any{"iss": issuer, "aud": "test-client", "sub": "new-auth0-user", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": nonce, "name": "New User", "email": "new@example.test"}
		switch mode {
		case "nonce":
			claims["nonce"] = "attacker"
		case "audience":
			claims["aud"] = "other-client"
		case "issuer":
			claims["iss"] = "https://attacker.test/"
		case "expired":
			claims["exp"] = time.Now().Add(-time.Hour).Unix()
		}
		raw := signedToken(t, key, claims)
		if mode == "signature" {
			parts := strings.Split(raw, ".")
			raw = parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString([]byte("invalid"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "never-persist-this", "token_type": "Bearer", "expires_in": 3600, "id_token": raw})
	}))
	defer provider.Close()
	issuer = provider.URL
	a.Auth = &Auth{OAuth: oauth2.Config{ClientID: "test-client", ClientSecret: "secret", RedirectURL: a.Origin + "/auth/callback", Endpoint: oauth2.Endpoint{AuthURL: issuer + "/authorize", TokenURL: issuer + "/token"}, Scopes: []string{"openid", "profile", "email"}}, Verifier: oidc.NewVerifier(issuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: "test-client"}), Client: provider.Client(), Issuer: issuer + "/"}
	for _, testMode := range []string{"nonce", "audience", "issuer", "expired", "signature", "success"} {
		t.Run(testMode, func(t *testing.T) {
			mode = testMode
			// A fresh handler keeps these sequential logins under the per-client rate limit.
			h := a.Handler()
			start := request(h, "GET", "/login", "", nil)
			if start.Code != 302 {
				t.Fatal(start.Code)
			}
			target, err := url.Parse(start.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			state := target.Query().Get("state")
			nonce = target.Query().Get("nonce")
			challenge = target.Query().Get("code_challenge")
			if state == "" || nonce == "" || challenge == "" || target.Query().Get("code_challenge_method") != "S256" {
				t.Fatal("missing OAuth protection")
			}
			cookies := start.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Value != state || !cookies[0].Secure {
				t.Fatal("login cookie missing")
			}
			call := func(state string, loginCookie bool) *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", a.Origin+"/auth/callback?"+url.Values{"state": {state}, "code": {"valid"}}.Encode(), nil)
				r.RemoteAddr = "127.0.0.1:43000"
				r.Header.Set("CF-Connecting-IP", "203.0.113.77")
				r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0) Chrome/120.0")
				if loginCookie {
					r.AddCookie(cookies[0])
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				return w
			}
			if w := call("wrong", true); w.Code != 400 {
				t.Fatal("state mismatch accepted")
			}
			if w := call(state, false); w.Code != 400 {
				t.Fatal("missing login cookie accepted")
			}
			w := call(state, true)
			if mode != "success" {
				if w.Code != 400 {
					t.Fatalf("invalid token accepted: %d", w.Code)
				}
			} else {
				if w.Code != 303 {
					t.Fatalf("login failed: %d", w.Code)
				}
				var session *http.Cookie
				for _, c := range w.Result().Cookies() {
					if c.Name == "__Host-support-session" {
						session = c
					}
				}
				if session == nil || session.Value == "" {
					t.Fatal("session not issued")
				}
				r := httptest.NewRequest("GET", a.Origin+"/tickets", nil)
				r.AddCookie(session)
				loaded, err := a.session(r)
				if err != nil || loaded.User.Role != "user" {
					t.Fatal("new login role is not user")
				}
				logins, _, err := a.Store.Logins(context.Background(), loaded.User.ID, 1)
				if err != nil || len(logins) != 1 || logins[0].IP != "203.0.113.77" || logins[0].UserAgent != "Mozilla/5.0 (Windows NT 10.0) Chrome/120.0" {
					t.Fatalf("login source not recorded: %+v %v", logins, err)
				}
				var stored string
				if err = a.Store.Pool.QueryRow(context.Background(), `SELECT token_hash FROM sessions WHERE user_id=$1`, loaded.User.ID).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				if stored == session.Value || stored != hash(session.Value) {
					t.Fatal("session token is not hashed")
				}
				logout := request(h, "POST", "/logout", session.Value, url.Values{"csrf": {loaded.CSRF}})
				if logout.Code != 303 {
					t.Fatal("logout failed")
				}
				if _, err = a.session(r); err == nil {
					t.Fatal("session still valid after logout")
				}
			}
			if w := call(state, true); w.Code != 400 {
				t.Fatal("callback replay accepted")
			}
		})
	}
}

func TestSignupOpensAuth0SignupScreen(t *testing.T) {
	a, _, _, _, _ := testApp(t)
	a.Auth = &Auth{OAuth: oauth2.Config{ClientID: "test-client", RedirectURL: a.Origin + "/auth/callback", Endpoint: oauth2.Endpoint{AuthURL: "https://sso.example.test/authorize"}, Scopes: []string{"openid"}}}
	h := a.Handler()
	for path, hint := range map[string]string{"/login": "", "/signup": "signup"} {
		res := request(h, "GET", path, "", nil)
		target, err := url.Parse(res.Header().Get("Location"))
		if res.Code != http.StatusFound || err != nil || target.Host != "sso.example.test" {
			t.Fatalf("%s: %d %q", path, res.Code, res.Header().Get("Location"))
		}
		q := target.Query()
		if q.Get("screen_hint") != hint || q.Get("state") == "" || q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" {
			t.Fatalf("%s: unexpected authorize parameters %v", path, q)
		}
		if !strings.Contains(res.Header().Get("Set-Cookie"), "__Host-support-login=") {
			t.Fatalf("%s: login state cookie missing", path)
		}
	}
}

func TestPendingLoginFlowsAreCapped(t *testing.T) {
	a, _, _, _, _ := testApp(t)
	a.Auth = &Auth{OAuth: oauth2.Config{ClientID: "test-client", RedirectURL: a.Origin + "/auth/callback", Endpoint: oauth2.Endpoint{AuthURL: "https://sso.example.test/authorize"}}}
	h := a.Handler()
	if _, err := a.Store.Pool.Exec(context.Background(), `INSERT INTO login_flows(state_hash,nonce,verifier,expires_at) SELECT md5(g::text),'n','v',now()+interval '10 minutes' FROM generate_series(1,20000) g`); err != nil {
		t.Fatal(err)
	}
	if res := request(h, "GET", "/login", "", nil); res.Code != http.StatusTooManyRequests {
		t.Fatal("login flow table grew past its cap", res.Code)
	}
	if _, err := a.Store.Pool.Exec(context.Background(), `UPDATE login_flows SET expires_at=now()-interval '1 second' WHERE state_hash IN (SELECT state_hash FROM login_flows LIMIT 1)`); err != nil {
		t.Fatal(err)
	}
	if res := request(h, "GET", "/login", "", nil); res.Code != http.StatusFound {
		t.Fatal("expired flows still counted against the cap", res.Code)
	}
}
