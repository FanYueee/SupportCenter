package database

import (
	"context"
	"net/mail"
	"slices"
	"strings"

	"supportcenter/internal/model"
)

const MaxParticipants = 20

// Bind an existing verified account or a manually provisioned staff account.
// Unknown addresses remain claimable only after a verified login.
const insertInvitation = `INSERT INTO ticket_invites(ticket_id,email_key,invited_by,user_id)
 VALUES($1,$2,$3,(SELECT id FROM users WHERE lower(email)=$2
  AND (email_verified OR role IN ('support','admin'))
  ORDER BY email_verified DESC,id LIMIT 1)) ON CONFLICT DO NOTHING`

func NormalizeInviteEmails(raw []string, ownerEmail string) ([]string, error) {
	emails := []string{}
	seen := map[string]bool{}
	for _, value := range raw {
		if strings.TrimSpace(value) == "" {
			continue
		}
		email, err := normalizedInviteEmail(value)
		if err != nil || strings.EqualFold(email, strings.TrimSpace(ownerEmail)) {
			return nil, ErrInvalid
		}
		if !seen[email] {
			emails = append(emails, email)
			seen[email] = true
		}
	}
	if len(emails) > MaxParticipants {
		return nil, ErrInvalid
	}
	slices.Sort(emails)
	return emails, nil
}

func (s *Store) loadTeams(ctx context.Context, tickets []model.Ticket) error {
	if len(tickets) == 0 {
		return nil
	}
	ids := make([]int64, len(tickets))
	byID := make(map[int64]int, len(tickets))
	for i := range tickets {
		ids[i] = tickets[i].ID
		byID[tickets[i].ID] = i
	}
	rows, err := s.Pool.Query(ctx, `SELECT ta.ticket_id,u.id,u.name,u.email,u.role FROM ticket_assignees ta JOIN users u ON u.id=ta.user_id WHERE ta.ticket_id=ANY($1) ORDER BY ta.ticket_id,ta.assigned_at,ta.user_id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ticketID int64
		var user model.User
		if err = rows.Scan(&ticketID, &user.ID, &user.Name, &user.Email, &user.Role); err != nil {
			return err
		}
		tickets[byID[ticketID]].Team = append(tickets[byID[ticketID]].Team, user)
	}
	return rows.Err()
}

func (s *Store) TicketPerson(ctx context.Context, viewer model.User, ticketID, personID int64) (model.User, error) {
	if !viewer.Staff() {
		return model.User{}, ErrNotFound
	}
	var person model.User
	err := s.Pool.QueryRow(ctx, `SELECT u.id,u.name,u.email,u.role FROM tickets t JOIN users u ON u.id=$2 WHERE t.id=$1 AND (t.user_id=u.id OR EXISTS(SELECT 1 FROM ticket_assignees ta WHERE ta.ticket_id=t.id AND ta.user_id=u.id))`, ticketID, personID).Scan(&person.ID, &person.Name, &person.Email, &person.Role)
	return person, notFound(err)
}

func normalizedInviteEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 || len(email) < 3 || strings.ContainsAny(email, "\r\n\x00") {
		return "", ErrInvalid
	}
	return email, nil
}

func (s *Store) Invite(ctx context.Context, owner model.User, ticketID int64, rawEmail string) error {
	email, err := normalizedInviteEmail(rawEmail)
	if err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var ownerEmail string
	err = tx.QueryRow(ctx, `SELECT u.email FROM tickets t JOIN users u ON u.id=t.user_id WHERE t.id=$1 AND t.user_id=$2 FOR UPDATE OF t`, ticketID, owner.ID).Scan(&ownerEmail)
	if err != nil {
		return notFound(err)
	}
	if strings.EqualFold(email, ownerEmail) {
		return ErrInvalid
	}
	var count int
	var invited bool
	if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_or(email_key=$2),false) FROM ticket_invites WHERE ticket_id=$1`, ticketID, email).Scan(&count, &invited); err != nil {
		return err
	}
	if count >= MaxParticipants && !invited {
		return ErrInvalid
	}
	tag, err := tx.Exec(ctx, insertInvitation, ticketID, email, owner.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err = tx.Exec(ctx, `UPDATE tickets SET updated_at=now(),version=version+1 WHERE id=$1`, ticketID); err != nil {
			return err
		}
		if err = recordEvent(ctx, tx, ticketID, owner.ID, "invite", "", email); err != nil {
			return err
		}
		if err = s.queueInviteEmail(ctx, tx, ticketID, owner.ID, email); err != nil {
			return err
		}
	}
	return commit(ctx, tx)
}

func (s *Store) Uninvite(ctx context.Context, owner model.User, ticketID int64, rawEmail string) error {
	email, err := normalizedInviteEmail(rawEmail)
	if err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	err = tx.QueryRow(ctx, `SELECT true FROM tickets WHERE id=$1 AND user_id=$2 FOR UPDATE`, ticketID, owner.ID).Scan(&exists)
	if err != nil {
		return notFound(err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM ticket_invites WHERE ticket_id=$1 AND email_key=$2`, ticketID, email)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err = tx.Exec(ctx, `UPDATE tickets SET updated_at=now(),version=version+1 WHERE id=$1`, ticketID); err != nil {
			return err
		}
		if err = recordEvent(ctx, tx, ticketID, owner.ID, "uninvite", email, ""); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE customer_emails SET state='cancelled',lease_until=NULL WHERE ticket_id=$1 AND recipient_email=$2 AND state='pending'`, ticketID, email); err != nil {
			return err
		}
	}
	return commit(ctx, tx)
}

func (s *Store) LeaveTicket(ctx context.Context, viewer model.User, ticketID int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var ownerID int64
	var email string
	var verified bool
	err = tx.QueryRow(ctx, `SELECT t.user_id,lower(u.email),u.email_verified FROM tickets t JOIN users u ON u.id=$2 WHERE t.id=$1 FOR UPDATE OF t`, ticketID, viewer.ID).Scan(&ownerID, &email, &verified)
	if err != nil {
		return notFound(err)
	}
	if ownerID == viewer.ID {
		return ErrNotFound
	}
	tag, err := tx.Exec(ctx, `DELETE FROM ticket_invites WHERE ticket_id=$1 AND
 (user_id=$3 OR (user_id IS NULL AND $4 AND email_key=$2))`, ticketID, email, viewer.ID, verified)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err = tx.Exec(ctx, `UPDATE tickets SET updated_at=now(),version=version+1 WHERE id=$1`, ticketID); err != nil {
		return err
	}
	if err = recordEvent(ctx, tx, ticketID, viewer.ID, "leave", email, ""); err != nil {
		return err
	}
	return commit(ctx, tx)
}

func (s *Store) Invites(ctx context.Context, viewer model.User, ticketID int64) ([]model.Participant, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, `SELECT true FROM tickets t WHERE id=$1 AND ($2 OR user_id=$3 OR EXISTS(
 SELECT 1 FROM ticket_invites i JOIN users u ON u.id=$3 WHERE i.ticket_id=t.id AND (i.user_id=u.id OR (i.user_id IS NULL AND u.email_verified AND i.email_key=lower(u.email)))))`, ticketID, viewer.Staff(), viewer.ID).Scan(&exists)
	if err != nil {
		return nil, notFound(err)
	}
	rows, err := s.Pool.Query(ctx, `SELECT i.email_key,COALESCE(i.user_id=viewer.id OR
 (i.user_id IS NULL AND viewer.email_verified AND i.email_key=lower(viewer.email)),false)
 FROM ticket_invites i LEFT JOIN users viewer ON viewer.id=$2
 WHERE i.ticket_id=$1 ORDER BY i.created_at,i.email_key`, ticketID, viewer.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	participants := []model.Participant{}
	for rows.Next() {
		var participant model.Participant
		if err = rows.Scan(&participant.Email, &participant.Self); err != nil {
			return nil, err
		}
		participants = append(participants, participant)
	}
	return participants, rows.Err()
}
