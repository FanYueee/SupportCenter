package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"supportcenter/internal/database"
	"supportcenter/internal/model"
	"sync"
	"testing"
	"time"
)

func TestAutoCloseSettingsAndNotifications(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	ctx := context.Background()
	form := url.Values{"csrf": {"csrf-staff"}, "timezone": {"UTC"}, "auto_close_days": {"2"}}
	if w := request(h, "POST", "/admin/automation", "staff", form); w.Code != 404 {
		t.Fatalf("staff configured rule: %d", w.Code)
	}
	form.Set("csrf", "csrf-admin")
	if w := request(h, "POST", "/admin/automation", "admin", url.Values{"timezone": {"UTC"}, "auto_close_days": {"2"}}); w.Code != 403 {
		t.Fatalf("missing CSRF: %d", w.Code)
	}
	for _, value := range []string{"-1", "366", "1.5", "invalid"} {
		form.Set("auto_close_days", value)
		if w := request(h, "POST", "/admin/automation", "admin", form); w.Code != 422 {
			t.Fatalf("accepted days %q: %d", value, w.Code)
		}
	}
	form.Set("auto_close_days", "2")
	if w := request(h, "POST", "/admin/automation", "admin", form); w.Code != 303 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	settings, err := a.Store.Automation(ctx)
	if err != nil || settings.AutoCloseDays != 2 {
		t.Fatalf("persisted setting: %+v %v", settings, err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "<b>auto-close notification</b>", Category: "other", Priority: "normal"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "normal", nil); err != nil {
		t.Fatal(err)
	}
	ticket, err := a.Store.Ticket(ctx, alice, id, false)
	if err != nil || ticket.SolvedAt == nil || ticket.AutoCloseAt == nil || ticket.AutoCloseAt.Sub(*ticket.SolvedAt) != 48*time.Hour {
		t.Fatalf("resolution deadline: %+v %v", ticket, err)
	}
	// Priority, assignment, and internal notes must not postpone the deadline.
	anchor := *ticket.SolvedAt
	if err = a.Store.Reply(ctx, staff, id, true, "private note", true, nil); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "urgent", &staff.ID); err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if !ticket.SolvedAt.Equal(anchor) {
		t.Fatal("metadata/internal note reset the deadline")
	}
	if n, err := a.Store.CloseSolvedTickets(ctx); err != nil || n != 0 {
		t.Fatalf("closed too early: %d %v", n, err)
	}
	items, _, err := a.Store.Notifications(ctx, alice.ID, 1)
	if err != nil || len(items) != 1 || items[0].Kind != "solved" {
		t.Fatalf("resolved notice: %+v %v", items, err)
	}
	noticeID := items[0].ID
	page := request(h, "GET", "/notifications", "alice", nil)
	if page.Code != 200 || strings.Contains(page.Body.String(), "<b>auto-close") || !strings.Contains(page.Body.String(), "&lt;b&gt;auto-close") {
		t.Fatalf("notice subject escaping: %d", page.Code)
	}
	if page := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil); !strings.Contains(page.Body.String(), "預計結案") {
		t.Fatal("customer deadline absent")
	}
	if w := request(h, "POST", fmt.Sprintf("/notifications/%d/read", noticeID), "bob", url.Values{"csrf": {"csrf-bob"}}); w.Code != 404 {
		t.Fatalf("other user read notice: %d", w.Code)
	}
	if w := request(h, "POST", "/notifications/read", "alice", nil); w.Code != 403 {
		t.Fatalf("read-all missing CSRF: %d", w.Code)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE tickets SET solved_at=now()-interval '3 days' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	// Parallel workers close the ticket only once and commit notices with the state.
	start := make(chan struct{})
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			n, err := a.Store.CloseSolvedTickets(ctx)
			counts <- n
			failures <- err
		}()
	}
	close(start)
	wg.Wait()
	close(counts)
	close(failures)
	total := 0
	for n := range counts {
		total += n
	}
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if total != 1 {
		t.Fatalf("concurrent closures: %d", total)
	}
	closed, err := a.Store.Ticket(ctx, alice, id, false)
	if err != nil || closed.Status != "closed" || closed.AutoClosedAt == nil || closed.SolvedAt != nil || closed.AutoCloseAt != nil || closed.Version != ticket.Version+1 {
		t.Fatalf("closed ticket: %+v %v", closed, err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "open", "normal", nil, ticket.Version); !errors.Is(err, database.ErrConflict) {
		t.Fatalf("stale form overwrote closure: %v", err)
	}
	if n, err := a.Store.CloseSolvedTickets(ctx); err != nil || n != 0 {
		t.Fatalf("repeated closure: %d %v", n, err)
	}
	items, _, _ = a.Store.Notifications(ctx, alice.ID, 1)
	if len(items) != 2 || items[0].Kind != "auto_closed" {
		t.Fatalf("closed notification missing/duplicated: %+v", items)
	}
	messages, _, err := a.Store.Messages(ctx, alice, id, false, 1)
	if err != nil || len(messages) != 2 || messages[1].Source != "automation" {
		t.Fatalf("public closure message: %+v %v", messages, err)
	}
	w := request(h, "POST", fmt.Sprintf("/notifications/%d/read", items[0].ID), "alice", url.Values{"csrf": {"csrf-alice"}})
	if w.Code != 303 || w.Header().Get("Location") != fmt.Sprintf("/tickets/%d", id) {
		t.Fatalf("open notice: %d", w.Code)
	}
	if n, err := a.Store.UnreadNotifications(ctx, alice.ID); err != nil || n != 1 {
		t.Fatalf("unread after opening: %d %v", n, err)
	}
	if w := request(h, "POST", "/notifications/read", "alice", url.Values{"csrf": {"csrf-alice"}}); w.Code != 303 {
		t.Fatalf("read all: %d", w.Code)
	}
	if n, _ := a.Store.UnreadNotifications(ctx, alice.ID); n != 0 {
		t.Fatal("read-all left unread notices")
	}
	if n, _ := a.Store.UnreadNotifications(ctx, bob.ID); n != 0 {
		t.Fatal("unrelated customer received a notice")
	}
}

func TestAutoCloseCancellationAndInvitationAccess(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	ctx := context.Background()
	settings, _ := a.Store.Automation(ctx)
	settings.AutoCloseDays = 1
	if err := a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "shared closure", Category: "other", Priority: "normal"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Invite(ctx, alice, id, bob.Email); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "normal", nil); err != nil {
		t.Fatal(err)
	}
	notices, _, err := a.Store.Notifications(ctx, bob.ID, 1)
	if err != nil || len(notices) != 1 {
		t.Fatalf("invited user notice: %+v %v", notices, err)
	}
	if err = a.Store.Uninvite(ctx, alice, id, bob.Email); err != nil {
		t.Fatal(err)
	}
	if n, _ := a.Store.UnreadNotifications(ctx, bob.ID); n != 0 {
		t.Fatal("revoked invitation leaks unread ticket notice")
	}
	if items, _, _ := a.Store.Notifications(ctx, bob.ID, 1); len(items) != 0 {
		t.Fatal("revoked invitation leaks subject")
	}
	if w := request(h, "POST", fmt.Sprintf("/notifications/%d/read", notices[0].ID), "bob", url.Values{"csrf": {"csrf-bob"}}); w.Code != 404 {
		t.Fatalf("revoked notification can be opened: %d", w.Code)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE tickets SET solved_at=now()-interval '2 days' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Reply(ctx, alice, id, false, "need more help", false, nil); err != nil {
		t.Fatal(err)
	}
	reopened, _ := a.Store.Ticket(ctx, alice, id, false)
	if reopened.Status != "open" || reopened.SolvedAt != nil || reopened.AutoCloseAt != nil {
		t.Fatalf("reply kept countdown: %+v", reopened)
	}
	if n, err := a.Store.CloseSolvedTickets(ctx); err != nil || n != 0 {
		t.Fatalf("reopened ticket closed: %d %v", n, err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "normal", nil); err != nil {
		t.Fatal(err)
	}
	ticket, _ := a.Store.Ticket(ctx, alice, id, false)
	if ticket.SolvedAt == nil || time.Since(*ticket.SolvedAt) > time.Minute {
		t.Fatal("new resolution did not start a fresh grace period")
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE tickets SET solved_at=now()-interval '2 days' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	settings.AutoCloseDays = 0
	if err = a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.AutoCloseAt != nil {
		t.Fatal("disabled rule still displays a deadline")
	}
	if n, err := a.Store.CloseSolvedTickets(ctx); err != nil || n != 0 {
		t.Fatalf("disabled rule closed ticket: %d %v", n, err)
	}
}

func TestAutoCloseRollsBackWithNotificationFailure(t *testing.T) {
	a, _, alice, _, staff := testApp(t)
	ctx := context.Background()
	settings, _ := a.Store.Automation(ctx)
	settings.AutoCloseDays = 1
	if err := a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "atomic closure", Category: "other", Priority: "normal"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "normal", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE tickets SET solved_at=now()-interval '2 days' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	_, err = a.Store.Pool.Exec(ctx, `CREATE FUNCTION reject_close_notice() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.kind='auto_closed' THEN RAISE EXCEPTION 'test notification failure'; END IF; RETURN NEW; END; $$;
 CREATE TRIGGER reject_close_notice BEFORE INSERT ON user_notifications FOR EACH ROW EXECUTE FUNCTION reject_close_notice()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.CloseSolvedTickets(ctx); err == nil {
		t.Fatal("notification failure accepted")
	}
	ticket, _ := a.Store.Ticket(ctx, alice, id, false)
	messages, _, _ := a.Store.Messages(ctx, alice, id, false, 1)
	if ticket.Status != "solved" || ticket.AutoClosedAt != nil || len(messages) != 1 {
		t.Fatalf("partial closure committed: %+v %+v", ticket, messages)
	}
}

func TestAutoCloseEnablingExistingResolvedTickets(t *testing.T) {
	a, _, alice, _, staff := testApp(t)
	ctx := context.Background()
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "existing resolved ticket", Category: "other", Priority: "normal"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "normal", nil); err != nil {
		t.Fatal(err)
	}
	ticket, _ := a.Store.Ticket(ctx, alice, id, false)
	if ticket.SolvedAt == nil || ticket.AutoCloseAt != nil {
		t.Fatal("default rule should track resolution without scheduling closure")
	}
	if n, err := a.Store.UnreadNotifications(ctx, alice.ID); err != nil || n != 0 {
		t.Fatalf("disabled rule created notice: %d %v", n, err)
	}
	anchor := *ticket.SolvedAt
	settings, _ := a.Store.Automation(ctx)
	for _, days := range []int{7, 3, 3} {
		settings.AutoCloseDays = days
		if err = a.Store.SaveAutomation(ctx, settings); err != nil {
			t.Fatal(err)
		}
		ticket, _ = a.Store.Ticket(ctx, alice, id, false)
		if ticket.AutoCloseAt == nil || ticket.AutoCloseAt.Sub(anchor) != time.Duration(days)*24*time.Hour {
			t.Fatalf("new policy not applied to existing ticket: %+v", ticket)
		}
		items, _, err := a.Store.Notifications(ctx, alice.ID, 1)
		if err != nil || len(items) != 1 || items[0].AutoCloseAt == nil || !items[0].AutoCloseAt.Equal(*ticket.AutoCloseAt) {
			t.Fatalf("existing resolution notice missing/duplicated/outdated: %+v %v", items, err)
		}
	}
	if err = a.Store.Update(ctx, staff, id, true, "closed", "normal", nil); err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.AutoClosedAt != nil || ticket.AutoCloseAt != nil {
		t.Fatal("manual closure mislabeled as automatic")
	}
}
