package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"supportcenter/internal/database"
	"supportcenter/internal/model"
)

func TestCreateWithEmailParticipantsAndPendingRegistration(t *testing.T) {
	a, h, alice, bob, _ := testApp(t)
	ctx := context.Background()
	if _, err := a.Store.Pool.Exec(ctx, `UPDATE users SET name='Bob Example',email_verified=true WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf": {"csrf-alice"}, "submission": {token()}, "subject": {"participants at creation"}, "category": {"other"}, "priority": {"normal"}, "body": {"private question"}, "participants": {" BOB@Example.Test, future@example.test, bob@example.test "}}
	created := request(h, "POST", "/tickets", "alice", form)
	if created.Code != 303 {
		t.Fatalf("create with participants: %d %s", created.Code, created.Body.String())
	}
	path := created.Header().Get("Location")
	var id int64
	if _, err := fmt.Sscanf(path, "/tickets/%d", &id); err != nil {
		t.Fatal(err)
	}
	participants, err := a.Store.Invites(ctx, alice, id)
	if err != nil || len(participants) != 2 || participants[0].UserID != bob.ID || participants[0].Name != "Bob Example" || participants[1].UserID != 0 {
		t.Fatalf("registered/pending participants: %+v %v", participants, err)
	}
	ownerPage := request(h, "GET", path, "alice", nil).Body.String()
	if !strings.Contains(ownerPage, "<strong>Bob Example</strong>") || strings.Contains(ownerPage, "<strong>bob@example.test</strong>") || strings.Contains(ownerPage, "等待加入") || !strings.Contains(ownerPage, "future@example.test") {
		t.Fatal("participant display must use verified account names and pending email invitations")
	}
	memberPage := request(h, "GET", path, "bob", nil)
	if memberPage.Code != 200 || !strings.Contains(memberPage.Body.String(), "退出客服單") || strings.Contains(memberPage.Body.String(), `action="`+path+`/invite"`) {
		t.Fatal("invited member must see their own exit action, without owner invite controls")
	}
	// Normalized participant lists are part of the retry fingerprint.
	form.Set("participants", "future@example.test,bob@example.test")
	if replay := request(h, "POST", "/tickets", "alice", form); replay.Code != 303 || replay.Header().Get("Location") != path {
		t.Fatalf("participant retry duplicated ticket: %d", replay.Code)
	}
	form.Set("participants", "different@example.test")
	if changed := request(h, "POST", "/tickets", "alice", form); changed.Code != 409 {
		t.Fatalf("changed invitees reused a submission key: %d", changed.Code)
	}
	future, err := a.Store.User(ctx, "future", "Future Colleague", "future@example.test", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Ticket(ctx, future, id, false); !errors.Is(err, database.ErrNotFound) {
		t.Fatal("unverified email gained pending invitation access")
	}
	future, err = a.Store.User(ctx, "future", "Future Colleague", "future@example.test", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Ticket(ctx, future, id, false); err != nil {
		t.Fatal("verified pending invite did not grant access", err)
	}
	if body := request(h, "GET", path, "alice", nil).Body.String(); !strings.Contains(body, "<strong>Future Colleague</strong>") || strings.Contains(body, "<strong>future@example.test</strong>") {
		t.Fatal("pending email did not become the account name")
	}
	// Creation does not remove the ability to invite another person later.
	if later := request(h, "POST", path+"/invite", "alice", url.Values{"csrf": {"csrf-alice"}, "email": {"later@example.test"}}); later.Code != 303 {
		t.Fatalf("later invitation rejected: %d", later.Code)
	}
}

func TestInvalidOrFailedCreationDoesNotLeavePartialInvitations(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	ctx := context.Background()
	for _, participants := range []string{"invalid-email", alice.Email, strings.Repeat("a", 5121), strings.Join(makeParticipantEmails(21), ",")} {
		form := url.Values{"csrf": {"csrf-alice"}, "subject": {"invalid participants"}, "category": {"other"}, "priority": {"normal"}, "body": {"question"}, "participants": {participants}}
		if page := request(h, "POST", "/tickets", "alice", form); page.Code != 422 || !strings.Contains(page.Body.String(), `name="participants"`) {
			t.Fatalf("invalid participants accepted or form discarded: %d", page.Code)
		}
	}
	if _, err := a.Store.Pool.Exec(ctx, `CREATE FUNCTION reject_invitation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.email_key='blocked@example.test' THEN RAISE EXCEPTION 'test invite failure'; END IF; RETURN NEW; END; $$;
 CREATE TRIGGER reject_invitation BEFORE INSERT ON ticket_invites FOR EACH ROW EXECUTE FUNCTION reject_invitation()`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.CreateWithParticipants(ctx, alice, model.Ticket{Subject: "atomic creation", Category: "other", Priority: "normal"}, "question", nil, []string{"a@example.test", "blocked@example.test"}); err == nil {
		t.Fatal("invitation failure was ignored")
	}
	var tickets, invites, events int
	if err := a.Store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tickets),(SELECT count(*) FROM ticket_invites),(SELECT count(*) FROM ticket_events)`).Scan(&tickets, &invites, &events); err != nil || tickets != 0 || invites != 0 || events != 0 {
		t.Fatalf("partial creation survived: %d %d %d %v", tickets, invites, events, err)
	}
}

func TestDirectParticipationIsBoundToInvitedAccount(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	ctx := context.Background()
	if _, err := a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.CreateWithParticipants(ctx, alice, model.Ticket{Subject: "direct membership", Category: "other", Priority: "normal"}, "question", nil, []string{bob.Email, "unknown@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/tickets/%d", id)
	if page := request(h, "GET", path, "bob", nil); page.Code != 200 || !strings.Contains(page.Body.String(), "退出客服單") {
		t.Fatal("existing account must join without accepting or verifying again")
	}
	participants, err := a.Store.Invites(ctx, alice, id)
	if err != nil || participants[0].UserID != bob.ID || participants[0].Name != bob.Name {
		t.Fatalf("existing account name: %+v %v", participants, err)
	}
	// Another Auth0 identity with the same email cannot claim a bound invitation.
	other, err := a.Store.User(ctx, "different-subject", "Another account", bob.Email, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Ticket(ctx, other, id, false); !errors.Is(err, database.ErrNotFound) {
		t.Fatal("a different account claimed an existing participant's invitation")
	}
	// An unregistered address still needs a verified identity when it first logs in.
	unknown, err := a.Store.User(ctx, "unknown", "Unknown account", "unknown@example.test", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Ticket(ctx, unknown, id, false); !errors.Is(err, database.ErrNotFound) {
		t.Fatal("unverified new account claimed an invitation")
	}
	if err = a.Store.LeaveTicket(ctx, unknown, id); !errors.Is(err, database.ErrNotFound) {
		t.Fatal("unverified new account removed an invitation")
	}
	// Membership follows the invited account even if its email later changes.
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET email='changed@example.test' WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	if page := request(h, "GET", path, "bob", nil); page.Code != 200 {
		t.Fatal("bound membership disappeared after an email change")
	}
	settings, _ := a.Store.Automation(ctx)
	settings.AutoCloseDays = 1
	if err = a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "normal", nil); err != nil {
		t.Fatal(err)
	}
	if notices, _, err := a.Store.Notifications(ctx, bob.ID, 1); err != nil || len(notices) != 1 {
		t.Fatal("direct participant did not receive a personal notification", err)
	}
	if notices, _, err := a.Store.Notifications(ctx, other.ID, 1); err != nil || len(notices) != 0 {
		t.Fatal("notification leaked to another account", err)
	}
	if err = a.Store.LeaveTicket(ctx, bob, id); err != nil {
		t.Fatal("bound participant could not leave after email change", err)
	}
	if _, err = a.Store.Ticket(ctx, bob, id, false); !errors.Is(err, database.ErrNotFound) {
		t.Fatal("bound participant kept access after leaving")
	}
}

func TestInvitedStaffJoinImmediatelyAsCustomers(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	var verified bool
	if err := a.Store.Pool.QueryRow(ctx, `SELECT email_verified FROM users WHERE id=$1`, staff.ID).Scan(&verified); err != nil || verified {
		t.Fatal("expected a manually provisioned staff account without verified email")
	}
	id, err := a.Store.CreateWithParticipants(ctx, alice, model.Ticket{Subject: "staff customer", Category: "other", Priority: "normal"}, "question", nil, []string{staff.Email})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/tickets/%d", id)
	if page := request(h, "GET", path, "staff", nil); page.Code != 200 || !strings.Contains(page.Body.String(), "退出客服單") {
		t.Fatal("invited staff account did not join as a customer immediately")
	}
	participants, err := a.Store.Invites(ctx, alice, id)
	if err != nil || len(participants) != 1 || participants[0].UserID != staff.ID || participants[0].Name != staff.Name {
		t.Fatalf("staff participant name: %+v %v", participants, err)
	}
	other, err := a.Store.User(ctx, "staff-email-impostor", "Different account", staff.Email, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Store.Ticket(ctx, other, id, false); !errors.Is(err, database.ErrNotFound) {
		t.Fatal("unverified account impersonated an invited staff member")
	}
	settings, _ := a.Store.Automation(ctx)
	settings.AutoCloseDays = 1
	if err = a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "normal", nil); err != nil {
		t.Fatal(err)
	}
	if notices, _, err := a.Store.Notifications(ctx, staff.ID, 1); err != nil || len(notices) != 1 {
		t.Fatal("invited staff member did not receive a customer notification", err)
	}
	if page := request(h, "POST", path+"/leave", "staff", url.Values{"csrf": {"csrf-staff"}}); page.Code != 303 {
		t.Fatal("invited staff member could not leave")
	}
	if page := request(h, "GET", path, "staff", nil); page.Code != 404 {
		t.Fatal("staff customer access persisted after leaving")
	}
	if notices, _, err := a.Store.Notifications(ctx, staff.ID, 1); err != nil || len(notices) != 0 {
		t.Fatal("staff kept customer notifications after leaving", err)
	}
	if page := request(h, "GET", "/admin"+path, "staff", nil); page.Code != 200 {
		t.Fatal("leaving as a customer incorrectly removed staff management access")
	}
}

func TestParticipantExitRevokesAccessAndNotifications(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	ctx := context.Background()
	settings, _ := a.Store.Automation(ctx)
	settings.AutoCloseDays = 1
	if err := a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	file, err := a.Files.Create(key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("private"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	id, err := a.Store.CreateWithParticipants(ctx, alice, model.Ticket{Subject: "leave private discussion", Category: "other", Priority: "normal"}, "question", []model.Attachment{{Name: "private.txt", Key: key, MIME: "text/plain", Size: 7}}, []string{bob.Email})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/tickets/%d", id)
	if page := request(h, "POST", path+"/leave", "bob", url.Values{"csrf": {"csrf-bob"}}); page.Code != 404 {
		t.Fatal("unverified participant can leave another email's invitation")
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "solved", "normal", nil); err != nil {
		t.Fatal(err)
	}
	var attachmentID int64
	if err = a.Store.Pool.QueryRow(ctx, `SELECT a.id FROM ticket_attachments a JOIN ticket_messages m ON m.id=a.message_id WHERE m.ticket_id=$1`, id).Scan(&attachmentID); err != nil {
		t.Fatal(err)
	}
	attachment := fmt.Sprintf("/attachments/%d", attachmentID)
	if page := request(h, "GET", attachment, "bob", nil); page.Code != 200 {
		t.Fatal("direct participant cannot access attachment")
	}
	if page := request(h, "POST", path+"/leave", "bob", nil); page.Code != 403 {
		t.Fatal("exit bypassed CSRF")
	}
	for _, session := range []string{"alice", "staff"} {
		if page := request(h, "POST", path+"/leave", session, url.Values{"csrf": {"csrf-" + session}}); page.Code != 404 {
			t.Fatal("owner or nonparticipant can remove an invitation")
		}
	}
	page := request(h, "POST", path+"/leave", "bob", url.Values{"csrf": {"csrf-bob"}, "email": {alice.Email}})
	if page.Code != 303 || page.Header().Get("Location") != "/tickets" {
		t.Fatalf("exit failed: %d", page.Code)
	}
	for _, url := range []string{path, attachment} {
		if page := request(h, "GET", url, "bob", nil); page.Code != 404 {
			t.Fatalf("access after exit: %s %d", url, page.Code)
		}
	}
	if page := request(h, "POST", path+"/reply", "bob", url.Values{"csrf": {"csrf-bob"}, "body": {"unauthorized reply"}}); page.Code != 404 {
		t.Fatal("exited participant can still reply")
	}
	if _, err = a.Store.Invites(ctx, bob, id); !errors.Is(err, database.ErrNotFound) {
		t.Fatal("exited participant can read membership list")
	}
	if items, _, err := a.Store.Notifications(ctx, bob.ID, 1); err != nil || len(items) != 0 {
		t.Fatal("exited participant still sees ticket notices")
	}
	if count, err := a.Store.UnreadNotifications(ctx, bob.ID); err != nil || count != 0 {
		t.Fatal("exited participant still has unread ticket notices")
	}
	var exits int
	if err = a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM ticket_events WHERE ticket_id=$1 AND user_id=$2 AND kind='leave'`, id, bob.ID).Scan(&exits); err != nil || exits != 1 {
		t.Fatal("participant exit was not audited")
	}
	if page := request(h, "GET", path, "alice", nil); page.Code != 200 {
		t.Fatal("participant exit removed the ticket owner")
	}
}

func makeParticipantEmails(count int) []string {
	emails := make([]string, count)
	for i := range emails {
		emails[i] = fmt.Sprintf("participant%d@example.test", i)
	}
	return emails
}
