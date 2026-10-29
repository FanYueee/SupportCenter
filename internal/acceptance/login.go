// Package acceptance is used only by the isolated acceptance executable.
// It is not imported by cmd/server and cannot enable a production login bypass.
package acceptance

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"supportcenter/internal/app"
	"supportcenter/internal/database"
	"supportcenter/internal/i18n"
	"supportcenter/internal/model"
	"supportcenter/web"
	"sync"
	"time"
)

func digest(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
func cookie(name, value string, age int) *http.Cookie {
	return &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age}
}
func Handler(core http.Handler, store *database.Store, origin, password string, accounts map[string]model.User) http.Handler {
	expected := sha256.Sum256([]byte(password))
	var mu sync.Mutex
	var window time.Time
	attempts := 0
	render := func(w http.ResponseWriter, r *http.Request, status int, message string) {
		csrf := rand.Text()
		http.SetCookie(w, cookie("__Host-acceptance-login", csrf, 600))
		var buf bytes.Buffer
		if err := web.AcceptanceLogin(csrf, message).Render(r.Context(), &buf); err != nil {
			http.Error(w, "Unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		w.Write(buf.Bytes())
	}
	return i18n.Middleware(app.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path == "/login" {
			render(w, r, 200, "")
			return
		}
		if r.Method != "POST" || r.URL.Path != "/acceptance/login" {
			core.ServeHTTP(w, r)
			return
		}
		mu.Lock()
		if time.Since(window) > time.Minute {
			window = time.Now()
			attempts = 0
		}
		attempts++
		allowed := attempts <= 20
		mu.Unlock()
		if !allowed {
			w.Header().Set("Retry-After", "60")
			render(w, r, 429, "請稍候再試。")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			render(w, r, 400, "無效表單。")
			return
		}
		c, err := r.Cookie("__Host-acceptance-login")
		if err != nil || c.Value == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.PostFormValue("csrf"))) != 1 || r.Header.Get("Origin") != origin {
			render(w, r, 403, "請重新載入登入頁面。")
			return
		}
		provided := sha256.Sum256([]byte(r.PostFormValue("password")))
		user, exists := accounts[r.PostFormValue("account")]
		if subtle.ConstantTimeCompare(expected[:], provided[:]) != 1 || !exists {
			render(w, r, 401, "驗收密碼不正確。")
			return
		}
		r = r.WithContext(database.WithRequestInfo(r.Context(), app.ClientRequestInfo(r)))
		tx, err := store.Pool.Begin(r.Context())
		if err != nil {
			render(w, r, 503, "暫時無法登入。")
			return
		}
		defer tx.Rollback(r.Context())
		if old, e := r.Cookie("__Host-support-session"); e == nil {
			_, err = tx.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash=$1`, digest(old.Value))
			if err != nil {
				render(w, r, 503, "暫時無法登入。")
				return
			}
		}
		token, csrf := rand.Text(), rand.Text()
		_, err = tx.Exec(r.Context(), `INSERT INTO sessions(token_hash,user_id,csrf,expires_at) VALUES($1,$2,$3,now()+interval '4 hours')`, digest(token), user.ID, csrf)
		if err == nil {
			err = store.LogAccountEvent(r.Context(), tx, user.ID, "login", model.DeviceLabel(r.UserAgent()))
		}
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			slog.Error("acceptance session failed")
			render(w, r, 503, "暫時無法登入。")
			return
		}
		http.SetCookie(w, cookie("__Host-support-session", token, 14400))
		http.SetCookie(w, cookie("__Host-acceptance-login", "", -1))
		target := "/tickets"
		if user.Staff() {
			target = "/admin/tickets"
		}
		http.Redirect(w, r, target, 303)
	})))
}
