package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func migrationFiles() ([]string, error) {
	files, e := fs.Glob(migrationFS, "migrations/*.sql")
	sort.Strings(files)
	return files, e
}
func (s *Store) Migrate(ctx context.Context) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(7149200251)"); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS keel_schema_migrations (version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL)"); e != nil {
		return e
	}
	files, e := migrationFiles()
	if e != nil {
		return e
	}
	for _, name := range files {
		data, e := migrationFS.ReadFile(name)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(data)
		checksum := hex.EncodeToString(sum[:])
		var old string
		e = tx.QueryRowContext(ctx, "SELECT checksum FROM keel_schema_migrations WHERE version=$1", name).Scan(&old)
		if e == nil {
			if old != checksum {
				return fmt.Errorf("migration checksum mismatch: %s", name)
			}
			continue
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if _, e = tx.ExecContext(ctx, string(data)); e != nil {
			return fmt.Errorf("migration %s failed: %w", name, e)
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO keel_schema_migrations VALUES($1,$2,$3)", name, checksum, time.Now().UTC()); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) CheckMigrations(ctx context.Context) error {
	files, e := migrationFiles()
	if e != nil {
		return e
	}
	var count int
	if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM keel_schema_migrations").Scan(&count); e != nil {
		return fmt.Errorf("database migration required; run keel-admin migrate")
	}
	if count != len(files) {
		return fmt.Errorf("database migration version differs from application")
	}
	for _, name := range files {
		data, _ := migrationFS.ReadFile(name)
		sum := sha256.Sum256(data)
		var have string
		if e = s.DB.QueryRowContext(ctx, "SELECT checksum FROM keel_schema_migrations WHERE version=$1", name).Scan(&have); e != nil || have != hex.EncodeToString(sum[:]) {
			return fmt.Errorf("database migration mismatch: %s", name)
		}
	}
	return nil
}
