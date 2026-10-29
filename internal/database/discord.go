package database

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

// The backlog stays bounded while Discord is unreachable or rate limiting.
const maxDiscordBacklog = 1000

var discordWebhookPath = regexp.MustCompile(`^/api/(?:v[0-9]{1,2}/)?webhooks/([0-9]{1,20})/([A-Za-z0-9_-]{1,100})/?$`)

// ParseDiscordWebhook accepts a webhook URL copied from Discord and returns its
// ID and token. Only Discord's own hosts are accepted, and the worker always
// posts to discord.com, so a saved value cannot send requests anywhere else.
func ParseDiscordWebhook(raw string) (id, token string, err error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 300 || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", "", ErrInvalid
	}
	switch u.Host {
	case "discord.com", "discordapp.com", "canary.discord.com", "ptb.discord.com":
	default:
		return "", "", ErrInvalid
	}
	match := discordWebhookPath.FindStringSubmatch(u.EscapedPath())
	if match == nil {
		return "", "", ErrInvalid
	}
	return match[1], match[2], nil
}

func (s *Store) DiscordSettings(ctx context.Context) (d model.DiscordSettings, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT webhook_id,on_ticket,on_reply,include_preview,last_error,last_sent_at,last_error_at FROM discord_settings WHERE id=1`).
		Scan(&d.WebhookID, &d.OnTicket, &d.OnReply, &d.IncludePreview, &d.LastError, &d.LastSentAt, &d.LastErrorAt)
	return
}

// DiscordWebhook returns the credential for sending; an empty ID means none is set.
func (s *Store) DiscordWebhook(ctx context.Context) (id, token string, preview bool, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT webhook_id,webhook_token,include_preview FROM discord_settings WHERE id=1`).Scan(&id, &token, &preview)
	return
}

// SaveDiscordSettings stores the event switches. A nil webhook keeps the
// current one, an empty string removes it and drops what is still queued.
func (s *Store) SaveDiscordSettings(ctx context.Context, d model.DiscordSettings, webhook *string) error {
	if webhook == nil {
		_, err := s.Pool.Exec(ctx, `UPDATE discord_settings SET on_ticket=$1,on_reply=$2,include_preview=$3,updated_at=now() WHERE id=1`, d.OnTicket, d.OnReply, d.IncludePreview)
		return err
	}
	var id, token string
	if *webhook != "" {
		var err error
		if id, token, err = ParseDiscordWebhook(*webhook); err != nil {
			return err
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE discord_settings SET webhook_id=$4,webhook_token=$5,on_ticket=$1,on_reply=$2,include_preview=$3,
 last_sent_at=NULL,last_error='',last_error_at=NULL,updated_at=now() WHERE id=1`, d.OnTicket, d.OnReply, d.IncludePreview, id, token); err != nil {
		return err
	}
	if id == "" {
		if _, err = tx.Exec(ctx, `UPDATE discord_notifications SET state='cancelled',lease_until=NULL WHERE state IN ('pending','sending')`); err != nil {
			return err
		}
	}
	return commit(ctx, tx)
}

// RecordDiscordResult keeps the latest outcome for the settings page; an empty
// code means the last delivery succeeded.
func (s *Store) RecordDiscordResult(ctx context.Context, code string) error {
	if code == "" {
		_, err := s.Pool.Exec(ctx, `UPDATE discord_settings SET last_sent_at=now(),last_error='',last_error_at=NULL WHERE id=1`)
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE discord_settings SET last_error=$1,last_error_at=now() WHERE id=1`, code)
	return err
}

// queueDiscord records a ticket or customer reply for the Discord worker, in
// the caller's transaction, when a webhook is set and that event is enabled.
func queueDiscord(ctx context.Context, tx pgx.Tx, ticketID, messageID int64, kind string) error {
	_, err := tx.Exec(ctx, `INSERT INTO discord_notifications(ticket_id,message_id,kind)
 SELECT $1,$2,$3 FROM discord_settings s WHERE s.id=1 AND s.webhook_id<>''
 AND CASE WHEN $3='ticket' THEN s.on_ticket ELSE s.on_reply END
 AND (SELECT count(*) FROM discord_notifications WHERE state IN ('pending','sending'))<$4`, ticketID, messageID, kind, maxDiscordBacklog)
	return err
}
