package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/database"
	"supportcenter/internal/model"
	"supportcenter/web"
)

func (a *App) cannedReplies(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	p := base(r)
	p.Active, p.Title = "canned", "罐頭訊息"
	p.Saved = r.URL.Query().Get("saved") == "1"
	if p.Saved && r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Replace-Url", "/admin/canned")
	}
	var err error
	if p.CannedReplies, err = a.Store.CannedReplies(r.Context()); err != nil {
		a.fail(w, r, err)
		return
	}
	if r.PathValue("id") != "" {
		id, ok := idFrom(r)
		found := false
		for _, v := range p.CannedReplies {
			if ok && v.ID == id {
				p.CannedReply, found = v, true
			}
		}
		if !found {
			a.problem(w, r, 404, "找不到罐頭訊息。")
			return
		}
	}
	a.render(w, r, p)
}

func (a *App) saveCannedReply(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	v := model.CannedReply{Title: strings.TrimSpace(r.PostFormValue("title")), Body: strings.TrimSpace(r.PostFormValue("body"))}
	if r.PathValue("id") != "" {
		var ok bool
		if v.ID, ok = idFrom(r); !ok {
			a.problem(w, r, 404, "找不到罐頭訊息。")
			return
		}
		v.Version, _ = strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	}
	if err := a.Store.SaveCannedReply(r.Context(), current(r).User, v); err != nil {
		a.cannedError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/canned?saved=1", http.StatusSeeOther)
}

func (a *App) deleteCannedReply(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到罐頭訊息。")
		return
	}
	version, _ := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err := a.Store.DeleteCannedReply(r.Context(), current(r).User, id, version); err != nil {
		a.cannedError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/canned?saved=1", http.StatusSeeOther)
}

func (a *App) cannedError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, database.ErrConflict):
		a.problem(w, r, 409, "罐頭訊息已被更新，請重新載入後再操作。")
	case errors.Is(err, database.ErrCannedLimit):
		a.problem(w, r, 422, "最多保留 100 則罐頭訊息，請先刪除不再使用的訊息。")
	case errors.Is(err, database.ErrInvalid):
		a.problem(w, r, 422, "請填寫標題（最多 80 字）與內容（最多 4,000 字）。")
	default:
		a.fail(w, r, err)
	}
}

// mentionSearch suggests tickets while someone types # in a message. On a
// staff ticket page the requester's tickets come first.
func (a *App) mentionSearch(w http.ResponseWriter, r *http.Request) {
	u := current(r).User
	owner, except, staff := u.ID, int64(0), false
	if r.PathValue("id") != "" {
		id, ok := idFrom(r)
		if !ok {
			a.problem(w, r, 404, "找不到客服單。")
			return
		}
		ticket, err := a.Store.Ticket(r.Context(), u, id, true)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		owner, except, staff = ticket.UserID, id, true
	} else if id, err := strconv.ParseInt(r.URL.Query().Get("except"), 10, 64); err == nil {
		except = id
	}
	refs, err := a.Store.MentionCandidates(r.Context(), u.ID, staff, owner, except, r.URL.Query().Get("q"))
	if err != nil {
		a.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	web.MentionOptions(refs).Render(r.Context(), w)
}

// activity answers each page's poll for new replies. Nothing new sends no
// content. A lost session stops the poll (HTMX's 286) instead of sending the
// page to sign in, which would throw away a reply being written; other errors
// stay quiet so the page shows no error box.
func (a *App) activity(w http.ResponseWriter, r *http.Request) {
	s, err := a.session(r)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) || errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(286)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
		return
	}
	latest, err := a.Store.LatestActivity(r.Context(), s.User)
	if err != nil {
		a.Log.Warn("activity check failed", "error_type", fmt.Sprintf("%T", err))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if since, err := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64); err == nil && since == latest {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	web.ActivitySignal(latest).Render(r.Context(), w)
}
