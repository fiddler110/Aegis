package compaction

import (
	"encoding/json"
	"fmt"

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

// pruneStaleToolResults deterministically shrinks the prefix of msgs (every
// message before the trailing keepRecent window) by:
//   - blanking read_file results for a path that was read again later
//   - truncating large grep/glob/ls dumps to a short preview
//
// This runs as a cheap pre-pass before falling back to LLM summarization: it
// never rewrites conversational text, only stale tool_result content, so it
// preserves exact wording for everything the model actually said. Returns the
// possibly-modified list and the number of characters removed.
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
	for i, m := range msgs {
		for _, blk := range m.Content {
			tr, ok := blk.(provider.ToolResultBlock)
			if !ok {
				continue
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
