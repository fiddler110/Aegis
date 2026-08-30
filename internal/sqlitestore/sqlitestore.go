// Package sqlitestore provides the shared SQLite connection bootstrap used by
// Aegis's small embedded FTS5 stores (internal/knowledge, internal/longmem):
// create the database directory, open the file with a per-connection busy
// timeout, put the connection in WAL mode, and restrict the resulting files to
// their owner. Schema migration and query logic are package-specific and stay
// in each caller; everything identical about getting a store on disk lives
// here.
//
// Permission hardening used to be on the caller's side of that line, and the
// line was drawn one step too early: all three stores then carried a
// byte-identical hardenDBPermissions differing only in one noun in a log
// message (CLN-1/CLN-3).
package sqlitestore

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// BusyTimeoutDSN makes every connection the pool opens wait up to 5s for a
// contended lock instead of failing with SQLITE_BUSY immediately (P63.4).
// SetMaxOpenConns(1) only serializes writers inside *this* process; an
// `aegis chat` CLI and a running `aegis serve` contend across processes, which
// is exactly the case WAL is meant to survive.
//
// It is a DSN parameter rather than a `db.Exec("PRAGMA busy_timeout=...")`
// because busy_timeout is per-connection state, not persisted in the database
// file the way journal_mode=WAL is: an Exec sets it on whichever pooled
// connection happens to serve it, and any connection the pool opens later
// (after an idle close or a connection error) silently reverts to the default.
// modernc.org/sqlite applies `_pragma=` params in newConn, so every connection
// gets it.
const BusyTimeoutDSN = "?_pragma=busy_timeout(5000)"

// Open opens (or creates) a single-connection, WAL-mode SQLite database at
// dbPath with BusyTimeoutDSN applied, creating dbPath's parent directory if
// needed. label names the store in error messages and log lines (e.g.
// "session", "knowledge", "longmem"). Callers are responsible for their own
// schema migration afterward.
//
// The database file is *not* hardened here: the WAL and shm sidecars do not
// exist until the connection has been used, so a caller hardens by calling
// HardenPermissions after its migration has run. Open cannot do it for them
// without hardening too early to catch the sidecars.
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

// HardenPermissions applies fsguard.RestrictToOwner (FIND-29/P24.16,
// FIND-18/P27.10) to the database file at dbPath and to its WAL-mode sidecars
// (-wal, -shm). It is a no-op on POSIX, where the file's mode bit already
// restricts it to its owner, and does the real work on Windows, where a new
// file inherits its parent directory's ACL instead.
//
// Call it after migration, so the sidecars the connection creates exist and
// can be hardened too.
//
// The main database file is created by Aegis itself, so a genuine ACL-set
// failure on it propagates as an error, the same treatment
// generateAndWriteToken gives daemon.token. The sidecars may not exist even
// then — fsguard.RestrictToOwner already treats a missing file as a no-op — so
// any other failure hardening one of them is only logged, not fatal: the
// primary db file being locked down already covers the bulk of the exposure,
// and a locked-down host should not fail every open over a WAL sidecar's ACL.
//
// label names the store in the sidecar warning; it was the only thing that
// differed between the three copies this replaces.
func HardenPermissions(dbPath, label string) error {
	if err := fsguard.RestrictToOwner(dbPath); err != nil {
		return fmt.Errorf("restrict %s db permissions: %w", label, err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecar := dbPath + suffix
		if err := fsguard.RestrictToOwner(sidecar); err != nil {
			slog.Default().Warn("failed to restrict db sidecar permissions", "store", label, "path", sidecar, "err", err)
		}
	}
	return nil
}
