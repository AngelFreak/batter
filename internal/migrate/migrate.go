// Package migrate applies the embedded SQL migrations at startup.
//
// Each file in db/migrations runs once, in filename order, inside its own
// transaction, and is recorded in schema_migrations. Migrations must be
// idempotent (IF NOT EXISTS etc.): databases created before this runner
// existed were built by Postgres's initdb.d scripts and have no record of
// what ran, so the runner re-applies everything on first contact.
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"

	"github.com/XpertaDK/batter/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// lockID is an arbitrary constant for pg_advisory_lock, so two instances
// starting together don't apply the same migration twice.
const lockID = 7_001_002_003

// Up applies all pending embedded migrations.
func Up(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	return UpFS(ctx, pool, db.Migrations, logger)
}

// UpFS applies pending migrations from fsys's migrations/*.sql.
func UpFS(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, logger *slog.Logger) error {
	files, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockID) //nolint:errcheck

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	versions, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	applied := make(map[string]bool, len(versions))
	for _, v := range versions {
		applied[v] = true
	}

	for _, file := range files {
		version := path.Base(file)
		if applied[version] {
			continue
		}
		sql, err := fs.ReadFile(fsys, file)
		if err != nil {
			return err
		}
		if err := apply(ctx, conn.Conn(), version, string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", version, err)
		}
		logger.Info("applied migration", "version", version)
	}
	return nil
}

func apply(ctx context.Context, conn *pgx.Conn, version, sql string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after Commit

	// No arguments: pgx uses the simple protocol, which allows a file of
	// multiple statements.
	if _, err := tx.Exec(ctx, sql); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
