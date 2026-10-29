package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/database"
	"supportcenter/web"
)

type Sender interface {
	Send(context.Context, Message) error
}

type Worker struct {
	Store  *database.Store
	Sender Sender
	Origin string
	// Origins maps a brand to its site, so customer links go back to the site
	// the ticket was opened on. Brands not listed use Origin.
	Origins map[string]string
	Log     *slog.Logger
}

type notification struct {
	ID, TicketID    int64
	To, Kind, Brand string
	Attempts        int
}

// site returns a brand's display name and its site origin.
func (w *Worker) site(brand string) (string, string) {
	if origin := w.Origins[brand]; origin != "" {
		return web.BrandByKey(brand).Name, origin
	}
	return web.BrandByKey(brand).Name, w.Origin
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		processed, err := w.ProcessOne(ctx)
		if err != nil && ctx.Err() == nil {
			w.Log.Error("email notification processing failed", "error_type", fmt.Sprintf("%T", err))
		}
		sent, err := w.ProcessCustomer(ctx)
		if err != nil && ctx.Err() == nil {
			w.Log.Error("customer email processing failed", "error_type", fmt.Sprintf("%T", err))
		}
		if processed || sent {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	// Skip notifications that are no longer relevant or have waited too long for SMTP setup.
	_, err := w.Store.Pool.Exec(ctx, `UPDATE email_notifications n SET state='cancelled',lease_until=NULL
	 WHERE state IN ('pending','sending') AND (created_at<now()-interval '7 days' OR NOT EXISTS (
	 SELECT 1 FROM tickets t JOIN users u ON u.id=n.recipient_user_id
	 JOIN staff_departments d ON d.user_id=u.id AND d.category='technical'
	 WHERE t.id=n.ticket_id AND t.category='technical' AND t.status NOT IN ('solved','closed')
	 AND u.role IN ('support','admin') AND u.email_verified))`)
	if err != nil {
		return false, err
	}
	var item notification
	err = w.Store.Pool.QueryRow(ctx, `WITH picked AS (
	 SELECT id FROM email_notifications WHERE available_at<=now() AND attempts<5
	 AND (state='pending' OR (state='sending' AND lease_until<now()))
	 ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1
	), claimed AS (
	 UPDATE email_notifications n SET state='sending',attempts=attempts+1,lease_until=now()+interval '2 minutes'
	 FROM picked WHERE n.id=picked.id RETURNING n.id,n.ticket_id,n.recipient_user_id,n.kind,n.attempts
	)
	 SELECT c.id,c.ticket_id,u.email,c.kind,c.attempts,t.brand FROM claimed c JOIN users u ON u.id=c.recipient_user_id JOIN tickets t ON t.id=c.ticket_id`,
	).Scan(&item.ID, &item.TicketID, &item.To, &item.Kind, &item.Attempts, &item.Brand)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	brand, _ := w.site(item.Brand)
	subject := fmt.Sprintf("[%s 支援中心] 技術客服單 #%04d", brand, item.TicketID)
	message := fmt.Sprintf("客服單 #%04d 已送至技術部門。\n\n%s/admin/tickets/%d\n", item.TicketID, w.Origin, item.TicketID)
	if item.Kind == "assignment" {
		message = fmt.Sprintf("客服單 #%04d 已指派給你。\n\n%s/admin/tickets/%d\n", item.TicketID, w.Origin, item.TicketID)
	}
	err = w.Sender.Send(ctx, Message{To: item.To, FromName: brand + " 支援中心", Subject: subject, Text: message})
	if err == nil {
		_, err = w.Store.Pool.Exec(ctx, `UPDATE email_notifications SET state='sent',sent_at=now(),lease_until=NULL WHERE id=$1 AND state='sending'`, item.ID)
		return true, err
	}
	state := "pending"
	if item.Attempts >= 5 {
		state = "failed"
	}
	backoff := time.Duration(1<<min(item.Attempts, 4)) * time.Minute
	_, updateErr := w.Store.Pool.Exec(ctx, `UPDATE email_notifications SET state=$2,available_at=now()+$3*interval '1 second',lease_until=NULL WHERE id=$1 AND state='sending'`, item.ID, state, int64(backoff.Seconds()))
	w.Log.Warn("email notification send failed", "notification_id", item.ID, "attempts", item.Attempts, "error_type", fmt.Sprintf("%T", err))
	return true, updateErr
}
