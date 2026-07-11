import { useState } from "preact/hooks";
import { api } from "../api";
import type { KnowledgeResponse, KnowledgeResult } from "../types";

// KnowledgePanel is the "Project knowledge" popover (P15.8): search the
// indexed project knowledge base (POST /knowledge action=query) and rebuild
// the index to pick up recent file changes (action=index). Both run directly
// against the daemon's store — no model turn is spent.
export function KnowledgePanel({ onClose }: { onClose: () => void }) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<KnowledgeResult[] | null>(null);
  const [searching, setSearching] = useState(false);
  const [indexing, setIndexing] = useState(false);
  const [indexNote, setIndexNote] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const search = async () => {
    const q = query.trim();
    if (!q || searching) return;
    setSearching(true);
    setError(null);
    try {
      const r = await api("/knowledge", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "query", query: q, limit: 10 }),
      });
      const body = (await r.json()) as KnowledgeResponse;
      setResults(body.results || []);
    } catch (e) {
      setError((e as Error).message);
      setResults(null);
    } finally {
      setSearching(false);
    }
  };

  const rebuild = async () => {
    if (indexing) return;
    setIndexing(true);
    setError(null);
    setIndexNote(null);
    try {
      const r = await api("/knowledge", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action: "index" }),
      });
      const body = (await r.json()) as KnowledgeResponse;
      setIndexNote(
        `Index rebuilt: ${body.doc_count ?? 0} document${(body.doc_count ?? 0) === 1 ? "" : "s"} indexed` +
          (body.embeddings_enabled ? " (semantic search on)." : ".")
      );
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setIndexing(false);
    }
  };

  return (
    <>
      <div class="backdrop" onClick={onClose} />
      <div class="panel wide" role="dialog" aria-label="Project knowledge">
        <div class="panel-head">
          <strong>Project knowledge</strong>
          <button class="linkish" onClick={onClose}>
            Close
          </button>
        </div>
        <p class="hint">
          Search an index of this project's documents and notes. If results look stale, rebuild the
          index to pick up recent changes.
        </p>
        <div class="kb-search">
          <input
            type="text"
            placeholder="Search project knowledge…"
            value={query}
            disabled={searching}
            onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") search();
            }}
          />
          <button disabled={searching || !query.trim()} onClick={search}>
            {searching ? "Searching…" : "Search"}
          </button>
        </div>
        {error && <p class="err">{error}</p>}
        {results !== null && results.length === 0 && !error && (
          <p class="hint">No matches. Try different words, or rebuild the index below.</p>
        )}
        {results !== null && results.length > 0 && (
          <div class="kb-results">
            {results.map((res, i) => (
              <div class="kb-row" key={i}>
                <div class="kb-title">{res.title || res.path}</div>
                <div class="kb-path">{res.path}</div>
                {res.snippet && <div class="kb-snippet">{res.snippet}</div>}
              </div>
            ))}
          </div>
        )}
        <div class="kb-index">
          <button class="secondary" disabled={indexing} onClick={rebuild}>
            {indexing ? "Rebuilding…" : "Rebuild index"}
          </button>
          {indexNote && <span class="hint">{indexNote}</span>}
        </div>
      </div>
    </>
  );
}
