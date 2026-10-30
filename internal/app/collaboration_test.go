package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"supportcenter/internal/model"
)

func TestEmailInvitationUsesVerifiedIdentity(t *testing.T) {
	a, h, alice, bob, _ := testApp(t)
	ctx := context.Background()
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "private invite", Category: "other", Priority: "low"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/tickets/%d", id)
	if got := request(h, "POST", path+"/invite", "bob", url.Values{"csrf": {"csrf-bob"}, "email": {"friend@example.test"}}); got.Code != 404 {
		t.Fatalf("non-owner invite: %d", got.Code)
	}
	if got := request(h, "POST", path+"/invite", "alice", url.Values{"csrf": {"csrf-alice"}, "email": {"not an email"}}); got.Code != 422 {
		t.Fatalf("invalid email: %d", got.Code)
	}
	if got := request(h, "POST", path+"/invite", "alice", url.Values{"csrf": {"csrf-alice"}, "email": {" BOB@Example.Test "}}); got.Code != 303 {
		t.Fatalf("invite: %d", got.Code)
	}
	if got := request(h, "GET", path, "bob", nil); got.Code != 404 {
		t.Fatalf("unverified invite access: %d", got.Code)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	if got := request(h, "GET", path, "bob", nil); got.Code != 200 {
		t.Fatalf("direct invite access: %d", got.Code)
	}
	if got := request(h, "GET", "/tickets", "bob", nil); got.Code != 200 || !strings.Contains(got.Body.String(), "private invite") {
		t.Fatalf("invited ticket list: %d", got.Code)
	}
	if got := request(h, "POST", path+"/reply", "bob", url.Values{"csrf": {"csrf-bob"}, "body": {"joining discussion"}}); got.Code != 303 {
		t.Fatalf("participant reply: %d", got.Code)
	}
	if got := request(h, "POST", path+"/update", "bob", url.Values{"csrf": {"csrf-bob"}, "status": {"closed"}}); got.Code != 404 {
		t.Fatalf("participant changed status: %d", got.Code)
	}
	if got := request(h, "POST", path+"/uninvite", "alice", url.Values{"csrf": {"csrf-alice"}, "email": {"bob@example.test"}}); got.Code != 303 {
		t.Fatalf("remove invite: %d", got.Code)
	}
	if got := request(h, "GET", path, "bob", nil); got.Code != 404 {
		t.Fatalf("revoked invite access: %d", got.Code)
	}
}

func TestMultipleStaffAndReopenQueue(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	admin, err := a.Store.User(ctx, "admin", "admin", "admin@example.test")
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "team", Category: "technical", Priority: "low"}, "question", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/admin/tickets/%d", id)
	form := url.Values{"csrf": {"csrf-staff"}, "status": {"new"}, "priority": {"low"}, "assignees": {fmt.Sprint(admin.ID)}, "assignee_action": {"add_self"}}
	if got := request(h, "POST", path+"/update", "staff", form); got.Code != 303 {
		t.Fatalf("team save: %d %s", got.Code, got.Body.String())
	}
	ticket, err := a.Store.Ticket(ctx, staff, id, true)
	if err != nil || ticket.Status != "open" || len(ticket.Team) != 2 {
		t.Fatalf("team: %+v %v", ticket, err)
	}
	list, _, _, err := a.Store.List(ctx, staff, true, "", "", "", 1, "mine")
	if err != nil || len(list) != 1 || len(list[0].Team) != 2 {
		t.Fatalf("mine: %+v %v", list, err)
	}
	form = url.Values{"csrf": {"csrf-staff"}, "status": {"open"}, "priority": {"low"}, "assignees": {"self", fmt.Sprint(admin.ID)}}
	if got := request(h, "POST", path+"/update", "staff", form); got.Code != 303 {
		t.Fatalf("save after self assignment: %d", got.Code)
	}
	form.Set("assignee_action", "remove_self")
	if got := request(h, "POST", path+"/update", "staff", form); got.Code != 303 {
		t.Fatalf("remove self: %d", got.Code)
	}
	ticket, err = a.Store.Ticket(ctx, staff, id, true)
	if err != nil || len(ticket.Team) != 1 || ticket.Team[0].ID != admin.ID {
		t.Fatalf("other staff was not preserved: %+v %v", ticket.Team, err)
	}
	form = url.Values{"csrf": {"csrf-staff"}, "status": {"open"}, "priority": {"low"}, "assignees": {fmt.Sprint(admin.ID)}, "assignee_action": {"add_self"}}
	if got := request(h, "POST", path+"/update", "staff", form); got.Code != 303 {
		t.Fatalf("reassign self: %d", got.Code)
	}
	ticket, err = a.Store.Ticket(ctx, staff, id, true)
	if err != nil || len(ticket.Team) != 2 {
		t.Fatalf("self reassign: %+v %v", ticket.Team, err)
	}
	if got := request(h, "GET", fmt.Sprintf("/admin/tickets/%d/people/%d/avatar", id, admin.ID), "alice", nil); got.Code != 404 {
		t.Fatalf("avatar access: %d", got.Code)
	}
	if got := request(h, "GET", fmt.Sprintf("/admin/tickets/%d/people/%d/avatar", id, admin.ID), "staff", nil); got.Code != 200 {
		t.Fatalf("staff avatar: %d", got.Code)
	}
	if err = a.Store.UpdateTeam(ctx, staff, id, true, "closed", "low", []int64{staff.ID, admin.ID}, ticket.Version); err != nil {
		t.Fatal(err)
	}
	newerID, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "newer", Category: "other", Priority: "urgent"}, "another question", nil)
	if err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if err = a.Store.UpdateTeam(ctx, alice, id, false, "new", "low", nil, ticket.Version); err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.Status != "new" || len(ticket.Team) != 0 || ticket.AssigneeID != nil {
		t.Fatalf("reopen: %+v", ticket)
	}
	if list, _, _, err = a.Store.List(ctx, staff, true, "", "", "", 1, "", "updated_desc"); err != nil || len(list) != 2 || list[0].ID != id {
		t.Fatalf("reopened ticket should lead recent updates: %+v %v", list, err)
	}
	if list, _, _, err = a.Store.List(ctx, staff, true, "", "", "", 1, "", "created_desc"); err != nil || len(list) != 2 || list[0].ID != newerID {
		t.Fatalf("creation sorting: %+v %v", list, err)
	}
	if list, _, _, err = a.Store.List(ctx, staff, true, "", "urgent", "", 1, "", "updated_desc"); err != nil || len(list) != 1 || list[0].ID != newerID {
		t.Fatalf("priority filter with sorting: %+v %v", list, err)
	}
}
