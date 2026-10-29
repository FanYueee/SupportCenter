package database

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"supportcenter/internal/model"
)

var ErrConflict = errors.New("ticket changed or submission key reused")
var ErrReplayed = errors.New("submission already committed")
var ErrCommitUnknown = errors.New("commit outcome unknown")

func checkOperation(ctx context.Context, tx pgx.Tx, u model.User, operations []model.Operation) (int64, error) {
	if len(operations) == 0 {
		return 0, nil
	}
	op := operations[0]
	if op.Key == "" || op.Hash == "" {
		return 0, ErrInvalid
	}
	// Serialize retries across processes before looking up the unique message key.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("%d:%s", u.ID, op.Key)); err != nil {
		return 0, err
	}
	var id int64
	var fingerprint string
	err := tx.QueryRow(ctx, `SELECT ticket_id,request_hash FROM ticket_messages WHERE user_id=$1 AND request_key=$2`, u.ID, op.Key).Scan(&id, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if fingerprint != op.Hash {
		return 0, ErrConflict
	}
	return id, ErrReplayed
}
func commit(ctx context.Context, tx pgx.Tx) error {
	err := tx.Commit(ctx)
	if err == nil {
		return nil
	}
	var serverError *pgconn.PgError
	if errors.Is(err, pgx.ErrTxCommitRollback) || errors.As(err, &serverError) {
		return err
	}
	// A lost COMMIT acknowledgement may still mean success. Keep files and let a
	// retry with the same submission key recover the original result.
	return fmt.Errorf("%w: %w", ErrCommitUnknown, err)
}
func recordEvent(ctx context.Context, tx pgx.Tx, id, actor int64, kind, previous, current string) error {
	info := requestInfo(ctx)
	_, err := tx.Exec(ctx, `INSERT INTO ticket_events(ticket_id,user_id,kind,previous,current,client_ip,user_agent) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, actor, kind, previous, current, info.IP, info.UserAgent)
	return err
}
func (s *Store) Events(ctx context.Context, u model.User, id int64) ([]model.Event, error) {
	if !u.Staff() {
		return nil, ErrNotFound
	}
	rows, err := s.Pool.Query(ctx, `SELECT u.name,e.kind,e.previous,e.current,e.created_at FROM ticket_events e JOIN users u ON u.id=e.user_id WHERE ticket_id=$1 ORDER BY e.id DESC LIMIT 20`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []model.Event{}
	for rows.Next() {
		var e model.Event
		if err = rows.Scan(&e.Actor, &e.Kind, &e.Previous, &e.Current, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
