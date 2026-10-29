package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
	"supportcenter/internal/database"
	"supportcenter/internal/model"
)

type Auth struct {
	OAuth    oauth2.Config
	Verifier *oidc.IDTokenVerifier
	Client   *http.Client
	Issuer   string
}
type Session struct {
	User model.User
	CSRF string
}
type sessionKey struct{}

func token() string        { return rand.Text() }
func hash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func equal(a, b string) bool {
	return a != "" && b != "" && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func NewAuth(ctx context.Context, domain, clientID, secret, origin string) (*Auth, error) {
	if strings.ContainsAny(domain, "/:?#@ ") || !strings.Contains(domain, ".") {
		return nil, fmt.Errorf("AUTH0_DOMAIN must be a hostname")
	}
	issuer := "https://" + domain + "/"
	client := &http.Client{Timeout: 10 * time.Second}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, client), issuer)
	if err != nil {
		return nil, errors.New("Auth0 discovery failed; check domain and network")
	}
	return &Auth{OAuth: oauth2.Config{ClientID: clientID, ClientSecret: secret, RedirectURL: origin + "/auth/callback", Endpoint: provider.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}, Verifier: provider.Verifier(&oidc.Config{ClientID: clientID}), Client: client, Issuer: issuer}, nil
}
func cookie(name, value string, age int) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age}
}
func (a *App) login(w http.ResponseWriter, r *http.Request) {
	if a.Auth == nil {
		a.problem(w, r, 503, "登入服務尚未設定。")
		return
	}
	state, nonce, verifier := token(), token(), oauth2.GenerateVerifier()
	// Pending flows are capped so a flood cannot grow the table without bound.
	res, err := a.Store.Pool.Exec(r.Context(), `INSERT INTO login_flows(state_hash,nonce,verifier,expires_at) SELECT $1,$2,$3,now()+interval '10 minutes' WHERE (SELECT count(*) FROM login_flows WHERE expires_at>now())<20000`, hash(state), nonce, verifier)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if res.RowsAffected() == 0 {
		w.Header().Set("Retry-After", "60")
		a.problem(w, r, 429, "登入請求過多，請稍候再試。")
		return
	}
	http.SetCookie(w, cookie("__Host-support-login", state, 600))
	origin, _ := a.site(r)
	options := []oauth2.AuthCodeOption{oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("redirect_uri", origin+"/auth/callback")}
	// /signup runs the same flow but opens the Auth0 Universal Login sign-up screen.
	if r.URL.Path == "/signup" {
		options = append(options, oauth2.SetAuthURLParam("screen_hint", "signup"))
	}
	http.Redirect(w, r, a.Auth.OAuth.AuthCodeURL(state, options...), http.StatusFound)
}
func (a *App) callback(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(database.WithRequestInfo(r.Context(), ClientRequestInfo(r)))
	if a.Auth == nil {
		a.problem(w, r, 503, "登入服務尚未設定。")
		return
	}
	c, err := r.Cookie("__Host-support-login")
	if err != nil || !equal(c.Value, r.URL.Query().Get("state")) {
		a.problem(w, r, 400, "登入驗證失敗，請重新登入。")
		return
	}
	http.SetCookie(w, cookie("__Host-support-login", "", -1))
	var nonce, verifier string
	err = a.Store.Pool.QueryRow(r.Context(), `DELETE FROM login_flows WHERE state_hash=$1 AND expires_at>now() RETURNING nonce,verifier`, hash(c.Value)).Scan(&nonce, &verifier)
	if err != nil || r.URL.Query().Get("code") == "" {
		a.problem(w, r, 400, "登入連結已失效，請重新登入。")
		return
	}
	ctx := oidc.ClientContext(r.Context(), a.Auth.Client)
	origin, _ := a.site(r)
	tok, err := a.Auth.OAuth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifier), oauth2.SetAuthURLParam("redirect_uri", origin+"/auth/callback"))
	if err != nil {
		a.problem(w, r, 400, "無法完成登入，請重新嘗試。")
		return
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		a.problem(w, r, 400, "登入驗證失敗。")
		return
	}
	id, err := a.Auth.Verifier.Verify(ctx, raw)
	if err != nil || !equal(id.Nonce, nonce) {
		a.problem(w, r, 400, "登入驗證失敗。")
		return
	}
	var claims struct {
		Name          string `json:"name"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err = id.Claims(&claims); err != nil || id.Subject == "" {
		a.problem(w, r, 400, "登入資料不完整。")
		return
	}
	if claims.Name == "" {
		claims.Name = "使用者"
	}
	u, err := a.Store.User(ctx, id.Subject, claims.Name, claims.Email, claims.EmailVerified)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	rawSession, csrf := token(), token()
	tx, err := a.Store.Pool.Begin(ctx)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	defer tx.Rollback(ctx)
	if old, e := r.Cookie("__Host-support-session"); e == nil {
		if _, err = tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, hash(old.Value)); err != nil {
			a.fail(w, r, err)
			return
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf,expires_at) VALUES($1,$2,$3,now()+interval '24 hours')`, hash(rawSession), u.ID, csrf)
	if err == nil {
		err = a.Store.LogAccountEvent(ctx, tx, u.ID, "login", model.DeviceLabel(r.UserAgent()))
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.SetCookie(w, cookie("__Host-support-session", rawSession, 86400))
	target := "/tickets"
	if u.Staff() {
		target = "/admin/tickets"
	}
	http.Redirect(w, r, target, 303)
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("__Host-support-session")
	if err == nil {
		tx, beginErr := a.Store.Pool.Begin(r.Context())
		if beginErr != nil {
			a.fail(w, r, beginErr)
			return
		}
		defer tx.Rollback(r.Context())
		_, err = tx.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash=$1`, hash(c.Value))
		if err == nil {
			err = a.Store.LogAccountEvent(r.Context(), tx, current(r).User.ID, "logout", "")
		}
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			a.fail(w, r, err)
			return
		}
	}
	http.SetCookie(w, cookie("__Host-support-session", "", -1))
	if a.Auth == nil {
		http.Redirect(w, r, "/", 303)
		return
	}
	origin, _ := a.site(r)
	target := a.Auth.Issuer + "v2/logout?" + url.Values{"client_id": {a.Auth.OAuth.ClientID}, "returnTo": {origin}}.Encode()
	http.Redirect(w, r, target, 303)
}
func (a *App) session(r *http.Request) (Session, error) {
	var s Session
	c, err := r.Cookie("__Host-support-session")
	if err != nil {
		return s, err
	}
	err = a.Store.Pool.QueryRow(r.Context(), `SELECT u.id,u.name,u.email,u.role,u.signature,s.csrf FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now()`, hash(c.Value)).Scan(&s.User.ID, &s.User.Name, &s.User.Email, &s.User.Role, &s.User.Signature, &s.CSRF)
	return s, err
}
func current(r *http.Request) Session { return r.Context().Value(sessionKey{}).(Session) }
func (a *App) protected(staff bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, err := a.session(r)
		if err != nil {
			if errors.Is(err, http.ErrNoCookie) || errors.Is(err, pgx.ErrNoRows) {
				if r.Header.Get("HX-Request") == "true" {
					if r.Method == http.MethodGet {
						w.Header().Set("HX-Redirect", "/")
					}
					a.problem(w, r, 401, "登入已過期。請在另一個分頁登入，再重新送出。")
				} else {
					http.Redirect(w, r, "/", 303)
				}
			} else {
				a.fail(w, r, err)
			}
			return
		}
		if staff && !s.User.Staff() {
			a.problem(w, r, 404, "找不到此頁面。")
			return
		}
		r = r.WithContext(database.WithRequestInfo(context.WithValue(r.Context(), sessionKey{}, s), ClientRequestInfo(r)))
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin, _ := a.site(r); r.Header.Get("Origin") != "" && r.Header.Get("Origin") != origin {
				a.problem(w, r, 403, "請重新整理頁面後再操作。")
				return
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				a.problem(w, r, 403, "請重新整理頁面後再操作。")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
			if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
				err = r.ParseMultipartForm(1 << 20)
				if r.MultipartForm != nil {
					defer r.MultipartForm.RemoveAll()
				}
			} else {
				r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
				err = r.ParseForm()
			}
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					a.problem(w, r, 413, "附件總大小超過限制。")
				} else {
					a.problem(w, r, 400, "無法讀取表單內容。")
				}
				return
			}
			csrf := r.Header.Get("X-CSRF-Token")
			if csrf == "" {
				csrf = r.PostFormValue("csrf")
			}
			if !equal(csrf, s.CSRF) {
				a.problem(w, r, 403, "頁面已過期，請重新整理後再送出。")
				return
			}
		}
		next(w, r)
	}
}
