package app

import (
	"net/http"
	"strings"

	"supportcenter/internal/model"
)

func (a *App) aiSettings(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	p := base(r)
	p.Active, p.Title = "ai", "AI 回覆"
	p.AIEnabled = a.AIEnabled
	p.Saved = r.URL.Query().Get("saved") == "1"
	var err error
	p.AIPrompts, err = a.Store.AIPrompts(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, p)
}

func (a *App) saveAISettings(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.problem(w, r, http.StatusBadRequest, "表單無效。")
		return
	}
	prompts := model.AIPrompts{
		General:   strings.TrimSpace(r.PostFormValue("general_prompt")),
		Billing:   strings.TrimSpace(r.PostFormValue("billing_prompt")),
		Technical: strings.TrimSpace(r.PostFormValue("technical_prompt")),
	}
	if err := a.Store.SaveAIPrompts(r.Context(), prompts); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/ai?saved=1", http.StatusSeeOther)
}
