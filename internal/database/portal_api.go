package database

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

// PortalUser resolves a Portal actor by email, ignoring case and surrounding
// spaces, because Portal accounts may lack a stable Auth0 subject. An existing
// account keeps its Auth0 subject, role, name and signature; when several share
// the address, a verified sign-in account wins over a Portal-only one so both
// entry points show the same tickets. Without a match a Portal-only account
// ("portal:<email>") is created.
func (s *Store) PortalUser(ctx context.Context, name, email string) (model.User, error) {
	key := strings.ToLower(strings.TrimSpace(email))
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback(ctx)
	// Concurrent requests for one address must not create two accounts.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "portal-user:"+key); err != nil {
		return model.User{}, err
	}
	var user model.User
	err = tx.QueryRow(ctx, `SELECT id,name,email,role,signature FROM users
 WHERE lower(trim(email))=$1 AND auth0_id NOT LIKE 'system:%'
 ORDER BY email_verified DESC,auth0_id LIKE 'portal:%',id LIMIT 1 FOR UPDATE`, key).
		Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.Signature)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO users(auth0_id,name,email,email_verified) VALUES('portal:'||$1,$2,$1,true)
 ON CONFLICT(auth0_id) DO UPDATE SET auth0_id=EXCLUDED.auth0_id RETURNING id,name,email,role,signature`, key, name).
			Scan(&user.ID, &user.Name, &user.Email, &user.Role, &user.Signature)
	}
	if err != nil {
		return model.User{}, err
	}
	// Recorded at most every five minutes, so busy Portal sessions do not write on every call.
	if _, err = tx.Exec(ctx, `UPDATE users SET portal_seen_at=now() WHERE id=$1 AND (portal_seen_at IS NULL OR portal_seen_at<now()-interval '5 minutes')`, user.ID); err != nil {
		return model.User{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE ticket_invites SET user_id=$1 WHERE user_id IS NULL AND email_key=$2`, user.ID, key); err != nil {
		return model.User{}, err
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
