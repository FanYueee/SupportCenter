package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"supportcenter/internal/model"
)

type referencesKey struct{}

// WithReferences carries the items a message tags, already checked with their
// source, into the transaction that stores the message.
func WithReferences(ctx context.Context, refs []model.Reference) context.Context {
	return context.WithValue(ctx, referencesKey{}, refs)
}

// saveReferences keeps a new message's tagged items on its ticket, refreshed
// to how the source describes them now. An item once tagged in public stays
// public when a staff note tags it again.
func saveReferences(ctx context.Context, tx pgx.Tx, ticketID int64, internal bool) error {
	refs, _ := ctx.Value(referencesKey{}).([]model.Reference)
	for _, ref := range refs {
		_, err := tx.Exec(ctx, `INSERT INTO ticket_references(ticket_id,kind,ref_id,trigger,kind_label,kind_label_en,title,detail,customer_url,staff_url,internal)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
 ON CONFLICT (ticket_id,kind,ref_id) DO UPDATE SET trigger=EXCLUDED.trigger,kind_label=EXCLUDED.kind_label,kind_label_en=EXCLUDED.kind_label_en,
  title=EXCLUDED.title,detail=EXCLUDED.detail,customer_url=EXCLUDED.customer_url,staff_url=EXCLUDED.staff_url,
  internal=ticket_references.internal AND EXCLUDED.internal,updated_at=now()`,
			ticketID, ref.Key, ref.ID, ref.Trigger, ref.Label, ref.LabelEn, ref.Title, ref.Detail, ref.CustomerURL, ref.StaffURL, internal)
		if err != nil {
			return err
		}
	}
	return nil
}

// TicketReferences lists the items tagged on a ticket the caller may already
// read, in the order they were first tagged. Staff also get the ones only
// their notes tag.
func (s *Store) TicketReferences(ctx context.Context, ticketID int64, staff bool) ([]model.Reference, error) {
	rows, err := s.Pool.Query(ctx, `SELECT kind,kind_label,kind_label_en,trigger,ref_id,title,detail,customer_url,staff_url,internal FROM ticket_references
 WHERE ticket_id=$1 AND ($2 OR NOT internal) ORDER BY created_at,kind,ref_id`, ticketID, staff)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Reference, error) {
		var r model.Reference
		err := row.Scan(&r.Key, &r.Label, &r.LabelEn, &r.Trigger, &r.ID, &r.Title, &r.Detail, &r.CustomerURL, &r.StaffURL, &r.Internal)
		return r, err
	})
}

// VerifiedEmail is the user's email when it is verified, else empty: only a
// verified address may stand for the customer when asking a source for items.
func (s *Store) VerifiedEmail(ctx context.Context, userID int64) (string, error) {
	var email string
	err := s.Pool.QueryRow(ctx, `SELECT CASE WHEN email_verified THEN lower(email) ELSE '' END FROM users WHERE id=$1`, userID).Scan(&email)
	return email, notFound(err)
}
