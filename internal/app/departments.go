package app

import (
	"net/http"
	"strconv"
)

func (a *App) departments(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	p := base(r)
	p.Active, p.Title = "departments", "客服分組"
	p.Saved = r.URL.Query().Get("saved") == "1"
	p.MailEnabled = a.MailEnabled
	var err error
	p.Staff, err = a.Store.Staff(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, p)
}

func (a *App) saveDepartments(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	personID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || personID < 1 {
		a.problem(w, r, 404, "找不到客服。")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.problem(w, r, 400, "表單無效。")
		return
	}
	if err := a.Store.SaveStaffDepartments(r.Context(), current(r).User, personID, r.PostForm["departments"]); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/departments?saved=1", http.StatusSeeOther)
}
