package database

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"supportcenter/internal/model"
)

type Store struct {
	Pool *pgxpool.Pool
	// MailEnabled is set when SMTP is configured; customer emails are only
	// queued then, so a later SMTP setup does not flush a stale backlog.
	MailEnabled bool
}

var ErrNotFound = errors.New("not found")
var ErrClosed = errors.New("ticket closed")
var ErrInvalid = errors.New("invalid input")

func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "10000"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{Pool: pool}, nil
}
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

const columns = `t.id,t.user_id,t.subject,t.category,t.status,t.priority,u.name,COALESCE(a.name,''),t.assigned_to,t.created_at,t.updated_at,t.version,t.assistance_mode,t.handoff_at,t.solved_at,
 CASE WHEN t.status='solved' THEN t.solved_at+(SELECT NULLIF(auto_close_days,0)*interval '24 hours' FROM automation_settings WHERE id=1) END,t.auto_closed_at`
const joins = ` FROM tickets t JOIN users u ON u.id=t.user_id LEFT JOIN users a ON a.id=t.assigned_to `

func scanTicket(row pgx.Row) (t model.Ticket, err error) {
	err = row.Scan(&t.ID, &t.UserID, &t.Subject, &t.Category, &t.Status, &t.Priority, &t.Owner, &t.Assignee, &t.AssigneeID, &t.CreatedAt, &t.UpdatedAt, &t.Version, &t.AssistanceMode, &t.HandoffAt, &t.SolvedAt, &t.AutoCloseAt, &t.AutoClosedAt)
	return t, notFound(err)
}
func (s *Store) List(ctx context.Context, u model.User, admin bool, status, priority, q string, page int, assignment string, sorts ...string) ([]model.Ticket, model.Counts, bool, error) {
	return s.list(ctx, u, admin, status, priority, q, page, assignment, "", sorts...)
}

func (s *Store) PortalList(ctx context.Context, u model.User, admin bool, status, priority, q string, page int, assignment, category, sort, phase string) ([]model.Ticket, model.Counts, bool, error) {
	if !oneOf(category, "", "other", "billing", "technical") {
		return nil, model.Counts{}, false, ErrInvalid
	}
	return s.list(ctx, u, admin, status, priority, q, page, assignment, category, sort, phase)
}

func (s *Store) list(ctx context.Context, u model.User, admin bool, status, priority, q string, page int, assignment, category string, sorts ...string) ([]model.Ticket, model.Counts, bool, error) {
	staff := admin && u.Staff()
	order := `t.updated_at DESC,t.id DESC`
	if len(sorts) > 2 {
		return nil, model.Counts{}, false, ErrInvalid
	}
	phase := ""
	if len(sorts) == 2 {
		phase = sorts[1]
	}
	if phase != "" && phase != "active" && phase != "done" {
		return nil, model.Counts{}, false, ErrInvalid
	}
	if len(sorts) >= 1 {
		switch sorts[0] {
		case "", "updated_desc":
		case "updated_asc":
			order = `t.updated_at ASC,t.id ASC`
		case "created_desc":
			order = `t.created_at DESC,t.id DESC`
		case "created_asc":
			order = `t.created_at ASC,t.id ASC`
		default:
			return nil, model.Counts{}, false, ErrInvalid
		}
	}
	var counts model.Counts
	assignmentCount, assignmentList := "", ""
	switch assignment {
	case "":
	case "mine":
		if !staff {
			return nil, counts, false, ErrInvalid
		}
		assignmentCount, assignmentList = ` AND EXISTS(SELECT 1 FROM ticket_assignees ta WHERE ta.ticket_id=t.id AND ta.user_id=$2)`, ` AND EXISTS(SELECT 1 FROM ticket_assignees ta WHERE ta.ticket_id=t.id AND ta.user_id=$2)`
	case "unassigned":
		if !staff {
			return nil, counts, false, ErrInvalid
		}
		assignmentCount, assignmentList = ` AND NOT EXISTS(SELECT 1 FROM ticket_assignees ta WHERE ta.ticket_id=t.id)`, ` AND NOT EXISTS(SELECT 1 FROM ticket_assignees ta WHERE ta.ticket_id=t.id)`
	default:
		return nil, counts, false, ErrInvalid
	}
	searchCount := ` AND ($4='' OR t.subject ILIKE '%' || $4 || '%')`
	searchList := ` AND ($5='' OR t.subject ILIKE '%' || $5 || '%')`
	var search any = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	number := strings.TrimPrefix(q, "#")
	if number != "" && number[0] >= '0' && number[0] <= '9' {
		if id, err := strconv.ParseInt(number, 10, 64); err == nil && id > 0 {
			searchCount = ` AND t.id=$4`
			searchList = ` AND t.id=$5`
			search = id
		}
	}
	err := s.Pool.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE t.status='new'),count(*) FILTER(WHERE t.status='open'),count(*) FILTER(WHERE t.status='waiting'),count(*) FILTER(WHERE t.status='on_hold'),count(*) FILTER(WHERE t.status='solved'),count(*) FILTER(WHERE t.status='closed') FROM tickets t WHERE ($1 OR t.user_id=$2 OR EXISTS(SELECT 1 FROM ticket_invites i JOIN users viewer ON viewer.id=$2 WHERE i.ticket_id=t.id AND (i.user_id=viewer.id OR (i.user_id IS NULL AND viewer.email_verified AND i.email_key=lower(viewer.email))))) AND ($3='' OR t.priority=$3)`+assignmentCount+searchCount+` AND ($5='' OR t.category=$5)`, staff, u.ID, priority, search, category).Scan(&counts.All, &counts.New, &counts.Open, &counts.Waiting, &counts.Hold, &counts.Solved, &counts.Closed)
	if err != nil {
		return nil, counts, false, err
	}
	if !staff {
		counts.Open += counts.Hold
	}
	// Search remains a bound literal even when it contains SQL or LIKE metacharacters.
	rows, err := s.Pool.Query(ctx, `SELECT `+columns+joins+`WHERE ($1 OR t.user_id=$2 OR EXISTS(SELECT 1 FROM ticket_invites i JOIN users viewer ON viewer.id=$2 WHERE i.ticket_id=t.id AND (i.user_id=viewer.id OR (i.user_id IS NULL AND viewer.email_verified AND i.email_key=lower(viewer.email))))) AND ($3='' OR t.status=$3 OR (NOT $1 AND $3='open' AND t.status='on_hold')) AND ($4='' OR t.priority=$4)`+assignmentList+searchList+` AND ($7='' OR ($7='active' AND t.status NOT IN ('solved','closed')) OR ($7='done' AND t.status IN ('solved','closed'))) AND ($8='' OR t.category=$8) ORDER BY `+order+` LIMIT 21 OFFSET $6`, staff, u.ID, status, priority, search, (page-1)*20, phase, category)
	if err != nil {
		return nil, counts, false, err
	}
	defer rows.Close()
	list := []model.Ticket{}
	for rows.Next() {
		t, e := scanTicket(rows)
		if e != nil {
			return nil, counts, false, e
		}
		list = append(list, t)
	}
	if err = rows.Err(); err != nil {
		return nil, counts, false, err
	}
	rows.Close()
	more := len(list) > 20
	if more {
		list = list[:20]
	}
	if err = s.loadTeams(ctx, list); err != nil {
		return nil, counts, false, err
	}
	return list, counts, more, nil
}
func (s *Store) Ticket(ctx context.Context, u model.User, id int64, admin bool) (model.Ticket, error) {
	t, err := scanTicket(s.Pool.QueryRow(ctx, `SELECT `+columns+joins+`WHERE t.id=$1 AND ($2 OR t.user_id=$3 OR EXISTS(SELECT 1 FROM ticket_invites i JOIN users viewer ON viewer.id=$3 WHERE i.ticket_id=t.id AND (i.user_id=viewer.id OR (i.user_id IS NULL AND viewer.email_verified AND i.email_key=lower(viewer.email)))))`, id, admin && u.Staff(), u.ID))
	if err != nil {
		return t, err
	}
	items := []model.Ticket{t}
	if err = s.loadTeams(ctx, items); err != nil {
		return t, err
	}
	return items[0], nil
}
func (s *Store) Messages(ctx context.Context, u model.User, id int64, admin bool, page int) ([]model.Message, bool, error) {
	staff := admin && u.Staff()
	rows, err := s.Pool.Query(ctx, `SELECT m.id,m.user_id,u.name,u.role,m.body,m.internal,m.created_at,m.source FROM ticket_messages m JOIN users u ON u.id=m.user_id JOIN tickets t ON t.id=m.ticket_id WHERE t.id=$1 AND ($2 OR t.user_id=$3 OR EXISTS(SELECT 1 FROM ticket_invites i JOIN users viewer ON viewer.id=$3 WHERE i.ticket_id=t.id AND (i.user_id=viewer.id OR (i.user_id IS NULL AND viewer.email_verified AND i.email_key=lower(viewer.email))))) AND ($2 OR NOT m.internal) AND NOT m.service_notice ORDER BY m.id DESC LIMIT 31 OFFSET $4`, id, staff, u.ID, (page-1)*30)
	if err != nil {
		return nil, false, err
	}
	list := []model.Message{}
	for rows.Next() {
		var m model.Message
		if err = rows.Scan(&m.ID, &m.AuthorID, &m.Author, &m.Role, &m.Body, &m.Internal, &m.CreatedAt, &m.Source); err != nil {
			rows.Close()
			return nil, false, err
		}
		list = append(list, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	more := len(list) > 30
	if more {
		list = list[:30]
	}
	ids := []int64{}
	for i := range list {
		ids = append(ids, list[i].ID)
	}
	if len(ids) > 0 {
		ar, e := s.Pool.Query(ctx, `SELECT id,message_id,name,storage_key,mime,size FROM ticket_attachments WHERE message_id=ANY($1) ORDER BY id`, ids)
		if e != nil {
			return nil, false, e
		}
		defer ar.Close()
		byID := map[int64][]model.Attachment{}
		for ar.Next() {
			var a model.Attachment
			var mid int64
			if e = ar.Scan(&a.ID, &mid, &a.Name, &a.Key, &a.MIME, &a.Size); e != nil {
				return nil, false, e
			}
			byID[mid] = append(byID[mid], a)
		}
		if e = ar.Err(); e != nil {
			return nil, false, e
		}
		for i := range list {
			list[i].Attachments = byID[list[i].ID]
		}
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, more, nil
}

// AvatarAuthor returns an author only if the viewer may read one of their messages on this ticket.
func (s *Store) AvatarAuthor(ctx context.Context, viewer model.User, ticketID, authorID int64) (model.User, error) {
	var author model.User
	err := s.Pool.QueryRow(ctx, `SELECT u.id,u.name,u.email,u.role FROM ticket_messages m JOIN tickets t ON t.id=m.ticket_id JOIN users u ON u.id=m.user_id WHERE t.id=$1 AND m.user_id=$2 AND ($3 OR t.user_id=$4 OR EXISTS(SELECT 1 FROM ticket_invites i JOIN users viewer ON viewer.id=$4 WHERE i.ticket_id=t.id AND (i.user_id=viewer.id OR (i.user_id IS NULL AND viewer.email_verified AND i.email_key=lower(viewer.email))))) AND ($3 OR NOT m.internal) LIMIT 1`, ticketID, authorID, viewer.Staff(), viewer.ID).Scan(&author.ID, &author.Name, &author.Email, &author.Role)
	return author, notFound(err)
}
func (s *Store) Staff(ctx context.Context) ([]model.User, error) {
	rows, err := s.Pool.Query(ctx, `SELECT u.id,u.name,u.email,u.role,
 ARRAY(SELECT d.category FROM staff_departments d WHERE d.user_id=u.id ORDER BY d.category)
 FROM users u WHERE u.role IN ('support','admin') AND u.auth0_id NOT LIKE 'system:%' ORDER BY u.name,u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []model.User{}
	for rows.Next() {
		var u model.User
		if err = rows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.Departments); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}
func insertMessage(ctx context.Context, tx pgx.Tx, id int64, u model.User, body string, internal bool, files []model.Attachment, operation ...model.Operation) (int64, error) {
	var key, fingerprint *string
	if len(operation) > 0 {
		key = &operation[0].Key
		fingerprint = &operation[0].Hash
	}
	var mid int64
	info := requestInfo(ctx)
	err := tx.QueryRow(ctx, `INSERT INTO ticket_messages(ticket_id,user_id,body,internal,request_key,request_hash,client_ip,user_agent) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, id, u.ID, body, internal, key, fingerprint, info.IP, info.UserAgent).Scan(&mid)
	if err != nil {
		return 0, err
	}
	for _, a := range files {
		_, err = tx.Exec(ctx, `INSERT INTO ticket_attachments(message_id,storage_key,name,mime,size) VALUES($1,$2,$3,$4,$5)`, mid, a.Key, a.Name, a.MIME, a.Size)
		if err != nil {
			return 0, err
		}
	}
	return mid, nil
}
func (s *Store) Create(ctx context.Context, u model.User, t model.Ticket, body string, files []model.Attachment, operation ...model.Operation) (int64, error) {
	return s.CreateWithParticipants(ctx, u, t, body, files, nil, operation...)
}

func (s *Store) CreateWithParticipants(ctx context.Context, u model.User, t model.Ticket, body string, files []model.Attachment, participants []string, operation ...model.Operation) (int64, error) {
	emails, err := NormalizeInviteEmails(participants, u.Email)
	if err != nil {
		return 0, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	if previous, e := checkOperation(ctx, tx, u, operation); e != nil {
		return previous, e
	}
	info := requestInfo(ctx)
	mode := t.AssistanceMode
	if mode == "" {
		mode = "human"
	}
	if mode != "human" && mode != "ai" {
		return 0, ErrInvalid
	}
	err = tx.QueryRow(ctx, `INSERT INTO tickets(user_id,subject,category,priority,client_ip,user_agent,assistance_mode,status) VALUES($1,$2,$3,$4,$5,$6,$7,CASE WHEN $7='ai' THEN 'open' ELSE 'new' END) RETURNING id`, u.ID, t.Subject, t.Category, t.Priority, info.IP, info.UserAgent, mode).Scan(&id)
	if err != nil {
		return 0, err
	}
	for _, email := range emails {
		if _, err = tx.Exec(ctx, insertInvitation, id, email, u.ID); err != nil {
			return 0, err
		}
		if err = recordEvent(ctx, tx, id, u.ID, "invite", "", email); err != nil {
			return 0, err
		}
		if err = s.queueInviteEmail(ctx, tx, id, u.ID, email); err != nil {
			return 0, err
		}
	}
	mid, err := insertMessage(ctx, tx, id, u, body, false, files, operation...)
	if err != nil {
		return 0, err
	}
	if err = insertAutomaticReply(ctx, tx, id); err != nil {
		return 0, err
	}
	if err = queueDiscord(ctx, tx, id, mid, "ticket"); err != nil {
		return 0, err
	}
	if mode == "ai" {
		if _, err = tx.Exec(ctx, `INSERT INTO ai_jobs(ticket_id,trigger_message_id) VALUES($1,$2)`, id, mid); err != nil {
			return 0, err
		}
	}
	if t.Category == "technical" {
		if err = queueTechnicalNotifications(ctx, tx, id, "technical", nil); err != nil {
			return 0, err
		}
	}
	return id, commit(ctx, tx)
}
func (s *Store) Reply(ctx context.Context, u model.User, id int64, admin bool, body string, internal bool, files []model.Attachment, operation ...model.Operation) error {
	return s.reply(ctx, u, id, admin, body, internal, files, "", 0, operation...)
}

// API replies can change status in the same transaction as the message. Existing
// HTML callers retain their original behavior through Reply above.
func (s *Store) PortalReply(ctx context.Context, u model.User, id int64, admin bool, body string, internal bool, files []model.Attachment, status string, version int64, operation model.Operation) error {
	return s.reply(ctx, u, id, admin, body, internal, files, status, version, operation)
}

func (s *Store) reply(ctx context.Context, u model.User, id int64, admin bool, body string, internal bool, files []model.Attachment, nextStatus string, expectedVersion int64, operation ...model.Operation) error {
	staff := admin && u.Staff()
	if internal && !staff {
		return ErrNotFound
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, e := checkOperation(ctx, tx, u, operation); e != nil {
		return e
	}
	var status, mode string
	var version int64
	err = tx.QueryRow(ctx, `SELECT t.status,t.assistance_mode,t.version FROM tickets t WHERE t.id=$1 AND ($2 OR t.user_id=$3 OR EXISTS(SELECT 1 FROM ticket_invites i JOIN users viewer ON viewer.id=$3 WHERE i.ticket_id=t.id AND (i.user_id=viewer.id OR (i.user_id IS NULL AND viewer.email_verified AND i.email_key=lower(viewer.email))))) FOR UPDATE`, id, staff, u.ID).Scan(&status, &mode, &version)
	if err != nil {
		return notFound(err)
	}
	if status == "closed" && !staff {
		return ErrClosed
	}
	if nextStatus != "" {
		if !staff || internal || !oneOf(nextStatus, "new", "open", "waiting", "on_hold", "solved", "closed") || (nextStatus == "new" && status != "new") {
			return ErrInvalid
		}
		if expectedVersion < 1 || expectedVersion != version {
			return ErrConflict
		}
	}
	mid, err := insertMessage(ctx, tx, id, u, body, internal, files, operation...)
	if err != nil {
		return err
	}
	// Private notes do not change the public ticket status or update timestamp.
	if !internal {
		next := "open"
		if staff {
			next = "waiting"
		}
		if nextStatus != "" {
			next = nextStatus
		}
		_, err = tx.Exec(ctx, `UPDATE tickets SET status=$2,assistance_mode=CASE WHEN $3 THEN 'human' ELSE assistance_mode END,updated_at=now(),version=version+1 WHERE id=$1`, id, next, staff)
		if err != nil {
			return err
		}
		if staff && mode == "ai" {
			if err = recordEvent(ctx, tx, id, u.ID, "handoff", "ai", "human"); err != nil {
				return err
			}
		}
		if status != next {
			if err = recordEvent(ctx, tx, id, u.ID, "status", status, next); err != nil {
				return err
			}
			if next == "solved" {
				if err = notifyResolved(ctx, tx, id); err != nil {
					return err
				}
			}
			if next == "closed" {
				if err = notifyClosed(ctx, tx, id); err != nil {
					return err
				}
			}
		}
		if next == "solved" || next == "closed" {
			if _, err = tx.Exec(ctx, `UPDATE email_notifications SET state='cancelled' WHERE ticket_id=$1 AND state='pending'`, id); err != nil {
				return err
			}
		}
		if !staff && mode == "ai" {
			if _, err = tx.Exec(ctx, `INSERT INTO ai_jobs(ticket_id,trigger_message_id) VALUES($1,$2)`, id, mid); err != nil {
				return err
			}
		}
		if !staff {
			if err = queueDiscord(ctx, tx, id, mid, "reply"); err != nil {
				return err
			}
		} else if err = s.QueueReplyEmails(ctx, tx, id, u.ID); err != nil {
			return err
		}
	}
	return commit(ctx, tx)
}
func (s *Store) Update(ctx context.Context, u model.User, id int64, admin bool, status, priority string, assignee *int64, expectedVersion ...int64) error {
	team := []int64{}
	if assignee != nil {
		team = append(team, *assignee)
	}
	return s.UpdateTeam(ctx, u, id, admin, status, priority, team, expectedVersion...)
}
func (s *Store) UpdateTeam(ctx context.Context, u model.User, id int64, admin bool, status, priority string, team []int64, expectedVersion ...int64) error {
	return s.updateTeamCategory(ctx, u, id, admin, status, priority, "", team, expectedVersion...)
}
func (s *Store) UpdateTeamCategory(ctx context.Context, u model.User, id int64, admin bool, status, priority, category string, team []int64, expectedVersion ...int64) error {
	return s.updateTeamCategory(ctx, u, id, admin, status, priority, category, team, expectedVersion...)
}
func (s *Store) updateTeamCategory(ctx context.Context, u model.User, id int64, admin bool, status, priority, category string, team []int64, expectedVersion ...int64) error {
	staff := admin && u.Staff()
	if !staff && status != "closed" && status != "open" && status != "new" {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var old, oldPriority, oldCategory, oldMode string
	var version int64
	err = tx.QueryRow(ctx, `SELECT status,priority,category,version,assistance_mode FROM tickets WHERE id=$1 AND ($2 OR user_id=$3) FOR UPDATE`, id, staff, u.ID).Scan(&old, &oldPriority, &oldCategory, &version, &oldMode)
	if err != nil {
		return notFound(err)
	}
	changingCategory := category != ""
	if category == "" {
		category = oldCategory
	}
	if (!staff && category != oldCategory) || (staff && changingCategory && category != "other" && category != "billing" && category != "technical") {
		return ErrInvalid
	}
	if len(expectedVersion) > 0 && version != expectedVersion[0] {
		return ErrConflict
	}
	if !staff && status == "new" && old != "new" && old != "closed" {
		return ErrInvalid
	}
	if staff {
		if status == "new" && old != "new" {
			return ErrInvalid
		}
		if old == "new" && status == "new" && len(team) > 0 {
			status = "open"
		}
	}
	var oldTeam []int64
	err = tx.QueryRow(ctx, `SELECT COALESCE(array_agg(user_id ORDER BY user_id),'{}'::bigint[]) FROM ticket_assignees WHERE ticket_id=$1`, id).Scan(&oldTeam)
	if err != nil {
		return err
	}
	if staff {
		if len(team) > 10 {
			return ErrInvalid
		}
		team = slices.Clone(team)
		slices.Sort(team)
		if slices.ContainsFunc(team, func(id int64) bool { return id < 1 }) {
			return ErrInvalid
		}
		for i := 1; i < len(team); i++ {
			if team[i] == team[i-1] {
				return ErrInvalid
			}
		}
		var validCount int
		err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE id=ANY($1::bigint[]) AND role IN ('support','admin') AND auth0_id NOT LIKE 'system:%'`, team).Scan(&validCount)
		if err != nil {
			return err
		}
		if validCount != len(team) {
			return ErrInvalid
		}
	}
	if old == status && (!staff || oldPriority == priority && oldCategory == category && slices.Equal(oldTeam, team)) {
		return commit(ctx, tx)
	}
	if staff {
		var primary *int64
		if len(team) > 0 {
			primary = &team[0]
		}
		_, err = tx.Exec(ctx, `UPDATE tickets SET status=$2,priority=$3,category=$4,assigned_to=$5,assistance_mode='human',updated_at=now(),version=version+1 WHERE id=$1`, id, status, priority, category, primary)
	} else {
		_, err = tx.Exec(ctx, `UPDATE tickets SET status=$2,assigned_to=CASE WHEN $2='new' THEN NULL ELSE assigned_to END,assistance_mode=CASE WHEN $2='new' THEN 'human' ELSE assistance_mode END,updated_at=now(),version=version+1 WHERE id=$1 AND user_id=$3`, id, status, u.ID)
	}
	if err != nil {
		return err
	}
	if status == "solved" || status == "closed" {
		if _, err = tx.Exec(ctx, `UPDATE email_notifications SET state='cancelled' WHERE ticket_id=$1 AND state='pending'`, id); err != nil {
			return err
		}
	}
	if staff && !slices.Equal(oldTeam, team) {
		if _, err = tx.Exec(ctx, `DELETE FROM ticket_assignees WHERE ticket_id=$1`, id); err != nil {
			return err
		}
		if len(team) > 0 {
			if _, err = tx.Exec(ctx, `INSERT INTO ticket_assignees(ticket_id,user_id) SELECT $1,unnest($2::bigint[])`, id, team); err != nil {
				return err
			}
		}
	}
	if !staff && status == "new" && len(oldTeam) > 0 {
		if _, err = tx.Exec(ctx, `DELETE FROM ticket_assignees WHERE ticket_id=$1`, id); err != nil {
			return err
		}
	}
	if old != status {
		if err = recordEvent(ctx, tx, id, u.ID, "status", old, status); err != nil {
			return err
		}
		if status == "solved" {
			if err = notifyResolved(ctx, tx, id); err != nil {
				return err
			}
		} else if status == "closed" {
			if err = notifyClosed(ctx, tx, id); err != nil {
				return err
			}
		}
	}
	teamText := func(ids []int64) string {
		parts := make([]string, len(ids))
		for i, v := range ids {
			parts[i] = fmt.Sprint(v)
		}
		return strings.Join(parts, ",")
	}
	if !staff && status == "new" && len(oldTeam) > 0 {
		if err = recordEvent(ctx, tx, id, u.ID, "assignment", teamText(oldTeam), ""); err != nil {
			return err
		}
	}
	if staff {
		if oldMode == "ai" {
			if err = recordEvent(ctx, tx, id, u.ID, "handoff", "ai", "human"); err != nil {
				return err
			}
		}
		if oldPriority != priority {
			if err = recordEvent(ctx, tx, id, u.ID, "priority", oldPriority, priority); err != nil {
				return err
			}
		}
		if oldCategory != category {
			if err = recordEvent(ctx, tx, id, u.ID, "category", oldCategory, category); err != nil {
				return err
			}
			body := "問題類別由「" + model.Category(oldCategory) + "」改為「" + model.Category(category) + "」"
			info := requestInfo(ctx)
			if _, err = tx.Exec(ctx, `INSERT INTO ticket_messages(ticket_id,user_id,body,source,client_ip,user_agent) VALUES($1,$2,$3,'system',$4,$5)`, id, u.ID, body, info.IP, info.UserAgent); err != nil {
				return err
			}
			if category == "technical" {
				if err = queueTechnicalNotifications(ctx, tx, id, "technical", nil); err != nil {
					return err
				}
			} else if oldCategory == "technical" {
				if _, err = tx.Exec(ctx, `UPDATE email_notifications SET state='cancelled' WHERE ticket_id=$1 AND state='pending'`, id); err != nil {
					return err
				}
			}
		}
		if !slices.Equal(oldTeam, team) {
			if err = recordEvent(ctx, tx, id, u.ID, "assignment", teamText(oldTeam), teamText(team)); err != nil {
				return err
			}
			if category == "technical" {
				added := []int64{}
				for _, personID := range team {
					if !slices.Contains(oldTeam, personID) {
						added = append(added, personID)
					}
				}
				if len(added) > 0 {
					if err = queueTechnicalNotifications(ctx, tx, id, "assignment", added); err != nil {
						return err
					}
				}
			}
		}
	}
	return commit(ctx, tx)
}
func (s *Store) Attachment(ctx context.Context, u model.User, id int64) (model.Attachment, error) {
	var a model.Attachment
	err := s.Pool.QueryRow(ctx, `SELECT a.id,a.name,a.storage_key,a.mime,a.size FROM ticket_attachments a JOIN ticket_messages m ON m.id=a.message_id JOIN tickets t ON t.id=m.ticket_id WHERE a.id=$1 AND ($2 OR (NOT m.internal AND (t.user_id=$3 OR EXISTS(SELECT 1 FROM ticket_invites i JOIN users viewer ON viewer.id=$3 WHERE i.ticket_id=t.id AND (i.user_id=viewer.id OR (i.user_id IS NULL AND viewer.email_verified AND i.email_key=lower(viewer.email)))))))`, id, u.Staff(), u.ID).Scan(&a.ID, &a.Name, &a.Key, &a.MIME, &a.Size)
	return a, notFound(err)
}
func (s *Store) User(ctx context.Context, subject, name, email string, verified ...bool) (model.User, error) {
	var u model.User
	isVerified := len(verified) > 0 && verified[0]
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return u, err
	}
	defer tx.Rollback(ctx)
	if isVerified {
		if err = s.claimAddress(ctx, tx, subject, email); err != nil {
			return u, err
		}
	}
	// A sign-in method added to an account leaves the account's name and address alone.
	err = tx.QueryRow(ctx, `SELECT u.id,u.name,u.email,u.role,u.signature FROM user_logins l JOIN users u ON u.id=l.user_id WHERE l.subject=$1`, subject).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.Signature)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO users(auth0_id,name,email,email_verified) VALUES($1,$2,$3,$4) ON CONFLICT(auth0_id) DO UPDATE SET name=CASE WHEN users.name_locked THEN users.name ELSE EXCLUDED.name END,email=EXCLUDED.email,email_verified=EXCLUDED.email_verified RETURNING id,name,email,role,signature`, subject, name, email, isVerified).Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.Signature)
	}
	if err == nil && isVerified {
		_, err = tx.Exec(ctx, `UPDATE ticket_invites SET user_id=$1
 WHERE user_id IS NULL AND email_key=lower($2)`, u.ID, u.Email)
	}
	if err != nil {
		return u, err
	}
	return u, commit(ctx, tx)
}

// claimAddress gives a sign-in method seen for the first time with a verified
// address the account already kept for that address: the one the Portal
// created, so the person sees the same tickets on both sites, or else the
// oldest verified one, so Google and a passkey reach the same account. It
// shares PortalUser's lock, so the two cannot race.
func (s *Store) claimAddress(ctx context.Context, tx pgx.Tx, subject, email string) error {
	key := strings.ToLower(strings.TrimSpace(email))
	if key == "" || strings.HasPrefix(subject, "portal:") || strings.HasPrefix(subject, "system:") {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "portal-user:"+key); err != nil {
		return err
	}
	var known bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE auth0_id=$1) OR EXISTS(SELECT 1 FROM user_logins WHERE subject=$1)`, subject).Scan(&known)
	if err != nil || known {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE users SET auth0_id=$1 WHERE auth0_id='portal:'||$2`, subject, key)
	if err != nil || tag.RowsAffected() > 0 {
		return err
	}
	var owner int64
	err = tx.QueryRow(ctx, `SELECT id FROM users WHERE lower(trim(email))=$1 AND email_verified
 AND auth0_id NOT LIKE 'system:%' AND auth0_id NOT LIKE 'portal:%' ORDER BY id LIMIT 1`, key).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO user_logins(subject,user_id) VALUES($1,$2)`, subject, owner)
	}
	if err == nil {
		// Shown in the account's history, so its owner notices a method they did not add.
		err = s.LogAccountEvent(ctx, tx, owner, "link", "")
	}
	return err
}
func (s *Store) SetRole(ctx context.Context, subject, role string) error {
	if role != "user" && role != "support" && role != "admin" {
		return ErrInvalid
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE users SET role=$2 WHERE auth0_id=$1 OR id=(SELECT user_id FROM user_logins WHERE subject=$1)`, subject, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("user must log in once before assigning a role")
	}
	return nil
}
