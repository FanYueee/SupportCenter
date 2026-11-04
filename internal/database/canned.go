package database

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

var ErrCannedLimit = errors.New("canned reply limit reached")

func ValidCannedReply(v model.CannedReply) bool {
	validText := func(s string, limit int) bool {
		return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= limit
	}
	return validText(v.Title, 80) && validText(v.Body, 4000)
}

// CannedReplies are offered to every staff member writing a reply.
func (s *Store) CannedReplies(ctx context.Context) ([]model.CannedReply, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,title,body,version FROM canned_replies ORDER BY title,id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[model.CannedReply])
}

func (s *Store) SaveCannedReply(ctx context.Context, actor model.User, v model.CannedReply) error {
	if actor.Role != "admin" || actor.ID < 1 || v.ID < 0 || !ValidCannedReply(v) || (v.ID > 0 && v.Version < 1) {
		return ErrInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role string
	if err = tx.QueryRow(ctx, `SELECT role FROM users WHERE id=$1 FOR SHARE`, actor.ID).Scan(&role); err != nil {
		return notFound(err)
	}
	if role != "admin" {
		return ErrInvalid
	}
	if v.ID == 0 {
		// Serialize creation so concurrent forms cannot pass the cap.
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(827190025)`); err != nil {
			return err
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM canned_replies`).Scan(&count); err != nil {
			return err
		}
		if count >= 100 {
			return ErrCannedLimit
		}
		if _, err = tx.Exec(ctx, `INSERT INTO canned_replies(title,body,created_by,updated_by) VALUES($1,$2,$3,$3)`, v.Title, v.Body, actor.ID); err != nil {
			return err
		}
	} else {
		tag, err := tx.Exec(ctx, `UPDATE canned_replies SET title=$1,body=$2,updated_by=$3,updated_at=now(),version=version+1 WHERE id=$4 AND version=$5`, v.Title, v.Body, actor.ID, v.ID, v.Version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteCannedReply(ctx context.Context, actor model.User, id, version int64) error {
	if actor.Role != "admin" || actor.ID < 1 || id < 1 || version < 1 {
		return ErrInvalid
	}
	tag, err := s.Pool.Exec(ctx, `DELETE FROM canned_replies WHERE id=$1 AND version=$2 AND EXISTS(SELECT 1 FROM users WHERE id=$3 AND role='admin')`, id, version, actor.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
