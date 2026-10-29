package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"supportcenter/internal/ai"
	"supportcenter/internal/model"
)

func TestAIInterruptedFinalAttemptDoesNotBlockQueue(t *testing.T) {
	a, _, alice, _, _ := testApp(t)
	ctx := context.Background()
	calls := 0
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"choices":[{"finish_reason":"stop","message":{"content":"已收到。"}}]}`)
	}))
	defer provider.Close()
	worker := &ai.Worker{Store: a.Store, Key: "test-key", Endpoint: provider.URL, Client: provider.Client()}
	stuck, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "stuck", Category: "other", Priority: "normal", AssistanceMode: "ai"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "next", Category: "other", Priority: "normal", AssistanceMode: "ai"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	// The final attempt was leased and then interrupted, e.g. by a restart.
	if _, err := a.Store.Pool.Exec(ctx, `UPDATE ai_jobs SET attempts=3,state='processing',lease_until=now()-interval '1 second' WHERE ticket_id=$1`, stuck); err != nil {
		t.Fatal(err)
	}
	if processed, err := worker.ProcessOnce(ctx); err != nil || !processed || calls != 0 {
		t.Fatalf("exhausted job: %v %v calls=%d", processed, err, calls)
	}
	if ticket, _ := a.Store.Ticket(ctx, alice, stuck, false); ticket.AssistanceMode != "human" {
		t.Fatal("exhausted job was not handed to a person")
	}
	if processed, err := worker.ProcessOnce(ctx); err != nil || !processed || calls != 1 {
		t.Fatalf("queue blocked behind the exhausted job: %v %v calls=%d", processed, err, calls)
	}
	if ticket, _ := a.Store.Ticket(ctx, alice, next, false); ticket.Status != "waiting" {
		t.Fatal("next job was not answered", ticket.Status)
	}
}

func TestAIDailyReplyCapHandsOff(t *testing.T) {
	a, _, alice, _, _ := testApp(t)
	ctx := context.Background()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("model called beyond the daily cap")
	}))
	defer provider.Close()
	worker := &ai.Worker{Store: a.Store, Key: "test-key", Endpoint: provider.URL, Client: provider.Client()}
	earlier, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "earlier", Category: "other", Priority: "normal"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Store.Pool.Exec(ctx, `INSERT INTO ticket_messages(ticket_id,user_id,body,internal,source) SELECT $1,$2,'答覆',false,'ai' FROM generate_series(1,30)`, earlier, alice.ID); err != nil {
		t.Fatal(err)
	}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "over cap", Category: "other", Priority: "normal", AssistanceMode: "ai"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	if processed, err := worker.ProcessOnce(ctx); err != nil || !processed {
		t.Fatalf("capped job: %v %v", processed, err)
	}
	if ticket, _ := a.Store.Ticket(ctx, alice, id, false); ticket.AssistanceMode != "human" || ticket.Status != "open" {
		t.Fatalf("over-cap ticket not handed to a person: %+v", ticket)
	}
}

func TestAICutOffAnswerIsNotPosted(t *testing.T) {
	a, _, alice, _, _ := testApp(t)
	ctx := context.Background()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"finish_reason":"length","message":{"content":"答案寫到一半"}}]}`)
	}))
	defer provider.Close()
	worker := &ai.Worker{Store: a.Store, Key: "test-key", Endpoint: provider.URL, Client: provider.Client()}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "long", Category: "other", Priority: "normal", AssistanceMode: "ai"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	if processed, err := worker.ProcessOnce(ctx); err != nil || !processed {
		t.Fatalf("cut-off job: %v %v", processed, err)
	}
	messages, _, err := a.Store.Messages(ctx, alice, id, false, 1)
	if err != nil || len(messages) != 1 {
		t.Fatalf("cut-off answer was posted: %+v %v", messages, err)
	}
	var state string
	if err := a.Store.Pool.QueryRow(ctx, `SELECT state FROM ai_jobs WHERE ticket_id=$1`, id).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("cut-off answer was not retried: %s %v", state, err)
	}
}
