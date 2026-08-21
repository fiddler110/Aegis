// Package opregister is the durable half of P65.1's startedTools record
// (P65.4): a row survives from the moment a tool call's effect begins until it
// finishes, so a daemon that dies in between leaves evidence a *new* process
// can read. Without it, repairOrphanedToolUses (internal/engine) has no way to
// tell a call that already ran from one that never started once the in-memory
// map that answered that question is gone with the process — it falls back to
// the conservative "never started" default unconditionally.
//
// A row's absence is the steady state: MarkStarted writes it, MarkFinished
// deletes it, and the only case a row survives past a single request is
// exactly the crash window this package exists for. The table never grows
// with session count or age the way an append-only log would.
package opregister

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// PendingCall is one tool call that started and never finished — either it is
// still genuinely running, or the process that started it died first. The
// caller (the engine, via Options.InitialStartedTools) treats every pending
// call the same way repairOrphanedToolUses always has: "may have run."
type PendingCall struct {
	ToolUseID string
	ToolName  string
	Input     json.RawMessage
	CreatedAt time.Time
}

// Store persists the operation register in SQLite. It shares the daemon's
// single session database connection (session.Store.DB()), the same way
// internal/checkpoint does, so it inherits WAL + busy_timeout + the
// single-writer serialization that connection is already configured with.
type Store struct {
	db *sql.DB
}

// NewStore creates the operation_registers table on db and returns a Store.
func NewStore(db *sql.DB) (*Store, error) {
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS operation_registers (
    session_id   TEXT NOT NULL,
    tool_use_id  TEXT NOT NULL,
    tool_name    TEXT NOT NULL,
    input        BLOB NOT NULL,
    created_at   INTEGER NOT NULL,
    PRIMARY KEY (session_id, tool_use_id)
);`)
	return err
}

// MarkStarted records that toolUseID's effect has begun. Called from the same
// seam as the engine's in-memory markToolStarted, immediately before Execute.
func (s *Store) MarkStarted(ctx context.Context, sessionID, toolUseID, toolName string, input json.RawMessage) error {
	if input == nil {
		input = json.RawMessage("null")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO operation_registers (session_id, tool_use_id, tool_name, input, created_at) VALUES (?, ?, ?, ?, ?)`,
		sessionID, toolUseID, toolName, []byte(input), time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("opregister: mark started: %w", err)
	}
	return nil
}

// MarkFinished clears toolUseID's row — its outcome (success or error) is now
// known and, if the process survives, will be persisted through the ordinary
// conversation flush like any other tool result. Only a row whose call never
// reached this point survives a crash.
func (s *Store) MarkFinished(ctx context.Context, sessionID, toolUseID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM operation_registers WHERE session_id = ? AND tool_use_id = ?`,
		sessionID, toolUseID)
	if err != nil {
		return fmt.Errorf("opregister: mark finished: %w", err)
	}
	return nil
}

// Pending returns every call for sessionID that started and never finished —
// the calls a fresh process must classify as "may have run" rather than
// "never started."
func (s *Store) Pending(ctx context.Context, sessionID string) ([]PendingCall, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_use_id, tool_name, input, created_at FROM operation_registers WHERE session_id = ? ORDER BY created_at`,
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("opregister: pending: %w", err)
	}
	defer rows.Close()
	var out []PendingCall
	for rows.Next() {
		var pc PendingCall
		var input []byte
		var created int64
		if err := rows.Scan(&pc.ToolUseID, &pc.ToolName, &input, &created); err != nil {
			return nil, fmt.Errorf("opregister: pending: %w", err)
		}
		pc.Input = json.RawMessage(input)
		pc.CreatedAt = time.UnixMilli(created)
		out = append(out, pc)
	}
	return out, rows.Err()
}
