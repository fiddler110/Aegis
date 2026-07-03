// Package knowledge provides a SQLite FTS5-backed project knowledge base
// populated from documentation files, README content, and code comments. It
// gives the agent persistent, queryable semantic depth beyond the structural
// repo-map (P3.3 — DeepWiki-style project knowledge base).
package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fiddler110/aegis/internal/embed"
	_ "modernc.org/sqlite"
)

// Result is one document matched by a search query.
type Result struct {
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

// Store is a SQLite FTS5 knowledge index for one project, optionally backed by
// a semantic embedding layer (P5.8).
type Store struct {
	db       *sql.DB
	root     string
	embedder embed.Embedder // nil = BM25-only (default)
}

// Open opens (or creates) a knowledge store at dbPath indexed against root.
// embedder is optional (nil disables semantic recall and keeps BM25-only
// search, the zero-config default).
func Open(root, dbPath string, embedder embed.Embedder) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open knowledge db: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db, root: root, embedder: embedder}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS docs (
    path       TEXT PRIMARY KEY,
    title      TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    indexed_at INTEGER NOT NULL DEFAULT 0
);
CREATE VIRTUAL TABLE IF NOT EXISTS docs_fts USING fts5(
    path UNINDEXED,
    title,
    body,
    content='docs',
    content_rowid='rowid'
);
CREATE TRIGGER IF NOT EXISTS docs_ai AFTER INSERT ON docs BEGIN
    INSERT INTO docs_fts(rowid, path, title, body) VALUES (new.rowid, new.path, new.title, new.body);
END;
CREATE TRIGGER IF NOT EXISTS docs_ad AFTER DELETE ON docs BEGIN
    INSERT INTO docs_fts(docs_fts, rowid, path, title, body) VALUES ('delete', old.rowid, old.path, old.title, old.body);
END;
CREATE TRIGGER IF NOT EXISTS docs_au AFTER UPDATE ON docs BEGIN
    INSERT INTO docs_fts(docs_fts, rowid, path, title, body) VALUES ('delete', old.rowid, old.path, old.title, old.body);
    INSERT INTO docs_fts(rowid, path, title, body) VALUES (new.rowid, new.path, new.title, new.body);
END;
CREATE TABLE IF NOT EXISTS docs_vec (
    path   TEXT PRIMARY KEY,
    vector BLOB NOT NULL
);`)
	return err
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Index walks the project root and indexes documentation files (*.md, *.txt,
// *.rst) and source file comment blocks. Returns the number of files indexed.
func (s *Store) Index(ctx context.Context) (int, error) {
	var count int

	ignoredDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		"dist": true, "build": true, "target": true, ".venv": true,
	}

	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if ignoredDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		ext := strings.ToLower(filepath.Ext(name))

		var body string
		switch {
		case ext == ".md" || ext == ".txt" || ext == ".rst":
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			body = string(data)
		case ext == ".go":
			body = extractGoComments(path)
		default:
			return nil
		}

		if strings.TrimSpace(body) == "" {
			return nil
		}

		rel, _ := filepath.Rel(s.root, path)
		rel = filepath.ToSlash(rel)
		title := deriveTitle(name, body)

		if err := s.upsert(ctx, rel, title, body); err != nil {
			return nil
		}
		count++
		return nil
	})
	return count, err
}

func (s *Store) upsert(ctx context.Context, path, title, body string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO docs (path, title, body, indexed_at) VALUES (?, ?, ?, ?)
         ON CONFLICT(path) DO UPDATE SET title=excluded.title, body=excluded.body, indexed_at=excluded.indexed_at`,
		path, title, body, now)
	if err != nil {
		return err
	}
	if s.embedder == nil {
		return nil
	}
	vecs, err := s.embedder.Embed(ctx, []string{truncateForEmbed(title + "\n" + body)})
	if err != nil || len(vecs) == 0 {
		// Semantic indexing is best-effort: a down/misconfigured embedder must
		// not block BM25 indexing, which is the load-bearing default path.
		return nil
	}
	_, _ = s.db.ExecContext(ctx,
		`INSERT INTO docs_vec (path, vector) VALUES (?, ?)
         ON CONFLICT(path) DO UPDATE SET vector=excluded.vector`,
		path, embed.EncodeVector(vecs[0]))
	return nil
}

// embedTextLimit caps how much text is sent per embedding call; long
// documents are truncated rather than chunked, keeping indexing cheap.
const embedTextLimit = 4000

func truncateForEmbed(s string) string {
	if len(s) > embedTextLimit {
		return s[:embedTextLimit]
	}
	return s
}

// Search queries the FTS5 index and returns up to limit results. When a
// semantic embedder is configured (P5.8), results are the reciprocal-rank
// fusion of the BM25 ranking and a cosine-similarity ranking over stored
// embeddings; otherwise it is BM25-only (the zero-config default).
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 5
	}
	// Pull a wider BM25 candidate pool than requested so fusion with the
	// semantic ranking has room to reorder, then trim to limit at the end.
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

	byPath := make(map[string]Result, len(bm25Results))
	bm25Ranking := make([]string, 0, len(bm25Results))
	for _, r := range bm25Results {
		byPath[r.Path] = r
		bm25Ranking = append(bm25Ranking, r.Path)
	}

	semRanking, semSnippets, err := s.semanticRanking(ctx, query, poolSize)
	if err != nil {
		// Semantic recall is best-effort: fall back to BM25-only on error
		// (e.g. embedder unreachable) rather than failing the search.
		if len(bm25Results) > limit {
			bm25Results = bm25Results[:limit]
		}
		return bm25Results, nil
	}
	for path, snippet := range semSnippets {
		if _, ok := byPath[path]; !ok {
			byPath[path] = snippet
		}
	}

	fused := embed.RRF(embed.DefaultRRFK, bm25Ranking, semRanking)
	paths := make([]string, 0, len(fused))
	for p := range fused {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		if fused[paths[i]] != fused[paths[j]] {
			return fused[paths[i]] > fused[paths[j]]
		}
		return paths[i] < paths[j]
	})
	if len(paths) > limit {
		paths = paths[:limit]
	}
	out := make([]Result, 0, len(paths))
	for _, p := range paths {
		r := byPath[p]
		r.Score = fused[p]
		out = append(out, r)
	}
	return out, nil
}

// bm25Search runs the raw FTS5 query and returns up to limit results ranked
// by BM25 only.
func (s *Store) bm25Search(ctx context.Context, query string, limit int) ([]Result, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT path, title, snippet(docs_fts, 2, '<b>', '</b>', '…', 30), bm25(docs_fts)
         FROM docs_fts
         WHERE docs_fts MATCH ?
         ORDER BY bm25(docs_fts)
         LIMIT ?`, ftsEscape(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.Path, &r.Title, &r.Snippet, &r.Score); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// semanticRanking embeds query and ranks all stored document vectors by
// cosine similarity, returning the top `limit` paths (best first) plus a
// fallback Result (title-less, snippet from the doc body) for any path not
// already present in the BM25 candidate set.
func (s *Store) semanticRanking(ctx context.Context, query string, limit int) ([]string, map[string]Result, error) {
	vecs, err := s.embedder.Embed(ctx, []string{query})
	if err != nil || len(vecs) == 0 {
		return nil, nil, fmt.Errorf("embed query: %w", err)
	}
	qv := vecs[0]

	rows, err := s.db.QueryContext(ctx, `SELECT v.path, v.vector, d.title, d.body FROM docs_vec v JOIN docs d ON d.path = v.path`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	type scored struct {
		path  string
		score float64
		title string
		body  string
	}
	var all []scored
	for rows.Next() {
		var path, title, body string
		var raw []byte
		if err := rows.Scan(&path, &raw, &title, &body); err != nil {
			return nil, nil, err
		}
		all = append(all, scored{path: path, score: embed.Cosine(qv, embed.DecodeVector(raw)), title: title, body: body})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].score > all[j].score })
	if len(all) > limit {
		all = all[:limit]
	}
	ranking := make([]string, 0, len(all))
	snippets := make(map[string]Result, len(all))
	for _, a := range all {
		ranking = append(ranking, a.path)
		snippets[a.path] = Result{Path: a.path, Title: a.title, Snippet: snippetOf(a.body, 200)}
	}
	return ranking, snippets, nil
}

func snippetOf(body string, n int) string {
	body = strings.TrimSpace(body)
	if len(body) <= n {
		return body
	}
	return body[:n] + "…"
}

// DocCount returns the number of documents currently in the index.
func (s *Store) DocCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM docs`).Scan(&n)
	return n, err
}

// ftsEscape wraps the query so unquoted special FTS5 characters don't break
// the MATCH expression.
func ftsEscape(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return `""`
	}
	// Simple prefix-search: wrap each word in double-quotes and add * suffix.
	words := strings.Fields(q)
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ReplaceAll(w, `"`, `""`)
		quoted = append(quoted, `"`+w+`"*`)
	}
	return strings.Join(quoted, " ")
}

// deriveTitle extracts a title from a document: the first heading line in
// Markdown, or the filename otherwise.
func deriveTitle(filename, body string) string {
	for _, line := range strings.SplitN(body, "\n", 20) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimPrefix(trimmed, "# ")
		}
		if strings.HasPrefix(trimmed, "//") {
			t := strings.TrimSpace(strings.TrimPrefix(trimmed, "//"))
			if t != "" && !strings.HasPrefix(t, "nolint") {
				return t
			}
		}
	}
	return strings.TrimSuffix(filename, filepath.Ext(filename))
}

// extractGoComments pulls doc-comment blocks (//...) from a Go source file.
func extractGoComments(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "// ") || trimmed == "//" {
			b.WriteString(strings.TrimPrefix(trimmed, "//"))
			b.WriteByte('\n')
		}
	}
	return b.String()
}
