package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"supportcenter/internal/ai"
	"supportcenter/internal/model"
	"supportcenter/internal/notify"
)

func verify(t *testing.T, a *App, users ...model.User) {
	t.Helper()
	for _, u := range users {
		if _, err := a.Store.Pool.Exec(context.Background(), `UPDATE users SET email_verified=true WHERE id=$1`, u.ID); err != nil {
			t.Fatal(err)
		}
	}
}

type queuedEmail struct {
	kind, to, state string
}

func customerQueue(t *testing.T, a *App, ticketID int64) []queuedEmail {
	t.Helper()
	rows, err := a.Store.Pool.Query(context.Background(), `SELECT kind,recipient_email,state FROM customer_emails WHERE ticket_id=$1 ORDER BY id`, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []queuedEmail
	for rows.Next() {
		var e queuedEmail
		rows.Scan(&e.kind, &e.to, &e.state)
		out = append(out, e)
	}
	return out
}

func pendingReplies(t *testing.T, a *App, ticketID int64) []string {
	var to []string
	for _, e := range customerQueue(t, a, ticketID) {
		if e.kind == "reply" && e.state == "pending" {
			to = append(to, e.to)
		}
	}
	return to
}

func mailWorker(a *App, sender notify.Sender) *notify.Worker {
	return &notify.Worker{Store: a.Store, Sender: sender, Origin: a.Origin, Site: a.Site, Log: a.Log}
}

// sendDue makes every queued email due and runs the worker until the queue is empty.
func sendDue(t *testing.T, a *App, worker *notify.Worker) {
	t.Helper()
	if _, err := a.Store.Pool.Exec(context.Background(), `UPDATE customer_emails SET available_at=now() WHERE state='pending'`); err != nil {
		t.Fatal(err)
	}
	for {
		processed, err := worker.ProcessCustomer(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !processed {
			return
		}
	}
}

func TestSSOSignInAdoptsThePortalAccountForTheSameEmail(t *testing.T) {
	a, _, _, _, _ := testApp(t)
	ctx := context.Background()
	portal, err := a.Store.PortalUser(ctx, "Portal Person", "Mixed@Example.test")
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, portal, model.Ticket{Subject: "from the portal", Category: "other", Priority: "low"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	// An unverified sign-in cannot claim the address.
	unverified, err := a.Store.User(ctx, "auth0|unverified", "Someone", "mixed@example.test", false)
	if err != nil || unverified.ID == portal.ID {
		t.Fatal("unverified sign-in took over the Portal account", err)
	}
	signedIn, err := a.Store.User(ctx, "auth0|person", "Person", "MIXED@example.test", true)
	if err != nil || signedIn.ID != portal.ID {
		t.Fatalf("verified sign-in did not adopt the Portal account: %d != %d %v", signedIn.ID, portal.ID, err)
	}
	if _, err = a.Store.Ticket(ctx, signedIn, id, false); err != nil {
		t.Fatal("adopted account cannot see its Portal ticket", err)
	}
	if again, _ := a.Store.User(ctx, "auth0|person", "Person", "mixed@example.test", true); again.ID != portal.ID {
		t.Fatal("next sign-in moved to another account")
	}
	if fromPortal, _ := a.Store.PortalUser(ctx, "Portal Person", "mixed@example.test"); fromPortal.ID != portal.ID {
		t.Fatal("Portal no longer maps to the shared account")
	}
	// Another sign-in method for the address joins the same account.
	if other, _ := a.Store.User(ctx, "google-oauth2|person", "Person", "mixed@example.test", true); other.ID != portal.ID {
		t.Fatal("a second sign-in method got an account of its own")
	}
}

func TestReplyEmailsReachRequesterAndParticipants(t *testing.T) {
	a, h, alice, bob, _ := testApp(t)
	ctx := context.Background()
	a.Store.MailEnabled = true
	verify(t, a, alice, bob)
	mail := &fakeMailSender{}
	a.Site.Name = "Example Cloud"
	worker := mailWorker(a, mail)
	id, err := a.Store.CreateWithParticipants(ctx, alice, model.Ticket{Subject: "連線逾時", Category: "technical", Priority: "low"}, "問題", nil, []string{bob.Email})
	if err != nil {
		t.Fatal(err)
	}
	sendDue(t, a, worker) // the invitation
	mail.sent = nil
	path := fmt.Sprintf("/tickets/%d", id)
	staffReply := func(body string, internal bool) {
		form := url.Values{"csrf": {"csrf-staff"}, "body": {body}}
		if internal {
			form.Set("internal", "true")
		}
		if res := request(h, "POST", "/admin"+path+"/reply", "staff", form); res.Code != http.StatusSeeOther {
			t.Fatal("staff reply failed", res.Code)
		}
	}
	staffReply("內部備註", true)
	request(h, "POST", path+"/reply", "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"customer follow-up"}})
	if to := pendingReplies(t, a, id); len(to) != 0 {
		t.Fatal("internal notes or customer replies queued emails", to)
	}
	staffReply("第一則回覆", false)
	staffReply("請試試 <b>重新連線</b>", false)
	if to := pendingReplies(t, a, id); strings.Join(to, ",") != alice.Email+","+bob.Email {
		t.Fatalf("each person should have one waiting email: %v", to)
	}
	// Bob turns emails off in his settings, which also drops the waiting one.
	if res := request(h, "POST", "/account/email", "bob", url.Values{"csrf": {"csrf-bob"}}); res.Code != http.StatusSeeOther {
		t.Fatal("email preference not saved", res.Code)
	}
	if to := pendingReplies(t, a, id); strings.Join(to, ",") != alice.Email {
		t.Fatalf("opted-out participant still queued: %v", to)
	}
	sendDue(t, a, worker)
	if len(mail.sent) != 1 {
		t.Fatalf("expected one email, got %+v", mail.sent)
	}
	m := mail.sent[0]
	link := fmt.Sprintf("%s/tickets/%d", a.Origin, id)
	if m.to != alice.Email || m.from != "Example Cloud 支援中心" || !strings.Contains(m.subject, fmt.Sprintf("#%04d", id)) || !strings.Contains(m.subject, "連線逾時") ||
		!strings.Contains(m.body, "請試試 <b>重新連線</b>") || !strings.Contains(m.body, link) || strings.Contains(m.body, "內部備註") {
		t.Fatalf("reply email: %+v", m)
	}
	if strings.Contains(m.html, "<b>重新連線</b>") || !strings.Contains(m.html, "&lt;b&gt;") || !strings.Contains(m.html, `href="`+link+`"`) {
		t.Fatal("reply text must be escaped in the HTML part")
	}
	// Someone who answered after the reply has seen it; no email.
	staffReply("第三則回覆", false)
	request(h, "POST", path+"/reply", "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"saw it"}})
	sendDue(t, a, worker)
	if len(mail.sent) != 1 {
		t.Fatal("emailed someone who already answered")
	}
	// Without the reply text the email only points to the ticket.
	if err = a.Store.SaveEmailSettings(ctx, model.EmailSettings{OnReply: true, OnInvite: true}); err != nil {
		t.Fatal(err)
	}
	staffReply("私密的回覆內容", false)
	sendDue(t, a, worker)
	if len(mail.sent) != 2 || strings.Contains(mail.sent[1].body, "私密的回覆內容") || strings.Contains(mail.sent[1].html, "私密的回覆內容") {
		t.Fatalf("reply text sent while disabled: %+v", mail.sent)
	}
	// Turning reply emails off stops them; so does a server without SMTP.
	a.Store.SaveEmailSettings(ctx, model.EmailSettings{OnInvite: true})
	staffReply("關閉後的回覆", false)
	a.Store.SaveEmailSettings(ctx, model.EmailSettings{OnReply: true, OnInvite: true})
	a.Store.MailEnabled = false
	staffReply("沒有 SMTP 的回覆", false)
	if to := pendingReplies(t, a, id); len(to) != 0 {
		t.Fatal("queued while switched off or without SMTP", to)
	}
}

func TestAIRepliesEmailTheRequester(t *testing.T) {
	a, _, alice, _, _ := testApp(t)
	ctx := context.Background()
	a.Store.MailEnabled = true
	verify(t, a, alice)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"請重新啟動路由器。"}}]}`)
	}))
	defer provider.Close()
	if _, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "AI", Category: "other", Priority: "low", AssistanceMode: "ai"}, "問題", nil); err != nil {
		t.Fatal(err)
	}
	if processed, err := (&ai.Worker{Store: a.Store, Key: "test-key", Endpoint: provider.URL, Client: provider.Client()}).ProcessOnce(ctx); err != nil || !processed {
		t.Fatal("AI reply failed", err)
	}
	mail := &fakeMailSender{}
	sendDue(t, a, mailWorker(a, mail))
	if len(mail.sent) != 1 || !strings.Contains(mail.sent[0].body, "AI 助理") || !strings.Contains(mail.sent[0].body, "請重新啟動路由器。") || mail.sent[0].from != "支援中心" {
		t.Fatalf("AI reply email: %+v", mail.sent)
	}
}

func TestInviteEmailsLookTheSameAndAreLimited(t *testing.T) {
	a, h, alice, bob, _ := testApp(t)
	ctx := context.Background()
	a.Store.MailEnabled = true
	verify(t, a, alice, bob)
	create := func(subject string) int64 {
		id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: subject, Category: "other", Priority: "low"}, "問題", nil)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	invite := func(id int64, email string) {
		if res := request(h, "POST", fmt.Sprintf("/tickets/%d/invite", id), "alice", url.Values{"csrf": {"csrf-alice"}, "email": {email}}); res.Code != http.StatusSeeOther {
			t.Fatal("invite failed", res.Code)
		}
	}
	if _, err := a.Store.Pool.Exec(ctx, `UPDATE users SET name='Bob Registered' WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	id := create("WIN A PRIZE at evil.example")
	invite(id, bob.Email)
	invite(id, "newcomer@example.test")
	mail := &fakeMailSender{}
	sendDue(t, a, mailWorker(a, mail))
	if len(mail.sent) != 2 {
		t.Fatalf("registered and unregistered invitees should both get one: %+v", mail.sent)
	}
	for _, m := range mail.sent {
		if !strings.Contains(m.body, alice.Email) || !strings.Contains(m.body, fmt.Sprintf("#%04d", id)) || strings.Contains(m.body+m.subject+m.html, "WIN A PRIZE") || strings.Contains(m.body+m.html, "Bob Registered") {
			t.Fatalf("invitation must name only the inviter and number: %+v", m)
		}
	}
	if strings.ReplaceAll(mail.sent[0].body, bob.Email, "X") != strings.ReplaceAll(mail.sent[1].body, "newcomer@example.test", "X") {
		t.Fatal("invitations differ between registered and unregistered addresses")
	}
	// Removing the invitation before it is sent drops the email.
	other := create("second")
	invite(other, "removed@example.test")
	request(h, "POST", fmt.Sprintf("/tickets/%d/uninvite", other), "alice", url.Values{"csrf": {"csrf-alice"}, "email": {"removed@example.test"}})
	if q := customerQueue(t, a, other); len(q) != 1 || q[0].state != "cancelled" {
		t.Fatalf("uninvited email still queued: %+v", q)
	}
	// An address gets at most three invitations a day, whoever sends them.
	for i := 0; i < 3; i++ {
		invite(create(fmt.Sprintf("flood %d", i)), "victim@example.test")
	}
	var victim int
	a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM customer_emails WHERE recipient_email='victim@example.test'`).Scan(&victim)
	if victim != 3 {
		t.Fatalf("per-address limit: %d", victim)
	}
	// One person sends at most 30 invitation emails a day.
	for i := 0; i < 2; i++ {
		if _, err := a.Store.CreateWithParticipants(ctx, alice, model.Ticket{Subject: "many", Category: "other", Priority: "low"}, "問題", nil, makeParticipantEmails(20)[:20]); err != nil {
			t.Fatal(err)
		}
	}
	var sentByAlice int
	a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM customer_emails WHERE kind='invite' AND actor_user_id=$1`, alice.ID).Scan(&sentByAlice)
	if sentByAlice != 30 {
		t.Fatalf("per-inviter limit: %d", sentByAlice)
	}
	// Unverified inviters cannot email strangers.
	if _, err := a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=false WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	bobTicket, err := a.Store.Create(ctx, bob, model.Ticket{Subject: "bob", Category: "other", Priority: "low"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Invite(ctx, bob, bobTicket, "stranger@example.test"); err != nil {
		t.Fatal(err)
	}
	if q := customerQueue(t, a, bobTicket); len(q) != 0 {
		t.Fatal("unverified inviter queued an email", q)
	}
}

func TestEmailSettingsAndTestMessage(t *testing.T) {
	a, h, _, _, _ := testApp(t)
	ctx := context.Background()
	if res := request(h, "GET", "/admin/email", "staff", nil); res.Code != http.StatusNotFound {
		t.Fatal("support staff opened email settings", res.Code)
	}
	if res := request(h, "POST", "/admin/email", "staff", url.Values{"csrf": {"csrf-staff"}}); res.Code != http.StatusNotFound {
		t.Fatal("support staff changed email settings", res.Code)
	}
	if page := request(h, "GET", "/admin/email", "admin", nil).Body.String(); !strings.Contains(page, "未設定 SMTP") || strings.Contains(page, `action="/admin/email/test"`) {
		t.Fatal("page must say SMTP is missing and hide the test button")
	}
	if res := request(h, "POST", "/admin/email/test", "admin", url.Values{"csrf": {"csrf-admin"}}); res.Code != http.StatusUnprocessableEntity {
		t.Fatal("test email without SMTP", res.Code)
	}
	if res := request(h, "POST", "/admin/email", "admin", url.Values{"csrf": {"csrf-admin"}, "on_invite": {"on"}}); res.Code != http.StatusSeeOther {
		t.Fatal("save failed", res.Code)
	}
	if s, _ := a.Store.EmailSettings(ctx); s.OnReply || s.IncludeReply || !s.OnInvite {
		t.Fatalf("switches not saved: %+v", s)
	}
	mail := &fakeMailSender{}
	a.Mailer, a.MailEnabled, a.MailFrom = mail, true, "noreply@support.example.test"
	if res := request(h, "POST", "/admin/email/test", "admin", url.Values{"csrf": {"csrf-admin"}}); res.Header().Get("Location") != "/admin/email?test=ok" || len(mail.sent) != 1 || mail.sent[0].to != "admin@example.test" {
		t.Fatalf("test email: %s %+v", res.Header().Get("Location"), mail.sent)
	}
	if page := request(h, "GET", "/admin/email?test=ok", "admin", nil).Body.String(); !strings.Contains(page, "noreply@support.example.test") || !strings.Contains(page, "測試信已寄出") {
		t.Fatal("success or sender not shown")
	}
	mail.err = errors.New("dial tcp: connection refused")
	if res := request(h, "POST", "/admin/email/test", "admin", url.Values{"csrf": {"csrf-admin"}}); res.Header().Get("Location") != "/admin/email?test=failed" {
		t.Fatal("failed test reported as success")
	}
	if page := request(h, "GET", "/admin/email?test=failed", "admin", nil).Body.String(); !strings.Contains(page, "無法連線到 SMTP 伺服器") {
		t.Fatal("failure reason not shown")
	}
	// Everyone can switch their own ticket emails off and back on.
	if page := request(h, "GET", "/account", "alice", nil).Body.String(); !strings.Contains(page, `name="email_notify" checked`) {
		t.Fatal("email preference should start on")
	}
	if res := request(h, "POST", "/account/email", "alice", url.Values{"email_notify": {"on"}}); res.Code != http.StatusForbidden {
		t.Fatal("preference changed without CSRF", res.Code)
	}
	request(h, "POST", "/account/email", "alice", url.Values{"csrf": {"csrf-alice"}})
	if page := request(h, "GET", "/account", "alice", nil).Body.String(); strings.Contains(page, `name="email_notify" checked`) {
		t.Fatal("email preference not switched off")
	}
}
