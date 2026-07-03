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
	"sort"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/embed"
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
	Kind    string  `json:"kind"` // "session" or "entity"
	Key     string  `json:"key"`  // session_id or "type:name"
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

// Store is the long-term memory backend, optionally backed by a semantic
// embedding layer (P5.8).
type Store struct {
	db       *sql.DB
	project  string         // canonical project name (workspace directory base name)
	embedder embed.Embedder // nil = BM25-only (default)
}

// Open opens (or creates) the long-term memory store at dbPath. project is a
// short name that scopes entity lookups (typically the workspace directory base
// name, e.g. "myrepo"). embedder is optional (nil disables semantic recall).
func Open(project, dbPath string, embedder embed.Embedder) (*Store, error) {
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
	s := &Store{db: db, project: project, embedder: embedder}
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
);
CREATE TABLE IF NOT EXISTS mem_vec (
    kind   TEXT NOT NULL,
    key    TEXT NOT NULL,
    vector BLOB NOT NULL,
    PRIMARY KEY (kind, key)
);`)
	return err
}

// embedTextLimit caps how much text is sent per embedding call.
const embedTextLimit = 4000

func truncateForEmbed(s string) string {
	if len(s) > embedTextLimit {
		return s[:embedTextLimit]
	}
	return s
}

// upsertVector embeds body (best-effort) and stores it under (kind, key). A
// down or misconfigured embedder must not block the load-bearing FTS write,
// so errors are swallowed.
func (s *Store) upsertVector(ctx context.Context, kind, key, body string) {
	if s.embedder == nil {
		return
	}
	vecs, err := s.embedder.Embed(ctx, []string{truncateForEmbed(body)})
	if err != nil || len(vecs) == 0 {
		return
	}
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO mem_vec (kind, key, vector) VALUES (?, ?, ?)
         ON CONFLICT(kind, key) DO UPDATE SET vector=excluded.vector`,
		kind, key, embed.EncodeVector(vecs[0]))
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
	key := sessionID + ":" + project
	_, _ = s.db.ExecContext(ctx, `DELETE FROM mem_fts WHERE kind='session' AND key=?`, key)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO mem_fts (kind, key, body) VALUES ('session', ?, ?)`,
		key, summary); err != nil {
		return err
	}
	s.upsertVector(ctx, "session", key, summary)
	return nil
}

// SearchMemory runs an FTS5 query over session facts and entities, returning
// up to limit results (ADK BaseMemoryService.SearchMemory). When a semantic
// embedder is configured (P5.8), results are the reciprocal-rank fusion of
// the BM25 ranking and a cosine-similarity ranking over stored embeddings;
// otherwise it is BM25-only (the zero-config default).
func (s *Store) SearchMemory(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	poolSize := limit * 4
	if poolSize < 20 {
		poolSize = 20
	}
	bm25Results, err := s.bm25Search(ctx, query, poolSize)
	if err != nil {
		return nil, err
	}
	if s.embedder == nil {
		if len(bm25Results) > limit {
			bm25Results = bm25Results[:limit]
		}
		return bm25Results, nil
	}

	byKey := make(map[string]SearchResult, len(bm25Results))
	bm25Ranking := make([]string, 0, len(bm25Results))
	for _, r := range bm25Results {
		fk := r.Kind + ":" + r.Key
		byKey[fk] = r
		bm25Ranking = append(bm25Ranking, fk)
	}

	semRanking, semFallback, err := s.semanticRanking(ctx, query, poolSize)
	if err != nil {
		if len(bm25Results) > limit {
			bm25Results = bm25Results[:limit]
		}
		return bm25Results, nil
	}
	for fk, r := range semFallback {
		if _, ok := byKey[fk]; !ok {
			byKey[fk] = r
		}
	}

	fused := embed.RRF(embed.DefaultRRFK, bm25Ranking, semRanking)
	keys := make([]string, 0, len(fused))
	for k := range fused {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if fused[keys[i]] != fused[keys[j]] {
			return fused[keys[i]] > fused[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]SearchResult, 0, len(keys))
	for _, k := range keys {
		r := byKey[k]
		r.Score = fused[k]
		out = append(out, r)
	}
	return out, nil
}

// bm25Search runs the raw FTS5 query and returns up to limit results ranked
// by BM25 only.
func (s *Store) bm25Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
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

// semanticRanking embeds query and ranks all stored (kind, key) vectors by
// cosine similarity, returning the top `limit` "kind:key" fused-keys (best
// first) plus a fallback SearchResult for any not already in the BM25 pool.
func (s *Store) semanticRanking(ctx context.Context, query string, limit int) ([]string, map[string]SearchResult, error) {
	vecs, err := s.embedder.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		return nil, nil, fmt.Errorf("embed query: %w", err)
	}
	qv := vecs[0]

	rows, err := s.db.QueryContext(ctx, `SELECT kind, key, vector FROM mem_vec`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	type scored struct {
		kind, key string
		score     float64
	}
	var all []scored
	for rows.Next() {
		var kind, key string
		var raw []byte
		if err := rows.Scan(&kind, &key, &raw); err != nil {
			return nil, nil, err
		}
		all = append(all, scored{kind: kind, key: key, score: embed.Cosine(qv, embed.DecodeVector(raw))})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
	if len(all) > limit {
		all = all[:limit]
	}
	ranking := make([]string, 0, len(all))
	fallback := make(map[string]SearchResult, len(all))
	for _, a := range all {
		fk := a.kind + ":" + a.key
		ranking = append(ranking, fk)
		fallback[fk] = SearchResult{Kind: a.kind, Key: a.key}
	}
	return ranking, fallback, nil
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
	body := entityType + " " + entityName + " " + facts
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO mem_fts (kind, key, body) VALUES ('entity', ?, ?)`,
		key, body); err != nil {
		return err
	}
	s.upsertVector(ctx, "entity", key, body)
	return nil
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
