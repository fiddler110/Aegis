package compaction

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/provider"
)

const (
	// staleSearchDumpThreshold is the content length above which an old
	// search/listing tool result is considered a candidate for pruning.
	staleSearchDumpThreshold = 500
	// staleSearchKeepChars is how much of a pruned search dump is kept as a
	// preview.
	staleSearchKeepChars = 300
)

// searchDumpTools are tools whose results tend to be large, disposable dumps
// once the model has already acted on them.
var searchDumpTools = map[string]bool{
	"grep": true,
	"glob": true,
	"ls":   true,
}

// writeEditContentFields maps a file-mutating tool name to the fields of its
// tool_use Input that carry the (potentially huge) verbatim file content — as
// opposed to the small structural fields like "path". These are the arguments
// blanked once the write/edit has a successful result: the content is durably
// on disk and re-readable, so keeping the literal bytes in every subsequent
// request buys nothing. write_file's whole payload is "content"; edit_file
// carries the before/after snippets in "old_string"/"new_string". multi_edit
// is handled separately (pruneMultiEditInput) because its content lives inside
// a nested "edits" array, not at the top level.
var writeEditContentFields = map[string][]string{
	"write_file": {"content"},
	"edit_file":  {"old_string", "new_string"},
}

// pruneStaleToolResults deterministically shrinks the prefix of msgs (every
// message before the trailing keepRecent window) by:
//   - blanking read_file results for a path that was read again later
//   - truncating large grep/glob/ls dumps to a short preview
//   - blanking read_file results for skill-reference files (a skill's own
//     directory), which are static and cheap to re-fetch (P36.2)
//   - blanking the verbatim file content in a successful write_file/edit_file
//     tool_use *Input* — the file is durably on disk and re-readable (P36.2)
//
// This runs as a cheap pre-pass before falling back to LLM summarization: it
// never rewrites conversational text, only stale tool_result content and the
// disposable file-content arguments of already-committed write/edit calls, so
// it preserves exact wording for everything the model actually said. Returns
// the possibly-modified list and the number of characters removed.
func pruneStaleToolResults(msgs []provider.Message, keepRecent int) ([]provider.Message, int) {
	cutoff := len(msgs) - keepRecent
	if cutoff <= 0 {
		return msgs, 0
	}

	type toolUse struct {
		name   string
		path   string
		callID string // canonical name+input key, for search-dump tools
	}
	uses := make(map[string]toolUse, len(msgs))
	for _, m := range msgs {
		for _, blk := range m.Content {
			if tu, ok := blk.(provider.ToolUseBlock); ok {
				u := toolUse{name: tu.Name, path: readFilePath(tu)}
				if searchDumpTools[tu.Name] {
					u.callID = tu.Name + "\x00" + string(canonicalizeJSON(tu.Input))
				}
				uses[tu.ID] = u
			}
		}
	}

	// Last index at which each path was read, so only the final read of a
	// given path is kept verbatim.
	lastRead := make(map[string]int)
	// Last index at which the exact same search (tool + canonicalized input)
	// was issued, so a search dump is only pruned when actually superseded by
	// an identical later call — not merely because it's old (P9). "Already
	// acted on" was previously an assumption from turn position alone; a
	// model that returns to an old search result many turns later without
	// re-running the search would otherwise find the detail already gone.
	lastSearchCall := make(map[string]int)
	// succeeded records tool_use IDs that produced a non-error result. Only a
	// write_file/edit_file whose result confirms the mutation landed is safe
	// to blank in its Input — a failed write left nothing on disk to re-read
	// (P36.2).
	succeeded := make(map[string]bool)
	for i, m := range msgs {
		for _, blk := range m.Content {
			tr, ok := blk.(provider.ToolResultBlock)
			if !ok {
				continue
			}
			if !tr.IsError {
				succeeded[tr.ToolUseID] = true
			}
			u, ok := uses[tr.ToolUseID]
			if !ok {
				continue
			}
			if u.name == "read_file" && u.path != "" {
				lastRead[u.path] = i
			}
			if u.callID != "" {
				lastSearchCall[u.callID] = i
			}
		}
	}

	pruned := 0
	out := make([]provider.Message, len(msgs))
	copy(out, msgs)

	for i := 0; i < cutoff; i++ {
		m := out[i]
		changed := false
		newContent := make([]provider.Block, len(m.Content))
		copy(newContent, m.Content)

		for bi, blk := range newContent {
			// write_file/edit_file store the verbatim file content in the
			// tool_use *Input* (call arguments), not the tool_result, so this
			// is the only place the loop rewrites a ToolUseBlock. Once the
			// mutation has a successful result the bytes live durably on disk
			// and are re-readable, so resending them every subsequent turn is
			// pure dead weight (a live threat-model run resent 18-88KB writes
			// unchanged for the rest of the conversation) — P36.2.
			if tu, ok := blk.(provider.ToolUseBlock); ok {
				if succeeded[tu.ID] {
					if input, removed, did := pruneWriteEditInput(tu); did {
						tu.Input = input
						pruned += removed
						newContent[bi] = tu
						changed = true
					}
				}
				continue
			}
			tr, ok := blk.(provider.ToolResultBlock)
			if !ok || tr.IsError {
				continue
			}
			u := uses[tr.ToolUseID]
			switch {
			case u.name == "read_file" && u.path != "" && lastRead[u.path] > i:
				pruned += len(tr.Content)
				tr.Content = fmt.Sprintf("[pruned: %s was re-read later in the conversation; stale content dropped]", u.path)
				newContent[bi] = tr
				changed = true
			case u.name == "read_file" && isSkillReferencePath(u.path):
				// Skill reference files (a skill's SKILL.md and the reference
				// docs a run is told to load) are static content read once per
				// run — the read_file "re-read later" rule never fires because
				// there's no second read. But because the content never
				// changes and is trivially re-fetchable from the skill dir,
				// dropping it from the pre-keepRecent prefix is always safe:
				// unlike a normal file it can't have gone stale (P36.2).
				pruned += len(tr.Content)
				tr.Content = fmt.Sprintf("[pruned: %s is a static skill-reference file; content dropped, re-read if needed]", u.path)
				newContent[bi] = tr
				changed = true
			case searchDumpTools[u.name] && len(tr.Content) > staleSearchDumpThreshold && lastSearchCall[u.callID] > i:
				removed := len(tr.Content) - staleSearchKeepChars
				tr.Content = tr.Content[:staleSearchKeepChars] + fmt.Sprintf("\n…[%d chars pruned - stale %s dump, superseded by an identical later search]", removed, u.name)
				pruned += removed
				newContent[bi] = tr
				changed = true
			}
		}
		if changed {
			m.Content = newContent
			out[i] = m
		}
	}
	return out, pruned
}

// readFilePath extracts the "path" argument from a read_file tool_use input,
// best-effort; returns "" for any other tool or on parse failure.
func readFilePath(tu provider.ToolUseBlock) string {
	if tu.Name != "read_file" {
		return ""
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(tu.Input, &args); err != nil {
		return ""
	}
	return args.Path
}

// isSkillReferencePath reports whether a read_file path points inside a
// skill's own directory — either a project/user skill (".aegis/skills/") or a
// materialized built-in skill ("builtin-skills/", the MaterializeBuiltins /
// MaterializeBuiltinsToProject layout under <dataDir> or <workDir>/.aegis).
// Content there is static reference material for the run, so a read of it is
// disposable once superseded by the keepRecent window. Matched by path
// substring (normalizing Windows separators) rather than threading the skill
// roots through the compaction seam, which would touch every caller for no
// behavioral gain — these path segments are reserved for skills and never
// collide with ordinary workspace files.
func isSkillReferencePath(path string) bool {
	if path == "" {
		return false
	}
	p := strings.ReplaceAll(path, "\\", "/")
	return strings.Contains(p, ".aegis/skills/") || strings.Contains(p, "builtin-skills/")
}

// pruneWriteEditInput rewrites a successful write_file/edit_file tool_use
// Input, replacing its verbatim file-content field(s) with a short marker
// while preserving the structural fields (notably "path") so the block stays
// valid, well-formed JSON that still deserializes into the tool's argument
// struct on replay. It returns the new Input, the number of characters removed
// (the net shrink in the serialized Input), and whether anything was pruned —
// false for any non-write/edit tool, unparseable input, or a payload already
// small enough that blanking it wouldn't actually save space.
func pruneWriteEditInput(tu provider.ToolUseBlock) (json.RawMessage, int, bool) {
	if tu.Name == "multi_edit" {
		return pruneMultiEditInput(tu)
	}
	fields, ok := writeEditContentFields[tu.Name]
	if !ok {
		return nil, 0, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(tu.Input, &raw); err != nil {
		return nil, 0, false
	}
	var path string
	if p, ok := raw["path"]; ok {
		_ = json.Unmarshal(p, &path)
	}
	changed := false
	for _, f := range fields {
		if blankContentField(raw, f, pruneVerb(tu.Name), path) {
			changed = true
		}
	}
	if !changed {
		return nil, 0, false
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return nil, 0, false
	}
	removed := len(tu.Input) - len(out)
	if removed <= 0 {
		// Marker(s) didn't actually shrink the payload (tiny content); leave
		// the original Input untouched so accounting never goes negative.
		return nil, 0, false
	}
	return out, removed, true
}

// pruneMultiEditInput is pruneWriteEditInput's counterpart for multi_edit,
// whose Input is {"edits":[{"path","old_string","new_string"}, ...]}: the
// verbatim before/after content lives once per element, not at the top level.
// It blanks old_string/new_string in every edit (preserving each edit's path)
// so a successful multi_edit's snippets stop riding along in every subsequent
// request — the same dead-weight the flat edit_file case removes (P36.2).
func pruneMultiEditInput(tu provider.ToolUseBlock) (json.RawMessage, int, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(tu.Input, &raw); err != nil {
		return nil, 0, false
	}
	editsRaw, ok := raw["edits"]
	if !ok {
		return nil, 0, false
	}
	var edits []map[string]json.RawMessage
	if err := json.Unmarshal(editsRaw, &edits); err != nil {
		return nil, 0, false
	}
	changed := false
	for _, e := range edits {
		var path string
		if p, ok := e["path"]; ok {
			_ = json.Unmarshal(p, &path)
		}
		for _, f := range []string{"old_string", "new_string"} {
			if blankContentField(e, f, "edited in", path) {
				changed = true
			}
		}
	}
	if !changed {
		return nil, 0, false
	}
	nb, err := json.Marshal(edits)
	if err != nil {
		return nil, 0, false
	}
	raw["edits"] = nb
	out, err := json.Marshal(raw)
	if err != nil {
		return nil, 0, false
	}
	removed := len(tu.Input) - len(out)
	if removed <= 0 {
		return nil, 0, false
	}
	return out, removed, true
}

// blankContentField replaces the string value of raw[field] with a short
// prune marker (preserving JSON structure), leaving structural fields like
// "path" intact. It reports whether it actually blanked a non-empty string —
// false for a missing field, a non-string value, or an already-empty one.
func blankContentField(raw map[string]json.RawMessage, field, verb, path string) bool {
	v, ok := raw[field]
	if !ok {
		return false
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil || s == "" {
		return false
	}
	marker := fmt.Sprintf("[pruned: %d chars %s to %s; re-read the file if needed]", len(s), verb, path)
	mb, err := json.Marshal(marker)
	if err != nil {
		return false
	}
	raw[field] = mb
	return true
}

// pruneVerb picks a natural-reading verb for a write/edit prune marker.
func pruneVerb(tool string) string {
	if tool == "edit_file" {
		return "edited in"
	}
	return "written to"
}

// canonicalizeJSON re-marshals a tool call's raw JSON input with map keys in
// their (Go-guaranteed alphabetical) order, so two calls with the same
// arguments in a different field order still produce identical dedup keys.
// Input that isn't valid JSON is returned unchanged.
func canonicalizeJSON(input json.RawMessage) json.RawMessage {
	var v any
	if err := json.Unmarshal(input, &v); err != nil {
		return input
	}
	out, err := json.Marshal(v)
	if err != nil {
		return input
	}
	return out
}
