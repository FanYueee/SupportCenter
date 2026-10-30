package app

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"supportcenter/internal/model"
)

// prioritySupport is what an account may choose when filing a ticket, with
// Level already resolved to the level in force. Staff may choose any priority.
func (a *App) prioritySupport(ctx context.Context, userID int64, staff bool) (model.PrioritySupport, error) {
	if staff {
		return model.PrioritySupport{Level: "urgent"}, nil
	}
	support, err := a.Store.PrioritySupport(ctx, userID)
	if support.Level = support.Current(time.Now()); support.Level == "low" {
		support.Until = nil
	}
	return support, err
}

// siteLocation is the timezone set under automation; end dates entered by
// admins last until midnight there.
func (a *App) siteLocation(ctx context.Context) (*time.Location, error) {
	settings, err := a.Store.Automation(ctx)
	if err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return time.UTC, nil
	}
	return location, nil
}

func (a *App) setPrioritySupport(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, http.StatusNotFound, "找不到此頁面。")
		return
	}
	id, ok := idFrom(r)
	if !ok {
		a.problem(w, r, http.StatusNotFound, "找不到使用者。")
		return
	}
	location, err := a.siteLocation(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	support := model.PrioritySupport{Level: r.PostFormValue("level")}
	switch until := r.PostFormValue("until"); {
	case support.Level == "low" || until == "":
	case until == r.PostFormValue("shown_until"):
		// An untouched date keeps the exact end time the store may have set.
		stored, err := a.Store.PrioritySupport(r.Context(), id)
		if err != nil {
			a.fail(w, r, err)
			return
		}
		support.Until = stored.Until
	default:
		day, err := time.ParseInLocation(time.DateOnly, until, location)
		if err != nil || day.Year() < 2000 || day.Year() > 2200 {
			a.problem(w, r, 422, "請輸入有效的到期日。")
			return
		}
		end := day.AddDate(0, 0, 1)
		support.Until = &end
	}
	if err = a.Store.SetPrioritySupport(r.Context(), id, support); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Log.Info("priority support changed", "user", id, "level", support.Level, "by", current(r).User.ID)
	back := url.Values{}
	if q := r.PostFormValue("q"); q != "" {
		back.Set("q", q)
	}
	if page := pageNum(r.PostFormValue("page")); page > 1 {
		back.Set("page", strconv.Itoa(page))
	}
	http.Redirect(w, r, "/admin/users?"+back.Encode(), http.StatusSeeOther)
}

// portalPriority accepts "normal" from Portals built before there were four
// priorities; it became "low".
func portalPriority(priority string) string {
	if priority == "normal" {
		return "low"
	}
	return priority
}

func portalPrioritySupport(support model.PrioritySupport, staff bool) map[string]any {
	current := support.Current(time.Now())
	allowed := model.Priorities[:slices.Index(model.Priorities, current)+1]
	if staff {
		allowed = model.Priorities
	}
	return map[string]any{"level": support.Level, "until": support.Until, "current": current, "allowedPriorities": allowed}
}

// portalSetPrioritySupport lets the Portal's store set what the actor bought:
// {"level": "high", "until": "2027-01-31T16:00:00Z"}. Without until the level
// does not end; "low" removes priority support.
func (a *App) portalSetPrioritySupport(w http.ResponseWriter, r *http.Request) {
	identity := portalCurrent(r)
	var input struct {
		Level string     `json:"level"`
		Until *time.Time `json:"until"`
	}
	if !portalDecode(w, r, &input) {
		return
	}
	if !slices.Contains(model.Priorities, input.Level) || input.Until != nil && (input.Until.Year() < 2000 || input.Until.Year() > 2200) {
		portalError(w, 422, "invalid")
		return
	}
	support := model.PrioritySupport{Level: input.Level, Until: input.Until}
	if support.Level == "low" {
		support.Until = nil
	}
	if err := a.Store.SetPrioritySupport(r.Context(), identity.User.ID, support); err != nil {
		a.portalFailure(w, err)
		return
	}
	a.Log.Info("priority support changed", "user", identity.User.ID, "level", support.Level, "by", "portal store")
	portalJSON(w, 200, portalPrioritySupport(support, false))
}
