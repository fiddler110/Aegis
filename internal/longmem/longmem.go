// Package longmem provides a SQLite FTS5-backed tiered long-term memory store
// modelled on ADK Go 2.0's BaseMemoryService interface: AddSession and
// SearchMemory for session-level facts, and an entity store keyed by
// (project, entity_type, entity_name) for cross-session structured facts about
// target systems, codebases, and decisions (P3.1).
package longmem

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SessionFact is a cross-session memory extracted from a session summary.
type SessionFact struct {
	SessionID string    `json:"session_id"`
	Project   string    `json:"project"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// Entity is a named entity with accumulated facts.
type Entity struct {
	Project    string    `json:"project"`
	EntityType string    `json:"entity_type"`
	EntityName string    `json:"entity_name"`
	Facts      string    `json:"facts"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SearchResult is one item returned by SearchMemory.
type SearchResult struct {
	Kind    string `json:"kind"`    // "session" or "entity"
	Key     string `json:"key"`     // session_id or "type:name"
	Snippet string `json:"snippet"`
	Score   float64 `json:"score"`
}

// Store is the long-term memory backend.
type Store struct {
	db      *sql.DB
	project string // canonical project name (workspace directory base name)
}

// Open opens (or creates) the long-term memory store at dbPath. project is a
// short name that scopes entity lookups (typically the workspace directory base
// name, e.g. "myrepo").
func Open(project, dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open longmem db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db, project: project}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// ProjectName returns the project scope this store is keyed to.
func (s *Store) ProjectName() string { return s.project }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS session_facts (
    session_id TEXT NOT NULL,
    project    TEXT NOT NULL DEFAULT '',
    summary    TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (session_id, project)
);
CREATE TABLE IF NOT EXISTS entities (
    project      TEXT NOT NULL,
    entity_type  TEXT NOT NULL,
    entity_name  TEXT NOT NULL,
    facts        TEXT NOT NULL DEFAULT '',
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (project, entity_type, entity_name)
);
CREATE VIRTUAL TABLE IF NOT EXISTS mem_fts USING fts5(
    kind UNINDEXED,
    key  UNINDEXED,
    body,
    tokenize='porter ascii'
);`)
	return err
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// AddSession records a session summary in the long-term store (ADK BaseMemoryService.AddSession).
// The summary is also inserted into the FTS index for SearchMemory.
func (s *Store) AddSession(ctx context.Context, sessionID, project, summary string) error {
	now := time.Now().UnixMilli()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO session_facts (session_id, project, summary, created_at) VALUES (?, ?, ?, ?)
         ON CONFLICT(session_id, project) DO UPDATE SET summary=excluded.summary, created_at=excluded.created_at`,
		sessionID, project, summary, now); err != nil {
		return err
	}
	// Rebuild FTS entry: delete old then insert new.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM mem_fts WHERE kind='session' AND key=?`, sessionID+":"+project)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mem_fts (kind, key, body) VALUES ('session', ?, ?)`,
		sessionID+":"+project, summary)
	return err
}

// SearchMemory runs an FTS5 query over session facts and entities, returning
// up to limit results (ADK BaseMemoryService.SearchMemory).
func (s *Store) SearchMemory(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT kind, key, snippet(mem_fts, 2, '<b>', '</b>', '…', 30), bm25(mem_fts)
         FROM mem_fts WHERE mem_fts MATCH ?
         ORDER BY bm25(mem_fts)
         LIMIT ?`, ftsEscape(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Kind, &r.Key, &r.Snippet, &r.Score); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertEntity stores or updates facts about a named entity.
func (s *Store) UpsertEntity(ctx context.Context, project, entityType, entityName, facts string) error {
	now := time.Now().UnixMilli()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO entities (project, entity_type, entity_name, facts, updated_at) VALUES (?, ?, ?, ?, ?)
         ON CONFLICT(project, entity_type, entity_name) DO UPDATE SET facts=excluded.facts, updated_at=excluded.updated_at`,
		project, entityType, entityName, facts, now); err != nil {
		return err
	}
	key := entityType + ":" + entityName + "@" + project
	_, _ = s.db.ExecContext(ctx, `DELETE FROM mem_fts WHERE kind='entity' AND key=?`, key)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mem_fts (kind, key, body) VALUES ('entity', ?, ?)`,
		key, entityType+" "+entityName+" "+facts)
	return err
}

// GetEntity retrieves the facts for a named entity.
func (s *Store) GetEntity(ctx context.Context, project, entityType, entityName string) (string, error) {
	var facts string
	err := s.db.QueryRowContext(ctx,
		`SELECT facts FROM entities WHERE project=? AND entity_type=? AND entity_name=?`,
		project, entityType, entityName).Scan(&facts)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return facts, err
}

func ftsEscape(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return `""`
	}
	words := strings.Fields(q)
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ReplaceAll(w, `"`, `""`)
		quoted = append(quoted, `"`+w+`"*`)
	}
	return strings.Join(quoted, " ")
}
