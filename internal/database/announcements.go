package database

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

var ErrAnnouncementLimit = errors.New("announcement limit reached")

const announcementColumns = `id,title,body,color,enabled,version,created_at,updated_at`

func ValidAnnouncement(v model.Announcement) bool {
	validText := func(s string, limit int) bool {
		return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= limit
	}
	return validText(v.Title, 120) && validText(v.Body, 2000) && (v.Color == "blue" || v.Color == "green" || v.Color == "red")
}

// VisibleAnnouncements contains only published, plain-text notices. The HTTP
// layer displays them only inside the authenticated workspace.
func (s *Store) VisibleAnnouncements(ctx context.Context) ([]model.Announcement, error) {
	rows, err := s.Pool.Query(ctx, `SELECT `+announcementColumns+` FROM announcements WHERE enabled ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Announcement])
}

func (s *Store) ManagedAnnouncements(ctx context.Context, actor model.User) ([]model.Announcement, error) {
	if actor.Role != "admin" {
		return nil, ErrInvalid
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+announcementColumns+` FROM announcements WHERE EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='admin') ORDER BY id DESC`, actor.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.Announcement])
}

func (s *Store) SaveAnnouncement(ctx context.Context, actor model.User, v model.Announcement) (int64, error) {
	if actor.Role != "admin" || actor.ID < 1 || v.ID < 0 || !ValidAnnouncement(v) || (v.ID > 0 && v.Version < 1) {
		return 0, ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var role string
	if err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR SHARE`, actor.ID).Scan(&role); err != nil {
		return 0, notFound(err)
	}
	if role != "admin" {
		return 0, ErrInvalid
	}
	if v.ID == 0 {
		// Serialize creation so concurrent forms cannot exceed the small banner cap.
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(827190024)`); err != nil {
			return 0, err
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM announcements`).Scan(&count); err != nil {
			return 0, err
		}
		if count >= 20 {
			return 0, ErrAnnouncementLimit
		}
		err = tx.QueryRow(ctx, `INSERT INTO announcements(title,body,color,enabled,created_by,updated_by) VALUES($1,$2,$3,$4,$5,$5) RETURNING id`, v.Title, v.Body, v.Color, v.Enabled, actor.ID).Scan(&v.ID)
	} else {
		var saved int64
		err = tx.QueryRow(ctx, `UPDATE announcements SET title=$1,body=$2,color=$3,enabled=$4,updated_by=$5,updated_at=now(),version=version+1 WHERE id=$6 AND version=$7 RETURNING id`, v.Title, v.Body, v.Color, v.Enabled, actor.ID, v.ID, v.Version).Scan(&saved)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, err
	}
	return v.ID, tx.Commit(ctx)
}

func (s *Store) ChangeAnnouncement(ctx context.Context, actor model.User, id, version int64, action string) error {
	if actor.Role != "admin" || actor.ID < 1 || id < 1 || version < 1 {
		return ErrInvalid
	}
	var query string
	switch action {
	case "publish", "disable":
		query = `UPDATE announcements SET enabled=$4,version=version+1,updated_at=now(),updated_by=$3 WHERE id=$1 AND version=$2 AND EXISTS(SELECT 1 FROM users WHERE id=$3 AND role='admin')`
		result, err := s.Pool.Exec(ctx, query, id, version, actor.ID, action == "publish")
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrConflict
		}
	case "delete":
		result, err := s.Pool.Exec(ctx, `DELETE FROM announcements WHERE id=$1 AND version=$2 AND EXISTS(SELECT 1 FROM users WHERE id=$3 AND role='admin')`, id, version, actor.ID)
		if err != nil {
			return err
		}
		if result.RowsAffected() != 1 {
			return ErrConflict
		}
	default:
		return ErrInvalid
	}
	return nil
}
