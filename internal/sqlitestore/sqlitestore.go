// Package sqlitestore provides the shared SQLite connection bootstrap used by
// Aegis's small embedded FTS5 stores (internal/knowledge, internal/longmem):
// create the database directory, open the file with a per-connection busy
// timeout, and put the connection in WAL mode. Schema migration, query logic,
// and file-permission hardening are package-specific and stay in each caller
// — this only covers the identical connection/pragma shape.
package sqlitestore

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// BusyTimeoutDSN makes every connection the pool opens wait up to 5s for a
// contended lock instead of failing with SQLITE_BUSY immediately (P63.4). It
// is a DSN parameter rather than a `PRAGMA busy_timeout` Exec because the
// setting is per-connection and is not persisted in the database file the
// way journal_mode=WAL is — an Exec would cover only whichever pooled
// connection served it. See internal/session for the longer note.
const BusyTimeoutDSN = "?_pragma=busy_timeout(5000)"

// Open opens (or creates) a single-connection, WAL-mode SQLite database at
// dbPath with BusyTimeoutDSN applied, creating dbPath's parent directory if
// needed. label names the store in error messages (e.g. "knowledge",
// "longmem"). Callers are responsible for their own schema migration and
// permission hardening afterward.
func Open(dbPath, label string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath+BusyTimeoutDSN)
	if err != nil {
		return nil, fmt.Errorf("open %s db: %w", label, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
