package app

import (
	"errors"
	"net/http"
	"strings"

	"supportcenter/internal/database"
	"supportcenter/internal/model"
	"supportcenter/internal/notify"
)

func (a *App) discordSettings(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	p := base(r)
	p.Active, p.Title = "discord", "Discord 通知"
	p.Saved = r.URL.Query().Get("saved") == "1"
	if test := r.URL.Query().Get("test"); test == "ok" || test == "failed" {
		p.DiscordTest = test
	}
	var err error
	if p.Discord, err = a.Store.DiscordSettings(r.Context()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, p)
}

func (a *App) saveDiscordSettings(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.problem(w, r, http.StatusBadRequest, "表單無效。")
		return
	}
	settings := model.DiscordSettings{
		OnTicket:       r.PostFormValue("on_ticket") == "on",
		OnReply:        r.PostFormValue("on_reply") == "on",
		IncludePreview: r.PostFormValue("include_preview") == "on",
	}
	// A blank field keeps the saved webhook, which is never sent back to the page.
	var webhook *string
	if raw := strings.TrimSpace(r.PostFormValue("webhook_url")); raw != "" {
		webhook = &raw
	} else if r.PostFormValue("remove_webhook") == "on" {
		webhook = new(string)
	}
	if err := a.Store.SaveDiscordSettings(r.Context(), settings, webhook); err != nil {
		if errors.Is(err, database.ErrInvalid) {
			a.problem(w, r, http.StatusUnprocessableEntity, "Webhook 網址無效，請貼上 Discord 頻道的 Webhook 網址。")
			return
		}
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/discord?saved=1", http.StatusSeeOther)
}

func (a *App) testDiscord(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	id, token, _, err := a.Store.DiscordWebhook(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if id == "" {
		a.problem(w, r, http.StatusUnprocessableEntity, "請先設定 Webhook 網址。")
		return
	}
	_, sendErr := a.Discord.Post(r.Context(), id, token, notify.DiscordTestMessage(a.Origin, a.Site.Title()))
	if err = a.Store.RecordDiscordResult(r.Context(), notify.ErrorCode(sendErr)); err != nil {
		a.fail(w, r, err)
		return
	}
	result := "ok"
	if sendErr != nil {
		result = "failed"
	}
	http.Redirect(w, r, "/admin/discord?test="+result, http.StatusSeeOther)
}
