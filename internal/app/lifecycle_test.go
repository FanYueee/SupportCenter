package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"supportcenter/internal/model"
)

func TestWaitingTicketsAreRemindedThenClosedThenLocked(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	a.Store.MailEnabled = true
	verify(t, a, alice)
	mail := &fakeMailSender{}
	worker := mailWorker(a, mail)
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "等待回覆", Category: "other", Priority: "low"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	sendDue(t, a, worker) // the confirmation
	mail.sent = nil
	path := fmt.Sprintf("/tickets/%d", id)
	if err = a.Store.Reply(ctx, staff, id, true, "請提供更多資訊", false, nil); err != nil {
		t.Fatal(err)
	}
	sendDue(t, a, worker) // the reply
	ticket, _ := a.Store.Ticket(ctx, alice, id, false)
	if ticket.Status != "waiting" || ticket.AutoCloseAt == nil || time.Until(*ticket.AutoCloseAt) < 23*time.Hour {
		t.Fatalf("waiting ticket should close in 24 hours: %+v", ticket)
	}
	if page := request(h, "GET", path, "alice", nil).Body.String(); !strings.Contains(page, "若未收到回覆，客服單將於") {
		t.Fatal("customer not told when the ticket closes")
	}
	advance := func(wantReminded, wantClosed int) {
		t.Helper()
		reminded, closed, err := a.Store.AdvanceWaitingTickets(ctx)
		if err != nil || reminded != wantReminded || closed != wantClosed {
			t.Fatalf("advance: reminded %d closed %d, want %d %d: %v", reminded, closed, wantReminded, wantClosed, err)
		}
	}
	waitedFor := func(hours int) {
		t.Helper()
		if _, err := a.Store.Pool.Exec(ctx, `UPDATE tickets SET waiting_at=now()-$2*interval '1 hour' WHERE id=$1`, id, hours); err != nil {
			t.Fatal(err)
		}
	}
	waitedFor(11)
	advance(0, 0)
	waitedFor(13)
	advance(1, 0)
	advance(0, 0) // one reminder per wait
	sendDue(t, a, worker)
	mail.sent = mail.sent[1:]
	if len(mail.sent) != 1 || mail.sent[0].to != alice.Email || !strings.Contains(mail.sent[0].subject, "正在等待您的回覆") || !strings.Contains(mail.sent[0].body, "11 小時內沒有回覆") {
		t.Fatalf("reminder email: %+v", mail.sent)
	}
	// Another staff reply restarts the wait, reminder included.
	if err = a.Store.Reply(ctx, staff, id, true, "補充說明", false, nil); err != nil {
		t.Fatal(err)
	}
	sendDue(t, a, worker)
	advance(0, 0)
	waitedFor(13)
	advance(1, 0)
	// A reminder still queued when the customer answers is dropped.
	mail.sent = nil
	if err = a.Store.Reply(ctx, alice, id, false, "這是資訊", false, nil); err != nil {
		t.Fatal(err)
	}
	sendDue(t, a, worker)
	if len(mail.sent) != 0 {
		t.Fatalf("reminded a customer who already answered: %+v", mail.sent)
	}
	if ticket, _ = a.Store.Ticket(ctx, alice, id, false); ticket.Status != "new" || ticket.AutoCloseAt != nil {
		t.Fatalf("customer reply should return the ticket to the queue: %+v", ticket)
	}
	// Unanswered for the whole wait, the ticket closes by itself.
	if err = a.Store.Reply(ctx, staff, id, true, "請再確認", false, nil); err != nil {
		t.Fatal(err)
	}
	waitedFor(25)
	advance(0, 1)
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.Status != "closed" || ticket.AutoClosedAt == nil || ticket.LockAt == nil || ticket.Locked() || time.Until(*ticket.LockAt) < 71*time.Hour {
		t.Fatalf("closed ticket should lock in 72 hours: %+v", ticket)
	}
	page := request(h, "GET", path, "alice", nil).Body.String()
	if !strings.Contains(page, "此客服單因未收到回覆，已自動結案。") || !strings.Contains(page, "前重新開啟，之後將鎖定。") || !strings.Contains(page, "重新開啟客服單") {
		t.Fatal("closed ticket should explain the reopening window")
	}
	// Within the window the customer can reopen it, back into the queue.
	if got := request(h, "POST", path+"/update", "alice", url.Values{"csrf": {"csrf-alice"}, "status": {"new"}}); got.Code != http.StatusSeeOther {
		t.Fatalf("reopen: %d", got.Code)
	}
	if ticket, _ = a.Store.Ticket(ctx, alice, id, false); ticket.Status != "new" {
		t.Fatalf("reopened: %+v", ticket)
	}
	// Once locked, nobody reopens or replies in public; staff may still note.
	if got := request(h, "POST", path+"/update", "alice", url.Values{"csrf": {"csrf-alice"}, "status": {"closed"}}); got.Code != http.StatusSeeOther {
		t.Fatalf("close: %d", got.Code)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE tickets SET closed_at=now()-interval '73 hours' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if got := request(h, "POST", path+"/update", "alice", url.Values{"csrf": {"csrf-alice"}, "status": {"new"}}); got.Code != http.StatusConflict {
		t.Fatalf("locked ticket reopened: %d", got.Code)
	}
	if got := request(h, "POST", path+"/reply", "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"再問一次"}}); got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "已鎖定") {
		t.Fatalf("customer replied to a locked ticket: %d", got.Code)
	}
	staffPath := fmt.Sprintf("/admin/tickets/%d", id)
	if got := request(h, "POST", staffPath+"/reply", "staff", url.Values{"csrf": {"csrf-staff"}, "body": {"公開回覆"}}); got.Code != http.StatusConflict {
		t.Fatalf("staff replied in public to a locked ticket: %d", got.Code)
	}
	if got := request(h, "POST", staffPath+"/update", "staff", url.Values{"csrf": {"csrf-staff"}, "status": {"open"}, "priority": {"low"}}); got.Code == http.StatusSeeOther {
		t.Fatal("staff reopened a locked ticket")
	}
	if got := request(h, "POST", staffPath+"/reply", "staff", url.Values{"csrf": {"csrf-staff"}, "body": {"內部紀錄"}, "internal": {"true"}}); got.Code != http.StatusSeeOther {
		t.Fatalf("internal note on a locked ticket: %d", got.Code)
	}
	page = request(h, "GET", path, "alice", nil).Body.String()
	if !strings.Contains(page, "此客服單已鎖定") || strings.Contains(page, "重新開啟客服單") || !strings.Contains(page, fmt.Sprintf(`href="/tickets/new?from=%d"`, id)) {
		t.Fatal("locked ticket should point to a new ticket instead of reopening")
	}
	if form := request(h, "GET", fmt.Sprintf("/tickets/new?from=%d", id), "alice", nil).Body.String(); !strings.Contains(form, fmt.Sprintf("延續客服單 #%d", id)) {
		t.Fatal("new ticket should start by mentioning the locked one")
	}
	if staffPage := request(h, "GET", staffPath, "staff", nil).Body.String(); !strings.Contains(staffPage, "只能新增內部備註") || !strings.Contains(staffPage, `name="internal" value="true"`) {
		t.Fatal("staff form on a locked ticket should only add notes")
	}
}

func TestLifecycleSettingsCanTurnStepsOff(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	if l, err := a.Store.Lifecycle(ctx); err != nil || l != (model.Lifecycle{RemindHours: 12, CloseHours: 24, LockHours: 72}) {
		t.Fatalf("defaults: %+v %v", l, err)
	}
	page := request(h, "GET", "/admin/automation", "admin", nil).Body.String()
	form := url.Values{"csrf": {"csrf-admin"}, "timezone": {"Asia/Taipei"}, "auto_close_days": {"7"}, "waiting_remind_hours": {"0"}, "waiting_close_hours": {"0"}, "closed_lock_hours": {"0"}}
	if !strings.Contains(page, `name="waiting_remind_hours"`) || request(h, "POST", "/admin/automation", "admin", form).Code != http.StatusSeeOther {
		t.Fatal("lifecycle settings not saved")
	}
	form.Set("waiting_remind_hours", "24")
	form.Set("waiting_close_hours", "12")
	if got := request(h, "POST", "/admin/automation", "admin", form); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a reminder after closing was accepted: %d", got.Code)
	}
	id, _ := a.Store.Create(ctx, alice, model.Ticket{Subject: "停用", Category: "other", Priority: "low"}, "問題", nil)
	a.Store.Reply(ctx, staff, id, true, "回覆", false, nil)
	a.Store.Pool.Exec(ctx, `UPDATE tickets SET waiting_at=now()-interval '30 days' WHERE id=$1`, id)
	if reminded, closed, err := a.Store.AdvanceWaitingTickets(ctx); err != nil || reminded != 0 || closed != 0 {
		t.Fatalf("switched-off steps ran: %d %d %v", reminded, closed, err)
	}
	a.Store.Update(ctx, alice, id, false, "closed", "low", nil)
	a.Store.Pool.Exec(ctx, `UPDATE tickets SET closed_at=now()-interval '30 days' WHERE id=$1`, id)
	if ticket, _ := a.Store.Ticket(ctx, alice, id, false); ticket.Locked() || ticket.LockAt != nil {
		t.Fatal("locked although locking is off")
	}
}
