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

func queueTechnicalNotifications(ctx context.Context, tx pgx.Tx, ticketID int64, kind string, recipients []int64) error {
	if kind != "technical" && kind != "assignment" {
		return ErrInvalid
	}
	_, err := tx.Exec(ctx, `INSERT INTO email_notifications(ticket_id,recipient_user_id,kind)
	 SELECT t.id,u.id,$2 FROM tickets t CROSS JOIN users u JOIN staff_departments d ON d.user_id=u.id
	 WHERE t.id=$1 AND t.status NOT IN ('solved','closed') AND t.category='technical'
	 AND d.category='technical' AND u.role IN ('support','admin') AND u.email_verified
 AND ($3::bigint[] IS NULL OR u.id=ANY($3::bigint[]))`, ticketID, kind, recipients)
	return err
}
