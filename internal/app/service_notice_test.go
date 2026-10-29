package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"supportcenter/internal/model"
)

func TestCurrentServiceNoticeDoesNotWriteMessages(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	settings, _ := a.Store.Automation(ctx)
	settings.Timezone = "UTC"
	settings.Days = make([]model.DaySchedule, 7)
	settings.OffHoursEnabled, settings.OffHoursBody = true, "<script>alert(1)</script> 非服務時間訊息"
	if err := a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "current service notice", Category: "other", Priority: "normal", AssistanceMode: "ai"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/tickets/%d", id)
	for i := 0; i < 2; i++ {
		page := request(h, "GET", path, "alice", nil)
		body := page.Body.String()
		if page.Code != 200 || !strings.Contains(body, "目前非客服服務時間") || strings.Contains(body, "<script>alert") || !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Fatalf("escaped current service notice: %d", page.Code)
		}
		if strings.Index(body, `class="service-notice"`) > strings.Index(body, `class="reply-form"`) {
			t.Fatal("notice is not above the reply form")
		}
		if strings.Count(body, "&lt;script&gt;alert(1)&lt;/script&gt; 非服務時間訊息") != 1 {
			t.Fatal("availability message should appear only once")
		}
	}
	if page := request(h, "GET", path, "bob", nil); page.Code != 404 {
		t.Fatalf("other user accessed ticket: %d", page.Code)
	}
	if body := request(h, "GET", fmt.Sprintf("/admin/tickets/%d", id), "staff", nil).Body.String(); strings.Contains(body, `class="service-notice"`) {
		t.Fatal("customer notice appears on staff workspace")
	}
	start, end := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	settings.LeaveEnabled, settings.LeaveStartAt, settings.LeaveEndAt, settings.LeaveBody = true, &start, &end, "臨時休假原文"
	if err = a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if body := request(h, "GET", path, "alice", nil).Body.String(); !strings.Contains(body, "客服休假中") || !strings.Contains(body, "臨時休假原文") {
		t.Fatal("leave does not override outside hours")
	}
	settings.LeaveEnabled, settings.OffHoursEnabled = false, false
	if err = a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if body := request(h, "GET", path, "alice", nil).Body.String(); strings.Contains(body, `class="service-notice"`) {
		t.Fatal("disabled notice still appears")
	}
	if messages, _, err := a.Store.Messages(ctx, alice, id, false, 1); err != nil || len(messages) != 1 {
		t.Fatalf("views/settings added conversation messages: %d %v", len(messages), err)
	}
	settings.OffHoursEnabled = true
	if err = a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "normal", nil); err != nil {
		t.Fatal(err)
	}
	if body := request(h, "GET", path, "alice", nil).Body.String(); strings.Contains(body, `class="service-notice"`) {
		t.Fatal("resolved ticket shows active service warning")
	}
}

func TestLegacyServiceNoticesAreArchivedWithoutLosingOtherMessages(t *testing.T) {
	a, _, alice, _, staff := testApp(t)
	ctx := context.Background()
	settings, err := a.Store.Automation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.Timezone = "UTC"
	settings.Days = make([]model.DaySchedule, 7)
	settings.OffHoursEnabled, settings.OffHoursBody = true, "outside hours"
	// Even a matching availability setting must not hide automatic closure.
	closure := "此客服單已依設定自動結案。如需繼續討論，請重新開啟客服單。"
	settings.LeaveBody = closure
	if err = a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "legacy availability", Category: "other", Priority: "normal"}, settings.OffHoursBody, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Reconstruct the pre-migration schema only in this test's isolated database.
	if _, err = a.Store.Pool.Exec(ctx, `ALTER TABLE ticket_messages DROP COLUMN service_notice`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `DELETE FROM schema_migrations WHERE name='020_service_notice_display.sql'`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `INSERT INTO ticket_messages(ticket_id,user_id,body,source) SELECT $1,$2,$3,'automation' FROM generate_series(1,40)`, id, alice.ID, settings.OffHoursBody); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"opening acknowledgement", closure} {
		if _, err = a.Store.Pool.Exec(ctx, `INSERT INTO ticket_messages(ticket_id,user_id,body,source) VALUES($1,$2,$3,'automation')`, id, alice.ID, body); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.Store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var retained, archived int
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE service_notice) FROM ticket_messages WHERE ticket_id=$1`, id).Scan(&retained, &archived); err != nil || retained != 43 || archived != 40 {
		t.Fatalf("migration retained=%d archived=%d err=%v", retained, archived, err)
	}
	// Editing the notice later must not restore archived duplicates.
	settings.OffHoursBody = "updated availability"
	if err = a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	for _, viewer := range []model.User{alice, staff} {
		messages, more, err := a.Store.Messages(ctx, viewer, id, viewer.Staff(), 1)
		if err != nil || len(messages) != 3 || more {
			t.Fatalf("visible messages=%d more=%v err=%v", len(messages), more, err)
		}
		for _, m := range messages {
			if m.Source == "automation" && m.Body == "outside hours" {
				t.Fatal("archived notice reappeared")
			}
		}
	}
}
