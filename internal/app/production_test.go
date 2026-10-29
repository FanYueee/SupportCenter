package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"strings"
	"supportcenter/internal/database"
	"supportcenter/internal/model"
	"sync"
	"testing"
	"time"
)

func TestRetryAndConcurrentUpdates(t *testing.T) {
	a, h, alice, _, staff := testApp(t)
	ctx := context.Background()
	key := token()
	form := url.Values{"csrf": {"csrf-alice"}, "submission": {key}, "subject": {"retry-safe ticket"}, "category": {"technical"}, "priority": {"normal"}, "body": {"first message"}}
	var wg sync.WaitGroup
	results := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(h, "POST", "/tickets", "alice", form)
			results <- fmt.Sprintf("%d:%s", w.Code, w.Header().Get("Location"))
		}()
	}
	wg.Wait()
	close(results)
	var first string
	for result := range results {
		if !strings.HasPrefix(result, "303:") {
			t.Fatal(result)
		}
		if first == "" {
			first = result
		} else if result != first {
			t.Fatal("duplicate request created a second ticket")
		}
	}
	tickets, _, _, err := a.Store.List(ctx, alice, false, "", "", "", 1, "")
	if err != nil || len(tickets) != 1 {
		t.Fatal("ticket retry was not deduplicated")
	}
	id := tickets[0].ID
	op := model.Operation{Key: token(), Hash: hash("same reply")}
	if err = a.Store.Reply(ctx, alice, id, false, "reply", false, nil, op); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Reply(ctx, alice, id, false, "reply", false, nil, op); !errors.Is(err, database.ErrReplayed) {
		t.Fatal("reply replay not detected", err)
	}
	changed := op
	changed.Hash = hash("different reply")
	if err = a.Store.Reply(ctx, alice, id, false, "different", false, nil, changed); !errors.Is(err, database.ErrConflict) {
		t.Fatal("reused key changed content")
	}
	messages, _, err := a.Store.Messages(ctx, alice, id, false, 1)
	if err != nil || len(messages) != 2 {
		t.Fatal("duplicate messages")
	}
	if err = a.Store.Update(ctx, staff, id, true, "closed", "urgent", &staff.ID, tickets[0].Version); !errors.Is(err, database.ErrConflict) {
		t.Fatal("stale update accepted")
	}
	fresh, err := a.Store.Ticket(ctx, alice, id, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "closed", "urgent", &staff.ID, fresh.Version); err != nil {
		t.Fatal(err)
	}
	events, err := a.Store.Events(ctx, staff, id)
	if err != nil || len(events) != 4 {
		t.Fatal("status/priority/assignment not recorded", err, len(events))
	}
	if _, err = a.Store.Events(ctx, alice, id); !errors.Is(err, database.ErrNotFound) {
		t.Fatal("customer accessed staff audit")
	}
	if err = a.Store.Update(ctx, staff, id, true, "open", "low", nil, fresh.Version); !errors.Is(err, database.ErrConflict) {
		t.Fatal("stale update overwrote committed state")
	}
	after, _ := a.Store.Ticket(ctx, alice, id, false)
	if after.Status != "closed" || after.Priority != "urgent" {
		t.Fatal("conflict changed ticket")
	}
}
func TestUnchangedTicketSaveKeepsPositionAndVersion(t *testing.T) {
	a, _, alice, _, staff := testApp(t)
	ctx := context.Background()
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "unchanged", Category: "technical", Priority: "normal"}, "details", nil)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := a.Store.Ticket(ctx, alice, id, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, staff, id, true, initial.Status, initial.Priority, nil, initial.Version); err != nil {
		t.Fatal(err)
	}
	if err = a.Store.Update(ctx, alice, id, false, initial.Status, "urgent", &staff.ID, initial.Version); err != nil {
		t.Fatal(err)
	}
	after, err := a.Store.Ticket(ctx, alice, id, false)
	if err != nil || after.Version != initial.Version || !after.UpdatedAt.Equal(initial.UpdatedAt) || after.Priority != initial.Priority {
		t.Fatal("unchanged save modified the ticket", err)
	}
	events, err := a.Store.Events(ctx, staff, id)
	if err != nil || len(events) != 0 {
		t.Fatal("unchanged save created audit events", err)
	}
	if err = a.Store.Update(ctx, staff, id, true, "closed", "urgent", &staff.ID, initial.Version); err != nil {
		t.Fatal(err)
	}
	changed, err := a.Store.Ticket(ctx, alice, id, false)
	if err != nil || changed.Version != initial.Version+1 || changed.Status != "closed" || changed.Priority != "urgent" {
		t.Fatal("real change was not recorded", err)
	}
	if err = a.Store.Update(ctx, staff, id, true, changed.Status, changed.Priority, &staff.ID, initial.Version); !errors.Is(err, database.ErrConflict) {
		t.Fatal("stale no-op save bypassed conflict check", err)
	}
}
func TestMigrationVersions(t *testing.T) {
	a, _, _, _, _ := testApp(t)
	ctx := context.Background()
	if err := a.Store.Migrate(ctx); err != nil {
		t.Fatal("migration rerun failed", err)
	}
	var count int
	if err := a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil || count != 25 {
		t.Fatal("migration ledger incorrect")
	}
	if _, err := a.Store.Pool.Exec(ctx, `UPDATE schema_migrations SET checksum='changed' WHERE name='001_init.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.Migrate(ctx); err == nil {
		t.Fatal("changed migration accepted")
	}
}
func TestDraftAndExpiredSession(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	id, err := a.Store.Create(context.Background(), alice, model.Ticket{Subject: "draft", Category: "other", Priority: "normal"}, "initial", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/tickets/%d/reply", id)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range map[string]string{"csrf": "csrf-alice", "submission": token(), "body": "保留這段回覆"} {
		writer.WriteField(k, v)
	}
	file, _ := writer.CreateFormFile("attachments", "fake.png")
	file.Write([]byte("not an image"))
	writer.Close()
	r := httptest.NewRequest("POST", a.Origin+path, &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.AddCookie(cookie("__Host-support-session", "alice", 1))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "保留這段回覆") {
		t.Fatal("native form lost draft")
	}
	a.Store.Pool.Exec(context.Background(), `UPDATE sessions SET expires_at=$1 WHERE token_hash=$2`, time.Now().Add(-time.Hour), hash("alice"))
	r = httptest.NewRequest("POST", a.Origin+path, strings.NewReader("body=keep"))
	r.Header.Set("HX-Request", "true")
	r.AddCookie(cookie("__Host-support-session", "alice", 1))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || w.Header().Get("HX-Redirect") != "" {
		t.Fatal("expired HTMX submission discarded draft by redirecting")
	}
}
