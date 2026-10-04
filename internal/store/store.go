// Package store keeps everything in one local SQLite file.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"slices"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store wraps the database. All methods are safe for concurrent use.
type Store struct {
	db *sql.DB
}

// Open opens or creates the database and applies migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// One connection keeps SQLite locking trivial. Always drain rows before the next query.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate applies migrations/NNN_*.sql in order; PRAGMA user_version is the last applied number.
func (s *Store) migrate() error {
	names, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(names); i++ {
		body, err := migrationsFS.ReadFile(names[i])
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", names[i], err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// Setting returns a stored value or def.
func (s *Store) Setting(ctx context.Context, key, def string) string {
	var v string
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v); err != nil {
		return def
	}
	return v
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}

// Backup writes a consistent copy of the database to path.
func (s *Store) Backup(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path)
	return err
}

// Cursor implements chain.Cursors.
func (s *Store) Cursor(ctx context.Context, chain, addr, stream string) int64 {
	var c int64
	s.db.QueryRowContext(ctx, "SELECT cursor FROM sync_cursors WHERE chain = ? AND address = ? AND stream = ?",
		chain, addr, stream).Scan(&c)
	return c
}

// SetCursor implements chain.Cursors.
func (s *Store) SetCursor(ctx context.Context, chain, addr, stream string, c int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_cursors (chain, address, stream, cursor) VALUES (?, ?, ?, ?)
		ON CONFLICT (chain, address, stream) DO UPDATE SET cursor = excluded.cursor`, chain, addr, stream, c)
	return err
}
