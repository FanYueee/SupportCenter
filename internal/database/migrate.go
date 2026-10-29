package database

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/jackc/pgx/v5"
	"sort"
	"supportcenter/db"
)

// Each file is immutable once recorded. Existing baseline installations adopt
// 001 safely; subsequent changes are applied once under a transaction lock.
func (s *Store) Migrate(ctx context.Context) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(827190031)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := db.Migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		data, err := db.Migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(data))
		var existing string
		err = tx.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE name=$1`, entry.Name()).Scan(&existing)
		if err == nil {
			if existing != checksum {
				return fmt.Errorf("migration %s changed after application", entry.Name())
			}
			continue
		}
		if err != pgx.ErrNoRows {
			return err
		}
		if _, err = tx.Exec(ctx, string(data)); err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO schema_migrations(name,checksum) VALUES($1,$2)`, entry.Name(), checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
