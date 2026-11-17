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

type recordedMail struct{ to, from, subject, body, html string }
type fakeMailSender struct {
	sent []recordedMail
	err  error
}

func (f *fakeMailSender) Send(_ context.Context, msg notify.Message) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, recordedMail{msg.To, msg.FromName, msg.Subject, msg.Text, msg.HTML})
	return nil
}

func TestDepartmentsCategoryTimelineAndDepartmentEmail(t *testing.T) {
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
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "technical routing", Category: "technical", Priority: "low"}, "help", nil)
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
	if err != nil || ticket.Status != "new" || len(ticket.Team) != 1 || ticket.Team[0].ID != staff.ID {
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
	if len(mail.sent) != 2 || mail.sent[0].to != staff.Email || !strings.Contains(mail.sent[0].body, path) || !strings.Contains(mail.sent[0].html, "technical routing") || !strings.Contains(mail.sent[1].html, "已指派給你") {
		t.Fatalf("unexpected notification: %+v", mail.sent)
	}
	update := url.Values{"csrf": {"csrf-admin"}, "version": {fmt.Sprint(ticket.Version)}, "status": {"open"}, "priority": {"low"}, "category": {"billing"}, "assignees": {fmt.Sprint(staff.ID)}}
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
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM email_notifications WHERE ticket_id=$1 AND kind='department' AND state='pending'`, id).Scan(&queued); err != nil || queued != 1 {
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
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM email_notifications WHERE ticket_id=$1 AND kind='department' AND state='cancelled'`, id).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("misroute cancellation: %d %v", queued, err)
	}
	if processed, err := worker.ProcessOne(ctx); err != nil || processed {
		t.Fatalf("cancelled notification sent: %v %v", processed, err)
	}
}

func TestNewTicketsEmailTheirDepartment(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	admin, err := a.Store.User(ctx, "admin", "admin", "admin@example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, staff.ID); err != nil {
		t.Fatal(err)
	}
	// One person may look after several departments.
	for id, departments := range map[int64][]string{staff.ID: {"billing", "other"}, admin.ID: {"technical"}} {
		if got := request(h, "POST", fmt.Sprintf("/admin/departments/%d", id), "admin", url.Values{"csrf": {"csrf-admin"}, "departments": departments}); got.Code != 303 {
			t.Fatalf("save departments: %d", got.Code)
		}
	}
	mail := &fakeMailSender{}
	worker := notify.Worker{Store: a.Store, Sender: mail, Origin: "https://support.example.test", Log: a.Log}
	for _, c := range []struct{ category, to string }{{"billing", staff.Email}, {"other", staff.Email}, {"technical", admin.Email}} {
		id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "routing " + c.category, Category: c.category, Priority: "low"}, "help", nil)
		if err != nil {
			t.Fatal(err)
		}
		mail.sent = nil
		for {
			processed, err := worker.ProcessOne(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !processed {
				break
			}
		}
		department := model.DepartmentName(c.category)
		if len(mail.sent) != 1 || mail.sent[0].to != c.to || !strings.Contains(mail.sent[0].subject, model.Category(c.category)+"客服單") ||
			!strings.Contains(mail.sent[0].html, "已送至"+department) || !strings.Contains(mail.sent[0].body, fmt.Sprintf("/admin/tickets/%d", id)) {
			t.Fatalf("%s ticket notified %+v", c.category, mail.sent)
		}
	}
}

// Staff can read what the customer wrote, and who they are, in the email
// itself: a new ticket, an assignment and each customer reply.
func TestStaffEmailsCarryTheCustomersMessage(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	admin, err := a.Store.User(ctx, "admin", "admin", "admin@example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	verify(t, a, staff, admin)
	if got := request(h, "POST", fmt.Sprintf("/admin/departments/%d", staff.ID), "admin", url.Values{"csrf": {"csrf-admin"}, "departments": {"technical"}}); got.Code != 303 {
		t.Fatalf("save departments: %d", got.Code)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET priority_level='high' WHERE id=$1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	mail := &fakeMailSender{}
	worker := notify.Worker{Store: a.Store, Sender: mail, Origin: "https://support.example.test", Log: a.Log}
	drain := func() []recordedMail {
		t.Helper()
		mail.sent = nil
		for {
			processed, err := worker.ProcessOne(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !processed {
				return mail.sent
			}
		}
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "節點連不上", Category: "technical", Priority: "high"}, "節點 3 從昨晚開始連不上\n錯誤碼 502", nil)
	if err != nil {
		t.Fatal(err)
	}
	sent := drain()
	if len(sent) != 1 || sent[0].to != staff.Email || !strings.Contains(sent[0].subject, "節點連不上") ||
		!strings.Contains(sent[0].body, "> 節點 3 從昨晚開始連不上\n> 錯誤碼 502") || !strings.Contains(sent[0].html, "錯誤碼 502") ||
		!strings.Contains(sent[0].body, "客戶：alice（alice@example.test）") || !strings.Contains(sent[0].body, "支援等級：進階") || !strings.Contains(sent[0].body, "優先級：高") {
		t.Fatalf("new ticket email should carry the message and customer: %+v", sent)
	}
	path := fmt.Sprintf("/tickets/%d", id)
	if res := request(h, "POST", path+"/reply", "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"補充：換了節點也一樣"}}); res.Code != 303 {
		t.Fatal("customer reply failed", res.Code)
	}
	// Nobody is on it yet, so its department hears.
	sent = drain()
	if len(sent) != 1 || sent[0].to != staff.Email || !strings.Contains(sent[0].subject, "有客戶回覆") || !strings.Contains(sent[0].body, "> 補充：換了節點也一樣") || strings.Contains(sent[0].body, "錯誤碼 502") {
		t.Fatalf("customer reply email: %+v", sent)
	}
	for _, internal := range []bool{true, false} {
		form := url.Values{"csrf": {"csrf-staff"}, "body": {"staff message"}}
		if internal {
			form.Set("internal", "true")
		}
		if res := request(h, "POST", "/admin"+path+"/reply", "staff", form); res.Code != 303 {
			t.Fatal("staff reply failed", res.Code)
		}
	}
	if sent = drain(); len(sent) != 0 {
		t.Fatalf("staff messages emailed staff: %+v", sent)
	}
	// Once someone works the ticket, only they hear, wherever their department.
	ticket, err := a.Store.Ticket(ctx, admin, id, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.UpdateTeam(ctx, admin, id, true, "open", "high", []int64{admin.ID}, ticket.Version); err != nil {
		t.Fatal(err)
	}
	drain()
	if res := request(h, "POST", path+"/reply", "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"第三則：附上截圖"}}); res.Code != 303 {
		t.Fatal("customer reply failed", res.Code)
	}
	if sent = drain(); len(sent) != 1 || sent[0].to != admin.Email || !strings.Contains(sent[0].body, "> 第三則：附上截圖") {
		t.Fatalf("the assignee should get the reply: %+v", sent)
	}
	// A ticket the AI answers does not email staff on every customer message.
	aiTicket, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "AI 處理", Category: "technical", Priority: "low", AssistanceMode: "ai"}, "第一則", nil)
	if err != nil {
		t.Fatal(err)
	}
	drain()
	if res := request(h, "POST", fmt.Sprintf("/tickets/%d/reply", aiTicket), "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"再問一次"}}); res.Code != 303 {
		t.Fatal("customer reply failed", res.Code)
	}
	var queued int
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM email_notifications WHERE ticket_id=$1 AND kind='reply'`, aiTicket).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("AI ticket reply emailed staff: %d %v", queued, err)
	}
}
