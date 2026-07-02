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
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Result is one document matched by a search query.
type Result struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Score   float64 `json:"score"`
}

// Store is a SQLite FTS5 knowledge index for one project.
type Store struct {
	db      *sql.DB
	root    string
}

// Open opens (or creates) a knowledge store at dbPath indexed against root.
func Open(root, dbPath string) (*Store, error) {
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
	s := &Store{db: db, root: root}
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
END;`)
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
	return err
}

// Search queries the FTS5 index and returns up to limit results.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		limit = 5
	}
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
