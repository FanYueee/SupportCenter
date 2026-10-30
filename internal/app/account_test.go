package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"supportcenter/internal/model"
)

func TestAccountSettingsAndAudit(t *testing.T) {
	a, h, alice, bob, _ := testApp(t)
	ctx := context.Background()
	if got := request(h, "GET", "/account", "", nil); got.Code != 303 {
		t.Fatalf("anonymous account: %d", got.Code)
	}
	if got := request(h, "GET", "/admin/users", "alice", nil); got.Code != 404 {
		t.Fatalf("customer users page: %d", got.Code)
	}
	if got := request(h, "GET", "/admin/users", "staff", nil); got.Code != 404 {
		t.Fatalf("support users page: %d", got.Code)
	}
	if got := request(h, "GET", "/admin/users?q=alice", "admin", nil); got.Code != 200 || !strings.Contains(got.Body.String(), alice.Email) || strings.Contains(got.Body.String(), bob.Email) {
		t.Fatalf("admin search: %d", got.Code)
	}
	if got := request(h, "POST", "/account/name", "alice", url.Values{"csrf": {"csrf-alice"}, "name": {"New Alice"}}); got.Code != 404 {
		t.Fatalf("customer changed name: %d", got.Code)
	}
	if got := request(h, "POST", "/account/name", "admin", url.Values{"csrf": {"csrf-admin"}, "name": {"管理員新名稱"}}); got.Code != 303 {
		t.Fatalf("admin rename: %d", got.Code)
	}
	admin, err := a.Store.User(ctx, "admin", "Auth0 原始名稱", "admin@example.test")
	if err != nil || admin.Name != "管理員新名稱" {
		t.Fatalf("admin name did not survive Auth0 refresh: %+v %v", admin, err)
	}
	tx, err := a.Store.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.LogAccountEvent(ctx, tx, alice.ID, "login", "Chrome · Windows"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := request(h, "GET", "/account", "alice", nil); got.Code != 200 || !strings.Contains(got.Body.String(), "Chrome · Windows") {
		t.Fatalf("own login history: %d", got.Code)
	}
	if got := request(h, "GET", "/account", "bob", nil); got.Code != 200 || strings.Contains(got.Body.String(), "Chrome · Windows") {
		t.Fatalf("other user saw login history: %d", got.Code)
	}
	if got := request(h, "POST", "/account/avatar", "alice", url.Values{"csrf": {"csrf-alice"}}); got.Code != 404 {
		t.Fatalf("removed avatar preference: %d", got.Code)
	}
	if got := request(h, "POST", "/account/signature", "alice", url.Values{"csrf": {"csrf-alice"}, "signature": {"— Alex"}}); got.Code != 404 {
		t.Fatalf("customer signature write: %d", got.Code)
	}
	if got := request(h, "POST", "/account/signature", "staff", url.Values{"csrf": {"csrf-staff"}, "signature": {"— Staff"}}); got.Code != 404 {
		t.Fatalf("support signature write: %d", got.Code)
	}
	if got := request(h, "GET", "/account", "alice", nil); got.Code != 200 || strings.Contains(got.Body.String(), "簽名檔") || strings.Contains(got.Body.String(), "avatar_source") {
		t.Fatalf("customer account still shows removed settings: %d", got.Code)
	}
	if got := request(h, "POST", "/account/signature", "admin", url.Values{"csrf": {"csrf-admin"}, "signature": {"— Admin"}}); got.Code != 303 {
		t.Fatalf("admin signature write: %d", got.Code)
	}
	if got := request(h, "GET", "/account", "admin", nil); got.Code != 200 || !strings.Contains(got.Body.String(), "— Admin") || !strings.Contains(got.Body.String(), "更新簽名檔") {
		t.Fatalf("admin signature setting missing: %d", got.Code)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "account test", Category: "technical", Priority: "low"}, "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.Pool.Exec(ctx, `UPDATE users SET signature='— Legacy' WHERE id=$1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/tickets/%d/reply", id)
	if got := request(h, "POST", path, "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"hello"}}); got.Code != 303 {
		t.Fatalf("reply: %d", got.Code)
	}
	messages, _, err := a.Store.Messages(ctx, alice, id, false, 1)
	if err != nil || len(messages) != 2 || messages[1].Body != "hello" {
		t.Fatalf("customer legacy signature was appended: %+v %v", messages, err)
	}
	adminTicket, err := a.Store.Create(ctx, admin, model.Ticket{Subject: "admin signature", Category: "technical", Priority: "low"}, "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := request(h, "POST", fmt.Sprintf("/tickets/%d/reply", adminTicket), "admin", url.Values{"csrf": {"csrf-admin"}, "body": {"answer"}}); got.Code != 303 {
		t.Fatalf("admin reply: %d", got.Code)
	}
	adminMessages, _, err := a.Store.Messages(ctx, admin, adminTicket, false, 1)
	if err != nil || len(adminMessages) != 2 || adminMessages[1].Body != "answer\n\n— Admin" {
		t.Fatalf("admin signature not appended: %+v %v", adminMessages, err)
	}
	if got := request(h, "GET", "/account", "alice", nil); got.Code != 200 || !strings.Contains(got.Body.String(), "回覆客服單") {
		t.Fatalf("reply activity missing: %d", got.Code)
	}
}

func TestActivityRecordsClientIPAndBrowser(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	post := func(path string, values url.Values) {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, a.Origin+path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0) Chrome/120.0")
		r.Header.Set("CF-Connecting-IP", "203.0.113.44")
		r.RemoteAddr = "172.18.0.1:54400"
		r.AddCookie(cookie("__Host-support-session", "alice", 86400))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 303 {
			t.Fatalf("POST %s: %d", path, w.Code)
		}
	}
	post("/tickets", url.Values{"csrf": {"csrf-alice"}, "submission": {token()}, "subject": {"record metadata"}, "category": {"technical"}, "priority": {"low"}, "body": {"hello"}})
	var id int64
	if err := a.Store.Pool.QueryRow(context.Background(), `SELECT id FROM tickets WHERE subject='record metadata' AND user_id=$1`, alice.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	post(fmt.Sprintf("/tickets/%d/reply", id), url.Values{"csrf": {"csrf-alice"}, "submission": {token()}, "body": {"followup"}})
	activities, _, err := a.Store.Activities(context.Background(), alice.ID, 1)
	if err != nil || len(activities) < 2 {
		t.Fatalf("read activities: %+v %v", activities, err)
	}
	for _, activity := range activities {
		if activity.IP != "203.0.113.44" || model.ActivityBrowser(activity) != "Chrome · Windows" {
			t.Fatalf("missing request metadata: %+v", activity)
		}
	}
	page := request(h, "GET", "/account", "alice", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "IP 203.0.113.44") || !strings.Contains(page.Body.String(), "Chrome · Windows") {
		t.Fatalf("activity metadata not shown: %d", page.Code)
	}
}

// Google and a passkey with the same verified address reach one account.
func TestSignInMethodsShareOneAccount(t *testing.T) {
	a, h, _, _, _ := testApp(t)
	ctx := context.Background()
	signIn := func(subject, name string, verified bool) model.User {
		t.Helper()
		u, err := a.Store.User(ctx, subject, name, " Admin@Example.test", verified)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	// Until the account's own address is verified, nobody can join it.
	if early := signIn("auth0|early", "Early", true); early.Role != "user" {
		t.Fatal("joined an account whose address was never verified")
	}
	admin := signIn("admin", "Admin", true)
	if unverified := signIn("auth0|unverified", "Someone", false); unverified.ID == admin.ID || unverified.Role != "user" {
		t.Fatal("an unverified sign-in joined the account")
	}
	for range 2 {
		if passkey := signIn("auth0|passkey", "admin.passkey", true); passkey.ID != admin.ID || passkey.Role != "admin" || passkey.Name != "Admin" {
			t.Fatalf("passkey reached %+v instead of the admin account", passkey)
		}
	}
	if again := signIn("admin", "Admin", true); again.ID != admin.ID {
		t.Fatal("the first sign-in method lost its account")
	}
	var links, accounts int
	a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM account_events WHERE user_id=$1 AND kind='link'`, admin.ID).Scan(&links)
	a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE auth0_id='auth0|passkey'`).Scan(&accounts)
	if links != 1 || accounts != 0 {
		t.Fatal("link recorded", links, "separate accounts", accounts)
	}
	if page := request(h, "GET", "/account", "admin", nil).Body.String(); !strings.Contains(page, "新增登入方式") {
		t.Fatal("account history does not show the added sign-in method")
	}
	if err := a.Store.SetRole(ctx, "auth0|passkey", "admin"); err != nil {
		t.Fatal("roles cannot be set through an added sign-in method", err)
	}
}
