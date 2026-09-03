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
// synchronous stays at SQLite's default FULL — an fsync per transaction.
// Use OpenDerived instead for a store that can afford NORMAL (PERF-02/P66.26).
//
// The database file is *not* hardened here: the WAL and shm sidecars do not
// exist until the connection has been used, so a caller hardens by calling
// HardenPermissions after its migration has run. Open cannot do it for them
// without hardening too early to catch the sidecars.
//
// P81.24 measured what "not hardened" actually means on Windows, because the
// threat model flagged that the driver's file-creation mode had never been
// traced. Measured with icacls against modernc.org/sqlite: the main file *and*
// both sidecars are created with a purely inherited DACL — the parent
// directory's SYSTEM / Administrators / <user> ACEs, marked (I) — and the
// 0o700 MkdirAll above does not change that, since a Go mode bit sets no ACL.
// So the exposure is real, and it is the sidecars as much as the database.
// The timing is narrower than it reads, though: `PRAGMA journal_mode=WAL`
// below is itself a write, so -wal and -shm exist by the time Open returns and
// a HardenPermissions call after migration does catch them.
func Open(dbPath, label string) (*sql.DB, error) {
	return open(dbPath, label, false)
}

// OpenDerived is Open with `synchronous=NORMAL` instead of SQLite's default
// FULL, cutting the fsync from every transaction to one per WAL checkpoint.
// Only for a store that is a derived/rebuildable cache: on power loss NORMAL
// can lose the last few committed transactions (never corrupt the file, since
// WAL mode's torn-page protection is independent of this setting), which is
// an acceptable trade for a store a re-index recovers and not for one holding
// the only copy of anything (PERF-02/P66.26). knowledge.db and longmem.db
// qualify; sessions.db — checkpoints, cost ledger, traces — does not.
func OpenDerived(dbPath, label string) (*sql.DB, error) {
	return open(dbPath, label, true)
}

func open(dbPath, label string, synchronousNormal bool) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	dsn := dbPath + BusyTimeoutDSN
	if synchronousNormal {
		dsn += "&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
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
//
// One residual window, measured rather than assumed (P81.24). This hardens the
// three files that exist *now*. SQLite deletes -wal and -shm when the last
// connection closes and recreates them on the next write, and a recreated file
// inherits the parent directory's DACL again — so a pooled connection dropped
// after an error re-exposes the sidecars for the rest of the process's life.
// The durable fix is not another call here: a protected *inheritable* DACL on
// the containing directory ("D:PAI(A;OICI;FA;;;OW)", which is what
// fsguard.RestrictToOwner writes) makes every file created in it afterwards
// inherit the owner-only ACE, sidecars included. That is verified by
// TestHardenedDirMakesSidecarsInheritOwnerOnly. It is deliberately not applied
// here because the directory these stores share is the daemon's data dir,
// whose ACL is a host-posture decision owned by its creator (internal/config,
// internal/server) rather than by a database bootstrap — see P81.24's
// follow-up. internal/tool/builtin's spill directory, which this package does
// not own but which had the same shape, *does* get the directory treatment,
// because that directory is created by and belongs to one subsystem.
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
