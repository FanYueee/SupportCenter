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

func TestMentionsShowOnlyTicketsTheViewerMayOpen(t *testing.T) {
	a, h, alice, bob, _ := testApp(t)
	ctx := context.Background()
	own, _ := a.Store.Create(ctx, alice, model.Ticket{Subject: "Alice 的 <b>舊單</b>", Category: "other", Priority: "low"}, "舊問題", nil)
	secret, _ := a.Store.Create(ctx, bob, model.Ticket{Subject: "Bob 的祕密", Category: "other", Priority: "low"}, "私人", nil)
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "新問題", Category: "other", Priority: "low"}, fmt.Sprintf("延續#%d，也看看 #%04d 和 C#%d", own, secret, own), nil)
	if err != nil {
		t.Fatal(err)
	}
	page := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil).Body.String()
	chip := fmt.Sprintf(`<a class="ticket-mention" href="/tickets/%d"><span class="ticket-mention-number">#%04d</span><span class="ticket-mention-subject">Alice 的 &lt;b&gt;舊單&lt;/b&gt;</span></a>`, own, own)
	if !strings.Contains(page, chip) || strings.Contains(page, "Bob 的祕密") || !strings.Contains(page, fmt.Sprintf("#%04d 和 C#%d", secret, own)) {
		t.Fatal("customer should see only their own mentioned ticket, escaped")
	}
	// The picker offers the customer's other tickets, never someone else's.
	if !strings.Contains(page, fmt.Sprintf(`data-mention="%d"`, own)) || strings.Contains(page, fmt.Sprintf(`data-mention="%d"`, secret)) || strings.Contains(page, fmt.Sprintf(`data-mention="%d"`, id)) {
		t.Fatal("mention picker lists the wrong tickets")
	}
	staffPage := request(h, "GET", fmt.Sprintf("/admin/tickets/%d", id), "staff", nil).Body.String()
	if !strings.Contains(staffPage, fmt.Sprintf(`href="/admin/tickets/%d"`, secret)) || !strings.Contains(staffPage, "Bob 的祕密") {
		t.Fatal("staff should see every mentioned ticket")
	}
	if !strings.Contains(staffPage, fmt.Sprintf(`data-mention="%d"`, own)) || strings.Contains(staffPage, fmt.Sprintf(`data-mention="%d"`, secret)) {
		t.Fatal("staff picker should offer the requester's tickets")
	}
}

func TestNewTicketFormChecks(t *testing.T) {
	_, h, _, _, _ := testApp(t)
	form := url.Values{"csrf": {"csrf-alice"}, "subject": {"無類別"}, "priority": {"low"}, "body": {"內容"}}
	page := request(h, "GET", "/tickets/new", "alice", nil).Body.String()
	if !strings.Contains(page, `type="radio" name="category" value="other" required>`) || strings.Contains(page, `name="category" value="other" required checked`) || strings.Contains(page, `<select name="category"`) {
		t.Fatal("categories should be buttons with none chosen")
	}
	if got := request(h, "POST", "/tickets", "alice", form); got.Code != http.StatusUnprocessableEntity || !strings.Contains(got.Body.String(), "請選擇問題類別") {
		t.Fatalf("ticket without a category: %d", got.Code)
	}
	// HTMX keeps its own form and is sent to the ticket on success.
	form.Set("category", "billing")
	r := httptest.NewRequest("POST", "https://support.example.test/tickets", strings.NewReader(withSubmission(form).Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Request", "true")
	r.AddCookie(cookie("__Host-support-session", "alice", 86400))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("HX-Redirect"), "/tickets/") {
		t.Fatalf("HTMX create: %d %q", w.Code, w.Header().Get("HX-Redirect"))
	}
	// A customer opens at most three tickets in ten minutes; staff are not limited.
	for i := 0; i < 2; i++ {
		if got := request(h, "POST", "/tickets", "alice", form); got.Code != http.StatusSeeOther {
			t.Fatalf("ticket %d: %d", i+2, got.Code)
		}
	}
	if got := request(h, "POST", "/tickets", "alice", form); got.Code != http.StatusTooManyRequests {
		t.Fatalf("fourth ticket in a row: %d", got.Code)
	}
	staffForm := withSubmission(form)
	staffForm.Set("csrf", "csrf-staff")
	for i := 0; i < 4; i++ {
		staffForm.Set("submission", token())
		if got := request(h, "POST", "/tickets", "staff", staffForm); got.Code != http.StatusSeeOther {
			t.Fatalf("staff ticket %d: %d", i+1, got.Code)
		}
	}
}

func withSubmission(form url.Values) url.Values {
	out := url.Values{}
	for key, values := range form {
		out[key] = append([]string(nil), values...)
	}
	out.Set("submission", token())
	return out
}

func TestTurnstileGuardsNewTickets(t *testing.T) {
	a, h, _, _, _ := testApp(t)
	var got url.Values
	reply := `{"success":true,"hostname":"support.example.test","action":"ticket"}`
	verifier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.PostForm
		fmt.Fprint(w, reply)
	}))
	defer verifier.Close()
	a.Turnstile = &Turnstile{SiteKey: "0x4AAAAAAAtestkey", Secret: "test-secret", VerifyURL: verifier.URL, Client: verifier.Client()}
	page := request(h, "GET", "/tickets/new", "alice", nil)
	if !strings.Contains(page.Body.String(), `data-sitekey="0x4AAAAAAAtestkey"`) || !strings.Contains(page.Header().Get("Content-Security-Policy"), "script-src 'self' https://challenges.cloudflare.com") ||
		!strings.Contains(page.Header().Get("Content-Security-Policy"), "frame-src https://challenges.cloudflare.com") || strings.Contains(page.Body.String(), "test-secret") {
		t.Fatal("new ticket page should show the check and allow only Cloudflare's script")
	}
	if list := request(h, "GET", "/tickets", "alice", nil); strings.Contains(list.Header().Get("Content-Security-Policy"), "challenges.cloudflare.com") {
		t.Fatal("other pages should keep the strict policy")
	}
	form := url.Values{"csrf": {"csrf-alice"}, "subject": {"機器人"}, "category": {"other"}, "priority": {"low"}, "body": {"內容"}}
	if res := request(h, "POST", "/tickets", "alice", form); res.Code != http.StatusUnprocessableEntity || !strings.Contains(res.Body.String(), "請完成人機驗證") {
		t.Fatalf("ticket without a token: %d", res.Code)
	}
	form.Set("cf-turnstile-response", "token-from-widget")
	if res := request(h, "POST", "/tickets", "alice", form); res.Code != http.StatusSeeOther || got.Get("secret") != "test-secret" || got.Get("response") != "token-from-widget" {
		t.Fatalf("verified ticket: %d %v", res.Code, got)
	}
	// A token from another site or form does not count.
	for _, bad := range []string{`{"success":true,"hostname":"evil.example","action":"ticket"}`, `{"success":true,"hostname":"support.example.test","action":"login"}`, `{"success":false}`} {
		reply = bad
		if res := request(h, "POST", "/tickets", "alice", form); res.Code != http.StatusUnprocessableEntity {
			t.Fatalf("accepted %s: %d", bad, res.Code)
		}
	}
}

func TestSupportFileTypes(t *testing.T) {
	pcap := append([]byte("\xd4\xc3\xb2\xa1"), make([]byte, 40)...)
	pcapng := append([]byte("\x0a\x0d\x0d\x0a\x1c\x00\x00\x00\x4d\x3c\x2b\x1a"), make([]byte, 40)...)
	gzip := []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\x03\x03\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	tar := make([]byte, 1024)
	copy(tar[257:], "ustar")
	for name, data := range map[string][]byte{
		"capture.pcap": pcap, "capture.cap": pcap, "capture.pcapng": pcapng, "logs.tar.gz": gzip, "logs.tar": tar,
		"bundle.7z": []byte("7z\xbc\xaf\x27\x1c\x00\x04"), "session.har": []byte(`{"log":{"entries":[]}}`), "config.yaml": []byte("port: 8080\n"),
		"rules.conf": []byte("listen 443;\n"), "data.csv": []byte("a,b\n1,2\n"),
	} {
		if _, err := fileType(data, name); err != nil {
			t.Errorf("%s rejected: %v", name, err)
		}
	}
	for name, data := range map[string][]byte{
		"fake.pcap": []byte("not a capture at all, just text"), "fake.pcapng": pcap, "fake.gz": []byte("plain"), "bad.json": []byte("{"),
		"bomb.gz": append(gzip[:16:16], 0xff, 0xff, 0xff, 0x7f), "script.html": []byte("<script>"), "binary.conf": []byte("a\x01b"),
	} {
		if _, err := fileType(data, name); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
