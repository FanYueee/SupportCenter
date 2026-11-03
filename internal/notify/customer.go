package notify

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math"
	"net/textproto"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"supportcenter/web"
)

const (
	customerAttempts = 5
	maxQuotedReply   = 4000 // characters of a reply quoted in an email
)

type customerEmail struct {
	ID, TicketID, RecipientID, MessageID int64
	Kind, To, Subject                    string
	Inviter                              string
	IncludeReply, Wanted                 bool
	Attempts                             int
	ClosesAt                             *time.Time // when a reminded ticket closes by itself
}

// ProcessCustomer sends one queued customer email and reports whether one was
// claimed. It checks again at send time that the email is still wanted: the
// person may have left the ticket or turned emails off, and a reminder is
// dropped once the ticket no longer waits on them.
func (w *Worker) ProcessCustomer(ctx context.Context) (bool, error) {
	if _, err := w.Store.Pool.Exec(ctx, `UPDATE customer_emails SET state=CASE WHEN attempts>=$1 THEN 'failed' ELSE 'cancelled' END,lease_until=NULL
	 WHERE state IN ('pending','sending') AND (created_at<now()-interval '3 days' OR (state='sending' AND lease_until<now() AND attempts>=$1))`, customerAttempts); err != nil {
		return false, err
	}
	var item customerEmail
	err := w.Store.Pool.QueryRow(ctx, `WITH picked AS (
	 SELECT id FROM customer_emails WHERE available_at<=now() AND attempts<$1
	 AND (state='pending' OR (state='sending' AND lease_until<now()))
	 ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1
	), claimed AS (
	 UPDATE customer_emails e SET state='sending',attempts=attempts+1,lease_until=now()+interval '2 minutes'
	 FROM picked WHERE e.id=picked.id
	 RETURNING e.id,e.ticket_id,e.kind,e.recipient_email,e.recipient_user_id,e.actor_user_id,e.message_id,e.attempts
	)
	 SELECT c.id,c.ticket_id,c.kind,c.recipient_email,COALESCE(c.recipient_user_id,0),COALESCE(c.message_id,0),c.attempts,t.subject,COALESCE(inviter.email,''),s.include_reply,
	 t.waiting_at+(SELECT NULLIF(waiting_close_hours,0)*interval '1 hour' FROM automation_settings WHERE id=1),
	 CASE WHEN c.kind='invite' THEN s.on_invite AND EXISTS(SELECT 1 FROM ticket_invites i WHERE i.ticket_id=t.id AND i.email_key=c.recipient_email)
	 ELSE (CASE c.kind WHEN 'created' THEN s.on_create WHEN 'reminder' THEN t.status='waiting' ELSE s.on_reply END) AND EXISTS(SELECT 1 FROM users u WHERE u.id=c.recipient_user_id AND u.email_notify AND u.email_verified AND lower(u.email)=c.recipient_email
	  AND (t.user_id=u.id OR EXISTS(SELECT 1 FROM ticket_invites i WHERE i.ticket_id=t.id AND (i.user_id=u.id OR (i.user_id IS NULL AND i.email_key=lower(u.email)))))) END
	 FROM claimed c JOIN tickets t ON t.id=c.ticket_id JOIN email_settings s ON s.id=1 LEFT JOIN users inviter ON inviter.id=c.actor_user_id`, customerAttempts,
	).Scan(&item.ID, &item.TicketID, &item.Kind, &item.To, &item.RecipientID, &item.MessageID, &item.Attempts, &item.Subject, &item.Inviter, &item.IncludeReply, &item.ClosesAt, &item.Wanted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	msg, err := Message{}, error(nil)
	if item.Wanted {
		msg, err = w.customerMessage(ctx, item)
	}
	// A reply email whose reply is gone is no longer wanted either.
	if !item.Wanted || errors.Is(err, pgx.ErrNoRows) {
		_, err = w.Store.Pool.Exec(ctx, `UPDATE customer_emails SET state='cancelled',lease_until=NULL WHERE id=$1 AND state='sending'`, item.ID)
		return true, err
	}
	if err != nil {
		return true, err
	}
	if sendErr := w.Sender.Send(ctx, msg); sendErr != nil {
		state := "pending"
		if item.Attempts >= customerAttempts {
			state = "failed"
		}
		backoff := time.Duration(1<<min(item.Attempts, 4)) * time.Minute
		if _, err = w.Store.Pool.Exec(ctx, `UPDATE customer_emails SET state=$2,available_at=now()+$3*interval '1 second',lease_until=NULL WHERE id=$1 AND state='sending'`, item.ID, state, int64(backoff.Seconds())); err != nil {
			return true, err
		}
		w.Log.Warn("customer email send failed", "email_id", item.ID, "attempts", item.Attempts, "code", MailErrorCode(sendErr))
		return true, w.Store.RecordEmailResult(ctx, MailErrorCode(sendErr))
	}
	if _, err = w.Store.Pool.Exec(ctx, `UPDATE customer_emails SET state='sent',sent_at=now(),lease_until=NULL WHERE id=$1 AND state='sending'`, item.ID); err != nil {
		return true, err
	}
	return true, w.Store.RecordEmailResult(ctx, "")
}

func (w *Worker) customerMessage(ctx context.Context, item customerEmail) (Message, error) {
	number := fmt.Sprintf("#%04d", item.TicketID)
	view := web.NewEmailView(w.Site, w.Origin, time.Now())
	view.Link, view.SettingsURL = fmt.Sprintf("%s/tickets/%d", w.Origin, item.TicketID), w.Origin+"/account"
	var subject string
	switch item.Kind {
	case "created":
		subject = fmt.Sprintf("[%s] 已收到您的客服單 %s：%s", view.Site, number, item.Subject)
		view.Label = "已收到"
		view.Heading = "已收到您的客服單 " + number
		view.Intro = []string{"我們已收到您的客服單，有新的回覆時會再寄信通知您。"}
		view.Details = []web.EmailDetail{{Label: "單號", Value: number}, {Label: "主旨", Value: item.Subject}}
		view.Action = "查看客服單"
	case "reminder":
		subject = fmt.Sprintf("[%s] 客服單 %s 正在等待您的回覆：%s", view.Site, number, item.Subject)
		view.Label = "等待回覆"
		view.Heading = "客服單 " + number + " 正在等待您的回覆"
		view.Intro = []string{"客服人員已回覆您的客服單，目前正在等待您的回應。"}
		if item.ClosesAt != nil {
			hours := max(1, int(math.Ceil(time.Until(*item.ClosesAt).Hours())))
			view.Intro = append(view.Intro, fmt.Sprintf("若 %d 小時內沒有回覆，客服單將自動結案。問題已經解決的話，不需要回覆。", hours))
		}
		view.Details = []web.EmailDetail{{Label: "單號", Value: number}, {Label: "主旨", Value: item.Subject}}
		view.Action = "查看及回覆"
	case "invite":
		// Only the inviter's verified address and the number: no ticket text,
		// so invitations cannot carry someone else's message to a stranger.
		subject = fmt.Sprintf("[%s] 您受邀參與客服單 %s", view.Site, number)
		view.Label = "參與邀請"
		view.Heading = "您受邀參與客服單 " + number
		view.Intro = []string{fmt.Sprintf("%s 邀請您參與 %s 的客服單，使用受邀的 Email 登入或註冊後，即可查看及回覆。", item.Inviter, view.Site)}
		view.Details = []web.EmailDetail{{Label: "單號", Value: number}, {Label: "邀請人", Value: item.Inviter}, {Label: "受邀 Email", Value: item.To}}
		view.Note = "如果您不認識邀請人，可以忽略這封信。"
		view.Action = "查看客服單"
	default:
		var body, source, author string
		var attachments int
		err := w.Store.Pool.QueryRow(ctx, `SELECT m.body,m.source,u.name,(SELECT count(*) FROM ticket_attachments a WHERE a.message_id=m.id)
		 FROM ticket_messages m JOIN users u ON u.id=m.user_id
		 WHERE m.id=$1 AND m.ticket_id=$2 AND NOT m.internal AND NOT m.service_notice AND (m.source='ai' OR (m.source='human' AND u.role IN ('support','admin')))`,
			item.MessageID, item.TicketID).Scan(&body, &source, &author, &attachments)
		if err != nil {
			return Message{}, err
		}
		if source == "ai" {
			author = "AI 助理"
		}
		subject = fmt.Sprintf("[%s] 客服單 %s 有新回覆：%s", view.Site, number, item.Subject)
		view.Label = "新回覆"
		view.Heading = "客服單 " + number + " 有新回覆"
		view.Details = []web.EmailDetail{{Label: "單號", Value: number}, {Label: "主旨", Value: item.Subject}}
		if item.IncludeReply {
			view.Intro = []string{"您的客服單有新的回覆，內容如下。"}
			view.QuoteAuthor = author
			view.Quote = strings.TrimSpace(body)
			if r := []rune(view.Quote); len(r) > maxQuotedReply {
				view.Quote = string(r[:maxQuotedReply]) + "…"
			}
			if attachments > 0 {
				view.Note = fmt.Sprintf("這則回覆附有 %d 個檔案，請登入查看。", attachments)
			}
		} else {
			view.Intro = []string{"您的客服單有新的回覆，請登入支援中心查看回覆內容。"}
		}
		view.Action = "查看及回覆"
	}
	return emailMessage(ctx, item.To, subject, view)
}

func emailMessage(ctx context.Context, to, subject string, view web.EmailView) (Message, error) {
	html, err := web.EmailHTML(ctx, view)
	if err != nil {
		return Message{}, err
	}
	return Message{To: to, FromName: view.Site, Subject: subject, Text: web.EmailText(view), HTML: html}, nil
}

// TestEmail is what the settings page sends to check the SMTP setup.
func TestEmail(ctx context.Context, to string, site web.Site, origin string) (Message, error) {
	view := web.NewEmailView(site, origin, time.Now())
	view.Label = "測試信"
	view.Heading = "Email 通知可以正常寄出"
	view.Intro = []string{"支援中心可以正常寄信。客戶會在開單、客服回覆、客服單等待回覆或受邀參與時收到通知。"}
	view.Action, view.Link = "開啟 Email 通知設定", origin+"/admin/email"
	return emailMessage(ctx, to, "["+view.Site+"] 測試信", view)
}

// MailErrorCode sorts a send failure into a code the settings page explains,
// without storing the server's reply.
func MailErrorCode(err error) string {
	var reply *textproto.Error
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &reply) && (reply.Code == 530 || reply.Code == 534 || reply.Code == 535):
		return "auth"
	case errors.As(err, &reply) && reply.Code >= 500:
		return "rejected"
	case errors.As(err, &reply):
		return "unavailable"
	case errors.As(err, &unknownCA), errors.As(err, &hostname), errors.As(err, &invalid), errors.As(err, &record), strings.Contains(err.Error(), "STARTTLS"):
		return "tls"
	}
	return "network"
}
