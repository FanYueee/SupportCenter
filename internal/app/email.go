package app

import (
	"net/http"

	"supportcenter/internal/model"
	"supportcenter/internal/notify"
)

func (a *App) emailSettings(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	p := base(r)
	p.Active, p.Title = "email", "Email 通知"
	p.MailEnabled, p.MailFrom = a.MailEnabled, a.MailFrom
	p.Saved = r.URL.Query().Get("saved") == "1"
	if test := r.URL.Query().Get("test"); test == "ok" || test == "failed" {
		p.MailTest = test
	}
	var err error
	if p.Email, err = a.Store.EmailSettings(r.Context()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, p)
}

func (a *App) saveEmailSettings(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.problem(w, r, http.StatusBadRequest, "表單無效。")
		return
	}
	settings := model.EmailSettings{
		OnReply:      r.PostFormValue("on_reply") == "on",
		IncludeReply: r.PostFormValue("include_reply") == "on",
		OnInvite:     r.PostFormValue("on_invite") == "on",
	}
	if err := a.Store.SaveEmailSettings(r.Context(), settings); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/email?saved=1", http.StatusSeeOther)
}

// testEmail sends a test message to the admin's own address only.
func (a *App) testEmail(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	if u.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	if a.Mailer == nil {
		a.problem(w, r, http.StatusUnprocessableEntity, "尚未設定 SMTP。")
		return
	}
	msg, err := notify.TestEmail(r.Context(), u.Email, a.Site.Title(), a.Origin)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	sendErr := a.Mailer.Send(r.Context(), msg)
	if err = a.Store.RecordEmailResult(r.Context(), notify.MailErrorCode(sendErr)); err != nil {
		a.fail(w, r, err)
		return
	}
	result := "ok"
	if sendErr != nil {
		result = "failed"
	}
	http.Redirect(w, r, "/admin/email?test="+result, http.StatusSeeOther)
}

func (a *App) updateEmailNotify(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.SetEmailNotify(r.Context(), current(r).User.ID, r.PostFormValue("email_notify") == "on"); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/account?saved=1", http.StatusSeeOther)
}
