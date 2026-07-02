// Package db owns the panel's SQLite backing store: connection open,
// versioned migrations, and shared queries used by other panel packages.
//
// Why pure-Go modernc.org/sqlite: keeps hexplus a single static binary
// with CGO_ENABLED=0. The perf gap vs mattn/go-sqlite3 is irrelevant
// for the panel's workload (~dozens of writes/min, ~hundreds of reads).
//
// Every schema change adds a new numbered migration in migrations.go
// and bumps the target version. Existing DBs get upgraded in-place on
// panel startup; a mismatch newer-than-supported version aborts startup.
package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/lolyhexey/hexplus/internal/paths"
)

// Open opens the panel DB at paths.PanelDBPath, creating the parent
// directory if needed, and runs any pending migrations. The returned
// *sql.DB is safe for concurrent use.
//
// PRAGMA choices:
//   - journal_mode=WAL: readers don't block on writers. Cheap and
//     universally supported on modernc.org/sqlite.
//   - foreign_keys=ON: SQLite defaults them off; the panel relies on
//     cascades so we turn them on for every connection.
//   - busy_timeout=5000: bounded retry when two goroutines write.
func Open() (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(paths.PanelDBPath), 0o750); err != nil {
		return nil, fmt.Errorf("mkdir panel state: %w", err)
	}

	dsn := "file:" + paths.PanelDBPath + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := Migrate(sqldb); err != nil {
		_ = sqldb.Close()
		return nil, err
	}
	return sqldb, nil
}

// currentVersion returns the schema_version pragma value; a fresh DB
// with no schema returns 0.
func currentVersion(sqldb *sql.DB) (int, error) {
	var v int
	if err := sqldb.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	return v, nil
}

// setVersion stamps the migration cursor. Written inside the same
// transaction as the migration DDL so a crash rolls back both.
func setVersion(tx *sql.Tx, v int) error {
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)); err != nil {
		return fmt.Errorf("write user_version=%d: %w", v, err)
	}
	return nil
}

// ErrTooNew is returned when the DB was written by a newer panel build
// than we know how to serve. Refusing to open in that case beats silently
// corrupting future-shape rows.
var ErrTooNew = errors.New("panel db is a newer schema version than this binary supports")
