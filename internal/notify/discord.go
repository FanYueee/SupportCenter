package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/database"
	"supportcenter/internal/model"
	"supportcenter/web"
)

// Discord posts to webhooks on discord.com. BaseURL is only changed by tests.
type Discord struct {
	BaseURL string
	Client  *http.Client
}

// DiscordError is a delivery failure; Code is what the settings page shows.
type DiscordError struct {
	Code       string // not_found, rejected, unavailable, network, rate_limited
	RetryAfter time.Duration
}

func (e *DiscordError) Error() string { return "discord webhook: " + e.Code }

// Permanent failures are not retried: the webhook is gone or the message refused.
func (e *DiscordError) Permanent() bool { return e.Code == "not_found" || e.Code == "rejected" }

var discordClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Post sends one message. The returned pause is how long Discord asks callers
// to wait before the next request to this webhook.
func (d Discord) Post(ctx context.Context, id, token string, payload any) (time.Duration, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	base := d.BaseURL
	if base == "" {
		base = "https://discord.com"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/webhooks/"+id+"/"+token, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "SupportCenter-Webhook/1")
	client := d.Client
	if client == nil {
		client = discordClient
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, &DiscordError{Code: "network"}
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	var pause time.Duration
	if res.Header.Get("X-RateLimit-Remaining") == "0" {
		pause = seconds(res.Header.Get("X-RateLimit-Reset-After"))
	}
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return pause, nil
	case res.StatusCode == http.StatusTooManyRequests:
		var limited struct {
			RetryAfter float64 `json:"retry_after"`
		}
		wait := seconds(res.Header.Get("Retry-After"))
		if json.Unmarshal(raw, &limited) == nil && limited.RetryAfter > 0 {
			wait = time.Duration(limited.RetryAfter * float64(time.Second))
		}
		return max(wait, time.Second), &DiscordError{Code: "rate_limited", RetryAfter: max(wait, time.Second)}
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusNotFound:
		return pause, &DiscordError{Code: "not_found"}
	case res.StatusCode >= 500:
		return pause, &DiscordError{Code: "unavailable"}
	}
	return pause, &DiscordError{Code: "rejected"}
}

// seconds parses Discord's decimal seconds, capped so a bad header cannot stall the queue.
func seconds(value string) time.Duration {
	f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || f <= 0 {
		return 0
	}
	return min(time.Duration(f*float64(time.Second)), 10*time.Minute)
}

// ErrorCode turns a Post error into the code stored for the settings page.
func ErrorCode(err error) string {
	var discord *DiscordError
	if errors.As(err, &discord) {
		return discord.Code
	}
	if err != nil {
		return "network"
	}
	return ""
}

type DiscordWorker struct {
	Store   *database.Store
	Discord Discord
	Origin  string
	Site    web.Site // named in each message, so a shared channel shows where it came from
	Log     *slog.Logger
	resume  time.Time
}

func (w *DiscordWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if wait := time.Until(w.resume); wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		processed, err := w.ProcessOnce(ctx)
		if err != nil && ctx.Err() == nil {
			w.Log.Error("discord notification processing failed", "error_type", fmt.Sprintf("%T", err))
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type discordItem struct {
	ID, TicketID, Attachments    int64
	Kind, Subject, Category      string
	Priority, Mode, Body, Author string
	CreatedAt                    time.Time
	Attempts                     int
}

const (
	discordBatch      = 10   // embeds per message
	discordCharBudget = 5500 // Discord allows 6000 characters across a message's embeds
	discordAttempts   = 5
)

// ProcessOnce sends up to ten queued events as one message. It reports whether
// anything was claimed, so Run keeps going while there is a backlog.
func (w *DiscordWorker) ProcessOnce(ctx context.Context) (bool, error) {
	// Events older than a day are no longer useful as alerts.
	if _, err := w.Store.Pool.Exec(ctx, `UPDATE discord_notifications SET state=CASE WHEN attempts>=$1 THEN 'failed' ELSE 'cancelled' END,lease_until=NULL
	 WHERE state IN ('pending','sending') AND (created_at<now()-interval '1 day' OR (state='sending' AND lease_until<now() AND attempts>=$1))`, discordAttempts); err != nil {
		return false, err
	}
	id, token, preview, err := w.Store.DiscordWebhook(ctx)
	if err != nil || id == "" {
		return false, err
	}
	rows, err := w.Store.Pool.Query(ctx, `WITH picked AS (
	 SELECT id FROM discord_notifications WHERE available_at<=now() AND attempts<$1
	 AND (state='pending' OR (state='sending' AND lease_until<now()))
	 ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT $2
	), claimed AS (
	 UPDATE discord_notifications n SET state='sending',attempts=attempts+1,lease_until=now()+interval '2 minutes'
	 FROM picked WHERE n.id=picked.id RETURNING n.id,n.ticket_id,n.message_id,n.kind,n.attempts
	)
	 SELECT c.id,c.ticket_id,c.kind,c.attempts,t.subject,t.category,t.priority,t.assistance_mode,m.body,m.created_at,u.name,
	 (SELECT count(*) FROM ticket_attachments a WHERE a.message_id=m.id)
	 FROM claimed c JOIN tickets t ON t.id=c.ticket_id JOIN ticket_messages m ON m.id=c.message_id JOIN users u ON u.id=m.user_id ORDER BY c.id`, discordAttempts, discordBatch)
	if err != nil {
		return false, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (discordItem, error) {
		var i discordItem
		err := row.Scan(&i.ID, &i.TicketID, &i.Kind, &i.Attempts, &i.Subject, &i.Category, &i.Priority, &i.Mode, &i.Body, &i.CreatedAt, &i.Author, &i.Attachments)
		return i, err
	})
	if err != nil || len(items) == 0 {
		return false, err
	}
	embeds, sent, held := []discordEmbed{}, []int64{}, []int64{}
	used := 0
	for _, item := range items {
		embed := w.embed(item, preview)
		if size := embed.size(); len(embeds) == 0 || used+size <= discordCharBudget {
			embeds, sent, used = append(embeds, embed), append(sent, item.ID), used+size
		} else {
			held = append(held, item.ID)
		}
	}
	// Items that did not fit go back to the queue without using an attempt.
	if len(held) > 0 {
		if _, err = w.Store.Pool.Exec(ctx, `UPDATE discord_notifications SET state='pending',attempts=attempts-1,lease_until=NULL WHERE id=ANY($1) AND state='sending'`, held); err != nil {
			return true, err
		}
	}
	pause, sendErr := w.Discord.Post(ctx, id, token, discordMessage{AllowedMentions: noMentions, Embeds: embeds})
	w.resume = time.Now().Add(pause)
	var discordErr *DiscordError
	switch {
	case sendErr == nil:
		if _, err = w.Store.Pool.Exec(ctx, `UPDATE discord_notifications SET state='sent',sent_at=now(),lease_until=NULL WHERE id=ANY($1) AND state='sending'`, sent); err != nil {
			return true, err
		}
		return true, w.Store.RecordDiscordResult(ctx, "")
	case errors.As(sendErr, &discordErr) && discordErr.Code == "rate_limited":
		_, err = w.Store.Pool.Exec(ctx, `UPDATE discord_notifications SET state='pending',attempts=attempts-1,lease_until=NULL,available_at=now()+$2*interval '1 millisecond' WHERE id=ANY($1) AND state='sending'`, sent, discordErr.RetryAfter.Milliseconds())
		return true, err
	}
	permanent := errors.As(sendErr, &discordErr) && discordErr.Permanent()
	backoff := time.Duration(1<<min(items[0].Attempts, 4)) * time.Minute
	if _, err = w.Store.Pool.Exec(ctx, `UPDATE discord_notifications SET state=CASE WHEN $2 OR attempts>=$3 THEN 'failed' ELSE 'pending' END,
	 available_at=now()+$4*interval '1 second',lease_until=NULL WHERE id=ANY($1) AND state='sending'`, sent, permanent, discordAttempts, int64(backoff.Seconds())); err != nil {
		return true, err
	}
	w.Log.Warn("discord notification send failed", "events", len(sent), "code", ErrorCode(sendErr))
	return true, w.Store.RecordDiscordResult(ctx, ErrorCode(sendErr))
}

type discordMessage struct {
	Content         string          `json:"content,omitempty"`
	AllowedMentions discordMentions `json:"allowed_mentions"`
	Embeds          []discordEmbed  `json:"embeds"`
}

// Customer text must never ping @everyone, roles or users.
type discordMentions struct {
	Parse []string `json:"parse"`
}

var noMentions = discordMentions{Parse: []string{}}

type discordEmbed struct {
	Author      *discordAuthor `json:"author,omitempty"`
	Title       string         `json:"title"`
	URL         string         `json:"url,omitempty"`
	Description string         `json:"description,omitempty"`
	Color       int            `json:"color"`
	Fields      []discordField `json:"fields,omitempty"`
	Footer      *discordFooter `json:"footer,omitempty"`
	Timestamp   string         `json:"timestamp,omitempty"`
}

type discordAuthor struct {
	Name string `json:"name"`
}

type discordFooter struct {
	Text string `json:"text"`
}

type discordField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

func (e discordEmbed) size() int {
	n := len([]rune(e.Title)) + len([]rune(e.Description))
	if e.Author != nil {
		n += len([]rune(e.Author.Name))
	}
	if e.Footer != nil {
		n += len([]rune(e.Footer.Text))
	}
	for _, f := range e.Fields {
		n += len([]rune(f.Name)) + len([]rune(f.Value))
	}
	return n
}

func (w *DiscordWorker) embed(item discordItem, preview bool) discordEmbed {
	event, color := "新客服單", 0x2563EB
	if item.Kind == "reply" {
		event, color = "客戶回覆", 0x16A34A
	}
	if item.Priority == "urgent" {
		color = 0xDC2626
	}
	embed := discordEmbed{
		Author:    &discordAuthor{Name: event},
		Title:     clip(fmt.Sprintf("#%04d %s", item.TicketID, escapeDiscord(oneLine(item.Subject))), 200),
		URL:       fmt.Sprintf("%s/admin/tickets/%d", w.Origin, item.TicketID),
		Color:     color,
		Footer:    &discordFooter{Text: clip(w.Site.Title(), 100)},
		Timestamp: item.CreatedAt.UTC().Format(time.RFC3339),
		Fields: []discordField{
			{Name: "客戶", Value: clip(escapeDiscord(oneLine(item.Author)), 80), Inline: true},
			{Name: "部門", Value: model.Category(item.Category), Inline: true},
			{Name: "優先級", Value: model.Priority(item.Priority), Inline: true},
		},
	}
	if item.Mode == "ai" {
		embed.Fields = append(embed.Fields, discordField{Name: "處理方式", Value: "AI 回覆", Inline: true})
	}
	if item.Attachments > 0 {
		embed.Fields = append(embed.Fields, discordField{Name: "附件", Value: strconv.FormatInt(item.Attachments, 10), Inline: true})
	}
	if text := []rune(oneLine(item.Body)); preview && len(text) > 0 {
		if len(text) > 200 {
			text = append(text[:200], '…')
		}
		embed.Description = clip(escapeDiscord(string(text)), 400)
	}
	return embed
}

// DiscordTestMessage is what the settings page sends to check a webhook.
func DiscordTestMessage(origin, site string) any {
	return discordMessage{AllowedMentions: noMentions, Embeds: []discordEmbed{{
		Author:      &discordAuthor{Name: "測試訊息"},
		Title:       clip(site+"已連線", 200),
		URL:         origin + "/admin/discord",
		Description: "新客服單與客戶回覆會傳送到這個頻道。",
		Color:       0x2563EB,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}}}
}

// oneLine collapses whitespace and drops control characters for a short preview.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
}

// Backslash-escape Discord markdown so customer text shows as typed: no bold,
// masked links, headings, quotes or mention syntax.
var discordMarkdown = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "~", `\~`, "`", "\\`", "|", `\|`,
	">", `\>`, "<", `\<`, "[", `\[`, "]", `\]`, "(", `\(`, ")", `\)`, "#", `\#`, "-", `\-`, "@", `\@`)

func escapeDiscord(s string) string {
	if s == "" {
		return "-"
	}
	return discordMarkdown.Replace(s)
}

// clip shortens escaped text without leaving a dangling backslash.
func clip(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	r = r[:limit-1]
	backslashes := 0
	for i := len(r) - 1; i >= 0 && r[i] == '\\'; i-- {
		backslashes++
	}
	if backslashes%2 == 1 {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}
