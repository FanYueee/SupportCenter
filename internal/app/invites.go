package app

import (
	"net/http"
	"strconv"
)

func (a *App) invite(w http.ResponseWriter, r *http.Request) {
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到客服單。")
		return
	}
	if err := a.Store.Invite(r.Context(), current(r).User, id, r.PostFormValue("email")); err != nil {
		a.fail(w, r, err)
		return
	}
	a.afterMutation(w, r, id, false)
}

func (a *App) uninvite(w http.ResponseWriter, r *http.Request) {
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, 404, "找不到客服單。")
		return
	}
	if err := a.Store.Uninvite(r.Context(), current(r).User, id, r.PostFormValue("email")); err != nil {
		a.fail(w, r, err)
		return
	}
	a.afterMutation(w, r, id, false)
}

func (a *App) leaveTicket(w http.ResponseWriter, r *http.Request) {
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, http.StatusNotFound, "找不到客服單。")
		return
	}
	if err := a.Store.LeaveTicket(r.Context(), current(r).User, id); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/tickets", http.StatusSeeOther)
}

func (a *App) ticketPersonAvatar(w http.ResponseWriter, r *http.Request) {
	id, ok := idFrom(r)
	personID, err := strconv.ParseInt(r.PathValue("person"), 10, 64)
	if !ok || err != nil || personID < 1 {
		http.NotFound(w, r)
		return
	}
	person, err := a.Store.TicketPerson(r.Context(), current(r).User, id, personID)
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.writeAvatar(w, r, person)
}
