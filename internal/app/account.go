package app

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (a *App) account(w http.ResponseWriter, r *http.Request) {
	p := base(r)
	p.Active, p.Title = "account", "個人設定"
	p.Saved = r.URL.Query().Get("saved") == "1"
	p.LoginPage = pageNum(r.URL.Query().Get("logins"))
	p.ActivityPage = pageNum(r.URL.Query().Get("activity"))
	var err error
	p.Logins, p.HasMoreLogins, err = a.Store.Logins(r.Context(), p.User.ID, p.LoginPage)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	p.Activities, p.HasMoreActivities, err = a.Store.Activities(r.Context(), p.User.ID, p.ActivityPage)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if p.EmailNotify, err = a.Store.EmailNotify(r.Context(), p.User.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, p)
}

func (a *App) updateName(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	if u.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	if err := a.Store.SetDisplayName(r.Context(), u, r.PostFormValue("name")); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/account?saved=1", http.StatusSeeOther)
}

func (a *App) updateSignature(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	if u.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	if err := a.Store.SetSignature(r.Context(), u, r.PostFormValue("signature")); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/account?saved=1", http.StatusSeeOther)
}

func (a *App) users(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	p := base(r)
	p.Active, p.Title = "users", "使用者"
	p.Page = pageNum(r.URL.Query().Get("page"))
	p.Query = strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(p.Query)) > 100 || !utf8.ValidString(p.Query) || strings.IndexFunc(p.Query, unicode.IsControl) >= 0 {
		a.problem(w, r, http.StatusBadRequest, "搜尋條件無效。")
		return
	}
	var err error
	p.Users, p.HasNext, err = a.Store.Users(r.Context(), p.Query, p.Page)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, p)
}
