package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"supportcenter/internal/model"
)

func TestCustomersFileUpToTheirPrioritySupport(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	ctx := context.Background()
	file := func(session, priority string) int {
		return request(h, "POST", "/tickets", session, url.Values{"csrf": {"csrf-" + session}, "subject": {"priority " + priority}, "category": {"other"}, "priority": {priority}, "body": {"問題"}}).Code
	}
	form := request(h, "GET", "/tickets/new", "alice", nil).Body.String()
	for _, want := range []string{`<option value="low">低</option>`, `<option value="medium" disabled>中 · 需升級</option>`, `<option value="urgent" disabled>緊急 · 需升級</option>`, "中、高、緊急需升級優先支援才能使用。"} {
		if !strings.Contains(form, want) {
			t.Errorf("new ticket form missing %s", want)
		}
	}
	if code := file("alice", "medium"); code != 422 {
		t.Fatal("a locked priority was accepted", code)
	}
	for _, priority := range []string{"normal", "", "critical"} {
		if code := file("alice", priority); code != 422 {
			t.Fatal("unknown priority accepted", priority, code)
		}
	}
	if code := file("alice", "low"); code != http.StatusSeeOther {
		t.Fatal("low priority rejected", code)
	}
	until := time.Now().Add(24 * time.Hour)
	if err := a.Store.SetPrioritySupport(ctx, alice.ID, model.PrioritySupport{Level: "high", Until: &until}); err != nil {
		t.Fatal(err)
	}
	if code := file("alice", "high"); code != http.StatusSeeOther {
		t.Fatal("bought priority rejected", code)
	}
	rejected := request(h, "POST", "/tickets", "alice", url.Values{"csrf": {"csrf-alice"}, "subject": {"too urgent"}, "category": {"other"}, "priority": {"urgent"}, "body": {"保留內容"}})
	if body := rejected.Body.String(); rejected.Code != 422 || !strings.Contains(body, "此優先級需升級優先支援") || !strings.Contains(body, "保留內容") || !strings.Contains(body, `<option value="low" selected>`) {
		t.Fatal("a priority above the bought level was accepted or lost the draft", rejected.Code)
	}
	if form = request(h, "GET", "/tickets/new", "alice", nil).Body.String(); !strings.Contains(form, `<option value="high">高</option>`) || !strings.Contains(form, "緊急需升級優先支援才能使用。") || !strings.Contains(form, "優先支援有效至") {
		t.Fatal("the form does not reflect the bought level")
	}
	past := time.Now().Add(-time.Minute)
	if err := a.Store.SetPrioritySupport(ctx, alice.ID, model.PrioritySupport{Level: "high", Until: &past}); err != nil {
		t.Fatal(err)
	}
	if code := file("alice", "high"); code != 422 {
		t.Fatal("an ended upgrade still applies", code)
	}
	if code := file("staff", "urgent"); code != http.StatusSeeOther {
		t.Fatal("staff are limited", code)
	}
	var priorities []string
	rows, _ := a.Store.Pool.Query(ctx, `SELECT priority FROM tickets ORDER BY id`)
	for rows.Next() {
		var priority string
		rows.Scan(&priority)
		priorities = append(priorities, priority)
	}
	if strings.Join(priorities, ",") != "low,high,urgent" {
		t.Fatal("stored priorities", priorities)
	}
}

func TestAdminsSetPrioritySupport(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	ctx := context.Background()
	path := fmt.Sprintf("/admin/users/%d/priority-support", alice.ID)
	set := func(session string, form url.Values) int {
		form.Set("csrf", "csrf-"+session)
		return request(h, "POST", path, session, form).Code
	}
	if code := set("staff", url.Values{"level": {"urgent"}}); code != http.StatusNotFound {
		t.Fatal("support staff changed priority support", code)
	}
	res := request(h, "POST", path, "admin", url.Values{"csrf": {"csrf-admin"}, "level": {"high"}, "until": {"2027-01-31"}, "q": {"alice"}, "page": {"2"}})
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/admin/users?page=2&q=alice" {
		t.Fatal("save failed", res.Code, res.Header().Get("Location"))
	}
	support, _ := a.Store.PrioritySupport(ctx, alice.ID)
	taipei, _ := time.LoadLocation("Asia/Taipei")
	if support.Level != "high" || support.Until == nil || !support.Until.Equal(time.Date(2027, 2, 1, 0, 0, 0, 0, taipei)) {
		t.Fatal("stored", support.Level, support.Until)
	}
	page := request(h, "GET", "/admin/users?q=alice", "admin", nil).Body.String()
	for _, want := range []string{`<span class="priority priority-high">高</span>`, "至 2027/01/31", `name="until" value="2027-01-31"`} {
		if !strings.Contains(page, want) {
			t.Errorf("users page missing %s", want)
		}
	}
	// Saving an untouched date keeps the exact end the store set.
	exact := time.Date(2027, 3, 1, 3, 4, 5, 0, time.UTC)
	a.Store.SetPrioritySupport(ctx, alice.ID, model.PrioritySupport{Level: "high", Until: &exact})
	if code := set("admin", url.Values{"level": {"urgent"}, "until": {"2027-03-01"}, "shown_until": {"2027-03-01"}}); code != http.StatusSeeOther {
		t.Fatal(code)
	}
	if support, _ = a.Store.PrioritySupport(ctx, alice.ID); support.Level != "urgent" || !support.Until.Equal(exact) {
		t.Fatal("untouched date moved", support.Level, support.Until)
	}
	if code := set("admin", url.Values{"level": {"medium"}}); code != http.StatusSeeOther {
		t.Fatal(code)
	}
	if support, _ = a.Store.PrioritySupport(ctx, alice.ID); support.Level != "medium" || support.Until != nil {
		t.Fatal("an empty date must mean no end", support.Level, support.Until)
	}
	set("admin", url.Values{"level": {"low"}, "until": {"2027-01-31"}})
	if support, _ = a.Store.PrioritySupport(ctx, alice.ID); support.Level != "low" || support.Until != nil {
		t.Fatal("low kept an end date", support.Level, support.Until)
	}
	for _, form := range []url.Values{{"level": {"normal"}}, {"level": {"high"}, "until": {"31/01/2027"}}, {"level": {"high"}, "until": {"1999-12-31"}}} {
		if code := set("admin", form); code != 422 {
			t.Error("accepted", form, code)
		}
	}
	if page = request(h, "GET", "/admin/users?q=staff", "admin", nil).Body.String(); !strings.Contains(page, "客服不受限") || strings.Contains(page, "/priority-support") {
		t.Fatal("staff rows offer priority support")
	}
}

func TestPortalStoreSetsPrioritySupport(t *testing.T) {
	a, h, key, _, _ := portalTestApp(t)
	ctx := context.Background()
	buyer := portalActor{Email: "Buyer@Example.test", Name: "Buyer", Hostname: portalTestHost}
	store := func(issuer string, input any) *testResponse {
		raw, _ := json.Marshal(input)
		return decodeResponse(portalSignedRequest(t, h, key, issuer, "POST", "/api/portal/v1/store/priority-support", buyer, "application/json", raw))
	}
	until := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	// Requests the Portal relays for members carry its usual issuer and never reach the store.
	if res := store("vproxy-portal", map[string]any{"level": "urgent"}); res.Code != http.StatusUnauthorized {
		t.Fatal("member issuer reached the store", res.Code)
	}
	if res := decodeResponse(portalSignedRequest(t, h, key, portalStoreIssuer, "GET", "/api/portal/v1/metadata", buyer, "", nil)); res.Code != http.StatusUnauthorized {
		t.Fatal("store issuer reached a member endpoint", res.Code)
	}
	res := store(portalStoreIssuer, map[string]any{"level": "high", "until": until})
	if res.Code != 200 || res.JSON["current"] != "high" || fmt.Sprint(res.JSON["allowedPriorities"]) != "[low medium high]" {
		t.Fatal("store upgrade failed", res.Code, res.JSON)
	}
	for _, input := range []any{map[string]any{"level": "normal"}, map[string]any{"level": "high", "until": "2027-01-31"}, map[string]any{"level": "high", "extra": true}} {
		if res = store(portalStoreIssuer, input); res.Code != 422 && res.Code != 400 {
			t.Error("accepted", input, res.Code)
		}
	}
	metadata := decodeResponse(portalRequest(t, h, key, "GET", "/api/portal/v1/metadata", buyer))
	support, _ := metadata.JSON["prioritySupport"].(map[string]any)
	end, _ := time.Parse(time.RFC3339, fmt.Sprint(support["until"]))
	if fmt.Sprint(metadata.JSON["priorities"]) != "[low medium high urgent]" || support["current"] != "high" || !end.Equal(until) {
		t.Fatal("metadata", metadata.JSON["priorities"], support)
	}
	create := func(priority string) *testResponse {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for k, v := range map[string]string{"subject": "needs " + priority, "body": "問題", "category": "other", "priority": priority, "submission": token()} {
			writer.WriteField(k, v)
		}
		writer.Close()
		return decodeResponse(portalSignedRequest(t, h, key, "vproxy-portal", "POST", "/api/portal/v1/tickets", buyer, writer.FormDataContentType(), body.Bytes()))
	}
	if res = create("urgent"); res.Code != http.StatusForbidden || res.JSON["error"] != "priority_locked" {
		t.Fatal("urgent above the bought level", res.Code, res.JSON)
	}
	for _, priority := range []string{"high", "normal"} {
		if res = create(priority); res.Code != 200 {
			t.Fatal("create failed", priority, res.Code, res.JSON)
		}
	}
	var stored []string
	rows, _ := a.Store.Pool.Query(ctx, `SELECT t.priority FROM tickets t JOIN users u ON u.id=t.user_id WHERE u.email='buyer@example.test' ORDER BY t.id`)
	for rows.Next() {
		var priority string
		rows.Scan(&priority)
		stored = append(stored, priority)
	}
	if strings.Join(stored, ",") != "high,low" {
		t.Fatal("older Portals' normal must file as low", stored)
	}
	if res = store(portalStoreIssuer, map[string]any{"level": "low", "until": until}); res.Code != 200 || res.JSON["until"] != nil || res.JSON["current"] != "low" {
		t.Fatal("removing priority support", res.Code, res.JSON)
	}
	account := decodeResponse(portalRequest(t, h, key, "GET", "/api/portal/v1/account", buyer))
	if support, _ = account.JSON["prioritySupport"].(map[string]any); support["current"] != "low" || fmt.Sprint(support["allowedPriorities"]) != "[low]" {
		t.Fatal("account", support)
	}
}

type testResponse struct {
	Code int
	JSON map[string]any
}

func decodeResponse(w interface {
	Result() *http.Response
}) *testResponse {
	res := w.Result()
	out := &testResponse{Code: res.StatusCode}
	json.NewDecoder(res.Body).Decode(&out.JSON)
	return out
}
