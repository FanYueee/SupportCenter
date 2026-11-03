package app

import (
	"context"
	"testing"

	"supportcenter/internal/model"
)

func TestDetailedTicketStatusFlow(t *testing.T) {
	a, _, alice, _, staff := testApp(t)
	ctx := context.Background()
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "status flow", Category: "other", Priority: "low"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := a.Store.Ticket(ctx, alice, id, false)
	if err != nil || ticket.Status != "new" {
		t.Fatalf("new ticket: %+v %v", ticket, err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "new", "low", &staff.ID, ticket.Version); err != nil {
		t.Fatal(err)
	}
	// Assigning someone does not start work; only staff choose "open".
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.Status != "new" {
		t.Fatalf("assignment changed the status: %s", ticket.Status)
	}
	if err = a.Store.Update(ctx, staff, id, true, "open", "low", &staff.ID, ticket.Version); err != nil {
		t.Fatal(err)
	}
	if ticket, _ = a.Store.Ticket(ctx, alice, id, false); ticket.Status != "open" {
		t.Fatalf("staff could not start work: %s", ticket.Status)
	}
	if err = a.Store.Update(ctx, staff, id, true, "on_hold", "low", &staff.ID, ticket.Version); err != nil {
		t.Fatal(err)
	}
	_, counts, _, err := a.Store.List(ctx, alice, false, "open", "", "", 1, "")
	if err != nil || counts.Hold != 1 || counts.Open != 1 {
		t.Fatalf("customer hold count: %+v %v", counts, err)
	}
	if model.StatusForViewer("on_hold", false) != "處理中" {
		t.Fatal("private hold status leaked")
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if err = a.Store.Update(ctx, staff, id, true, "solved", "low", &staff.ID, ticket.Version); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Reply(ctx, alice, id, false, "still need help", false, nil); err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.Status != "new" {
		t.Fatalf("reply did not return the solved ticket to the queue: %s", ticket.Status)
	}
	if err = a.Store.Update(ctx, staff, id, true, "open", "low", &staff.ID, ticket.Version); err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if err = a.Store.Update(ctx, staff, id, true, "new", "low", &staff.ID, ticket.Version); err == nil {
		t.Fatal("staff moved a ticket in progress back to new")
	}
}
