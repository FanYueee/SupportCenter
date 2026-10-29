package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"supportcenter/internal/database"
	"supportcenter/internal/model"
)

func (a *App) announcements(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	p := base(r)
	p.Active, p.Title = "announcements", "公告"
	p.Saved = r.URL.Query().Get("saved") == "1"
	if p.Saved && r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Replace-Url", "/admin/announcements")
	}
	p.Announcement = model.Announcement{Color: "blue", Enabled: true}
	var err error
	p.ManagedAnnouncements, err = a.Store.ManagedAnnouncements(r.Context(), p.User)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	if r.PathValue("id") != "" {
		id, ok := idFrom(r)
		found := false
		if ok {
			for _, v := range p.ManagedAnnouncements {
				if v.ID == id {
					p.Announcement = v
					found = true
					break
				}
			}
		}
		if !found {
			a.problem(w, r, 404, "找不到公告。")
			return
		}
	}
	a.render(w, r, p)
}

func (a *App) saveAnnouncement(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.problem(w, r, 400, "表單無效。")
		return
	}
	v := model.Announcement{Title: strings.TrimSpace(r.PostForm.Get("title")), Body: strings.TrimSpace(r.PostForm.Get("body")), Color: r.PostForm.Get("color"), Enabled: r.PostForm.Get("enabled") == "1"}
	if !valid(r.PostForm.Get("enabled"), "", "1") {
		a.problem(w, r, 422, "請檢查表單選項後重試。")
		return
	}
	if r.PathValue("id") != "" {
		var ok bool
		v.ID, ok = idFrom(r)
		if !ok {
			a.problem(w, r, 404, "找不到公告。")
			return
		}
		v.Version, _ = strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	}
	if _, err := a.Store.SaveAnnouncement(r.Context(), current(r).User, v); err != nil {
		a.announcementError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/announcements?saved=1", http.StatusSeeOther)
}

func (a *App) changeAnnouncement(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到公告。")
		return
	}
	version, _ := strconv.ParseInt(r.PostFormValue("version"), 10, 64)
	if err := a.Store.ChangeAnnouncement(r.Context(), current(r).User, id, version, r.PathValue("action")); err != nil {
		a.announcementError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/announcements?saved=1", http.StatusSeeOther)
}

func (a *App) announcementError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, database.ErrConflict):
		a.problem(w, r, 409, "公告已被更新，請重新載入後再操作。")
	case errors.Is(err, database.ErrAnnouncementLimit):
		a.problem(w, r, 422, "最多保留 20 則公告，請先刪除不再使用的公告。")
	case errors.Is(err, database.ErrInvalid):
		a.problem(w, r, 422, "請填寫公告標題與內容，並選擇有效的顏色。")
	default:
		a.fail(w, r, err)
	}
}
