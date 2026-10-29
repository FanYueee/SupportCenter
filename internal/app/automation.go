package app

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"supportcenter/internal/model"
)

func (a *App) automation(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	p := base(r)
	p.Active, p.Title = "automation", "自動回覆"
	p.Saved = r.URL.Query().Get("saved") == "1"
	var err error
	p.Automation, err = a.Store.Automation(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	a.render(w, r, p)
}

func (a *App) saveAutomation(w http.ResponseWriter, r *http.Request) {
	if current(r).User.Role != "admin" {
		a.problem(w, r, 404, "找不到此頁面。")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.problem(w, r, 400, "表單無效。")
		return
	}
	settings := model.AutomationSettings{
		Timezone:        strings.TrimSpace(r.PostFormValue("timezone")),
		OnCreateEnabled: r.PostFormValue("on_create_enabled") == "on",
		OnCreateBody:    strings.TrimSpace(r.PostFormValue("on_create_body")),
		OffHoursEnabled: r.PostFormValue("off_hours_enabled") == "on",
		OffHoursBody:    strings.TrimSpace(r.PostFormValue("off_hours_body")),
		LeaveEnabled:    r.PostFormValue("leave_enabled") == "on",
		LeaveBody:       strings.TrimSpace(r.PostFormValue("leave_body")),
	}
	if value := r.PostFormValue("auto_close_days"); value != "" {
		days, err := strconv.Atoi(value)
		if err != nil || days < 0 || days > 365 {
			a.problem(w, r, 422, "自動結案天數必須為 0 至 365 天。")
			return
		}
		settings.AutoCloseDays = days
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		a.problem(w, r, 422, "時區無效。")
		return
	}
	parseLeaveTime := func(value string) (*time.Time, bool) {
		if value == "" {
			return nil, true
		}
		at, err := time.ParseInLocation("2006-01-02T15:04", value, loc)
		if err != nil || at.In(loc).Format("2006-01-02T15:04") != value {
			return nil, false
		}
		return &at, true
	}
	var validStart, validEnd bool
	settings.LeaveStartAt, validStart = parseLeaveTime(r.PostFormValue("leave_start"))
	settings.LeaveEndAt, validEnd = parseLeaveTime(r.PostFormValue("leave_end"))
	if !validStart || !validEnd || (settings.LeaveStartAt == nil) != (settings.LeaveEndAt == nil) ||
		(settings.LeaveStartAt != nil && !settings.LeaveStartAt.Before(*settings.LeaveEndAt)) ||
		(settings.LeaveEnabled && (settings.LeaveStartAt == nil || settings.LeaveBody == "")) {
		a.problem(w, r, 422, "請填妥有效的休假起迄時間與訊息。")
		return
	}
	for day := 0; day < 7; day++ {
		index := strconv.Itoa(day)
		schedule := model.DaySchedule{Enabled: r.PostFormValue("day_enabled_"+index) == "on"}
		starts, ends := r.PostForm["day_start_"+index], r.PostForm["day_end_"+index]
		if len(starts) != len(ends) || len(starts) > 8 {
			a.problem(w, r, 422, "服務時段無效，每天最多設定 8 個時段。")
			return
		}
		for i := range starts {
			start, end := starts[i], ends[i]
			if start == "" && end == "" {
				continue
			}
			if start == "" || end == "" {
				a.problem(w, r, 422, "請填妥每個時段的開始與結束時間。")
				return
			}
			schedule.Ranges = append(schedule.Ranges, model.TimeRange{Start: start, End: end})
		}
		settings.Days = append(settings.Days, schedule)
	}
	if err := a.Store.SaveAutomation(r.Context(), settings); err != nil {
		a.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/automation?saved=1", http.StatusSeeOther)
}
