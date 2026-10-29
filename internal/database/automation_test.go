package database

import (
	"testing"
	"time"

	"supportcenter/internal/model"
)

func testSchedule() model.AutomationSettings {
	days := make([]model.DaySchedule, 7)
	for i := 0; i < 5; i++ {
		days[i] = model.DaySchedule{Enabled: true, Ranges: []model.TimeRange{{Start: "09:00", End: "18:00"}}}
	}
	days[0].Ranges = []model.TimeRange{{Start: "09:00", End: "12:00"}, {Start: "13:00", End: "18:00"}}
	days[1].Ranges = []model.TimeRange{{Start: "10:00", End: "14:00"}}
	return model.AutomationSettings{Timezone: "Asia/Taipei", Days: days, OnCreateEnabled: true, OnCreateBody: "收到", OffHoursEnabled: true, OffHoursBody: "非服務時間"}
}

func TestAutomaticBody(t *testing.T) {
	a := testSchedule()
	for _, tc := range []struct{ at, want string }{
		{"2026-09-28T00:59:00Z", ""},
		{"2026-09-28T01:00:00Z", "收到"},
		{"2026-09-28T04:00:00Z", ""},
		{"2026-09-28T05:00:00Z", "收到"},
		{"2026-09-29T01:30:00Z", ""},
		{"2026-09-29T02:00:00Z", "收到"},
		{"2026-09-29T06:00:00Z", ""},
		{"2026-09-26T03:00:00Z", ""},
	} {
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		if got := automaticBody(a, at); got != tc.want {
			t.Errorf("at %s: %q want %q", tc.at, got, tc.want)
		}
	}
	a.OffHoursEnabled = false
	if got := automaticBody(a, time.Date(2026, 9, 26, 3, 0, 0, 0, time.UTC)); got != "收到" {
		t.Fatalf("fallback: %q", got)
	}
	a.OffHoursEnabled = true
	start := time.Date(2026, 9, 28, 0, 30, 0, 0, time.UTC)
	end := time.Date(2026, 9, 28, 6, 30, 0, 0, time.UTC)
	a.LeaveEnabled, a.LeaveStartAt, a.LeaveEndAt, a.LeaveBody = true, &start, &end, "臨時休假"
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{start.Add(-time.Second), ""},
		{start, ""},
		{time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC), ""},
		{time.Date(2026, 9, 28, 4, 0, 0, 0, time.UTC), ""},
		{end, "收到"},
	} {
		if got := automaticBody(a, tc.at); got != tc.want {
			t.Errorf("leave at %s: %q want %q", tc.at, got, tc.want)
		}
	}
	a.LeaveEnabled = false
	if got := automaticBody(a, start); got != "" {
		t.Fatalf("disabled leave was sent: %q", got)
	}
}

func TestAutomationValidation(t *testing.T) {
	a := testSchedule()
	if !validAutomation(a) {
		t.Fatal("valid settings rejected")
	}
	for _, mutate := range []func(*model.AutomationSettings){
		func(a *model.AutomationSettings) { a.Timezone = "unknown/zone" },
		func(a *model.AutomationSettings) {
			a.Days[0].Ranges = []model.TimeRange{{Start: "12:00", End: "09:00"}}
		},
		func(a *model.AutomationSettings) {
			a.Days[0].Ranges = []model.TimeRange{{Start: "09:00", End: "12:00"}, {Start: "11:59", End: "18:00"}}
		},
		func(a *model.AutomationSettings) { a.Days[0].Ranges = make([]model.TimeRange, 9) },
		func(a *model.AutomationSettings) { a.Days = a.Days[:6] },
		func(a *model.AutomationSettings) { a.Days[0].Ranges = nil },
		func(a *model.AutomationSettings) { a.OnCreateBody = "" },
		func(a *model.AutomationSettings) { a.LeaveEnabled = true },
		func(a *model.AutomationSettings) { a.LeaveStartAt = &time.Time{} },
	} {
		b := a
		b.Days = append([]model.DaySchedule(nil), a.Days...)
		mutate(&b)
		if validAutomation(b) {
			t.Fatalf("invalid settings accepted: %+v", b)
		}
	}
	a.Days[0].Ranges = []model.TimeRange{{Start: "12:00", End: "18:00"}, {Start: "09:00", End: "12:00"}}
	if !validAutomation(a) {
		t.Fatal("adjacent unsorted intervals rejected")
	}
	start := time.Now().UTC()
	end := start.Add(time.Hour)
	a.LeaveEnabled, a.LeaveStartAt, a.LeaveEndAt, a.LeaveBody = true, &start, &end, "休假中"
	if !validAutomation(a) {
		t.Fatal("valid leave rejected")
	}
	a.LeaveBody = ""
	if validAutomation(a) {
		t.Fatal("empty leave message accepted")
	}
	a.LeaveBody = "休假中"
	a.LeaveEndAt = &start
	if validAutomation(a) {
		t.Fatal("zero-length leave accepted")
	}
}

func TestCurrentServiceNotice(t *testing.T) {
	a := testSchedule()
	for _, tc := range []struct{ at, kind string }{
		{"2026-09-28T00:59:00Z", "off_hours"},
		{"2026-09-28T01:00:00Z", ""},
		{"2026-09-28T04:00:00Z", "off_hours"},
		{"2026-09-28T05:00:00Z", ""},
		{"2026-09-29T01:30:00Z", "off_hours"},
		{"2026-09-29T02:00:00Z", ""},
	} {
		at, _ := time.Parse(time.RFC3339, tc.at)
		notice := activeServiceNotice(a, at)
		if tc.kind == "" {
			if notice != nil {
				t.Fatalf("working hours displayed %q", notice.Body)
			}
			continue
		}
		if notice == nil || notice.Kind != tc.kind || notice.Body != a.OffHoursBody {
			t.Fatalf("break/outside hours notice: %+v", notice)
		}
	}
	start := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	a.LeaveEnabled, a.LeaveStartAt, a.LeaveEndAt, a.LeaveBody = true, &start, &end, "管理員的休假訊息"
	if notice := activeServiceNotice(a, start); notice == nil || notice.Kind != "leave" || notice.Body != a.LeaveBody {
		t.Fatalf("leave priority: %+v", notice)
	}
	if notice := activeServiceNotice(a, end); notice != nil {
		t.Fatal("expired leave is still displayed")
	}
	a.LeaveEnabled, a.OffHoursEnabled = false, false
	if notice := activeServiceNotice(a, start.Add(-time.Hour)); notice != nil {
		t.Fatal("disabled rule displayed a notice")
	}
}
