package ai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/database"
)

const Endpoint = "https://opencode.ai/zen/go/v1/chat/completions"
const Model = "deepseek-v4.1-flash"

type Worker struct {
	Store    *database.Store
	Key      string
	Endpoint string // The server uses the fixed official endpoint; tests may inject a local HTTP server.
	Client   *http.Client
	Log      *slog.Logger
}

type job struct {
	id, ticketID, triggerID int64
	attempts                int
	created                 time.Time
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (w *Worker) client() *http.Client {
	if w.Client != nil {
		return w.Client
	}
	return &http.Client{Timeout: 25 * time.Second}
}

func (w *Worker) endpoint() string {
	if w.Endpoint != "" {
		return w.Endpoint
	}
	return Endpoint
}

func (w *Worker) claim(ctx context.Context) (j job, found bool, err error) {
	err = w.Store.Pool.QueryRow(ctx, `UPDATE ai_jobs SET state='processing',attempts=attempts+1,lease_until=now()+interval '2 minutes'
 WHERE id=(SELECT id FROM ai_jobs WHERE available_at<=now() AND (state='pending' OR (state='processing' AND lease_until<now())) ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 RETURNING id,ticket_id,trigger_message_id,attempts,created_at`).Scan(&j.id, &j.ticketID, &j.triggerID, &j.attempts, &j.created)
	if errors.Is(err, pgx.ErrNoRows) {
		return job{}, false, nil
	}
	return j, err == nil, err
}

func (w *Worker) contextMessages(ctx context.Context, j job) ([]message, bool, error) {
	var subject, mode, status, departmentPrompt string
	var newest int64
	err := w.Store.Pool.QueryRow(ctx, `SELECT t.subject,t.assistance_mode,t.status,
 COALESCE((SELECT latest.trigger_message_id FROM ai_jobs latest WHERE latest.ticket_id=t.id ORDER BY latest.trigger_message_id DESC LIMIT 1),0),
 CASE t.category WHEN 'technical' THEN p.technical_prompt WHEN 'billing' THEN p.billing_prompt ELSE p.general_prompt END
 FROM tickets t CROSS JOIN ai_prompt_settings p WHERE t.id=$1 AND p.id=1`, j.ticketID).Scan(&subject, &mode, &status, &newest, &departmentPrompt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if mode != "ai" || status == "closed" || status == "solved" || newest != j.triggerID {
		return nil, false, nil
	}
	rows, err := w.Store.Pool.Query(ctx, `SELECT m.source,m.body FROM ticket_messages m WHERE m.ticket_id=$1 AND NOT m.internal AND m.source IN ('human','ai') ORDER BY m.id DESC LIMIT 12`, j.ticketID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := []message{}
	remaining := 12000
	for rows.Next() {
		var source, body string
		if err := rows.Scan(&source, &body); err != nil {
			return nil, false, err
		}
		if remaining <= 0 {
			break
		}
		runes := []rune(body)
		if len(runes) > remaining {
			runes = runes[:remaining]
		}
		remaining -= len(runes)
		role := "user"
		if source == "ai" {
			role = "assistant"
		}
		items = append(items, message{Role: role, Content: string(runes)})
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	for i, k := 0, len(items)-1; i < k; i, k = i+1, k-1 {
		items[i], items[k] = items[k], items[i]
	}
	items = append([]message{{Role: "user", Content: subject}}, items...)
	if departmentPrompt != "" {
		return append([]message{{Role: "system", Content: departmentPrompt}}, items...), true, nil
	}
	return items, true, nil
}

func (w *Worker) generate(ctx context.Context, j job, messages []message) (string, error) {
	payload, err := json.Marshal(struct {
		Model     string    `json:"model"`
		Messages  []message `json:"messages"`
		MaxTokens int       `json:"max_tokens"`
	}{Model: Model, Messages: messages, MaxTokens: 1000})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+w.Key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "supportcenter/1.0")
	session := sha256.Sum256([]byte(w.Key + ":" + strconv.FormatInt(j.ticketID, 10)))
	req.Header.Set("x-opencode-session", "supportcenter-"+hex.EncodeToString(session[:16]))
	resp, err := w.client().Do(req)
	if err != nil {
		return "", errors.New("AI service unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("AI service rejected request")
	}
	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data); err != nil || len(data.Choices) == 0 {
		return "", errors.New("invalid AI response")
	}
	answer := strings.TrimSpace(data.Choices[0].Message.Content)
	if answer == "" || !utf8.ValidString(answer) || strings.ContainsRune(answer, 0) || len([]rune(answer)) > 4000 {
		return "", errors.New("invalid AI answer")
	}
	return answer, nil
}

func (w *Worker) finish(ctx context.Context, j job, answer string, failure bool) error {
	tx, err := w.Store.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, mode string
	err = tx.QueryRow(ctx, `SELECT status,assistance_mode FROM tickets WHERE id=$1 FOR UPDATE`, j.ticketID).Scan(&status, &mode)
	if err != nil {
		return err
	}
	var state string
	err = tx.QueryRow(ctx, `SELECT state FROM ai_jobs WHERE id=$1 FOR UPDATE`, j.id).Scan(&state)
	if err != nil {
		return err
	}
	if state != "processing" {
		return tx.Commit(ctx)
	}
	var newest int64
	err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT trigger_message_id FROM ai_jobs WHERE ticket_id=$1 ORDER BY trigger_message_id DESC LIMIT 1),0)`, j.ticketID).Scan(&newest)
	if err != nil {
		return err
	}
	if mode != "ai" || status == "closed" || status == "solved" || newest != j.triggerID {
		_, err = tx.Exec(ctx, `UPDATE ai_jobs SET state='cancelled',lease_until=NULL WHERE id=$1`, j.id)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if failure {
		if j.attempts < 3 && time.Since(j.created) < 24*time.Hour {
			_, err = tx.Exec(ctx, `UPDATE ai_jobs SET state='pending',lease_until=NULL,available_at=now()+($2::integer * interval '15 seconds') WHERE id=$1`, j.id, j.attempts*j.attempts)
			if err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
		_, err = tx.Exec(ctx, `UPDATE ai_jobs SET state='failed',lease_until=NULL WHERE id=$1`, j.id)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE tickets SET assistance_mode='human',handoff_at=now(),status='open',updated_at=now(),version=version+1 WHERE id=$1`, j.ticketID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	var authorID int64
	err = tx.QueryRow(ctx, `INSERT INTO users(auth0_id,name,email,role) VALUES('system:ai','AI 助理','ai@local.invalid','user') ON CONFLICT(auth0_id) DO UPDATE SET auth0_id=EXCLUDED.auth0_id RETURNING id`).Scan(&authorID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ticket_messages(ticket_id,user_id,body,internal,source) VALUES($1,$2,$3,false,'ai')`, j.ticketID, authorID, answer)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE tickets SET status='waiting',updated_at=now(),version=version+1 WHERE id=$1`, j.ticketID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE ai_jobs SET state='done',lease_until=NULL WHERE id=$1`, j.id)
	return errOrCommit(ctx, tx, err)
}

func errOrCommit(ctx context.Context, tx pgx.Tx, err error) error {
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ProcessOnce handles at most one queued answer. A ticket is rechecked under lock
// after the network call so handoffs and newer customer messages cannot receive a stale answer.
func (w *Worker) ProcessOnce(ctx context.Context) (bool, error) {
	j, found, err := w.claim(ctx)
	if err != nil || !found {
		return found, err
	}
	if time.Since(j.created) >= 24*time.Hour {
		return true, w.finish(ctx, j, "", true)
	}
	messages, active, err := w.contextMessages(ctx, j)
	if err != nil {
		return true, w.finish(ctx, j, "", true)
	}
	if !active {
		return true, w.finish(ctx, j, "", false)
	}
	answer, err := w.generate(ctx, j, messages)
	if err != nil {
		return true, w.finish(ctx, j, "", true)
	}
	return true, w.finish(ctx, j, answer, false)
}

func (w *Worker) Run(ctx context.Context) {
	log := w.Log
	if log == nil {
		log = slog.Default()
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := w.ProcessOnce(ctx); err != nil && ctx.Err() == nil {
			log.Error("AI job processing failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
