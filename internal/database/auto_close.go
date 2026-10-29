package database

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// The resolution timestamp, rather than updated_at, anchors the grace period.
// A shared settings lock makes disabling/changing the rule serialize with closure.
func (s *Store) CloseSolvedTickets(ctx context.Context) (int, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var days int
	if err = tx.QueryRow(ctx, `SELECT auto_close_days FROM automation_settings WHERE id=1 FOR SHARE`).Scan(&days); err != nil {
		return 0, err
	}
	if days == 0 {
		return 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT id FROM tickets WHERE status='solved'
  AND solved_at<=clock_timestamp()-$1*interval '24 hours'
  ORDER BY solved_at,id LIMIT 50 FOR UPDATE SKIP LOCKED`, days)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	var actor int64
	if err = tx.QueryRow(ctx, `INSERT INTO users(auth0_id,name,email,role)
  VALUES('system:automation','自動回覆','automation@local.invalid','user')
  ON CONFLICT(auth0_id) DO UPDATE SET auth0_id=EXCLUDED.auth0_id RETURNING id`).Scan(&actor); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err = tx.Exec(ctx, `UPDATE tickets SET status='closed',auto_closed_at=clock_timestamp(),updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, id); err != nil {
			return 0, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO ticket_messages(ticket_id,user_id,body,internal,source)
   VALUES($1,$2,'此客服單已依設定自動結案。如需繼續討論，請重新開啟客服單。',false,'automation')`, id, actor); err != nil {
			return 0, err
		}
		if err = recordEvent(ctx, tx, id, actor, "status", "solved", "closed"); err != nil {
			return 0, err
		}
		if err = notifyAutoClosed(ctx, tx, id); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// id=0 applies the rule to existing resolved tickets after a settings change.
func notifyResolved(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO user_notifications(user_id,ticket_id,kind,cycle_at)
  SELECT DISTINCT u.id,t.id,'solved',t.solved_at FROM tickets t
  JOIN automation_settings cfg ON cfg.id=1 AND cfg.auto_close_days>0
  JOIN users u ON u.id=t.user_id OR EXISTS(
   SELECT 1 FROM ticket_invites i WHERE i.ticket_id=t.id AND (i.user_id=u.id OR (i.user_id IS NULL AND u.email_verified AND i.email_key=lower(u.email))))
  WHERE ($1::bigint=0 OR t.id=$1) AND t.status='solved' AND t.solved_at IS NOT NULL
  ON CONFLICT(user_id,ticket_id,kind,cycle_at) DO NOTHING`, id)
	return err
}
func notifyAutoClosed(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO user_notifications(user_id,ticket_id,kind,cycle_at)
  SELECT DISTINCT u.id,t.id,'auto_closed',t.auto_closed_at FROM tickets t
  JOIN users u ON u.id=t.user_id OR EXISTS(
   SELECT 1 FROM ticket_invites i WHERE i.ticket_id=t.id AND (i.user_id=u.id OR (i.user_id IS NULL AND u.email_verified AND i.email_key=lower(u.email))))
  WHERE t.id=$1 ON CONFLICT(user_id,ticket_id,kind,cycle_at) DO NOTHING`, id)
	return err
}
