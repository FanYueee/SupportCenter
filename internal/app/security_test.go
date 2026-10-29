package app

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"supportcenter/internal/database"
	"supportcenter/internal/model"
	"supportcenter/web"
)

func TestFileValidation(t *testing.T) {
	var z bytes.Buffer
	zw := zip.NewWriter(&z)
	f, _ := zw.Create("log.txt")
	f.Write([]byte("hello"))
	zw.Close()
	for _, tc := range []struct {
		name string
		data []byte
		good bool
	}{{"a.txt", []byte("hello\nworld"), true}, {"a.log", []byte("2026-09-28 error"), true}, {"a.png", []byte("<script>alert(1)</script>"), false}, {"a.jpg", []byte("fake image"), false}, {"a.pdf", []byte("%PDF-1.7\nfake"), false}, {"a.pdf", []byte("%PDF-1.7\n%%EOF"), true}, {"a.txt", []byte{'a', 0, 'b'}, false}, {"a.txt", []byte{0xff}, false}, {"a.html", []byte("hi"), false}, {"a.zip", z.Bytes(), true}, {"a.zip", []byte("PKbad"), false}} {
		t.Run(tc.name+fmt.Sprint(tc.good), func(t *testing.T) {
			_, err := fileType(tc.data, tc.name)
			if (err == nil) != tc.good {
				t.Fatalf("accepted=%v want %v", err == nil, tc.good)
			}
		})
	}
	if safeName(`../../secret.txt`) != "secret.txt" || safeName("bad\r\nname.txt") != "badname.txt" {
		t.Fatal("unsafe filename")
	}
}
func TestEscapedHTML(t *testing.T) {
	payload := `<script>alert("xss")</script>`
	p := model.Page{User: model.User{Name: payload}, Title: payload, Active: "detail", Ticket: model.Ticket{Subject: payload, Status: "open"}, Messages: []model.Message{{Author: payload, Body: payload}}, Page: 1, MessagePage: 1}
	var out bytes.Buffer
	if err := web.Document(p).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), payload) || !strings.Contains(out.String(), "&lt;script&gt;") {
		t.Fatal("user content not escaped")
	}
}
func TestCookieSecurity(t *testing.T) {
	c := cookie("__Host-support-session", "secret", 86400)
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" {
		t.Fatal("insecure cookie")
	}
}

// Each integration run uses a fresh schema; existing data is never truncated.
func testApp(t *testing.T) (*App, http.Handler, model.User, model.User, model.User) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + strings.ToLower(token())
	schema = strings.ReplaceAll(schema, "-", "_")
	// Generated identifier contains only [a-z0-9_], not user input.
	if _, err = pool.Exec(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	isolated, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := &database.Store{Pool: isolated}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Store: s, Files: root, Origin: "https://support.example.test", Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	t.Cleanup(func() {
		root.Close()
		isolated.Close()
		pool.Exec(ctx, `DROP SCHEMA "`+schema+`" CASCADE`)
		pool.Close()
	})
	user := func(sub, role string) model.User {
		u, e := s.User(ctx, sub, sub, sub+"@example.test")
		if e != nil {
			t.Fatal(e)
		}
		if e = s.SetRole(ctx, sub, role); e != nil {
			t.Fatal(e)
		}
		u.Role = role
		_, e = s.Pool.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`, hash(sub), u.ID, "csrf-"+sub)
		if e != nil {
			t.Fatal(e)
		}
		return u
	}
	u := user("alice", "user")
	v := user("bob", "user")
	staff := user("staff", "support")
	user("admin", "admin")
	return a, a.Handler(), u, v, staff
}
func request(h http.Handler, method, path, session string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		copyForm := url.Values{}
		for key, values := range form {
			copyForm[key] = append([]string(nil), values...)
		}
		form = copyForm
		if method == "POST" && (path == "/tickets" || strings.HasSuffix(path, "/reply")) && !form.Has("submission") {
			form.Set("submission", token())
		}
		if method == "POST" && strings.HasSuffix(path, "/update") && !form.Has("version") {
			form.Set("version", "1")
			page := request(h, "GET", strings.TrimSuffix(path, "/update"), session, nil)
			match := regexp.MustCompile(`name="version" value="([0-9]+)"`).FindStringSubmatch(page.Body.String())
			if len(match) == 2 {
				form.Set("version", match[1])
			}
		}
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, "https://support.example.test"+path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if session != "" {
		r.AddCookie(cookie("__Host-support-session", session, 86400))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestAuthorizationAndTicketFlow(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	ctx := context.Background()
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "Alice private ticket", Category: "technical", Priority: "normal"}, "private message", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/tickets/%d", id)
	adminPath := fmt.Sprintf("/admin/tickets/%d", id)
	for _, tc := range []struct {
		method, path, user string
		form               url.Values
		want               int
	}{
		{"GET", path, "", nil, 303}, {"GET", path, "alice", nil, 200}, {"GET", path, "bob", nil, 404},
		{"GET", "/admin", "alice", nil, 404}, {"GET", "/admin/tickets", "alice", nil, 404}, {"GET", adminPath, "alice", nil, 404},
		{"GET", adminPath, "staff", nil, 200}, {"GET", adminPath, "admin", nil, 200},
		{"POST", path + "/reply", "bob", url.Values{"csrf": {"csrf-bob"}, "body": {"hacked"}}, 404},
		{"POST", path + "/update", "bob", url.Values{"csrf": {"csrf-bob"}, "status": {"closed"}}, 404},
		{"POST", path + "/reply", "alice", url.Values{"csrf": {"wrong"}, "body": {"hacked"}}, 403},
		{"POST", path + "/reply", "alice", url.Values{"body": {"missing CSRF"}}, 403},
		{"POST", path + "/reply", "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"note"}, "internal": {"true"}}, 403},
		{"GET", "/logout", "alice", nil, 405},
	} {
		t.Run(tc.method+tc.path+tc.user+fmt.Sprint(tc.want), func(t *testing.T) {
			w := request(h, tc.method, tc.path, tc.user, tc.form)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d: %s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	if err = a.Store.Reply(ctx, staff, id, true, "internal secret", true, nil); err != nil {
		t.Fatal(err)
	}
	userPage := request(h, "GET", path, "alice", nil)
	staffPage := request(h, "GET", adminPath, "staff", nil)
	if strings.Contains(userPage.Body.String(), "internal secret") || !strings.Contains(staffPage.Body.String(), "internal secret") {
		t.Fatal("private note visibility incorrect")
	}
	if userPage.Header().Get("Cache-Control") != "no-store" || !strings.Contains(userPage.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("missing security headers")
	}
	if err = a.Store.Reply(ctx, staff, id, true, "please reply", false, nil); err != nil {
		t.Fatal(err)
	}
	ticket, _ := a.Store.Ticket(ctx, alice, id, false)
	if ticket.Status != "waiting" {
		t.Fatal("staff response should request user reply")
	}
	w := request(h, "POST", path+"/reply", "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"Here is more info"}})
	if w.Code != 303 {
		t.Fatalf("reply: %d", w.Code)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.Status != "open" {
		t.Fatal("user reply should reopen queue")
	}
	w = request(h, "POST", path+"/update", "alice", url.Values{"csrf": {"csrf-alice"}, "status": {"closed"}, "priority": {"urgent"}})
	if w.Code != 303 {
		t.Fatal(w.Code)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.Status != "closed" || ticket.Priority != "normal" {
		t.Fatal("user changed protected priority")
	}
	w = request(h, "POST", path+"/reply", "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"closed reply"}})
	if w.Code != 409 {
		t.Fatal("closed ticket accepted reply")
	}
	if err = a.Store.Update(ctx, staff, id, true, "open", "urgent", &staff.ID); err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.AssigneeID == nil || *ticket.AssigneeID != staff.ID || ticket.Priority != "urgent" {
		t.Fatal("staff metadata not updated")
	}
	if err = a.Store.Update(ctx, staff, id, true, "open", "urgent", &bob.ID); err == nil {
		t.Fatal("assigned to normal user")
	}
	// SQL injection is stored as text and cannot broaden ownership scope.
	attack := `' OR 1=1; DROP TABLE tickets; --`
	_, err = a.Store.Create(ctx, bob, model.Ticket{Subject: attack, Category: "other", Priority: "normal"}, attack, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, _, _, err := a.Store.List(ctx, alice, false, "", "", attack, 1, "")
	if err != nil || len(list) != 0 {
		t.Fatal("SQL input altered ownership query")
	}
	list, _, _, err = a.Store.List(ctx, bob, false, "", "", attack, 1, "")
	if err != nil || len(list) != 1 {
		t.Fatal("SQL text was not treated literally")
	}
	// A crafted request from another origin must fail even with a valid CSRF token.
	r := httptest.NewRequest("POST", "https://support.example.test"+path+"/update", strings.NewReader("csrf=csrf-alice&status=open"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://attacker.test")
	r.AddCookie(cookie("__Host-support-session", "alice", 1))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin mutation allowed")
	}
}

func TestStaffCanAssignTicketToSelf(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	id, err := a.Store.Create(context.Background(), alice, model.Ticket{Subject: "assign me", Category: "technical", Priority: "normal"}, "details", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/admin/tickets/%d/update", id)
	response := request(h, "POST", path, "staff", url.Values{"csrf": {"csrf-staff"}, "status": {"open"}, "priority": {"normal"}, "assignee": {"self"}})
	if response.Code != 303 {
		t.Fatalf("self assignment returned %d", response.Code)
	}
	ticket, err := a.Store.Ticket(context.Background(), staff, id, true)
	if err != nil || ticket.AssigneeID == nil || *ticket.AssigneeID != staff.ID {
		t.Fatalf("ticket not assigned to staff: %+v %v", ticket, err)
	}
}
func TestTicketNumberSearchRespectsOwnershipAndCounts(t *testing.T) {
	a, _, alice, bob, staff := testApp(t)
	ctx := context.Background()
	aliceID, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "connection problem", Category: "technical", Priority: "urgent"}, "details", nil)
	if err != nil {
		t.Fatal(err)
	}
	bobID, err := a.Store.Create(ctx, bob, model.Ticket{Subject: "billing problem", Category: "billing", Priority: "normal"}, "details", nil)
	if err != nil {
		t.Fatal(err)
	}
	search := func(u model.User, admin bool, q, status string) ([]model.Ticket, model.Counts) {
		t.Helper()
		list, counts, _, e := a.Store.List(ctx, u, admin, status, "", q, 1, "")
		if e != nil {
			t.Fatal(e)
		}
		return list, counts
	}
	if list, counts := search(alice, false, fmt.Sprintf("#%d", bobID), ""); len(list) != 0 || counts.All != 0 {
		t.Fatal("another customer's ticket appeared in number search")
	}
	if list, counts := search(alice, false, fmt.Sprintf("%d", bobID), ""); len(list) != 0 || counts.All != 0 {
		t.Fatal("another customer's ticket appeared in bare number search")
	}
	if list, counts := search(alice, false, fmt.Sprintf("#%d", aliceID), ""); len(list) != 1 || list[0].ID != aliceID || counts.All != 1 || counts.New != 1 {
		t.Fatal("own ticket number search or counts incorrect")
	}
	if list, counts := search(alice, false, fmt.Sprintf("%04d", aliceID), ""); len(list) != 1 || list[0].ID != aliceID || counts.All != 1 || counts.New != 1 {
		t.Fatal("own bare ticket number search or counts incorrect")
	}
	if list, counts := search(alice, false, "connection", "closed"); len(list) != 0 || counts.All != 1 || counts.New != 1 || counts.Closed != 0 {
		t.Fatal("subject search counts should stay scoped to the search across status tabs")
	}
	if list, counts := search(alice, false, "%", ""); len(list) != 0 || counts.All != 0 {
		t.Fatal("LIKE wildcard was not treated as literal search text")
	}
	if list, counts := search(staff, true, fmt.Sprintf("#%d", bobID), ""); len(list) != 1 || list[0].ID != bobID || counts.All != 1 {
		t.Fatal("staff ticket number search incorrect")
	}
	if list, counts := search(staff, true, fmt.Sprintf("%d", bobID), ""); len(list) != 1 || list[0].ID != bobID || counts.All != 1 {
		t.Fatal("staff bare ticket number search incorrect")
	}
}
func TestPrivateAttachmentAndRollback(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	ctx := context.Background()
	key := hash(token())
	if err := a.Files.WriteFile(key, []byte("private attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "attachment", Category: "technical", Priority: "normal"}, "hello", []model.Attachment{{Key: key, Name: "log.txt", MIME: "text/plain", Size: 18}})
	if err != nil {
		t.Fatal(err)
	}
	var aid int64
	a.Store.Pool.QueryRow(ctx, `SELECT id FROM ticket_attachments WHERE storage_key=$1`, key).Scan(&aid)
	path := fmt.Sprintf("/attachments/%d", aid)
	for _, tc := range []struct {
		user   string
		status int
	}{{"alice", 200}, {"bob", 404}, {"staff", 200}, {"", 303}} {
		w := request(h, "GET", path, tc.user, nil)
		if w.Code != tc.status {
			t.Fatalf("download %s: %d", tc.user, w.Code)
		}
		if tc.status == 200 && !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatal("download is not attachment")
		}
	}
	if _, err = a.Store.Attachment(ctx, bob, aid); err == nil {
		t.Fatal("bob read alice attachment")
	}
	privateKey := hash(token())
	a.Files.WriteFile(privateKey, []byte("secret"), 0600)
	if err = a.Store.Reply(ctx, staff, id, true, "private note", true, []model.Attachment{{Key: privateKey, Name: "secret.txt", MIME: "text/plain", Size: 6}}); err != nil {
		t.Fatal(err)
	}
	a.Store.Pool.QueryRow(ctx, `SELECT id FROM ticket_attachments WHERE storage_key=$1`, privateKey).Scan(&aid)
	if w := request(h, "GET", fmt.Sprintf("/attachments/%d", aid), "alice", nil); w.Code != 404 {
		t.Fatal("private note attachment leaked")
	}
	// A symlink in the private directory cannot escape the attachment root.
	outside := t.TempDir() + "/outside"
	os.WriteFile(outside, []byte("outside secret"), 0600)
	a.Files.Remove(key)
	os.Symlink(outside, a.Files.Name()+"/"+key)
	if w := request(h, "GET", path, "alice", nil); w.Code != 404 {
		t.Fatal("symlink attachment served")
	}
	// If a later file is invalid, the earlier persisted file is removed.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	f, _ := writer.CreateFormFile("attachments", "valid.txt")
	f.Write([]byte("valid"))
	f, _ = writer.CreateFormFile("attachments", "invalid.png")
	f.Write([]byte("fake"))
	writer.Close()
	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err = req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	defer req.MultipartForm.RemoveAll()
	before, _ := os.ReadDir(a.Files.Name())
	if _, err = a.uploads(req); err == nil {
		t.Fatal("invalid upload accepted")
	}
	after, _ := os.ReadDir(a.Files.Name())
	if len(before) != len(after) {
		t.Fatal("partial upload leaked")
	}
}
func TestPaginationAndHTMX(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	for i := 0; i < 22; i++ {
		_, err := a.Store.Create(ctx, alice, model.Ticket{Subject: fmt.Sprintf("ticket %02d", i), Category: "other", Priority: "normal"}, "message", nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	items, counts, next, err := a.Store.List(ctx, alice, false, "", "", "", 1, "")
	if err != nil || len(items) != 20 || counts.All != 22 || !next {
		t.Fatal("incorrect first page")
	}
	items, _, next, err = a.Store.List(ctx, alice, false, "", "", "", 2, "")
	if err != nil || len(items) != 2 || next {
		t.Fatal("incorrect second page")
	}
	staffItems, staffCounts, staffNext, err := a.Store.List(ctx, staff, true, "", "normal", "", 1, "")
	if err != nil || len(staffItems) != 20 || staffCounts.All != 22 || !staffNext {
		t.Fatal("priority-filtered first page incorrect")
	}
	staffItems, _, staffNext, err = a.Store.List(ctx, staff, true, "", "normal", "", 2, "")
	if err != nil || len(staffItems) != 2 || staffNext {
		t.Fatal("priority-filtered second page incorrect")
	}
	id := items[0].ID
	for i := 0; i < 31; i++ {
		if err = a.Store.Reply(ctx, alice, id, false, "reply", false, nil); err != nil {
			t.Fatal(err)
		}
	}
	messages, more, err := a.Store.Messages(ctx, alice, id, false, 1)
	if err != nil || len(messages) != 30 || !more {
		t.Fatal("messages not paginated")
	}
	for i := 1; i < len(messages); i++ {
		if messages[i].ID <= messages[i-1].ID {
			t.Fatal("messages not chronological")
		}
	}
	r := httptest.NewRequest("GET", "https://support.example.test/tickets?status=open", nil)
	r.AddCookie(cookie("__Host-support-session", "alice", 1))
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "<!doctype") || !strings.Contains(w.Body.String(), `id="workspace"`) {
		t.Fatal("HTMX did not return fragment")
	}
	// Expired sessions are rejected.
	a.Store.Pool.Exec(ctx, `UPDATE sessions SET expires_at=$1 WHERE token_hash=$2`, time.Now().Add(-time.Hour), hash("alice"))
	if w = request(h, "GET", "/tickets", "alice", nil); w.Code != 303 {
		t.Fatal("expired session accepted")
	}
}

func TestStaffPriorityFilter(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	ctx := context.Background()
	var urgentAliceID int64
	for _, item := range []struct {
		user     model.User
		subject  string
		priority string
	}{{alice, "urgent Alice", "urgent"}, {alice, "low Alice", "normal"}, {bob, "urgent Bob", "urgent"}} {
		id, err := a.Store.Create(ctx, item.user, model.Ticket{Subject: item.subject, Category: "other", Priority: item.priority}, "message", nil)
		if err != nil {
			t.Fatal(err)
		}
		if item.subject == "urgent Alice" {
			urgentAliceID = id
		}
	}
	if err := a.Store.Update(ctx, staff, urgentAliceID, true, "closed", "urgent", &staff.ID); err != nil {
		t.Fatal(err)
	}
	items, counts, more, err := a.Store.List(ctx, staff, true, "", "urgent", "", 1, "")
	if err != nil || len(items) != 2 || counts.All != 2 || counts.New != 1 || counts.Closed != 1 || more {
		t.Fatalf("staff priority filter failed: items=%d counts=%+v more=%v err=%v", len(items), counts, more, err)
	}
	closed, _, _, err := a.Store.List(ctx, staff, true, "closed", "urgent", "", 1, "")
	if err != nil || len(closed) != 1 || closed[0].ID != urgentAliceID {
		t.Fatal("status and priority filters did not combine")
	}
	assigned, assignedCounts, _, err := a.Store.List(ctx, staff, true, "", "urgent", "", 1, "mine")
	if err != nil || len(assigned) != 1 || assigned[0].ID != urgentAliceID || assignedCounts.All != 1 || assignedCounts.Closed != 1 {
		t.Fatal("assigned-to-me filter did not combine with priority and counts")
	}
	unassigned, unassignedCounts, _, err := a.Store.List(ctx, staff, true, "", "urgent", "", 1, "unassigned")
	if err != nil || len(unassigned) != 1 || unassigned[0].Owner != bob.Name || unassignedCounts.All != 1 || unassignedCounts.New != 1 {
		t.Fatal("unassigned filter did not combine with priority and counts")
	}
	page := request(h, "GET", "/admin/tickets?priority=urgent", "staff", nil)
	if page.Code != 200 || strings.Contains(page.Body.String(), "urgent Alice") || !strings.Contains(page.Body.String(), "urgent Bob") || strings.Contains(page.Body.String(), "low Alice") || !strings.Contains(page.Body.String(), "priority=urgent") {
		t.Fatal("staff priority filter did not render or persist")
	}
	completed := request(h, "GET", "/admin/tickets?priority=urgent&phase=done", "staff", nil)
	if completed.Code != 200 || !strings.Contains(completed.Body.String(), "urgent Alice") || strings.Contains(completed.Body.String(), "urgent Bob") {
		t.Fatal("completed priority filter did not render")
	}
	if customer := request(h, "GET", "/tickets?priority=urgent", "alice", nil); customer.Code != 400 {
		t.Fatal("customer priority filter accepted")
	}
	if invalid := request(h, "GET", "/admin/tickets?priority=invalid", "staff", nil); invalid.Code != 400 {
		t.Fatal("invalid priority filter accepted")
	}
	if customer := request(h, "GET", "/tickets?assigned=mine", "alice", nil); customer.Code != 400 {
		t.Fatal("customer assignment filter accepted")
	}
	if invalid := request(h, "GET", "/admin/tickets?assigned=invalid", "staff", nil); invalid.Code != 400 {
		t.Fatal("invalid assignment filter accepted")
	}
}

func TestCreateUploadThroughHTTP(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	submit := func(fileCount int, csrf string) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		for k, v := range map[string]string{"submission": token(), "csrf": csrf, "subject": "新客服單 <script>alert(1)</script>", "category": "technical", "priority": "normal", "body": "需要協助的問題"} {
			writer.WriteField(k, v)
		}
		for i := 0; i < fileCount; i++ {
			part, _ := writer.CreateFormFile("attachments", fmt.Sprintf("log-%d.txt", i))
			part.Write([]byte("log contents"))
		}
		writer.Close()
		req := httptest.NewRequest("POST", a.Origin+"/tickets", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.AddCookie(cookie("__Host-support-session", "alice", 1))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := submit(1, "wrong"); w.Code != 403 {
		t.Fatal("multipart CSRF not checked")
	}
	if w := submit(4, "csrf-alice"); w.Code != 422 {
		t.Fatal("too many files accepted")
	}
	files, _ := os.ReadDir(a.Files.Name())
	if len(files) != 0 {
		t.Fatal("rejected upload persisted")
	}
	w := submit(1, "csrf-alice")
	if w.Code != 303 {
		t.Fatalf("create failed: %d %s", w.Code, w.Body.String())
	}
	path := w.Header().Get("Location")
	page := request(h, "GET", path, "alice", nil)
	if !strings.Contains(page.Body.String(), "log-0.txt") || strings.Contains(page.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("upload not shown or XSS unescaped")
	}
	items, _, _, err := a.Store.List(context.Background(), alice, false, "", "", "", 1, "")
	if err != nil || len(items) != 1 {
		t.Fatal("ticket not persisted")
	}
	// HTMX mutation returns the fresh detail fragment and preserves ownership.
	req := httptest.NewRequest("POST", a.Origin+path+"/reply", strings.NewReader(url.Values{"csrf": {"csrf-alice"}, "body": {"補充資訊"}, "submission": {token()}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(cookie("__Host-support-session", "alice", 1))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "補充資訊") || strings.Contains(w.Body.String(), "<!doctype html>") {
		t.Fatal("reply did not return updated fragment")
	}
	invalid := request(h, "POST", "/tickets", "alice", url.Values{"csrf": {"csrf-alice"}, "subject": {"保留輸入內容"}, "body": {"描述也要保留"}, "category": {"invalid"}, "priority": {"normal"}})
	if invalid.Code != 422 || !strings.Contains(invalid.Body.String(), "保留輸入內容") || !strings.Contains(invalid.Body.String(), "描述也要保留") {
		t.Fatal("invalid form lost draft")
	}
	for _, priority := range []string{"low", "high"} {
		invalid = request(h, "POST", "/tickets", "alice", url.Values{"csrf": {"csrf-alice"}, "subject": {"不允許的優先級"}, "body": {"描述"}, "category": {"technical"}, "priority": {priority}})
		if invalid.Code != 422 {
			t.Fatalf("customer could create %s priority: %d", priority, invalid.Code)
		}
	}
	invalid = request(h, "POST", "/tickets", "alice", url.Values{"csrf": {"csrf-alice"}, "subject": {"舊類別"}, "body": {"描述"}, "category": {"account"}, "priority": {"normal"}})
	if invalid.Code != 422 {
		t.Fatalf("customer could create legacy category: %d", invalid.Code)
	}
}
