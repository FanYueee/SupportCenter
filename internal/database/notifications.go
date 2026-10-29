package database

import (
	"context"
	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

// Recheck ticket membership so a revoked invitation cannot leak its subject.
const notificationAccess = `n.user_id=$1 AND (t.user_id=$1 OR EXISTS(
 SELECT 1 FROM ticket_invites i JOIN users viewer ON viewer.id=$1
 WHERE i.ticket_id=t.id AND (i.user_id=viewer.id OR (i.user_id IS NULL AND viewer.email_verified AND i.email_key=lower(viewer.email)))))`

func notifyClosed(ctx context.Context, tx pgx.Tx, id int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO user_notifications(user_id,ticket_id,kind,cycle_at)
 SELECT DISTINCT u.id,t.id,'closed',e.created_at FROM tickets t
 JOIN LATERAL (SELECT created_at FROM ticket_events
  WHERE ticket_id=t.id AND kind='status' AND current='closed' ORDER BY id DESC LIMIT 1) e ON true
 JOIN users u ON u.id=t.user_id OR EXISTS(
  SELECT 1 FROM ticket_invites i WHERE i.ticket_id=t.id AND (i.user_id=u.id OR (i.user_id IS NULL AND u.email_verified AND i.email_key=lower(u.email))))
 WHERE t.id=$1 AND t.status='closed' AND t.auto_closed_at IS NULL
 ON CONFLICT(user_id,ticket_id,kind,cycle_at) DO NOTHING`, id)
	return err
}

func (s *Store) UnreadNotifications(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM user_notifications n JOIN tickets t ON t.id=n.ticket_id
 WHERE `+notificationAccess+` AND n.read_at IS NULL`, userID).Scan(&count)
	return count, err
}
func (s *Store) Notifications(ctx context.Context, userID int64, page int) ([]model.Notification, bool, error) {
	if page < 1 || page > 1000 {
		return nil, false, ErrInvalid
	}
	rows, err := s.Pool.Query(ctx, `SELECT n.id,n.ticket_id,n.kind,t.subject,n.created_at,n.read_at,
	 CASE WHEN t.status='solved' AND n.cycle_at=t.solved_at THEN t.solved_at+(SELECT NULLIF(auto_close_days,0)*interval '24 hours' FROM automation_settings WHERE id=1) END
 FROM user_notifications n JOIN tickets t ON t.id=n.ticket_id WHERE `+notificationAccess+`
 ORDER BY n.id DESC LIMIT 21 OFFSET $2`, userID, (page-1)*20)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := []model.Notification{}
	for rows.Next() {
		var item model.Notification
		if err = rows.Scan(&item.ID, &item.TicketID, &item.Kind, &item.Subject, &item.CreatedAt, &item.ReadAt, &item.AutoCloseAt); err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(items) > 20
	if more {
		items = items[:20]
	}
	return items, more, nil
}
func (s *Store) ReadNotification(ctx context.Context, userID, id int64) (int64, error) {
	var ticketID int64
	err := s.Pool.QueryRow(ctx, `UPDATE user_notifications n SET read_at=COALESCE(n.read_at,now())
 FROM tickets t WHERE t.id=n.ticket_id AND `+notificationAccess+` AND n.id=$2 RETURNING n.ticket_id`, userID, id).Scan(&ticketID)
	return ticketID, notFound(err)
}
func (s *Store) ReadAllNotifications(ctx context.Context, userID int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE user_notifications n SET read_at=now() FROM tickets t
 WHERE t.id=n.ticket_id AND `+notificationAccess+` AND n.read_at IS NULL`, userID)
	return err
}
