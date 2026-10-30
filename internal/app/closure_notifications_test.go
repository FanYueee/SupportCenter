package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"supportcenter/internal/model"
)

func TestManualClosurePreservesResolvedNotice(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	ctx := context.Background()
	settings, _ := a.Store.Automation(ctx)
	settings.AutoCloseDays = 2
	if err := a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "manual closure", Category: "other", Priority: "low"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Invite(ctx, alice, id, bob.Email); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "low", nil); err != nil {
		t.Fatal(err)
	}
	before, _, err := a.Store.Notifications(ctx, alice.ID, 1)
	if err != nil || len(before) != 1 {
		t.Fatalf("resolved notices: %+v %v", before, err)
	}
	if _, err = a.Store.ReadNotification(ctx, alice.ID, before[0].ID); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/admin/tickets/%d/update", id)
	if w := request(h, "POST", path, "staff", url.Values{"csrf": {"csrf-staff"}, "status": {"closed"}, "priority": {"low"}}); w.Code != 303 {
		t.Fatalf("close: %d", w.Code)
	}
	for _, viewer := range []model.User{alice, bob} {
		items, more, err := a.Store.Notifications(ctx, viewer.ID, 1)
		if err != nil || more || len(items) != 2 || items[0].Kind != "closed" || items[1].Kind != "solved" || items[0].ReadAt != nil || items[1].AutoCloseAt != nil {
			t.Fatalf("closure history for %d: %+v %v", viewer.ID, items, err)
		}
		if viewer.ID == alice.ID && (items[1].ID != before[0].ID || items[1].ReadAt == nil) {
			t.Fatal("original resolved notice/read state lost")
		}
	}
	body := request(h, "GET", "/notifications", "alice", nil).Body.String()
	if !strings.Contains(body, "客服單已結案") || !strings.Contains(body, "客服單已解決") || strings.Contains(body, "預計結案") {
		t.Fatal("notification page does not show both historical events")
	}
	// Repeated saves and priority edits do not send another closure notice.
	if err = a.Store.Update(ctx, staff, id, true, "closed", "urgent", nil); err != nil {
		t.Fatal(err)
	}
	items, _, _ := a.Store.Notifications(ctx, alice.ID, 1)
	if len(items) != 2 {
		t.Fatal("metadata edit duplicated closure notification")
	}
	if err = a.Store.Uninvite(ctx, alice, id, bob.Email); err != nil {
		t.Fatal(err)
	}
	if items, _, err := a.Store.Notifications(ctx, bob.ID, 1); err != nil || len(items) != 0 {
		t.Fatal("revoked recipient can still read closure notifications")
	}
	// Owner-initiated closure still sends a notice when automatic closure is off.
	settings.AutoCloseDays = 0
	if err = a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, alice, id, false, "new", "", nil); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, alice, id, false, "closed", "", nil); err != nil {
		t.Fatal(err)
	}
	items, _, _ = a.Store.Notifications(ctx, alice.ID, 1)
	if len(items) != 3 || items[0].Kind != "closed" || items[0].ID == items[1].ID {
		t.Fatal("a later closure cycle did not create its own notification")
	}
}

func TestMigrationRepairsMissingClosureNotice(t *testing.T) {
	a, _, alice, bob, staff := testApp(t)
	ctx := context.Background()
	settings, _ := a.Store.Automation(ctx)
	settings.AutoCloseDays = 2
	if err := a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "repair missing closure", Category: "other", Priority: "low"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Invite(ctx, alice, id, bob.Email); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "low", nil); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.ReadAllNotifications(ctx, alice.ID); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "closed", "low", nil); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Uninvite(ctx, alice, id, bob.Email); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the missing notices in this test's isolated schema only.
	if _, err = a.Store.Pool.Exec(ctx, `DELETE FROM user_notifications WHERE kind='closed'`); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Pool.Exec(ctx, `DELETE FROM schema_migrations WHERE name='021_manual_closure_notifications.sql'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = a.Store.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	items, _, err := a.Store.Notifications(ctx, alice.ID, 1)
	if err != nil || len(items) != 2 || items[0].Kind != "closed" || items[0].ReadAt != nil || items[1].Kind != "solved" || items[1].ReadAt == nil {
		t.Fatalf("repaired history: %+v %v", items, err)
	}
	var revokedNotices int
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM user_notifications WHERE user_id=$1 AND kind='closed'`, bob.ID).Scan(&revokedNotices); err != nil || revokedNotices != 0 {
		t.Fatal("migration sent closure notification to revoked recipient")
	}
}
