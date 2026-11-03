package database

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

// Invitation emails go to addresses the inviter chooses, so they are capped:
// per inviter, and per recipient so nobody can be flooded.
const (
	maxInviteEmailsPerInviter = 30
	maxInviteEmailsPerAddress = 3
)

func (s *Store) EmailSettings(ctx context.Context) (e model.EmailSettings, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT on_create,on_reply,include_reply,on_invite,last_error,last_sent_at,last_error_at FROM email_settings WHERE id=1`).
		Scan(&e.OnCreate, &e.OnReply, &e.IncludeReply, &e.OnInvite, &e.LastError, &e.LastSentAt, &e.LastErrorAt)
	return
}

// SaveEmailSettings also drops waiting emails of a kind that was switched off.
func (s *Store) SaveEmailSettings(ctx context.Context, e model.EmailSettings) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE email_settings SET on_create=$1,on_reply=$2,include_reply=$3,on_invite=$4,updated_at=now() WHERE id=1`, e.OnCreate, e.OnReply, e.IncludeReply, e.OnInvite); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE customer_emails SET state='cancelled',lease_until=NULL WHERE state='pending' AND ((kind='created' AND NOT $1) OR (kind='reply' AND NOT $2) OR (kind='invite' AND NOT $3))`, e.OnCreate, e.OnReply, e.OnInvite); err != nil {
		return err
	}
	return commit(ctx, tx)
}

// RecordEmailResult keeps the latest outcome for the settings page; an empty
// code means the last email was accepted by the SMTP server.
func (s *Store) RecordEmailResult(ctx context.Context, code string) error {
	if code == "" {
		_, err := s.Pool.Exec(ctx, `UPDATE email_settings SET last_sent_at=now(),last_error='',last_error_at=NULL WHERE id=1`)
		return err
	}
	_, err := s.Pool.Exec(ctx, `UPDATE email_settings SET last_error=$1,last_error_at=now() WHERE id=1`, code)
	return err
}

func (s *Store) EmailNotify(ctx context.Context, userID int64) (on bool, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT email_notify FROM users WHERE id=$1`, userID).Scan(&on)
	return on, notFound(err)
}

// SetEmailNotify switches the person's ticket emails and drops any still waiting.
func (s *Store) SetEmailNotify(ctx context.Context, userID int64, on bool) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `UPDATE users SET email_notify=$2 WHERE id=$1`, userID, on); err != nil {
		return err
	}
	if !on {
		if _, err = tx.Exec(ctx, `UPDATE customer_emails SET state='cancelled',lease_until=NULL WHERE state='pending' AND recipient_email=(SELECT lower(email) FROM users WHERE id=$1)`, userID); err != nil {
			return err
		}
	}
	return commit(ctx, tx)
}

// queueCreatedEmail confirms a new ticket to its requester, if their address
// is verified and they kept emails on.
func (s *Store) queueCreatedEmail(ctx context.Context, tx pgx.Tx, ticketID, userID int64) error {
	if !s.MailEnabled {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO customer_emails(ticket_id,kind,recipient_email,recipient_user_id)
SELECT $1,'created',lower(u.email),u.id FROM users u JOIN email_settings s ON s.id=1
 WHERE u.id=$2 AND s.on_create AND u.email_verified AND u.email_notify AND u.auth0_id NOT LIKE 'system:%'`, ticketID, userID)
	return err
}

// QueueReplyEmails tells the ticket's requester and joined participants about a
// staff or AI reply. Only verified addresses that kept emails on are used.
// Every reply gets its own email, sent right away and quoting that reply.
func (s *Store) QueueReplyEmails(ctx context.Context, tx pgx.Tx, ticketID, messageID, authorID int64) error {
	if !s.MailEnabled {
		return nil
	}
	_, err := tx.Exec(ctx, `WITH people AS (
 SELECT user_id AS id FROM tickets WHERE id=$1
 UNION SELECT user_id FROM ticket_invites WHERE ticket_id=$1 AND user_id IS NOT NULL
 UNION SELECT u.id FROM ticket_invites i JOIN users u ON i.user_id IS NULL AND lower(u.email)=i.email_key AND u.email_verified WHERE i.ticket_id=$1
)
INSERT INTO customer_emails(ticket_id,kind,recipient_email,recipient_user_id,message_id)
SELECT DISTINCT ON (lower(u.email)) $1,'reply',lower(u.email),u.id,$2
 FROM people p JOIN users u ON u.id=p.id JOIN email_settings s ON s.id=1
 WHERE s.on_reply AND u.email_verified AND u.email_notify AND u.id<>$3 AND u.auth0_id NOT LIKE 'system:%'
 ORDER BY lower(u.email),u.id`, ticketID, messageID, authorID)
	return err
}

// queueInviteEmail tells an invited address about the ticket. Only verified
// inviters can send one, the email carries no ticket content, and it is sent
// whether or not the address has an account.
func (s *Store) queueInviteEmail(ctx context.Context, tx pgx.Tx, ticketID, inviterID int64, email string) error {
	if !s.MailEnabled {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO customer_emails(ticket_id,kind,recipient_email,actor_user_id)
SELECT $1,'invite',$3,$2 FROM email_settings s JOIN users inviter ON inviter.id=$2
 WHERE s.id=1 AND s.on_invite AND inviter.email_verified
 AND NOT EXISTS(SELECT 1 FROM users u WHERE lower(u.email)=$3 AND NOT u.email_notify)
 AND (SELECT count(*) FROM customer_emails WHERE kind='invite' AND actor_user_id=$2 AND created_at>now()-interval '1 day')<$4
 AND (SELECT count(*) FROM customer_emails WHERE kind='invite' AND recipient_email=$3 AND created_at>now()-interval '1 day')<$5
ON CONFLICT (ticket_id,kind,recipient_email) WHERE state='pending' AND kind='invite' DO NOTHING`, ticketID, inviterID, strings.ToLower(email), maxInviteEmailsPerInviter, maxInviteEmailsPerAddress)
	return err
}
