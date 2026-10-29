package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"supportcenter/internal/model"
	"supportcenter/internal/notify"
)

type recordedMail struct{ to, subject, body string }
type fakeMailSender struct{ sent []recordedMail }

func (f *fakeMailSender) Send(_ context.Context, to, subject, body string) error {
	f.sent = append(f.sent, recordedMail{to, subject, body})
	return nil
}

func TestDepartmentsCategoryTimelineAndTechnicalEmail(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	admin, err := a.Store.User(ctx, "admin", "admin", "admin@example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, staff.ID); err != nil {
		t.Fatal(err)
	}
	departmentPath := fmt.Sprintf("/admin/departments/%d", staff.ID)
	if got := request(h, "POST", departmentPath, "staff", url.Values{"csrf": {"csrf-staff"}, "departments": {"technical"}}); got.Code != 404 {
		t.Fatalf("support user changed groups: %d", got.Code)
	}
	if got := request(h, "POST", departmentPath, "admin", url.Values{"csrf": {"csrf-admin"}, "departments": {"invalid"}}); got.Code != 422 {
		t.Fatalf("invalid group accepted: %d", got.Code)
	}
	if got := request(h, "POST", departmentPath, "admin", url.Values{"csrf": {"csrf-admin"}, "departments": {"technical"}}); got.Code != 303 {
		t.Fatalf("admin group save: %d", got.Code)
	}
	if got := request(h, "GET", "/admin/departments", "admin", nil); got.Code != 200 || !strings.Contains(got.Body.String(), "技術部門") {
		t.Fatalf("group settings unavailable: %d", got.Code)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "technical routing", Category: "technical", Priority: "normal"}, "help", nil)
	if err != nil {
		t.Fatal(err)
	}
	var queued int
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM email_notifications WHERE ticket_id=$1 AND recipient_user_id=$2 AND state='pending'`, id, staff.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("technical create notification: %d %v", queued, err)
	}
	path := fmt.Sprintf("/admin/tickets/%d", id)
	page := request(h, "GET", path, "admin", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "指派自己") || !strings.Contains(page.Body.String(), "指派 staff") || !strings.Contains(page.Body.String(), `hx-select="#ticket-sidebar"`) {
		t.Fatalf("assignment controls missing: %d self=%v staff=%v partial=%v", page.Code, strings.Contains(page.Body.String(), "指派自己"), strings.Contains(page.Body.String(), "指派 staff"), strings.Contains(page.Body.String(), `hx-select="#ticket-sidebar"`))
	}
	ticket, err := a.Store.Ticket(ctx, admin, id, true)
	if err != nil {
		t.Fatal(err)
	}
	assign := url.Values{"csrf": {"csrf-admin"}, "version": {fmt.Sprint(ticket.Version)}, "assignee_id": {fmt.Sprint(staff.ID)}}
	if got := request(h, "POST", path+"/assign", "admin", assign); got.Code != 303 {
		t.Fatalf("quick assignment: %d %s", got.Code, got.Body.String())
	}
	ticket, err = a.Store.Ticket(ctx, admin, id, true)
	if err != nil || ticket.Status != "open" || len(ticket.Team) != 1 || ticket.Team[0].ID != staff.ID {
		t.Fatalf("assigned ticket: %+v %v", ticket, err)
	}
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM email_notifications WHERE ticket_id=$1 AND recipient_user_id=$2 AND kind='assignment'`, id, staff.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("assignment notification: %d %v", queued, err)
	}
	if got := request(h, "POST", path+"/assign", "admin", assign); got.Code != 409 {
		t.Fatalf("stale assignment accepted: %d", got.Code)
	}
	mail := &fakeMailSender{}
	worker := notify.Worker{Store: a.Store, Sender: mail, Origin: "https://support.example.test", Log: a.Log}
	for i := 0; i < 2; i++ {
		processed, err := worker.ProcessOne(ctx)
		if err != nil || !processed {
			t.Fatalf("send notification %d: %v %v", i, processed, err)
		}
	}
	if len(mail.sent) != 2 || mail.sent[0].to != staff.Email || !strings.Contains(mail.sent[0].body, path) {
		t.Fatalf("unexpected notification: %+v", mail.sent)
	}
	update := url.Values{"csrf": {"csrf-admin"}, "version": {fmt.Sprint(ticket.Version)}, "status": {"open"}, "priority": {"normal"}, "category": {"billing"}, "assignees": {fmt.Sprint(staff.ID)}}
	if got := request(h, "POST", path+"/update", "admin", update); got.Code != 303 {
		t.Fatalf("category update: %d %s", got.Code, got.Body.String())
	}
	page = request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "message-system") || !strings.Contains(page.Body.String(), "問題類別由「技術支援」改為「帳務支援」") {
		t.Fatalf("customer cannot see category history: %d", page.Code)
	}
	if got := request(h, "POST", fmt.Sprintf("/tickets/%d/update", id), "alice", url.Values{"csrf": {"csrf-alice"}, "status": {"closed"}, "category": {"technical"}}); got.Code != 422 {
		t.Fatalf("customer changed category: %d", got.Code)
	}
	var events int
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM ticket_events WHERE ticket_id=$1 AND kind='category' AND previous='technical' AND current='billing'`, id).Scan(&events); err != nil || events != 1 {
		t.Fatalf("category audit event: %d %v", events, err)
	}
	ticket, err = a.Store.Ticket(ctx, admin, id, true)
	if err != nil {
		t.Fatal(err)
	}
	update.Set("version", fmt.Sprint(ticket.Version))
	update.Set("category", "technical")
	if got := request(h, "POST", path+"/update", "admin", update); got.Code != 303 {
		t.Fatalf("return to technical department: %d", got.Code)
	}
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM email_notifications WHERE ticket_id=$1 AND kind='technical' AND state='pending'`, id).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("rerouting notification: %d %v", queued, err)
	}
	ticket, err = a.Store.Ticket(ctx, admin, id, true)
	if err != nil {
		t.Fatal(err)
	}
	update.Set("version", fmt.Sprint(ticket.Version))
	update.Set("category", "other")
	if got := request(h, "POST", path+"/update", "admin", update); got.Code != 303 {
		t.Fatalf("correct technical misroute: %d", got.Code)
	}
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM email_notifications WHERE ticket_id=$1 AND kind='technical' AND state='cancelled'`, id).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("misroute cancellation: %d %v", queued, err)
	}
	if processed, err := worker.ProcessOne(ctx); err != nil || processed {
		t.Fatalf("cancelled notification sent: %v %v", processed, err)
	}
}
