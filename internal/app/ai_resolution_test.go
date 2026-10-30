package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"supportcenter/internal/ai"
	"supportcenter/internal/model"
)

func TestAIResolutionPermissionAndFollowUp(t *testing.T) {
	a, h, alice, bob, staff := testApp(t)
	ctx := context.Background()
	settings, _ := a.Store.Automation(ctx)
	settings.AutoCloseDays = 3
	if err := a.Store.SaveAutomation(ctx, settings); err != nil {
		t.Fatal(err)
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"請確認問題是否解決。"}}]}`)
	}))
	defer provider.Close()
	worker := &ai.Worker{Store: a.Store, Key: "test-key", Endpoint: provider.URL, Client: provider.Client()}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "AI confirmation", Category: "technical", Priority: "low", AssistanceMode: "ai"}, "need help", nil)
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/tickets/%d/resolve", id)
	ticket, _ := a.Store.Ticket(ctx, alice, id, false)
	form := url.Values{"csrf": {"csrf-alice"}, "version": {strconv.FormatInt(ticket.Version, 10)}}
	if w := request(h, "POST", path, "alice", form); w.Code != 422 {
		t.Fatalf("confirmed unfinished AI job: %d", w.Code)
	}
	if _, err = worker.ProcessOnce(ctx); err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	form.Set("version", strconv.FormatInt(ticket.Version, 10))
	if w := request(h, "POST", path, "", form); w.Code != 303 {
		t.Fatalf("unauthenticated confirmation: %d", w.Code)
	}
	if w := request(h, "POST", path, "alice", url.Values{"version": {form.Get("version")}}); w.Code != 403 {
		t.Fatalf("missing CSRF: %d", w.Code)
	}
	if err = a.Store.Invite(ctx, alice, id, bob.Email); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"bob", "staff"} {
		other := url.Values{"csrf": {"csrf-" + user}, "version": {form.Get("version")}}
		if w := request(h, "POST", path, user, other); w.Code != 404 {
			t.Fatalf("%s confirmed owner's ticket: %d", user, w.Code)
		}
	}
	// A new reply invalidates the old answer's confirmation button.
	if err = a.Store.Reply(ctx, alice, id, false, "still need help", false, nil); err != nil {
		t.Fatal(err)
	}
	if w := request(h, "POST", path, "alice", form); w.Code != 409 {
		t.Fatalf("stale answer confirmed: %d", w.Code)
	}
	if _, err = worker.ProcessOnce(ctx); err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	form.Set("version", strconv.FormatInt(ticket.Version, 10))
	if w := request(h, "POST", path, "alice", form); w.Code != 303 {
		t.Fatalf("confirmation: %d %s", w.Code, w.Body.String())
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.Status != "solved" || ticket.AssistanceMode != "ai" || ticket.AutoCloseAt == nil {
		t.Fatalf("resolved state/countdown: %+v", ticket)
	}
	if items, _, err := a.Store.Notifications(ctx, alice.ID, 1); err != nil || len(items) != 1 {
		t.Fatalf("resolution notice: %+v %v", items, err)
	}
	if body := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil).Body.String(); strings.Contains(body, "問題解決了嗎") || !strings.Contains(body, "預計結案") {
		t.Fatal("completed AI ticket still asks for confirmation")
	}
	// Resolution retains AI mode: a subsequent public reply starts another AI turn.
	if err = a.Store.Reply(ctx, alice, id, false, "one more question", false, nil); err != nil {
		t.Fatal(err)
	}
	if processed, err := worker.ProcessOnce(ctx); err != nil || !processed {
		t.Fatalf("AI follow-up after resolution: %v %v", processed, err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	if ticket.Status != "waiting" || ticket.AssistanceMode != "ai" || ticket.AutoCloseAt != nil {
		t.Fatalf("AI follow-up state: %+v", ticket)
	}
	// A real staff reply takes over; the customer's old AI action cannot resolve it.
	if err = a.Store.Reply(ctx, staff, id, true, "staff answer", false, nil); err != nil {
		t.Fatal(err)
	}
	ticket, _ = a.Store.Ticket(ctx, alice, id, false)
	form.Set("version", strconv.FormatInt(ticket.Version, 10))
	if w := request(h, "POST", path, "alice", form); w.Code != 422 {
		t.Fatalf("AI action resolved human ticket: %d", w.Code)
	}
}
