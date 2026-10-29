package database

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

func (s *Store) Automation(ctx context.Context) (model.AutomationSettings, error) {
	return automation(ctx, s.Pool)
}

type automationQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func automation(ctx context.Context, q automationQuery) (a model.AutomationSettings, err error) {
	err = q.QueryRow(ctx, `SELECT timezone,weekly_schedule,on_create_enabled,on_create_body,off_hours_enabled,off_hours_body,leave_enabled,leave_start_at,leave_end_at,leave_body,auto_close_days FROM automation_settings WHERE id=1`).Scan(
		&a.Timezone, &a.Days, &a.OnCreateEnabled, &a.OnCreateBody, &a.OffHoursEnabled, &a.OffHoursBody, &a.LeaveEnabled, &a.LeaveStartAt, &a.LeaveEndAt, &a.LeaveBody, &a.AutoCloseDays)
	return
}

func validClock(value string) bool {
	if len(value) != 5 || value[2] != ':' {
		return false
	}
	for _, i := range []int{0, 1, 3, 4} {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	h := int(value[0]-'0')*10 + int(value[1]-'0')
	m := int(value[3]-'0')*10 + int(value[4]-'0')
	return h < 24 && m < 60
}

func validAutomation(a model.AutomationSettings) bool {
	if a.AutoCloseDays < 0 || a.AutoCloseDays > 365 || len(a.Days) != 7 ||
		!utf8.ValidString(a.OnCreateBody) || !utf8.ValidString(a.OffHoursBody) || !utf8.ValidString(a.LeaveBody) ||
		strings.ContainsRune(a.OnCreateBody, 0) || strings.ContainsRune(a.OffHoursBody, 0) || strings.ContainsRune(a.LeaveBody, 0) ||
		len([]rune(a.OnCreateBody)) > 20000 || len([]rune(a.OffHoursBody)) > 20000 || len([]rune(a.LeaveBody)) > 20000 ||
		(a.OnCreateEnabled && strings.TrimSpace(a.OnCreateBody) == "") || (a.OffHoursEnabled && strings.TrimSpace(a.OffHoursBody) == "") ||
		(a.LeaveStartAt == nil) != (a.LeaveEndAt == nil) ||
		(a.LeaveStartAt != nil && !a.LeaveStartAt.Before(*a.LeaveEndAt)) ||
		(a.LeaveEnabled && (a.LeaveStartAt == nil || strings.TrimSpace(a.LeaveBody) == "")) {
		return false
	}
	for _, day := range a.Days {
		if len(day.Ranges) > 8 || (day.Enabled && len(day.Ranges) == 0) {
			return false
		}
		ranges := slices.Clone(day.Ranges)
		slices.SortFunc(ranges, func(x, y model.TimeRange) int { return strings.Compare(x.Start, y.Start) })
		for i, period := range ranges {
			if !validClock(period.Start) || !validClock(period.End) || period.Start >= period.End || (i > 0 && ranges[i-1].End > period.Start) {
				return false
			}
		}
	}
	if len(a.Timezone) > 64 || strings.ContainsAny(a.Timezone, "\r\n\x00") {
		return false
	}
	_, err := time.LoadLocation(a.Timezone)
	return err == nil
}

func (s *Store) SaveAutomation(ctx context.Context, a model.AutomationSettings) error {
	if !validAutomation(a) {
		return ErrInvalid
	}
	a.Days = slices.Clone(a.Days)
	for i := range a.Days {
		a.Days[i].Ranges = slices.Clone(a.Days[i].Ranges)
		if a.Days[i].Ranges == nil {
			a.Days[i].Ranges = []model.TimeRange{}
		}
		slices.SortFunc(a.Days[i].Ranges, func(x, y model.TimeRange) int { return strings.Compare(x.Start, y.Start) })
	}
	schedule, err := json.Marshal(a.Days)
	if err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var previousDays int
	if err = tx.QueryRow(ctx, `SELECT auto_close_days FROM automation_settings WHERE id=1 FOR UPDATE`).Scan(&previousDays); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE automation_settings SET timezone=$1,weekly_schedule=$2::jsonb,on_create_enabled=$3,on_create_body=$4,off_hours_enabled=$5,off_hours_body=$6,leave_enabled=$7,leave_start_at=$8,leave_end_at=$9,leave_body=$10,auto_close_days=$11,updated_at=now() WHERE id=1`,
		a.Timezone, string(schedule), a.OnCreateEnabled, a.OnCreateBody, a.OffHoursEnabled, a.OffHoursBody, a.LeaveEnabled, a.LeaveStartAt, a.LeaveEndAt, a.LeaveBody, a.AutoCloseDays)
	if err != nil {
		return err
	}
	if previousDays != a.AutoCloseDays && a.AutoCloseDays > 0 {
		if err = notifyResolved(ctx, tx, 0); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func automaticBody(a model.AutomationSettings, now time.Time) string {
	if notice := activeServiceNotice(a, now); notice != nil {
		return ""
	}
	if _, err := time.LoadLocation(a.Timezone); err != nil {
		return ""
	}
	if a.OnCreateEnabled {
		return a.OnCreateBody
	}
	return ""
}

// This is current service availability, not a new conversation message.
func (s *Store) ServiceNotice(ctx context.Context, at time.Time) (*model.ServiceNotice, error) {
	a, err := s.Automation(ctx)
	if err != nil {
		return nil, err
	}
	return activeServiceNotice(a, at), nil
}

func activeServiceNotice(a model.AutomationSettings, now time.Time) *model.ServiceNotice {
	if a.LeaveEnabled && a.LeaveStartAt != nil && a.LeaveEndAt != nil && !now.Before(*a.LeaveStartAt) && now.Before(*a.LeaveEndAt) {
		return &model.ServiceNotice{Kind: "leave", Body: a.LeaveBody}
	}
	loc, err := time.LoadLocation(a.Timezone)
	if err != nil {
		return nil
	}
	local := now.In(loc)
	weekday := (int(local.Weekday()) + 6) % 7
	clock := local.Format("15:04")
	working := false
	if len(a.Days) == 7 && a.Days[weekday].Enabled {
		for _, period := range a.Days[weekday].Ranges {
			if clock >= period.Start && clock < period.End {
				working = true
				break
			}
		}
	}
	if !working && a.OffHoursEnabled {
		return &model.ServiceNotice{Kind: "off_hours", Body: a.OffHoursBody}
	}
	return nil
}

func insertAutomaticReply(ctx context.Context, tx pgx.Tx, ticketID int64) error {
	a, err := automation(ctx, tx)
	if err != nil {
		return err
	}
	body := automaticBody(a, time.Now())
	if body == "" {
		return nil
	}
	var authorID int64
	err = tx.QueryRow(ctx, `INSERT INTO users(auth0_id,name,email,role) VALUES('system:automation','自動回覆','automation@local.invalid','user') ON CONFLICT(auth0_id) DO UPDATE SET auth0_id=EXCLUDED.auth0_id RETURNING id`).Scan(&authorID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ticket_messages(ticket_id,user_id,body,internal,source) VALUES($1,$2,$3,false,'automation')`, ticketID, authorID, body)
	return err
}
