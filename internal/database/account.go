package database

import (
	"context"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

func (s *Store) LogAccountEvent(ctx context.Context, tx pgx.Tx, userID int64, kind, detail string) error {
	if userID < 1 || !oneOf(kind, "login", "logout", "avatar", "name", "signature", "link") || len([]rune(detail)) > 100 {
		return ErrInvalid
	}
	info := requestInfo(ctx)
	_, err := tx.Exec(ctx, `INSERT INTO account_events(user_id,kind,detail,client_ip,user_agent) VALUES($1,$2,$3,$4,$5)`, userID, kind, detail, info.IP, info.UserAgent)
	return err
}

func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func (s *Store) SetDisplayName(ctx context.Context, user model.User, name string) error {
	name = strings.TrimSpace(name)
	if user.Role != "admin" || len([]rune(name)) < 1 || len([]rune(name)) > 80 || !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE users SET name=$2,name_locked=true WHERE id=$1 AND (name<>$2 OR NOT name_locked)`, user.ID, name)
	if err == nil && tag.RowsAffected() > 0 {
		err = s.LogAccountEvent(ctx, tx, user.ID, "name", "")
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetSignature(ctx context.Context, user model.User, signature string) error {
	signature = strings.TrimSpace(signature)
	if user.Role != "admin" || len([]rune(signature)) > 500 || !utf8.ValidString(signature) || strings.ContainsRune(signature, 0) {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE users SET signature=$2 WHERE id=$1 AND role='admin' AND signature<>$2`, user.ID, signature)
	if err == nil && tag.RowsAffected() > 0 {
		err = s.LogAccountEvent(ctx, tx, user.ID, "signature", "")
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Logins(ctx context.Context, userID int64, page int) ([]model.Activity, bool, error) {
	rows, err := s.Pool.Query(ctx, `SELECT kind,detail,created_at,client_ip,user_agent FROM account_events WHERE user_id=$1 AND kind='login' ORDER BY created_at DESC,id DESC LIMIT 21 OFFSET $2`, userID, (page-1)*20)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	list := []model.Activity{}
	for rows.Next() {
		var a model.Activity
		if err = rows.Scan(&a.Kind, &a.Detail, &a.CreatedAt, &a.IP, &a.UserAgent); err != nil {
			return nil, false, err
		}
		list = append(list, a)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(list) > 20
	if more {
		list = list[:20]
	}
	return list, more, nil
}

func (s *Store) Activities(ctx context.Context, userID int64, page int) ([]model.Activity, bool, error) {
	rows, err := s.Pool.Query(ctx, `SELECT kind,detail,ticket_id,created_at,client_ip,user_agent FROM (
	 SELECT e.kind,e.detail,0::bigint AS ticket_id,e.created_at,e.client_ip,e.user_agent FROM account_events e WHERE e.user_id=$1 AND e.kind<>'login'
	 UNION ALL SELECT 'created','',t.id,t.created_at,t.client_ip,t.user_agent FROM tickets t WHERE t.user_id=$1
	 UNION ALL SELECT CASE WHEN m.internal THEN 'note' ELSE 'reply' END,'',m.ticket_id,m.created_at,m.client_ip,m.user_agent FROM ticket_messages m WHERE m.user_id=$1 AND m.source<>'system' AND EXISTS (SELECT 1 FROM ticket_messages first WHERE first.ticket_id=m.ticket_id AND first.id<m.id)
	 UNION ALL SELECT e.kind,e.current,e.ticket_id,e.created_at,e.client_ip,e.user_agent FROM ticket_events e WHERE e.user_id=$1
	) history ORDER BY created_at DESC,kind,ticket_id DESC NULLS LAST LIMIT 21 OFFSET $2`, userID, (page-1)*20)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	list := []model.Activity{}
	for rows.Next() {
		var a model.Activity
		if err = rows.Scan(&a.Kind, &a.Detail, &a.TicketID, &a.CreatedAt, &a.IP, &a.UserAgent); err != nil {
			return nil, false, err
		}
		list = append(list, a)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(list) > 20
	if more {
		list = list[:20]
	}
	return list, more, nil
}

func (s *Store) Users(ctx context.Context, query string, page int) ([]model.UserSummary, bool, error) {
	rows, err := s.Pool.Query(ctx, `SELECT u.id,u.name,u.email,u.role,u.created_at,u.portal_seen_at,
	 (SELECT e.created_at FROM account_events e WHERE e.user_id=u.id AND e.kind='login' ORDER BY e.created_at DESC,e.id DESC LIMIT 1),
	 u.priority_level,u.priority_until
	 FROM users u WHERE u.auth0_id NOT LIKE 'system:%' AND ($1='' OR u.name ILIKE '%'||$1||'%' OR u.email ILIKE '%'||$1||'%') ORDER BY u.id DESC LIMIT 21 OFFSET $2`, query, (page-1)*20)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	list := []model.UserSummary{}
	for rows.Next() {
		var item model.UserSummary
		if err = rows.Scan(&item.User.ID, &item.User.Name, &item.User.Email, &item.User.Role, &item.CreatedAt, &item.LastPortal, &item.LastLogin, &item.PrioritySupport.Level, &item.PrioritySupport.Until); err != nil {
			return nil, false, err
		}
		list = append(list, item)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(list) > 20
	if more {
		list = list[:20]
	}
	return list, more, nil
}

// PrioritySupport is the account's priority support as stored; use Current
// for the level in force.
func (s *Store) PrioritySupport(ctx context.Context, userID int64) (model.PrioritySupport, error) {
	var p model.PrioritySupport
	err := s.Pool.QueryRow(ctx, `SELECT priority_level,priority_until FROM users WHERE id=$1`, userID).Scan(&p.Level, &p.Until)
	return p, notFound(err)
}

// SetPrioritySupport replaces an account's priority support level and when it ends.
func (s *Store) SetPrioritySupport(ctx context.Context, userID int64, p model.PrioritySupport) error {
	if !slices.Contains(model.Priorities, p.Level) {
		return ErrInvalid
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE users SET priority_level=$2,priority_until=$3 WHERE id=$1 AND auth0_id NOT LIKE 'system:%'`, userID, p.Level, p.Until)
	if err == nil && tag.RowsAffected() == 0 {
		err = ErrNotFound
	}
	return err
}
