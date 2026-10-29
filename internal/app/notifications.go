package app

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

func (a *App) notifications(w http.ResponseWriter, r *http.Request) {
	p := base(r)
	p.Active, p.Title = "notifications", "通知"
	p.Page = min(pageNum(r.URL.Query().Get("page")), 1000)
	var err error
	p.Notifications, p.HasNext, err = a.Store.Notifications(r.Context(), p.User.ID, p.Page)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, p)
}
func (a *App) readNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到通知。")
		return
	}
	ticketID, err := a.Store.ReadNotification(r.Context(), current(r).User.ID, id)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/tickets/"+strconv.FormatInt(ticketID, 10), http.StatusSeeOther)
}
func (a *App) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.ReadAllNotifications(r.Context(), current(r).User.ID); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}
func (a *App) AutoClose(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		jobCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		_, err := a.Store.CloseSolvedTickets(jobCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			a.Log.Error("automatic ticket closure failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
