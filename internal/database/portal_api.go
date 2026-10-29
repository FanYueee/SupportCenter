package database

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

// A Portal assertion resolves the same Auth0 subject, never an email-only account.
// Existing display names, roles, signatures, ownership and sessions are preserved.
func (s *Store) PortalUser(ctx context.Context, subject, name, email string) (model.User, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO users(auth0_id,name,email,email_verified)
 VALUES($1,$2,$3,true) ON CONFLICT(auth0_id) DO NOTHING`, subject, name, email); err != nil {
		return model.User{}, err
	}
	var user model.User
	var verified bool
	err = tx.QueryRow(ctx, `SELECT id,name,email,role,signature,email_verified FROM users WHERE auth0_id=$1 FOR UPDATE`, subject).
		Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.Signature, &verified)
	if err != nil {
		return user, err
	}
	if !strings.EqualFold(strings.TrimSpace(user.Email), email) {
		return model.User{}, ErrConflict
	}
	if !verified {
		if _, err = tx.Exec(ctx, `UPDATE users SET email_verified=true WHERE id=$1`, user.ID); err != nil {
			return user, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE ticket_invites SET user_id=$1 WHERE user_id IS NULL AND email_key=lower($2)`, user.ID, email); err != nil {
		return user, err
	}
	return user, tx.Commit(ctx)
}

func (s *Store) ClaimPortalNonce(ctx context.Context, issuer, nonce string, expires time.Time) (bool, error) {
	if _, err := s.Pool.Exec(ctx, `DELETE FROM portal_api_nonces WHERE expires_at<now()`); err != nil {
		return false, err
	}
	tag, err := s.Pool.Exec(ctx, `INSERT INTO portal_api_nonces(issuer,nonce,expires_at) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, issuer, nonce, expires)
	return err == nil && tag.RowsAffected() == 1, err
}

// Read-only account lookup is useful to operators without exposing Auth0 subjects.
func (s *Store) PortalStaff(ctx context.Context) ([]model.User, error) {
	rows, err := s.Pool.Query(ctx, `SELECT u.id,u.name,u.email,u.role,u.signature,
 COALESCE((SELECT array_agg(category ORDER BY category) FROM staff_departments d WHERE d.user_id=u.id),'{}'::text[])
 FROM users u WHERE u.role IN ('support','admin') AND u.auth0_id NOT LIKE 'system:%' ORDER BY u.name,u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.User, error) {
		var user model.User
		err := row.Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.Signature, &user.Departments)
		return user, err
	})
}
