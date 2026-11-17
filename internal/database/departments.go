package database

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

// SaveStaffDepartments only accepts a real staff member and the three ticket-routing groups.
func (s *Store) SaveStaffDepartments(ctx context.Context, actor model.User, userID int64, categories []string) error {
	if actor.Role != "admin" || userID < 1 || len(categories) > len(model.Departments) {
		return ErrInvalid
	}
	categories = slices.Clone(categories)
	slices.Sort(categories)
	for i, category := range categories {
		if category != "other" && category != "billing" && category != "technical" {
			return ErrInvalid
		}
		if i > 0 && categories[i-1] == category {
			return ErrInvalid
		}
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 AND auth0_id NOT LIKE 'system:%' FOR UPDATE`, userID).Scan(&role)
	if err != nil {
		return notFound(err)
	}
	if role != "admin" && role != "support" {
		return ErrInvalid
	}
	if _, err = tx.Exec(ctx, `DELETE FROM staff_departments WHERE user_id=$1`, userID); err != nil {
		return err
	}
	for _, category := range categories {
		if _, err = tx.Exec(ctx, `INSERT INTO staff_departments(user_id,category) VALUES($1,$2)`, userID, category); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// queueDepartmentNotifications emails the staff in the ticket's department:
// all of them for a new or rerouted ticket, or only the ones just assigned.
// The email quotes messageID, or the customer's latest message when it is 0.
func queueDepartmentNotifications(ctx context.Context, tx pgx.Tx, ticketID int64, kind string, recipients []int64, messageID int64) error {
	if kind != "department" && kind != "assignment" {
		return ErrInvalid
	}
	_, err := tx.Exec(ctx, `INSERT INTO email_notifications(ticket_id,recipient_user_id,kind,message_id)
	 SELECT t.id,u.id,$2,NULLIF($4::bigint,0) FROM tickets t CROSS JOIN users u JOIN staff_departments d ON d.user_id=u.id
	 WHERE t.id=$1 AND t.status NOT IN ('solved','closed')
	 AND d.category=t.category AND u.role IN ('support','admin') AND u.email_verified
 AND ($3::bigint[] IS NULL OR u.id=ANY($3::bigint[]))`, ticketID, kind, recipients, messageID)
	return err
}

// queueReplyNotifications emails staff a customer's reply: the people working
// the ticket, or its department while nobody is. A ticket the AI is answering
// waits for the handoff instead, and nobody hears about their own message.
func queueReplyNotifications(ctx context.Context, tx pgx.Tx, ticketID, messageID, authorID int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO email_notifications(ticket_id,recipient_user_id,kind,message_id)
	 SELECT t.id,u.id,'reply',$2 FROM tickets t JOIN users u ON u.role IN ('support','admin') AND u.email_verified AND u.id<>$3
	 WHERE t.id=$1 AND t.status NOT IN ('solved','closed') AND t.assistance_mode='human'
	 AND CASE WHEN EXISTS(SELECT 1 FROM ticket_assignees a WHERE a.ticket_id=t.id)
	  THEN EXISTS(SELECT 1 FROM ticket_assignees a WHERE a.ticket_id=t.id AND a.user_id=u.id)
	  ELSE EXISTS(SELECT 1 FROM staff_departments d WHERE d.user_id=u.id AND d.category=t.category) END`, ticketID, messageID, authorID)
	return err
}
