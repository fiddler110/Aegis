import { marked } from "marked";
import DOMPurify from "dompurify";

// Assistant text is markdown — the models write headings, tables, fenced code
// and lists — but the transcript rendered it as a single <p>, so every one of
// those structures arrived as one undifferentiated blob of text. This turns it
// back into structure.
//
// The input is model output, which is untrusted in the only sense that
// matters here: a prompt-injection vector can put arbitrary text into it, and
// marked passes raw HTML through by design. So every rendered string goes
// through DOMPurify before it reaches innerHTML — that sanitize call is the
// security boundary, not marked's own escaping.

// A wide table must scroll inside the message rather than stretching the
// transcript pane, so it is wrapped in its own overflow container (styled by
// .md .table-wrap).
const renderer = new marked.Renderer();
const baseTable = renderer.table.bind(renderer);
renderer.table = (token) => `<div class="table-wrap">${baseTable(token)}</div>`;

marked.setOptions({
  gfm: true, // tables, strikethrough, autolinks
  breaks: true, // a single newline is a line break, which is what chat prose means by it
  renderer,
});

// FORBID_TAGS/ATTR are belt-and-braces over DOMPurify's defaults: none of these
// have any legitimate place in a chat answer, and a transcript pane is not a
// document viewer.
const PURIFY_OPTS = {
  FORBID_TAGS: ["style", "form", "input", "button", "iframe", "object", "embed"],
  FORBID_ATTR: ["style", "onerror", "onload", "srcset"],
  ADD_ATTR: ["target", "rel"],
};

// Links open in a new tab and cannot reach back into this page via
// window.opener.
DOMPurify.addHook("afterSanitizeAttributes", (node) => {
  if (node.tagName === "A" && node.getAttribute("href")) {
    node.setAttribute("target", "_blank");
    node.setAttribute("rel", "noopener noreferrer");
  }
});

// Transcript re-renders every item on every state change, and text streams in
// token by token — so without a cache, each SSE event would re-parse and
// re-sanitize the entire conversation history. The key is the source string,
// so a block that has not changed costs a Map lookup.
const CACHE_MAX = 256;
const cache = new Map<string, string>();

function memo(src: string, html: string): string {
  if (cache.size >= CACHE_MAX) {
    // Insertion order == eviction order; the oldest entries are the blocks
    // furthest up the transcript, which are also the least likely to change.
    const oldest = cache.keys().next();
    if (!oldest.done) cache.delete(oldest.value);
  }
  cache.set(src, html);
  return html;
}

export function renderMarkdown(src: string): string {
  const hit = cache.get(src);
  if (hit !== undefined) return hit;
  try {
    const html = marked.parse(src, { async: false }) as string;
    return memo(src, DOMPurify.sanitize(html, PURIFY_OPTS));
  } catch {
    // A renderer that throws must not blank the answer: fall back to the
    // escaped source, which is exactly the pre-existing behavior.
    return memo(src, DOMPurify.sanitize(src));
  }
}
