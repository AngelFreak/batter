package migrate_test

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/XpertaDK/batter/db"
	"github.com/XpertaDK/batter/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Needs BATTER_TEST_DATABASE_URL (a role that may create databases); skips otherwise.
func freshDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv("BATTER_TEST_DATABASE_URL")
	if adminURL == "" {
		t.Skip("BATTER_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("batter_migrate_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		admin.Close()
	})
	return pool
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func appliedVersions(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var v []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		v = append(v, s)
	}
	return v
}

func allMigrationFiles(t *testing.T) []string {
	t.Helper()
	names, err := fs.Glob(db.Migrations, "migrations/*.sql")
	if err != nil || len(names) == 0 {
		t.Fatalf("no embedded migrations: %v", err)
	}
	for i, n := range names {
		names[i] = n[len("migrations/"):]
	}
	return names
}

func TestUpAppliesEmbeddedMigrationsToFreshDB(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, quiet); err != nil {
		t.Fatal(err)
	}
	if got, want := appliedVersions(t, pool), allMigrationFiles(t); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("applied %v, want %v", got, want)
	}
	// 002's CHECK constraint must exist (ADD COLUMN IF NOT EXISTS ... CHECK).
	if _, err := pool.Exec(ctx, "INSERT INTO devices (serial, status) VALUES ('x', 'bogus')"); err == nil {
		t.Fatal("devices.status CHECK constraint missing")
	}
}

func TestUpIsNoOpWhenCurrent(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := migrate.Up(ctx, pool, quiet); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	if got := len(appliedVersions(t, pool)); got != len(allMigrationFiles(t)) {
		t.Fatalf("%d versions recorded, want %d", got, len(allMigrationFiles(t)))
	}
}

// Databases created before the runner existed were built by Postgres's
// initdb.d scripts: schema present, no schema_migrations. Up must adopt them.
func TestUpAdoptsDatabaseBuiltByInitdbScripts(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()
	for _, name := range allMigrationFiles(t) {
		sql, err := db.Migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("simulate initdb %s: %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, "INSERT INTO users (username, password_hash) VALUES ('keep', 'x')"); err != nil {
		t.Fatal(err)
	}

	if err := migrate.Up(ctx, pool, quiet); err != nil {
		t.Fatalf("Up on initdb-built database: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE username = 'keep'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("existing data lost: count=%d err=%v", n, err)
	}
	if got := len(appliedVersions(t, pool)); got != len(allMigrationFiles(t)) {
		t.Fatalf("%d versions recorded, want %d", got, len(allMigrationFiles(t)))
	}
}

func TestUpStopsAtFailingMigrationAndRollsItBack(t *testing.T) {
	pool := freshDB(t)
	ctx := context.Background()
	fsys := fstest.MapFS{
		"migrations/001_ok.sql":    {Data: []byte("CREATE TABLE a (id int);")},
		"migrations/002_bad.sql":   {Data: []byte("CREATE TABLE b (id int); SELECT no_such_function();")},
		"migrations/003_after.sql": {Data: []byte("CREATE TABLE c (id int);")},
	}
	err := migrate.UpFS(ctx, pool, fsys, quiet)
	if err == nil {
		t.Fatal("expected error from failing migration")
	}
	if want := "002_bad.sql"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q should name %s", err, want)
	}
	if got := fmt.Sprint(appliedVersions(t, pool)); got != "[001_ok.sql]" {
		t.Fatalf("applied %s, want [001_ok.sql]", got)
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('b') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Fatalf("failed migration's partial work not rolled back (table b exists=%v, err=%v)", exists, err)
	}
}
