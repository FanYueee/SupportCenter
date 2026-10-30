package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"supportcenter/internal/model"
	"supportcenter/internal/notify"
)

const (
	discordTestID    = "123456789012345678"
	discordTestToken = "secret-Token_value"
	discordTestURL   = "https://discord.com/api/webhooks/" + discordTestID + "/" + discordTestToken
)

// discordServer stands in for discord.com and records each webhook post.
type discordServer struct {
	*httptest.Server
	mu       sync.Mutex
	paths    []string
	messages []map[string]any
	reply    func(w http.ResponseWriter)
}

func newDiscordServer(t *testing.T) *discordServer {
	d := &discordServer{reply: func(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var message map[string]any
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || json.Unmarshal(raw, &message) != nil {
			t.Errorf("unexpected webhook request: %s %s", r.Method, raw)
		}
		d.mu.Lock()
		d.paths, d.messages = append(d.paths, r.URL.Path), append(d.messages, message)
		reply := d.reply
		d.mu.Unlock()
		reply(w)
	}))
	t.Cleanup(d.Close)
	return d
}

func (d *discordServer) client() notify.Discord {
	return notify.Discord{BaseURL: d.URL, Client: d.Client()}
}

func embeds(message map[string]any) []map[string]any {
	var out []map[string]any
	for _, e := range message["embeds"].([]any) {
		out = append(out, e.(map[string]any))
	}
	return out
}

func saveDiscord(t *testing.T, h http.Handler, form url.Values) int {
	t.Helper()
	form.Set("csrf", "csrf-admin")
	return request(h, "POST", "/admin/discord", "admin", form).Code
}

func TestDiscordSettingsAreAdminOnlyAndHideToken(t *testing.T) {
	a, h, _, _, _ := testApp(t)
	ctx := context.Background()
	if res := request(h, "GET", "/admin/discord", "staff", nil); res.Code != http.StatusNotFound {
		t.Fatal("support staff opened Discord settings", res.Code)
	}
	if res := request(h, "POST", "/admin/discord", "staff", url.Values{"csrf": {"csrf-staff"}, "webhook_url": {discordTestURL}}); res.Code != http.StatusNotFound {
		t.Fatal("support staff changed Discord settings", res.Code)
	}
	if res := request(h, "POST", "/admin/discord", "admin", url.Values{"webhook_url": {discordTestURL}}); res.Code != http.StatusForbidden {
		t.Fatal("settings saved without CSRF token", res.Code)
	}
	for _, bad := range []string{"http://discord.com/api/webhooks/1/x", "https://evil.example/api/webhooks/1/x", discordTestURL + "?wait=true"} {
		if code := saveDiscord(t, h, url.Values{"webhook_url": {bad}, "on_ticket": {"on"}}); code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid webhook %q: %d", bad, code)
		}
	}
	if id, _, _, _ := a.Store.DiscordWebhook(ctx); id != "" {
		t.Fatal("invalid webhook was stored")
	}
	if code := saveDiscord(t, h, url.Values{"webhook_url": {discordTestURL}, "on_ticket": {"on"}, "on_reply": {"on"}, "include_preview": {"on"}}); code != http.StatusSeeOther {
		t.Fatal("valid webhook rejected", code)
	}
	page := request(h, "GET", "/admin/discord?saved=1", "admin", nil).Body.String()
	if !strings.Contains(page, discordTestID) || strings.Contains(page, discordTestToken) || !strings.Contains(page, `action="/admin/discord/test"`) {
		t.Fatal("settings page must show the webhook ID and never its token")
	}
	// A blank URL keeps the saved webhook; the switches still change.
	if code := saveDiscord(t, h, url.Values{"on_ticket": {"on"}}); code != http.StatusSeeOther {
		t.Fatal("saving switches failed", code)
	}
	settings, _ := a.Store.DiscordSettings(ctx)
	if id, token, preview, _ := a.Store.DiscordWebhook(ctx); id != discordTestID || token != discordTestToken || preview || !settings.OnTicket || settings.OnReply {
		t.Fatalf("blank URL changed the webhook or ignored switches: %+v", settings)
	}
	if code := saveDiscord(t, h, url.Values{"remove_webhook": {"on"}}); code != http.StatusSeeOther {
		t.Fatal("removing webhook failed", code)
	}
	if id, token, _, _ := a.Store.DiscordWebhook(ctx); id != "" || token != "" {
		t.Fatal("webhook was not removed")
	}
}

func discordQueue(t *testing.T, a *App) []string {
	t.Helper()
	rows, err := a.Store.Pool.Query(context.Background(), `SELECT kind FROM discord_notifications ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	kinds := []string{}
	for rows.Next() {
		var kind string
		rows.Scan(&kind)
		kinds = append(kinds, kind)
	}
	return kinds
}

func TestDiscordQueuesNewTicketsAndCustomerReplies(t *testing.T) {
	a, h, alice, bob, _ := testApp(t)
	ctx := context.Background()
	if _, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "before setup", Category: "other", Priority: "low"}, "問題", nil); err != nil {
		t.Fatal(err)
	}
	if kinds := discordQueue(t, a); len(kinds) != 0 {
		t.Fatal("queued without a webhook", kinds)
	}
	saveDiscord(t, h, url.Values{"webhook_url": {discordTestURL}, "on_ticket": {"on"}, "on_reply": {"on"}})
	if err := a.Store.SetPrioritySupport(ctx, alice.ID, model.PrioritySupport{Level: "urgent"}); err != nil {
		t.Fatal(err)
	}
	created := request(h, "POST", "/tickets", "alice", url.Values{"csrf": {"csrf-alice"}, "subject": {"needs help"}, "category": {"technical"}, "priority": {"urgent"}, "body": {"問題"}, "participants": {bob.Email}})
	if created.Code != http.StatusSeeOther {
		t.Fatal("create failed", created.Code)
	}
	path := created.Header().Get("Location")
	request(h, "POST", path+"/reply", "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"owner reply"}})
	if _, err := a.Store.Pool.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, bob.ID); err != nil {
		t.Fatal(err)
	}
	request(h, "POST", path+"/reply", "bob", url.Values{"csrf": {"csrf-bob"}, "body": {"participant reply"}})
	// Staff replies and internal notes are not customer activity.
	request(h, "POST", "/admin"+path+"/reply", "staff", url.Values{"csrf": {"csrf-staff"}, "body": {"staff reply"}})
	request(h, "POST", "/admin"+path+"/reply", "staff", url.Values{"csrf": {"csrf-staff"}, "body": {"internal note"}, "internal": {"true"}})
	if kinds := discordQueue(t, a); strings.Join(kinds, ",") != "ticket,reply,reply" {
		t.Fatalf("queued events: %v", kinds)
	}
	// Each event can be switched off.
	saveDiscord(t, h, url.Values{})
	request(h, "POST", path+"/reply", "alice", url.Values{"csrf": {"csrf-alice"}, "body": {"muted reply"}})
	request(h, "POST", "/tickets", "alice", url.Values{"csrf": {"csrf-alice"}, "subject": {"muted"}, "category": {"other"}, "priority": {"low"}, "body": {"問題"}})
	if kinds := discordQueue(t, a); len(kinds) != 3 {
		t.Fatalf("disabled events were queued: %v", kinds)
	}
}

func discordWorker(a *App, d *discordServer) *notify.DiscordWorker {
	return &notify.DiscordWorker{Store: a.Store, Discord: d.client(), Origin: a.Origin, Site: a.Site, Log: a.Log}
}

func TestDiscordWorkerDeliversEscapedBatches(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	ctx := context.Background()
	d := newDiscordServer(t)
	a.Site.Name = "Example Cloud"
	worker := discordWorker(a, d)
	saveDiscord(t, h, url.Values{"webhook_url": {discordTestURL}, "on_ticket": {"on"}, "on_reply": {"on"}, "include_preview": {"on"}})
	first, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "**Down** _now_", Category: "technical", Priority: "urgent"}, "@everyone **bold** [click](https://evil.example)\n> quoted", nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 11; i++ {
		if _, err = a.Store.Create(ctx, alice, model.Ticket{Subject: strings.Repeat("長", 160), Category: "other", Priority: "low"}, strings.Repeat("內容", 2000), nil); err != nil {
			t.Fatal(err)
		}
	}
	for worked := true; worked; {
		if worked, err = worker.ProcessOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	total := 0
	for i, message := range d.messages {
		if d.paths[i] != "/api/webhooks/"+discordTestID+"/"+discordTestToken {
			t.Fatal("posted to", d.paths[i])
		}
		if parse := message["allowed_mentions"].(map[string]any)["parse"].([]any); len(parse) != 0 {
			t.Fatal("mentions allowed", parse)
		}
		size := 0
		for _, e := range embeds(message) {
			size += len([]rune(fmt.Sprint(e["title"], e["description"])))
		}
		if n := len(embeds(message)); n > 10 || size > 6000 {
			t.Fatalf("message over Discord limits: %d embeds, %d characters", n, size)
		}
		total += len(embeds(message))
	}
	if len(d.messages) < 2 || total != 12 {
		t.Fatalf("12 events in %d messages, %d embeds", len(d.messages), total)
	}
	e := embeds(d.messages[0])[0]
	wantTitle := fmt.Sprintf(`#%04d \*\*Down\*\* \_now\_`, first)
	wantDescription := `\@everyone \*\*bold\*\* \[click\]\(https://evil.example\) \> quoted`
	if e["title"] != wantTitle || e["url"] != fmt.Sprintf("%s/admin/tickets/%d", a.Origin, first) || e["description"] != wantDescription || e["color"] != float64(0xDC2626) {
		t.Fatalf("embed: %v", e)
	}
	if author := e["author"].(map[string]any)["name"]; author != "新客服單" {
		t.Fatal("event label", author)
	}
	// A channel shared by several sites can tell them apart.
	if footer := e["footer"].(map[string]any)["text"]; footer != "Example Cloud 支援中心" {
		t.Fatal("site label", footer)
	}
	var unsent int
	var lastSent *time.Time
	a.Store.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM discord_notifications WHERE state<>'sent'),(SELECT last_sent_at FROM discord_settings)`).Scan(&unsent, &lastSent)
	if unsent != 0 || lastSent == nil {
		t.Fatal("delivered events not recorded", unsent, lastSent)
	}
	// Without previews only the summary fields are sent.
	saveDiscord(t, h, url.Values{"on_reply": {"on"}})
	if err = a.Store.Reply(ctx, alice, first, false, "private details", false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = worker.ProcessOnce(ctx); err != nil {
		t.Fatal(err)
	}
	last := embeds(d.messages[len(d.messages)-1])[0]
	if _, ok := last["description"]; ok || last["author"].(map[string]any)["name"] != "客戶回覆" {
		t.Fatalf("preview sent while disabled: %v", last)
	}
}

func TestDiscordWorkerRetriesAndReportsFailures(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	ctx := context.Background()
	d := newDiscordServer(t)
	worker := discordWorker(a, d)
	saveDiscord(t, h, url.Values{"webhook_url": {discordTestURL}, "on_ticket": {"on"}})
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "retry", Category: "other", Priority: "low"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	state := func() (s string, attempts int, ready bool) {
		a.Store.Pool.QueryRow(ctx, `SELECT state,attempts,available_at<=now() FROM discord_notifications WHERE ticket_id=$1`, id).Scan(&s, &attempts, &ready)
		return
	}
	// Rate limits wait without using up an attempt.
	d.reply = func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"retry_after":2.5,"global":false}`)
	}
	if _, err = worker.ProcessOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if s, attempts, ready := state(); s != "pending" || attempts != 0 || ready {
		t.Fatalf("rate limited event: %s %d ready=%v", s, attempts, ready)
	}
	if processed, _ := worker.ProcessOnce(ctx); processed {
		t.Fatal("rate limited event was retried early")
	}
	// Discord being down is retried later; a deleted webhook is not.
	a.Store.Pool.Exec(ctx, `UPDATE discord_notifications SET available_at=now()`)
	d.reply = func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) }
	worker.ProcessOnce(ctx)
	settings, _ := a.Store.DiscordSettings(ctx)
	if s, attempts, ready := state(); s != "pending" || attempts != 1 || ready || settings.LastError != "unavailable" {
		t.Fatalf("server error: %s %d ready=%v %q", s, attempts, ready, settings.LastError)
	}
	a.Store.Pool.Exec(ctx, `UPDATE discord_notifications SET available_at=now()`)
	d.reply = func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) }
	worker.ProcessOnce(ctx)
	settings, _ = a.Store.DiscordSettings(ctx)
	if s, _, _ := state(); s != "failed" || settings.LastError != "not_found" {
		t.Fatalf("deleted webhook: %s %q", s, settings.LastError)
	}
	page := request(h, "GET", "/admin/discord", "admin", nil).Body.String()
	if !strings.Contains(page, "Discord 找不到這個 Webhook") {
		t.Fatal("settings page does not explain the failure")
	}
	// Removing the webhook drops what is still queued.
	if _, err = a.Store.Create(ctx, alice, model.Ticket{Subject: "queued", Category: "other", Priority: "low"}, "問題", nil); err != nil {
		t.Fatal(err)
	}
	saveDiscord(t, h, url.Values{"remove_webhook": {"on"}})
	var pending int
	a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM discord_notifications WHERE state IN ('pending','sending')`).Scan(&pending)
	if pending != 0 {
		t.Fatal("queued events kept after removing the webhook", pending)
	}
}

func TestDiscordTestMessage(t *testing.T) {
	a, h, _, _, _ := testApp(t)
	d := newDiscordServer(t)
	a.Discord = d.client()
	send := func() *httptest.ResponseRecorder {
		return request(h, "POST", "/admin/discord/test", "admin", url.Values{"csrf": {"csrf-admin"}})
	}
	if res := send(); res.Code != http.StatusUnprocessableEntity {
		t.Fatal("test sent without a webhook", res.Code)
	}
	if res := request(h, "POST", "/admin/discord/test", "staff", url.Values{"csrf": {"csrf-staff"}}); res.Code != http.StatusNotFound {
		t.Fatal("support staff sent a test message", res.Code)
	}
	saveDiscord(t, h, url.Values{"webhook_url": {discordTestURL}})
	if res := send(); res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/admin/discord?test=ok" || len(d.messages) != 1 {
		t.Fatal("test message failed", res.Code, res.Header().Get("Location"))
	}
	if title := embeds(d.messages[0])[0]["title"]; title != "支援中心已連線" {
		t.Fatal("test message title", title)
	}
	if page := request(h, "GET", "/admin/discord?test=ok", "admin", nil).Body.String(); !strings.Contains(page, "測試訊息已送出") {
		t.Fatal("success not shown")
	}
	d.reply = func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) }
	if res := send(); res.Header().Get("Location") != "/admin/discord?test=failed" {
		t.Fatal("failed test reported as success", res.Header().Get("Location"))
	}
	if page := request(h, "GET", "/admin/discord?test=failed", "admin", nil).Body.String(); !strings.Contains(page, "Discord 找不到這個 Webhook") {
		t.Fatal("failure reason not shown")
	}
}
