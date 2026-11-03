package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
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

func (s *Store) Lifecycle(ctx context.Context) (l model.Lifecycle, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT waiting_remind_hours,waiting_close_hours,closed_lock_hours FROM automation_settings WHERE id=1`).
		Scan(&l.RemindHours, &l.CloseHours, &l.LockHours)
	return
}

func (s *Store) SaveLifecycle(ctx context.Context, l model.Lifecycle) error {
	if l.RemindHours < 0 || l.RemindHours > 720 || l.CloseHours < 0 || l.CloseHours > 720 || l.LockHours < 0 || l.LockHours > 8760 ||
		(l.RemindHours > 0 && l.CloseHours > 0 && l.RemindHours >= l.CloseHours) {
		return ErrInvalid
	}
	_, err := s.Pool.Exec(ctx, `UPDATE automation_settings SET waiting_remind_hours=$1,waiting_close_hours=$2,closed_lock_hours=$3,updated_at=now() WHERE id=1`,
		l.RemindHours, l.CloseHours, l.LockHours)
	return err
}

// waitingClosedBody is the message left on a ticket closed after waiting too
// long for the customer.
const waitingClosedBody = "此客服單因未收到回覆，已自動結案。"

// AdvanceWaitingTickets reminds customers who have not answered a ticket
// waiting on them, and closes the ticket once the wait runs out. Both count
// from when the ticket started waiting; a ticket already due to close gets
// no reminder.
func (s *Store) AdvanceWaitingTickets(ctx context.Context) (reminded, closed int, err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)
	var l model.Lifecycle
	if err = tx.QueryRow(ctx, `SELECT waiting_remind_hours,waiting_close_hours FROM automation_settings WHERE id=1 FOR SHARE`).Scan(&l.RemindHours, &l.CloseHours); err != nil {
		return 0, 0, err
	}
	due := func(query string, hours ...any) ([]int64, error) {
		rows, err := tx.Query(ctx, query, hours...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[int64])
	}
	if l.RemindHours > 0 {
		ids, err := due(`SELECT id FROM tickets WHERE status='waiting' AND reminded_at IS NULL
 AND waiting_at<=clock_timestamp()-$1*interval '1 hour' AND ($2=0 OR waiting_at>clock_timestamp()-$2*interval '1 hour')
 ORDER BY waiting_at,id LIMIT 50 FOR UPDATE SKIP LOCKED`, l.RemindHours, l.CloseHours)
		if err != nil {
			return 0, 0, err
		}
		for _, id := range ids {
			if _, err = tx.Exec(ctx, `UPDATE tickets SET reminded_at=clock_timestamp() WHERE id=$1`, id); err != nil {
				return 0, 0, err
			}
			if err = s.queueReminderEmails(ctx, tx, id); err != nil {
				return 0, 0, err
			}
		}
		reminded = len(ids)
	}
	if l.CloseHours > 0 {
		ids, err := due(`SELECT id FROM tickets WHERE status='waiting' AND waiting_at<=clock_timestamp()-$1*interval '1 hour'
 ORDER BY waiting_at,id LIMIT 50 FOR UPDATE SKIP LOCKED`, l.CloseHours)
		if err != nil {
			return 0, 0, err
		}
		var actor int64
		if len(ids) > 0 {
			if err = tx.QueryRow(ctx, `INSERT INTO users(auth0_id,name,email,role)
  VALUES('system:automation','自動回覆','automation@local.invalid','user')
  ON CONFLICT(auth0_id) DO UPDATE SET auth0_id=EXCLUDED.auth0_id RETURNING id`).Scan(&actor); err != nil {
				return 0, 0, err
			}
		}
		for _, id := range ids {
			if _, err = tx.Exec(ctx, `UPDATE tickets SET status='closed',auto_closed_at=clock_timestamp(),updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, id); err != nil {
				return 0, 0, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO ticket_messages(ticket_id,user_id,body,internal,source) VALUES($1,$2,$3,false,'automation')`, id, actor, waitingClosedBody); err != nil {
				return 0, 0, err
			}
			if err = recordEvent(ctx, tx, id, actor, "status", "waiting", "closed"); err != nil {
				return 0, 0, err
			}
			if err = notifyAutoClosed(ctx, tx, id); err != nil {
				return 0, 0, err
			}
			if _, err = tx.Exec(ctx, `UPDATE email_notifications SET state='cancelled' WHERE ticket_id=$1 AND state='pending'`, id); err != nil {
				return 0, 0, err
			}
		}
		closed = len(ids)
	}
	return reminded, closed, tx.Commit(ctx)
}
