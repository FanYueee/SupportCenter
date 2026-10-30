package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"supportcenter/internal/ai"
	"supportcenter/internal/model"
)

func TestAIReplyAndHandoff(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	ctx := context.Background()
	var calls int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("x-opencode-session") != "" {
			t.Error("missing OpenAI authorization or leftover provider header")
		}
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Store            *bool  `json:"store"`
			ReasoningEffort  string `json:"reasoning_effort"`
			SafetyIdentifier string `json:"safety_identifier"`
			MaxTokens        int    `json:"max_completion_tokens"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Model != ai.DefaultModel || payload.Store == nil || *payload.Store || payload.ReasoningEffort != "low" || payload.MaxTokens <= 0 || !strings.HasPrefix(payload.SafetyIdentifier, "supportcenter-") || strings.Contains(payload.SafetyIdentifier, "alice") || len(payload.Messages) != calls*2 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "節點連線" || payload.Messages[1].Role != "user" || payload.Messages[1].Content != "節點連線逾時" {
			t.Errorf("unexpected provider request: %+v %v", payload, err)
		}
		if calls == 2 && (payload.Messages[2].Role != "assistant" || payload.Messages[3].Content != "今天早上，台北節點") {
			t.Error("follow-up lost the previous AI answer or new customer message")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"請提供發生時間與節點地區。"}}]}`)
	}))
	defer provider.Close()
	worker := &ai.Worker{Store: a.Store, Key: "test-key", Endpoint: provider.URL, Client: provider.Client()}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "節點連線", Category: "technical", Priority: "low", AssistanceMode: "ai"}, "節點連線逾時", nil)
	if err != nil {
		t.Fatal(err)
	}
	initial, _ := a.Store.Ticket(ctx, alice, id, false)
	if initial.Status != "open" || !strings.Contains(request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil).Body.String(), "AI 處理中") {
		t.Fatal("new AI ticket is not visibly processing")
	}
	if processed, err := worker.ProcessOnce(ctx); err != nil || !processed {
		t.Fatalf("AI job: %v %v", processed, err)
	}
	if processed, err := worker.ProcessOnce(ctx); err != nil || processed {
		t.Fatalf("duplicate AI job: %v %v", processed, err)
	}
	messages, _, err := a.Store.Messages(ctx, alice, id, false, 1)
	if err != nil || len(messages) != 2 || messages[1].Source != "ai" || messages[1].Body != "請提供發生時間與節點地區。" {
		t.Fatalf("AI response: %+v %v", messages, err)
	}
	ticket, err := a.Store.Ticket(ctx, alice, id, false)
	if err != nil || ticket.Status != "waiting" || ticket.AssistanceMode != "ai" {
		t.Fatalf("ticket after AI: %+v %v", ticket, err)
	}
	if err := a.Store.Reply(ctx, alice, id, false, "今天早上，台北節點", false, nil); err != nil {
		t.Fatal(err)
	}
	if processed, err := worker.ProcessOnce(ctx); err != nil || !processed {
		t.Fatalf("follow-up AI job: %v %v", processed, err)
	}
	page := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "alice", nil)
	if !strings.Contains(page.Body.String(), "AI 已回覆 · 待您確認") || !strings.Contains(page.Body.String(), fmt.Sprintf("/tickets/%d/resolve", id)) {
		t.Fatal("AI confirmation missing after follow-up")
	}
	if err := a.Store.Reply(ctx, alice, id, false, "仍然無法連線", false, nil); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.RequestHuman(ctx, alice, id); err != nil {
		t.Fatal(err)
	}
	if processed, err := worker.ProcessOnce(ctx); err != nil || !processed {
		t.Fatalf("cancelled AI job: %v %v", processed, err)
	}
	messages, _, err = a.Store.Messages(ctx, alice, id, false, 1)
	if err != nil || len(messages) != 5 || calls != 2 {
		t.Fatalf("AI answered after handoff: %d %+v %v", calls, messages, err)
	}
}

func TestAIReplyForAdminCreatingCustomerTicket(t *testing.T) {
	a, h, _, _, _ := testApp(t)
	a.AIEnabled = true
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"已收到你的問題。"}}]}`)
	}))
	defer provider.Close()
	form := url.Values{"csrf": {"csrf-admin"}, "subject": {"Admin customer ticket"}, "category": {"other"}, "priority": {"low"}, "body": {"需要協助"}}
	got := request(h, "POST", "/tickets", "admin", form)
	if got.Code != http.StatusSeeOther {
		t.Fatalf("ticket creation: %d %s", got.Code, got.Body.String())
	}
	var id int64
	if err := a.Store.Pool.QueryRow(context.Background(), `SELECT id FROM tickets WHERE subject=$1`, "Admin customer ticket").Scan(&id); err != nil {
		t.Fatal(err)
	}
	page := request(h, "GET", fmt.Sprintf("/tickets/%d", id), "admin", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `hx-trigger="every 5s"`) {
		t.Fatalf("AI reply polling missing: %d", page.Code)
	}
	worker := &ai.Worker{Store: a.Store, Key: "test-key", Endpoint: provider.URL, Client: provider.Client()}
	if processed, err := worker.ProcessOnce(context.Background()); err != nil || !processed {
		t.Fatalf("AI job: %v %v", processed, err)
	}
	var state, status, source string
	if err := a.Store.Pool.QueryRow(context.Background(), `SELECT j.state,t.status,m.source FROM ai_jobs j JOIN tickets t ON t.id=j.ticket_id JOIN ticket_messages m ON m.ticket_id=t.id AND m.source='ai' WHERE t.id=$1`, id).Scan(&state, &status, &source); err != nil || state != "done" || status != "waiting" || source != "ai" {
		t.Fatalf("AI did not answer admin-owned customer ticket: %s %s %s %v", state, status, source, err)
	}
	page = request(h, "GET", fmt.Sprintf("/tickets/%d", id), "admin", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "已收到你的問題。") || !strings.Contains(page.Body.String(), "AI 助理已回覆") || strings.Contains(page.Body.String(), `hx-trigger="every 5s"`) {
		t.Fatalf("AI reply not visible or polling did not stop: %d", page.Code)
	}
}

func TestAIFailureTransfersToHuman(t *testing.T) {
	a, _, alice, _, _ := testApp(t)
	ctx := context.Background()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		fmt.Fprint(w, `{"error":"do not leak this"}`)
	}))
	defer provider.Close()
	worker := &ai.Worker{Store: a.Store, Key: "test-key", Endpoint: provider.URL, Client: provider.Client()}
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "AI failure", Category: "other", Priority: "low", AssistanceMode: "ai"}, "問題", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the final leased attempt without waiting for backoff in a test.
	if _, err := a.Store.Pool.Exec(ctx, `UPDATE ai_jobs SET attempts=2 WHERE ticket_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if processed, err := worker.ProcessOnce(ctx); err != nil || !processed {
		t.Fatalf("failed AI job: %v %v", processed, err)
	}
	ticket, err := a.Store.Ticket(ctx, alice, id, false)
	if err != nil || ticket.AssistanceMode != "human" || ticket.Status != "open" || ticket.HandoffAt == nil {
		t.Fatalf("AI failure not handed to staff: %+v %v", ticket, err)
	}
	var state string
	if err := a.Store.Pool.QueryRow(ctx, `SELECT state FROM ai_jobs WHERE ticket_id=$1`, id).Scan(&state); err != nil || state != "failed" {
		t.Fatalf("job state %s: %v", state, err)
	}
}

func TestAIEnabledCreationDisclosesAndQueues(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	a.AIEnabled = true
	if got := request(h, "GET", "/tickets/new", "alice", nil); got.Code != 200 || !strings.Contains(got.Body.String(), "公開訊息會傳送至 OpenAI") {
		t.Fatalf("AI disclosure missing: %d", got.Code)
	}
	form := url.Values{"csrf": {"csrf-alice"}, "subject": {"AI create"}, "category": {"other"}, "priority": {"low"}, "body": {"hello"}}
	got := request(h, "POST", "/tickets", "alice", form)
	if got.Code != 303 {
		t.Fatalf("AI creation: %d %s", got.Code, got.Body.String())
	}
	var mode, state string
	err := a.Store.Pool.QueryRow(context.Background(), `SELECT t.assistance_mode,j.state FROM tickets t JOIN ai_jobs j ON j.ticket_id=t.id WHERE t.user_id=$1`, alice.ID).Scan(&mode, &state)
	if err != nil || mode != "ai" || state != "pending" {
		t.Fatalf("AI creation did not queue: %s %s %v", mode, state, err)
	}
}

func TestAIPromptsAdminAndDepartmentRouting(t *testing.T) {
	a, h, alice, _, _ := testApp(t)
	a.AIModel = ai.DefaultModel
	path := "/admin/ai"
	if got := request(h, "GET", path, "alice", nil); got.Code != 404 {
		t.Fatalf("user read: %d", got.Code)
	}
	if got := request(h, "GET", path, "staff", nil); got.Code != 404 {
		t.Fatalf("staff read: %d", got.Code)
	}
	form := url.Values{"csrf": {"csrf-admin"}, "general_prompt": {"一般部門專用"}, "billing_prompt": {"帳務部門專用"}, "technical_prompt": {"技術部門專用 <script>alert(1)</script>"}}
	staffForm := url.Values{"csrf": {"csrf-staff"}, "general_prompt": {"不應儲存"}}
	if got := request(h, "POST", path, "staff", staffForm); got.Code != 404 {
		t.Fatalf("staff write: %d", got.Code)
	}
	if got := request(h, "POST", path, "admin", url.Values{"general_prompt": {"x"}}); got.Code != 403 {
		t.Fatalf("missing CSRF: %d", got.Code)
	}
	if got := request(h, "POST", path, "admin", form); got.Code != 303 {
		t.Fatalf("admin save: %d", got.Code)
	}
	page := request(h, "GET", path, "admin", nil)
	if page.Code != 200 || !strings.Contains(page.Body.String(), ai.DefaultModel) || !strings.Contains(page.Body.String(), "留空就不送系統指令") || !strings.Contains(page.Body.String(), "技術部門專用 &lt;script&gt;") || strings.Contains(page.Body.String(), "<script>alert(1)</script>") {
		t.Fatalf("settings page did not render escaped prompts: %d", page.Code)
	}
	if got := request(h, "POST", path, "admin", url.Values{"csrf": {"csrf-admin"}, "general_prompt": {strings.Repeat("中", 4001)}}); got.Code != 422 {
		t.Fatalf("oversize prompt accepted: %d", got.Code)
	}
	stored, err := a.Store.AIPrompts(context.Background())
	if err != nil || stored.General != "一般部門專用" || stored.Billing != "帳務部門專用" || !strings.HasPrefix(stored.Technical, "技術部門專用") {
		t.Fatalf("stored prompts: %+v %v", stored, err)
	}
	var gotSystems []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Model != "gpt-6.1-luna" || len(payload.Messages) == 0 || payload.Messages[0].Role != "system" {
			t.Errorf("provider payload: %+v %v", payload, err)
		} else {
			gotSystems = append(gotSystems, payload.Messages[0].Content)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"已收到，會協助處理。"}}]}`)
	}))
	defer provider.Close()
	// OPENAI_MODEL overrides the default model.
	worker := &ai.Worker{Store: a.Store, Key: "test-key", Model: "gpt-6.1-luna", Endpoint: provider.URL, Client: provider.Client()}
	for _, category := range []string{"other", "billing", "technical"} {
		_, err := a.Store.Create(context.Background(), alice, model.Ticket{Subject: category, Category: category, Priority: "low", AssistanceMode: "ai"}, "測試問題", nil)
		if err != nil {
			t.Fatal(err)
		}
		if processed, err := worker.ProcessOnce(context.Background()); err != nil || !processed {
			t.Fatalf("AI job %s: %v %v", category, processed, err)
		}
	}
	if len(gotSystems) != 3 {
		t.Fatalf("provider calls: %d", len(gotSystems))
	}
	for i, want := range []string{"一般部門專用", "帳務部門專用", "技術部門專用"} {
		if gotSystems[i] != []string{"一般部門專用", "帳務部門專用", "技術部門專用 <script>alert(1)</script>"}[i] || !strings.Contains(gotSystems[i], want) {
			t.Fatalf("wrong department prompt at %d", i)
		}
		for j, other := range []string{"一般部門專用", "帳務部門專用", "技術部門專用"} {
			if i != j && strings.Contains(gotSystems[i], other) {
				t.Fatalf("prompt from another department at %d", i)
			}
		}
	}
}

func TestAIEmptyPromptSendsNoSystemMessage(t *testing.T) {
	a, _, alice, _, _ := testApp(t)
	ctx := context.Background()
	if err := a.Store.SaveAIPrompts(ctx, model.AIPrompts{General: "不可作為帳務預設指令", Technical: "不可作為帳務預設指令"}); err != nil {
		t.Fatal(err)
	}
	var roles []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if len(payload.Messages) != 2 || payload.Messages[0].Content != "No system prompt" || payload.Messages[1].Content != "請問帳單" {
			t.Errorf("application injected content: %+v", payload.Messages)
		}
		for _, m := range payload.Messages {
			roles = append(roles, m.Role)
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"收到"}}]}`)
	}))
	defer provider.Close()
	id, err := a.Store.Create(ctx, alice, model.Ticket{Subject: "No system prompt", Category: "billing", Priority: "low", AssistanceMode: "ai"}, "請問帳單", nil)
	if err != nil {
		t.Fatal(err)
	}
	worker := &ai.Worker{Store: a.Store, Key: "test-key", Endpoint: provider.URL, Client: provider.Client()}
	if processed, err := worker.ProcessOnce(ctx); err != nil || !processed {
		t.Fatalf("AI job: %v %v", processed, err)
	}
	if len(roles) != 2 || roles[0] != "user" || roles[1] != "user" {
		t.Fatalf("unexpected message roles: %v", roles)
	}
	var state string
	if err := a.Store.Pool.QueryRow(ctx, `SELECT state FROM ai_jobs WHERE ticket_id=$1`, id).Scan(&state); err != nil || state != "done" {
		t.Fatalf("AI job state: %s %v", state, err)
	}
}
