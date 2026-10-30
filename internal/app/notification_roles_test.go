package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"supportcenter/internal/model"
)

func TestStatusNotificationsOnlyGoToTicketParticipants(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	ctx := context.Background()
	settings, _ := a.Store.Automation(ctx)
	settings.AutoCloseDays = 1
	if err := a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	admin, err := a.Store.User(ctx, "admin", "Admin", "admin@example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "customer notices only", Category: "other", Priority: "low"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=ANY($1::bigint[])`, []int64{bob.ID, staff.ID}); err != nil {
		t.Fatal(err)
	}
	for _, person := range []model.User{bob, admin} {
		if err = a.Store.Invite(ctx, alice, id, person.Email); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range []string{"solved", "closed"} {
		if err = a.Store.Update(ctx, staff, id, true, status, "low", &staff.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.Store.Update(ctx, alice, id, false, "new", "", nil); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "low", &staff.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE tickets SET solved_at=now()-interval '2 days' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if count, err := a.Store.CloseSolvedTickets(ctx); err != nil || count != 1 {
		t.Fatalf("auto closure: %d %v", count, err)
	}
	for _, person := range []model.User{alice, bob, staff, admin} {
		var count int
		if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM user_notifications WHERE user_id=$1`, person.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		want := 0
		if person.ID != staff.ID {
			want = 4
		}
		if count != want {
			t.Fatalf("%s received %d notices, want %d", person.Role, count, want)
		}
	}
	// Assignment alone gives no customer notifications or access to theirs.
	items, _, err := a.Store.Notifications(ctx, alice.ID, 1)
	if err != nil || len(items) != 4 {
		t.Fatalf("owner notification history: %+v %v", items, err)
	}
	if page := request(h, "GET", "/notifications", "staff", nil); page.Code != 200 || strings.Contains(page.Body.String(), "customer notices only") {
		t.Fatal("assigned staff can see another customer's notification")
	}
	if page := request(h, "POST", fmt.Sprintf("/notifications/%d/read", items[0].ID), "staff", url.Values{"csrf": {"csrf-staff"}}); page.Code != 404 {
		t.Fatalf("assigned staff can read another customer's notice: %d", page.Code)
	}
}

func TestStaffRoleKeepsNotificationsForOwnTickets(t *testing.T) {
	for _, role := range []string{"support", "admin"} {
		t.Run(role, func(t *testing.T) {
			a, h, alice, _, staff := testApp(t)
			ctx := context.Background()
			settings, _ := a.Store.Automation(ctx)
			settings.AutoCloseDays = 1
			if err := a.Store.SaveAutomation(ctx, settings); err != nil {
				t.Fatal(err)
			}
			id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "promoted ticket owner", Category: "other", Priority: "low"}, "question", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = a.Store.Update(ctx, staff, id, true, "solved", "low", nil); err != nil {
				t.Fatal(err)
			}
			items, _, err := a.Store.Notifications(ctx, alice.ID, 1)
			if err != nil || len(items) != 1 {
				t.Fatalf("customer notification missing: %+v %v", items, err)
			}
			noticeID := items[0].ID
			if err = a.Store.SetRole(ctx, "alice", role); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/tickets", "/admin/tickets", "/account"} {
				page := request(h, "GET", path, "alice", nil)
				if page.Code != 200 || !strings.Contains(page.Body.String(), `class="icon-button notification-link"`) {
					t.Fatalf("personal notification bell missing on %s: %d", path, page.Code)
				}
			}
			if page := request(h, "GET", "/notifications", "alice", nil); page.Code != 200 || !strings.Contains(page.Body.String(), "promoted ticket owner") {
				t.Fatalf("personal notification page missing: %d", page.Code)
			}
			if count, err := a.Store.UnreadNotifications(ctx, alice.ID); err != nil || count != 1 {
				t.Fatalf("personal unread count=%d err=%v", count, err)
			}
			if items, _, err := a.Store.Notifications(ctx, alice.ID, 1); err != nil || len(items) != 1 || items[0].ID != noticeID {
				t.Fatal("promotion lost personal notification history")
			}
			page := request(h, "POST", fmt.Sprintf("/notifications/%d/read", noticeID), "alice", url.Values{"csrf": {"csrf-alice"}})
			if page.Code != 303 || page.Header().Get("Location") != fmt.Sprintf("/tickets/%d", id) {
				t.Fatal("personal notification did not open the customer ticket view")
			}
			if page := request(h, "POST", "/notifications/read", "alice", url.Values{"csrf": {"csrf-alice"}}); page.Code != 303 {
				t.Fatalf("personal read-all failed: %d", page.Code)
			}
			if err = a.Store.Update(ctx, staff, id, true, "closed", "low", nil); err != nil {
				t.Fatal(err)
			}
			var count, read int
			if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*),count(read_at) FROM user_notifications WHERE user_id=$1`, alice.ID).Scan(&count, &read); err != nil || count != 2 || read != 1 {
				t.Fatalf("personal closure notice or historical read state lost: %d %d %v", count, read, err)
			}
		})
	}
}
