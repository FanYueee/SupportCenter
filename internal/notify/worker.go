package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/database"
	"supportcenter/internal/model"
	"supportcenter/web"
)

type Sender interface {
	Send(context.Context, Message) error
}

type Worker struct {
	Store  *database.Store
	Sender Sender
	Origin string
	Site   web.Site
	Log    *slog.Logger
}

type notification struct {
	ID, TicketID, MessageID     int64
	Subject, Category, To, Kind string
	Attempts                    int
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
	// Skip notifications that are no longer relevant or have waited too long
	// for SMTP setup: the recipient must still be staff in the ticket's
	// department or working the ticket.
	_, err := w.Store.Pool.Exec(ctx, `UPDATE email_notifications n SET state='cancelled',lease_until=NULL
	 WHERE state IN ('pending','sending') AND (created_at<now()-interval '7 days' OR NOT EXISTS (
	 SELECT 1 FROM tickets t JOIN users u ON u.id=n.recipient_user_id
	 WHERE t.id=n.ticket_id AND t.status NOT IN ('solved','closed')
	 AND u.role IN ('support','admin') AND u.email_verified
	 AND (EXISTS(SELECT 1 FROM staff_departments d WHERE d.user_id=u.id AND d.category=t.category)
	  OR EXISTS(SELECT 1 FROM ticket_assignees a WHERE a.ticket_id=t.id AND a.user_id=u.id))))`)
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
	 FROM picked WHERE n.id=picked.id RETURNING n.id,n.ticket_id,n.recipient_user_id,n.kind,n.attempts,n.message_id
	)
	 SELECT c.id,c.ticket_id,COALESCE(c.message_id,0),t.subject,t.category,u.email,c.kind,c.attempts FROM claimed c JOIN users u ON u.id=c.recipient_user_id JOIN tickets t ON t.id=c.ticket_id`,
	).Scan(&item.ID, &item.TicketID, &item.MessageID, &item.Subject, &item.Category, &item.To, &item.Kind, &item.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	msg, err := w.staffMessage(ctx, item)
	if err == nil {
		err = w.Sender.Send(ctx, msg)
	}
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

// staffMessage tells staff what happened and quotes what the customer wrote,
// with the ticket's particulars, so the email can be read without signing in.
func (w *Worker) staffMessage(ctx context.Context, item notification) (Message, error) {
	number := fmt.Sprintf("#%04d", item.TicketID)
	view := web.NewEmailView(w.Site, w.Origin, time.Now())
	// lead says what happened; quoting adds what follows when there is a message.
	var subject, lead, quoting string
	switch item.Kind {
	case "assignment":
		subject = fmt.Sprintf("[%s] 客服單 %s 已指派給你：%s", view.Site, number, item.Subject)
		view.Label = "指派給你"
		view.Heading = "客服單 " + number + " 已指派給你"
		lead, quoting = "請查看並回覆這張客服單", "客戶最新的訊息如下"
	case "reply":
		subject = fmt.Sprintf("[%s] 客服單 %s 有客戶回覆：%s", view.Site, number, item.Subject)
		view.Label = "客戶回覆"
		view.Heading = "客服單 " + number + " 有客戶回覆"
		lead, quoting = "客戶回覆了這張客服單", "內容如下"
	default:
		department := model.DepartmentName(item.Category)
		subject = fmt.Sprintf("[%s] %s客服單 %s：%s", view.Site, model.Category(item.Category), number, item.Subject)
		view.Label = department
		view.Heading = "客服單 " + number + " 已送至" + department
		lead, quoting = "有一張"+model.Category(item.Category)+"客服單需要處理", "客戶的訊息如下"
	}
	view.Intro = []string{lead + "。"}
	var owner, email, priority, level string
	var until *time.Time
	if err := w.Store.Pool.QueryRow(ctx, `SELECT u.name,u.email,t.priority,u.priority_level,u.priority_until FROM tickets t JOIN users u ON u.id=t.user_id WHERE t.id=$1`, item.TicketID).
		Scan(&owner, &email, &priority, &level, &until); err != nil {
		return Message{}, err
	}
	customer := owner
	if email != "" {
		customer += "（" + email + "）"
	}
	view.Details = []web.EmailDetail{
		{Label: "單號", Value: number},
		{Label: "主旨", Value: item.Subject},
		{Label: "客戶", Value: customer},
		{Label: "類別", Value: model.Category(item.Category)},
		{Label: "優先級", Value: model.Priority(priority)},
		{Label: "支援等級", Value: model.SupportTier(model.PrioritySupport{Level: level, Until: until}.Current(time.Now()))},
	}
	// The message the email is about, or else what the customer said last.
	var body, author string
	var attachments int
	err := w.Store.Pool.QueryRow(ctx, `SELECT m.body,u.name,(SELECT count(*) FROM ticket_attachments a WHERE a.message_id=m.id)
	 FROM ticket_messages m JOIN tickets t ON t.id=m.ticket_id JOIN users u ON u.id=m.user_id
	 WHERE m.ticket_id=$1 AND NOT m.internal AND NOT m.service_notice AND m.source='human'
	 AND CASE WHEN $2>0 THEN m.id=$2 ELSE m.user_id=t.user_id OR u.role='user' END
	 ORDER BY m.id DESC LIMIT 1`, item.TicketID, item.MessageID).Scan(&body, &author, &attachments)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil // nothing to quote: the email still says what happened
	} else if err == nil {
		refs, err := w.Store.TicketReferences(ctx, item.TicketID, true)
		if err != nil {
			return Message{}, err
		}
		// A message may be only files.
		if view.Quote = quoted(model.ReferenceText(body, refs)); view.Quote != "" {
			view.QuoteAuthor = author
			view.Intro[0] = lead + "，" + quoting + "。"
		}
		if attachments > 0 {
			view.Note = fmt.Sprintf("這則訊息附有 %d 個檔案，請開啟客服單查看。", attachments)
		}
	}
	if err != nil {
		return Message{}, err
	}
	view.Action, view.Link = "開啟客服單", fmt.Sprintf("%s/admin/tickets/%d", w.Origin, item.TicketID)
	return emailMessage(ctx, item.To, subject, view)
}

// quoted trims a message for quoting, cutting a very long one short.
func quoted(body string) string {
	body = strings.TrimSpace(body)
	if r := []rune(body); len(r) > maxQuotedReply {
		return string(r[:maxQuotedReply]) + "…"
	}
	return body
}
