package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"supportcenter/internal/model"
)

func TestReferenceSignatureMatchesThePortalsVector(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	raw, _ := json.Marshal(referenceActor{Hostname: "support.vproxy.cloud", Customer: "alice@example.com", Viewer: "alice@example.com", ViewerRole: "customer"})
	actor := base64.RawURLEncoding.EncodeToString(raw)
	if actor != "eyJob3N0bmFtZSI6InN1cHBvcnQudnByb3h5LmNsb3VkIiwiY3VzdG9tZXIiOiJhbGljZUBleGFtcGxlLmNvbSIsInZpZXdlciI6ImFsaWNlQGV4YW1wbGUuY29tIiwidmlld2VyUm9sZSI6ImN1c3RvbWVyIn0" {
		t.Fatal("actor encoding", actor)
	}
	got := referenceSignature(key, "/api/support/v1/references/search?kind=invoice&q=2024&limit=8", "1767225600", "0123456789abcdef0123456789abcdef", actor)
	if got != "75cd5ec19702faa514935f45bca7e9e78f4e6ea7ebee3f49ee70f48f4f8aded8" {
		t.Fatal("signature", got)
	}
}

// fakePortal answers like the Portal: it checks each signature and serves
// every customer only their own items. Titles try to break out of the page.
type fakePortal struct {
	*httptest.Server
	mu     sync.Mutex
	actors []referenceActor
	down   bool
}

func newFakePortal(t *testing.T, key []byte) *fakePortal {
	p := &fakePortal{}
	owned := map[string][]string{"alice@example.test": {"inv-a1", "inv-a2", "svc-a1"}, "bob@example.test": {"inv-b1"}}
	p.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := base64.RawURLEncoding.DecodeString(r.Header.Get("X-Portal-Actor"))
		var actor referenceActor
		json.Unmarshal(raw, &actor)
		p.mu.Lock()
		p.actors = append(p.actors, actor)
		down := p.down
		p.mu.Unlock()
		want := referenceSignature(key, r.URL.RequestURI(), r.Header.Get("X-Portal-Timestamp"), r.Header.Get("X-Portal-Nonce"), r.Header.Get("X-Portal-Actor"))
		if down || r.Header.Get("X-Portal-Issuer") != referenceIssuer || r.Header.Get("X-Portal-Signature") != want || r.Header.Get("X-Portal-Body-SHA256") != emptyBodySHA256 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		item := func(id string) map[string]string {
			return map[string]string{"id": id, "title": "<b>" + id + "</b>", "detail": "NT$1,200", "customerUrl": "https://" + r.Host + "/me/" + id, "staffUrl": "https://" + r.Host + "/admin/" + id}
		}
		items := []map[string]string{}
		prefix := map[string]string{"invoice": "inv", "service": "svc"}[r.URL.Query().Get("kind")]
		switch r.URL.Path {
		case "/refs/kinds":
			json.NewEncoder(w).Encode(map[string]any{"kinds": []map[string]string{{"key": "invoice", "label": "帳單", "labelEn": "Invoices", "trigger": "$"}, {"key": "service", "label": "服務", "labelEn": "Services", "trigger": "!"}, {"key": "bad", "label": "x", "trigger": "#"}}})
			return
		case "/refs/search":
			for _, id := range owned[actor.Customer] {
				if strings.HasPrefix(id, prefix) && strings.Contains(id, r.URL.Query().Get("q")) {
					items = append(items, item(id))
				}
			}
			// Off-site links are dropped.
			items = append(items, map[string]string{"id": "inv-evil", "title": "evil", "customerUrl": "https://evil.example/x", "staffUrl": "https://evil.example/y"})
		case "/refs/items":
			for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
				for _, mine := range owned[actor.Customer] {
					if mine == id && strings.HasPrefix(id, prefix) {
						items = append(items, item(id))
					}
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *fakePortal) lastActor() referenceActor {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.actors[len(p.actors)-1]
}

func referenceTestApp(t *testing.T) (*App, http.Handler, *fakePortal, model.User, model.User, model.User) {
	a, _, alice, bob, staff := testApp(t)
	key := make([]byte, 32)
	portal := newFakePortal(t, key)
	a.References = &References{URL: portal.URL + "/refs", Key: key, Host: "support.example.test", Client: portal.Client()}
	if _, err := a.Store.Pool.Exec(context.Background(), `UPDATE users SET email_verified=true`); err != nil {
		t.Fatal(err)
	}
	return a, a.Handler(), portal, alice, bob, staff
}

func TestTagsAreCheckedWithThePortalAndShownToEachViewer(t *testing.T) {
	a, h, portal, alice, _, _ := referenceTestApp(t)
	ctx := context.Background()
	created := request(h, "POST", "/tickets", "alice", url.Values{"csrf": {"csrf-alice"}, "subject": {"帳單問題"}, "category": {"billing"}, "priority": {"low"},
		"body": {"請看 $inv-a1，還有別人的 $inv-b1 和服務 !svc-a1"}})
	if created.Code != http.StatusSeeOther {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var id int64
	fmt.Sscanf(created.Header().Get("Location"), "/tickets/%d", &id)
	if actor := portal.lastActor(); actor.Customer != alice.Email || actor.ViewerRole != "customer" || actor.Hostname != "support.example.test" {
		t.Fatalf("the Portal was asked as %+v", actor)
	}
	refs, err := a.Store.TicketReferences(ctx, id, true)
	if err != nil || len(refs) != 2 {
		t.Fatalf("only alice's items are kept: %+v %v", refs, err)
	}
	page := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil).Body.String()
	chip := fmt.Sprintf(`<a class="ticket-mention reference-tag" href="%s/me/inv-a1" target="_blank" rel="noopener noreferrer"><span class="ticket-mention-number">帳單</span><span class="ticket-mention-subject">&lt;b&gt;inv-a1&lt;/b&gt;</span></a>`, portal.URL)
	if !strings.Contains(page, chip) || strings.Contains(page, "/me/inv-b1") || !strings.Contains(page, "$inv-b1") {
		t.Fatal("customers see their tags linked to their own pages, others as text")
	}
	if !strings.Contains(page, `class="reference-item"`) || strings.Contains(page, "/admin/inv-a1") {
		t.Fatal("the related items list links customers to their own pages")
	}
	staffPath := fmt.Sprintf("/admin/tickets/%d", id)
	staffPage := request(h, "GET", staffPath, "staff", nil).Body.String()
	if !strings.Contains(staffPage, portal.URL+"/admin/inv-a1") || strings.Contains(staffPage, portal.URL+"/me/inv-a1") {
		t.Fatal("staff open the Portal's staff pages")
	}
	if !strings.Contains(staffPage, `data-reference-triggers="$!"`) || !strings.Contains(staffPage, `data-trigger="$"`) {
		t.Fatal("staff can tag the owner's items")
	}
	// A staff note's tags stay with staff.
	if got := request(h, "POST", staffPath+"/reply", "staff", url.Values{"csrf": {"csrf-staff"}, "body": {"內部：$inv-a2"}, "internal": {"true"}}); got.Code != http.StatusSeeOther {
		t.Fatalf("note: %d", got.Code)
	}
	if actor := portal.lastActor(); actor.Customer != alice.Email || actor.ViewerRole != "staff" || actor.Viewer != "staff@example.test" {
		t.Fatalf("staff ask about the owner's items: %+v", actor)
	}
	if refs, _ := a.Store.TicketReferences(ctx, id, false); len(refs) != 2 {
		t.Fatal("a note's tag reached the customer", refs)
	}
	if refs, _ := a.Store.TicketReferences(ctx, id, true); len(refs) != 3 {
		t.Fatal("staff should see the note's tag", refs)
	}
	// Emails name the item instead of its id.
	a.Store.MailEnabled = true
	mail := &fakeMailSender{}
	if got := request(h, "POST", staffPath+"/reply", "staff", url.Values{"csrf": {"csrf-staff"}, "body": {"已確認 $inv-a1"}}); got.Code != http.StatusSeeOther {
		t.Fatalf("reply: %d", got.Code)
	}
	sendDue(t, a, mailWorker(a, mail))
	if len(mail.sent) == 0 || !strings.Contains(mail.sent[len(mail.sent)-1].body, "已確認 [帳單：<b>inv-a1</b>]") {
		t.Fatalf("the email should name the tagged item: %+v", mail.sent)
	}
	// When the Portal is down the message still goes out, its tags as text.
	portal.mu.Lock()
	portal.down = true
	portal.mu.Unlock()
	if got := request(h, "POST", fmt.Sprintf("/tickets/%d/reply", id), "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"服務 !svc-a1 還是不行"}}); got.Code != http.StatusSeeOther {
		t.Fatalf("reply while the Portal is down: %d", got.Code)
	}
}

func TestTagSuggestionsAreTheOwnersItems(t *testing.T) {
	_, h, portal, alice, bob, _ := referenceTestApp(t)
	suggest := func(session, path string) string {
		t.Helper()
		got := request(h, "GET", path, session, nil)
		if got.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, got.Code)
		}
		return got.Body.String()
	}
	if found := suggest("bob", "/tickets/references?trigger=%24&q="); !strings.Contains(found, `data-insert="$inv-b1"`) || strings.Contains(found, "inv-a1") || strings.Contains(found, "inv-evil") {
		t.Fatal("bob sees only his own items", found)
	}
	if portal.lastActor().Customer != bob.Email {
		t.Fatal("asked for the wrong customer")
	}
	if got := request(h, "GET", "/tickets/references?trigger=%23", "bob", nil); got.Code != http.StatusNotFound {
		t.Fatal("an unknown trigger has no suggestions", got.Code)
	}
	created := request(h, "POST", "/tickets", "alice", url.Values{"csrf": {"csrf-alice"}, "subject": {"x"}, "category": {"other"}, "priority": {"low"}, "body": {"問題"}})
	var ticket int64
	fmt.Sscanf(created.Header().Get("Location"), "/tickets/%d", &ticket)
	if found := suggest("staff", fmt.Sprintf("/admin/tickets/%d/references?trigger=%%21&q=svc", ticket)); !strings.Contains(found, `data-insert="!svc-a1"`) || !strings.Contains(found, "&lt;b&gt;svc-a1&lt;/b&gt;") {
		t.Fatal("staff see the owner's items, escaped", found)
	}
	if actor := portal.lastActor(); actor.Customer != alice.Email || actor.ViewerRole != "staff" {
		t.Fatalf("staff search asked as %+v", actor)
	}
	if got := request(h, "GET", fmt.Sprintf("/admin/tickets/%d/references?trigger=%%24", ticket), "bob", nil); got.Code == http.StatusOK {
		t.Fatal("customers cannot search another customer's items")
	}
}

func TestPortalOpensTicketsAboutAnItemAndReturnsTags(t *testing.T) {
	a, h, key, _, _ := portalTestApp(t)
	portal := newFakePortal(t, key)
	a.References = &References{URL: portal.URL + "/refs", Key: key, Host: "support.example.test", Client: portal.Client()}
	if _, err := a.Store.Pool.Exec(context.Background(), `UPDATE users SET email_verified=true`); err != nil {
		t.Fatal(err)
	}
	h = a.Handler()
	actor := portalActor{Email: "alice@example.test", Name: "Alice", Hostname: portalTestHost}
	create := func(references string) *testResponse {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for k, v := range map[string]string{"subject": "服務異常", "body": "看 !svc-a1", "category": "technical", "priority": "low", "submission": token(), "references": references} {
			writer.WriteField(k, v)
		}
		writer.Close()
		return decodeResponse(portalSignedRequest(t, h, key, "vproxy-portal", "POST", "/api/portal/v1/tickets", actor, writer.FormDataContentType(), body.Bytes()))
	}
	if res := create(`[{"kind":"service","id":"bad id"}]`); res.Code != http.StatusUnprocessableEntity {
		t.Fatal("malformed references", res.Code)
	}
	res := create(`[{"kind":"invoice","id":"inv-a2"},{"kind":"invoice","id":"inv-b1"}]`)
	if res.Code != 200 {
		t.Fatal("create", res.Code, res.JSON)
	}
	id := int64(res.JSON["id"].(float64))
	detail := decodeResponse(portalRequest(t, h, key, "GET", fmt.Sprintf("/api/portal/v1/tickets/%d", id), actor))
	refs, _ := detail.JSON["references"].([]any)
	if len(refs) != 2 {
		t.Fatal("the named invoice and the tagged service, not bob's invoice", detail.JSON["references"])
	}
	tag := refs[0].(map[string]any)
	if tag["token"] == nil || tag["customerUrl"] == nil || tag["staffUrl"] != nil {
		t.Fatal("customers get the token and their own link only", tag)
	}
	messages, _ := detail.JSON["messages"].([]any)
	first, _ := messages[len(messages)-1].(map[string]any)
	if tagged, _ := first["references"].([]any); len(tagged) != 1 || tagged[0].(map[string]any)["token"] != "!svc-a1" {
		t.Fatal("each message lists the tags it writes", first["references"])
	}
}

func TestStaffSeeTheOwnersItemsBesideTheTicket(t *testing.T) {
	a, h, portal, alice, _, _ := referenceTestApp(t)
	ctx := context.Background()
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "x", Category: "other", Priority: "low"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/admin/tickets/%d/customer-items", id)
	if page := request(h, "GET", fmt.Sprintf("/admin/tickets/%d", id), "staff", nil).Body.String(); !strings.Contains(page, `hx-get="`+path+`"`) {
		t.Fatal("the staff sidebar should load the owner's items")
	}
	if page := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil).Body.String(); strings.Contains(page, "customer-items") {
		t.Fatal("customers do not get the staff list")
	}
	got := request(h, "GET", path, "staff", nil)
	body := got.Body.String()
	if got.Code != http.StatusOK || !strings.Contains(body, "客戶的帳單") || !strings.Contains(body, "客戶的服務") ||
		!strings.Contains(body, `href="`+portal.URL+`/admin/inv-a1" target="_blank"`) || !strings.Contains(body, `data-tag-insert="!svc-a1"`) ||
		strings.Contains(body, "inv-b1") || strings.Contains(body, "evil") || !strings.Contains(body, "&lt;b&gt;inv-a1&lt;/b&gt;") {
		t.Fatalf("staff should see the owner's items, escaped, linking to the Portal's staff pages: %d %s", got.Code, body)
	}
	if actor := portal.lastActor(); actor.Customer != alice.Email || actor.ViewerRole != "staff" {
		t.Fatalf("asked as %+v", actor)
	}
	if got := request(h, "GET", path, "alice", nil); got.Code == http.StatusOK {
		t.Fatal("customers cannot load the staff list")
	}
	portal.mu.Lock()
	portal.down = true
	portal.mu.Unlock()
	if body := request(h, "GET", path, "staff", nil).Body.String(); !strings.Contains(body, "暫時無法取得資料") {
		t.Fatal("a Portal outage should say so", body)
	}
	if _, err = a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=false WHERE id=$1`, alice.ID); err != nil {
		t.Fatal(err)
	}
	if body := request(h, "GET", path, "staff", nil).Body.String(); !strings.Contains(body, "尚未驗證") {
		t.Fatal("an unverified owner is not looked up", body)
	}
}
