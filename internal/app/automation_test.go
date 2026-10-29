package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"supportcenter/internal/model"
)

func TestAutomationAdminAndCreate(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	if got := request(h, "GET", "/admin/automation", "alice", nil); got.Code != 404 {
		t.Fatalf("user read: %d", got.Code)
	}
	if got := request(h, "GET", "/admin/automation", "staff", nil); got.Code != 404 {
		t.Fatalf("staff read: %d", got.Code)
	}
	if got := request(h, "GET", "/admin/automation", "admin", nil); got.Code != 200 {
		t.Fatalf("admin read: %d", got.Code)
	}
	form := url.Values{"csrf": {"csrf-admin"}, "timezone": {"Asia/Taipei"}, "day_enabled_0": {"on"}, "day_start_0": {"13:00", "09:00", ""}, "day_end_0": {"18:00", "12:00", ""}, "day_enabled_1": {"on"}, "day_start_1": {"10:00"}, "day_end_1": {"14:00"}, "on_create_enabled": {"on"}, "on_create_body": {"已收到客服單"}, "off_hours_enabled": {"on"}, "off_hours_body": {"目前非服務時間"}}
	if got := request(h, "POST", "/admin/automation", "alice", form); got.Code != 404 {
		t.Fatalf("user write: %d", got.Code)
	}
	if got := request(h, "POST", "/admin/automation", "admin", url.Values{"timezone": {"UTC"}}); got.Code != 403 {
		t.Fatalf("csrf: %d", got.Code)
	}
	if got := request(h, "POST", "/admin/automation", "admin", form); got.Code != 303 {
		t.Fatalf("admin save: %d %s", got.Code, got.Body.String())
	}
	settings, err := a.Store.Automation(context.Background())
	if err != nil || len(settings.Days) != 7 || !settings.Days[0].Enabled || !settings.Days[1].Enabled || settings.Days[2].Enabled || !settings.OnCreateEnabled || !settings.OffHoursEnabled || len(settings.Days[0].Ranges) != 2 || settings.Days[0].Ranges[0].Start != "09:00" || settings.Days[1].Ranges[0].Start != "10:00" {
		t.Fatalf("settings: %+v %v", settings, err)
	}
	bad := url.Values{}
	for key, values := range form {
		bad[key] = append([]string(nil), values...)
	}
	bad["day_start_0"] = []string{"09:00", "11:00"}
	bad["day_end_0"] = []string{"12:00", "18:00"}
	if got := request(h, "POST", "/admin/automation", "admin", bad); got.Code != 422 {
		t.Fatalf("overlap accepted: %d", got.Code)
	}
	settings.OffHoursEnabled = false
	if err = a.Store.SaveAutomation(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(context.Background(), alice, model.Ticket{Subject: "automation", Category: "other", Priority: "normal"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _, err := a.Store.Messages(context.Background(), alice, id, false, 1)
	if err != nil || len(msgs) != 2 || msgs[1].Source != "automation" {
		t.Fatalf("messages: %+v %v", msgs, err)
	}
	if msgs[1].Body != "已收到客服單" {
		t.Fatalf("unexpected body %q", msgs[1].Body)
	}
	page := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "自動回覆") {
		t.Fatalf("automatic reply hidden: %d", page.Code)
	}
	if got := request(h, "GET", "/admin/users", "admin", nil); strings.Contains(got.Body.String(), "automation@local.invalid") {
		t.Fatal("system user visible in admin listing")
	}
}

func TestTemporaryLeaveOverridesScheduledReplies(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour).In(loc).Format("2006-01-02T15:04")
	end := time.Now().Add(time.Hour).In(loc).Format("2006-01-02T15:04")
	form := url.Values{
		"csrf":              {"csrf-admin"},
		"timezone":          {"Asia/Taipei"},
		"on_create_enabled": {"on"},
		"on_create_body":    {"一般開單回覆"},
		"leave_enabled":     {"on"},
		"leave_start":       {start},
		"leave_end":         {end},
		"leave_body":        {"臨時休假，回來後處理。"},
	}
	if got := request(h, "POST", "/admin/automation", "admin", form); got.Code != 303 {
		t.Fatalf("save leave: %d %s", got.Code, got.Body.String())
	}
	settings, err := a.Store.Automation(context.Background())
	if err != nil || !settings.LeaveEnabled || settings.LeaveStartAt == nil || settings.LeaveEndAt == nil || settings.LeaveBody != "臨時休假，回來後處理。" {
		t.Fatalf("saved leave: %+v %v", settings, err)
	}
	page := request(h, "GET", "/admin/automation", "admin", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), `name="leave_start" value="`+start+`"`) || !strings.Contains(page.Body.String(), `name="leave_end" value="`+end+`"`) {
		t.Fatalf("leave dates not shown in configured timezone: %d", page.Code)
	}
	invalid := url.Values{}
	for key, values := range form {
		invalid[key] = append([]string(nil), values...)
	}
	invalid.Set("leave_end", start)
	if got := request(h, "POST", "/admin/automation", "admin", invalid); got.Code != 422 {
		t.Fatalf("invalid leave accepted: %d", got.Code)
	}
	id, err := a.Store.Create(context.Background(), alice, model.Ticket{Subject: "leave auto reply", Category: "other", Priority: "normal"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	messages, _, err := a.Store.Messages(context.Background(), alice, id, false, 1)
	if err != nil || len(messages) != 1 {
		t.Fatalf("leave did not override normal reply: %+v %v", messages, err)
	}
	if body := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil).Body.String(); !strings.Contains(body, "臨時休假，回來後處理。") || !strings.Contains(body, `class="service-notice"`) {
		t.Fatal("leave notice is not above the reply form")
	}
	form.Del("leave_enabled")
	if got := request(h, "POST", "/admin/automation", "admin", form); got.Code != 303 {
		t.Fatalf("disable leave: %d", got.Code)
	}
	id, err = a.Store.Create(context.Background(), alice, model.Ticket{Subject: "normal auto reply", Category: "other", Priority: "normal"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	messages, _, err = a.Store.Messages(context.Background(), alice, id, false, 1)
	if err != nil || len(messages) != 2 || messages[1].Body != "一般開單回覆" {
		t.Fatalf("normal reply not restored: %+v %v", messages, err)
	}
}

func TestAIHandoffOwnerOnly(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	id, err := a.Store.Create(context.Background(), alice, model.Ticket{Subject: "AI handoff", Category: "technical", Priority: "normal", AssistanceMode: "ai"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/tickets/%d/handoff", id)
	if got := request(h, "POST", path, "bob", url.Values{"csrf": {"csrf-bob"}}); got.Code != 404 {
		t.Fatalf("other owner: %d", got.Code)
	}
	if got := request(h, "POST", path, "alice", nil); got.Code != 403 {
		t.Fatalf("csrf: %d", got.Code)
	}
	if got := request(h, "POST", path, "alice", url.Values{"csrf": {"csrf-alice"}}); got.Code != 303 {
		t.Fatalf("handoff: %d", got.Code)
	}
	ticket, err := a.Store.Ticket(context.Background(), alice, id, false)
	if err != nil || ticket.AssistanceMode != "human" || ticket.Status != "open" || ticket.HandoffAt == nil {
		t.Fatalf("ticket: %+v %v", ticket, err)
	}
	if got := request(h, "POST", path, "alice", url.Values{"csrf": {"csrf-alice"}}); got.Code != 303 {
		t.Fatalf("repeat: %d", got.Code)
	}
}
