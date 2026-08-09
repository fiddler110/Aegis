package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/filetracker"
	"github.com/fiddler110/aegis/internal/tool"
)

// pendingMarkerRe matches the scaffolded placeholders a skill drive fills:
// `<!-- PENDING -->` and the keyed `<!-- PENDING: section-key -->`. Kept
// deliberately loose on inner whitespace, since the markers are written by
// several scaffold scripts.
var pendingMarkerRe = regexp.MustCompile(`<!--\s*PENDING\s*(?::\s*([^>]*?)\s*)?-->`)

// fillMarkerTool replaces one scaffolded `<!-- PENDING -->` marker with real
// content, selected by ordinal or by key rather than by reproducing text.
//
// It exists because anchored editing is the single hardest thing to ask of a
// small local model. edit_file requires old_string to reproduce bytes already
// in the file — exactly, from memory, through JSON escaping — and a model that
// gets one character wrong gets no partial credit, only "old_string not found
// in document". Measured (P38.1 re-test, 2026-08-09): the structured multi-turn
// fill probe failed reproducibly that way, six consecutive rounds, on a model
// that scored 100% on plain tool-calling. The reproduction is the failure, not
// the reasoning: the same model wrote a well-formed architecture document in
// the same run.
//
// The fill loop never actually needs reproduction. Every target is already a
// uniquely identifiable marker, so the model can name one with a small integer
// and supply only new text. A wrong selector is also recoverable here in a way
// a wrong old_string is not: the error enumerates the markers that do exist, so
// the next attempt has the answer in front of it rather than another guess.
type fillMarkerTool struct {
	root    string
	tracker *filetracker.Tracker
}

func (t *fillMarkerTool) Name() string                { return "fill_marker" }
func (t *fillMarkerTool) Capability() tool.Capability { return tool.CapWrite }
func (t *fillMarkerTool) Description() string {
	return "Replace one `<!-- PENDING -->` placeholder in a scaffolded file with real content. Select the marker by its 1-based `index`, or by `key` for a keyed `<!-- PENDING: key -->` marker. Call with only `path` to list the file's remaining markers. Prefer this over edit_file for filling scaffolded documents: it needs no exact-text match."
}
func (t *fillMarkerTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string","description":"workspace-relative path to the scaffolded file"},"index":{"type":"integer","description":"1-based position of the marker to fill, as reported by listing. Omit both index and key to list the file's markers instead of filling one."},"key":{"type":"string","description":"key of a keyed PENDING marker, as an alternative to index"},"content":{"type":"string","description":"text replacing the whole marker comment; required when filling"}},"required":["path"]}`)
}
func (t *fillMarkerTool) OutputSchema() json.RawMessage {
	return schema(`{"type":"object","properties":{"path":{"type":"string"},"filled":{"type":"integer"},"remaining":{"type":"integer"}},"required":["path"]}`)
}

// marker is one located placeholder: where it sits, and the key it carries.
type marker struct {
	start, end int
	key        string
}

func findPendingMarkers(content string) []marker {
	locs := pendingMarkerRe.FindAllStringSubmatchIndex(content, -1)
	out := make([]marker, 0, len(locs))
	for _, m := range locs {
		mk := marker{start: m[0], end: m[1]}
		if m[2] >= 0 {
			mk.key = content[m[2]:m[3]]
		}
		out = append(out, mk)
	}
	return out
}

// describeMarkers renders the enumerated list used both for an explicit listing
// and for a failed selector — the same text either way, so a model that
// mis-selects gets exactly the information it needed in the first place.
func describeMarkers(path, content string, markers []marker) string {
	if len(markers) == 0 {
		return fmt.Sprintf("%s has no remaining `<!-- PENDING -->` markers", path)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s has %d remaining marker(s):\n", path, len(markers))
	for i, mk := range markers {
		line := 1 + strings.Count(content[:mk.start], "\n")
		if mk.key != "" {
			fmt.Fprintf(&b, "  index %d — key %q (line %d)\n", i+1, mk.key, line)
		} else {
			fmt.Fprintf(&b, "  index %d — (no key, line %d)\n", i+1, line)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (t *fillMarkerTool) Execute(ctx context.Context, input json.RawMessage) (tool.Result, error) {
	var args struct {
		Path    string  `json:"path"`
		Index   *int    `json:"index"`
		Key     *string `json:"key"`
		Content *string `json:"content"`
	}
	if err := parseArgs(input, &args); err != nil {
		return tool.Result{}, err
	}
	abs, err := resolveWrite(ctx, t.root, args.Path)
	if err != nil {
		return tool.Result{}, err
	}
	content, errMsg := readTextForEdit(abs, args.Path, effectiveRoot(ctx, t.root))
	if errMsg != "" {
		return tool.Result{Content: errMsg, IsError: true}, nil
	}
	markers := findPendingMarkers(content)

	// No selector: this is a listing request, which is a read and must not trip
	// the write tracker or leave a checkpoint.
	if args.Index == nil && args.Key == nil {
		return tool.Result{Content: describeMarkers(args.Path, content, markers)}, nil
	}
	if args.Content == nil {
		return tool.Result{Content: "content is required when filling a marker (omit index and key to list markers instead)", IsError: true}, nil
	}
	if len(markers) == 0 {
		return tool.Result{Content: describeMarkers(args.Path, content, markers), IsError: true}, nil
	}

	sel, errMsg := selectMarker(args.Path, content, markers, args.Index, args.Key)
	if errMsg != "" {
		return tool.Result{Content: errMsg, IsError: true}, nil
	}

	if t.tracker != nil {
		if err := t.tracker.CheckWrite(abs); err != nil {
			return tool.Result{Content: err.Error(), IsError: true}, nil
		}
	}
	updated := content[:markers[sel].start] + *args.Content + content[markers[sel].end:]
	checkpoint.SnapshotterFrom(ctx).Capture(abs)
	if err := writePreservingMode(abs, []byte(updated)); err != nil {
		return tool.Result{Content: fmt.Sprintf("write failed: %v", err), IsError: true}, nil
	}
	if t.tracker != nil {
		t.tracker.RecordWrite(abs)
		t.tracker.RecordAgentWrite(abs, content, updated)
	}
	// Report the *new* listing, not just a count. Filling a marker renumbers
	// every marker after it, so any listing the model is working from goes
	// stale the moment it succeeds — which showed up live as a run of
	// "index N is out of range" errors from a model doing nothing wrong
	// (qwen3:14b, 2026-08-09). Handing back fresh indices with the success
	// removes the whole error class instead of recovering from it, and costs a
	// few lines on a call the model was making anyway.
	remaining := findPendingMarkers(updated)
	msg := fmt.Sprintf("filled marker %d in %s.", sel+1, args.Path)
	if len(remaining) == 0 {
		return tool.Result{Content: msg + " No markers remain in this file."}, nil
	}
	return tool.Result{Content: msg + " Indices have shifted — " + describeMarkers(args.Path, updated, remaining)}, nil
}

// selectMarker resolves index/key to a position, returning a caller-facing
// message on failure that always enumerates what is actually there.
func selectMarker(path, content string, markers []marker, index *int, key *string) (int, string) {
	if index != nil {
		i := *index - 1
		if i < 0 || i >= len(markers) {
			return 0, fmt.Sprintf("index %d is out of range.\n%s", *index, describeMarkers(path, content, markers))
		}
		// A key given alongside an index must agree, or the model is working
		// from a stale listing and the index it picked belongs to something
		// else — silently trusting the index would fill the wrong section.
		if key != nil && *key != "" && markers[i].key != *key {
			return 0, fmt.Sprintf("index %d has key %q, not %q — the listing this came from is stale.\n%s",
				*index, markers[i].key, *key, describeMarkers(path, content, markers))
		}
		return i, ""
	}
	var hits []int
	for i, mk := range markers {
		if mk.key == *key {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], ""
	case 0:
		return 0, fmt.Sprintf("no marker with key %q.\n%s", *key, describeMarkers(path, content, markers))
	default:
		return 0, fmt.Sprintf("key %q matches %d markers — select one by index instead.\n%s", *key, len(hits), describeMarkers(path, content, markers))
	}
}
