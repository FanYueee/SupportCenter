package database

import (
	"context"

	"supportcenter/internal/model"
)

// Only the owner may confirm an AI answer. A version check prevents a stale
// confirmation from marking a newer customer message as resolved.
func (s *Store) ResolveAI(ctx context.Context, owner model.User, ticketID, expectedVersion int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, mode string
	var version int64
	if err = tx.QueryRow(ctx, `SELECT status,assistance_mode,version FROM tickets WHERE id=$1 AND user_id=$2 FOR UPDATE`, ticketID, owner.ID).Scan(&status, &mode, &version); err != nil {
		return notFound(err)
	}
	if version != expectedVersion {
		return ErrConflict
	}
	if mode != "ai" || status != "waiting" {
		return ErrInvalid
	}
	if _, err = tx.Exec(ctx, `UPDATE tickets SET status='solved',updated_at=now(),version=version+1 WHERE id=$1`, ticketID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE ai_jobs SET state='cancelled',lease_until=NULL WHERE ticket_id=$1 AND state IN ('pending','processing')`, ticketID); err != nil {
		return err
	}
	if err = recordEvent(ctx, tx, ticketID, owner.ID, "status", "waiting", "solved"); err != nil {
		return err
	}
	if err = notifyResolved(ctx, tx, ticketID); err != nil {
		return err
	}
	return commit(ctx, tx)
}

// RequestHuman is owner-only and safe to repeat after a successful handoff.
func (s *Store) RequestHuman(ctx context.Context, owner model.User, ticketID int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, mode string
	err = tx.QueryRow(ctx, `SELECT status,assistance_mode FROM tickets WHERE id=$1 AND user_id=$2 FOR UPDATE`, ticketID, owner.ID).Scan(&status, &mode)
	if err != nil {
		return notFound(err)
	}
	if mode == "human" {
		return tx.Commit(ctx)
	}
	if status == "closed" {
		return ErrClosed
	}
	_, err = tx.Exec(ctx, `UPDATE tickets SET assistance_mode='human',handoff_at=now(),status='open',assigned_to=NULL,updated_at=now(),version=version+1 WHERE id=$1`, ticketID)
	if err != nil {
		return err
	}
	if err = recordEvent(ctx, tx, ticketID, owner.ID, "handoff", "ai", "human"); err != nil {
		return err
	}
	if status != "open" {
		if err = recordEvent(ctx, tx, ticketID, owner.ID, "status", status, "open"); err != nil {
			return err
		}
	}
	return commit(ctx, tx)
}
