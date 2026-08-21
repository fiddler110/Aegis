package engine

import (
	"encoding/json"
	"fmt"

	"github.com/fiddler110/aegis/internal/provider"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// P74.16: a reactive clip path for a turn the provider rejected as too large,
// alongside the proactive per-call/per-round caps in
// internal/tool/builtin/truncate.go and roundcap.go. Those all fire on Aegis's
// own size estimate before a request is sent; this fires after the provider
// itself said no, on the one batch that caused it.

// maxOverflowClipRounds bounds how many times one run may clip a trailing
// tool-result batch and retry after a context-overflow error. Without a bound,
// a window too small to hold even one clipped result would clip forever
// without ever making progress — the same failure mode
// maxPhase6OverflowResets bounds at the phased drive's whole-conversation-reset
// granularity (internal/drive/drive.go).
const maxOverflowClipRounds = 3

// overflowClipKeepBytes is how much of each oversized tool result survives a
// clip. Small enough that clipping several results in one batch frees real
// headroom, large enough that what remains is still useful context rather than
// a bare pointer.
const overflowClipKeepBytes = 2000

// clipOverflowBatch clips the most recent tool-result-bearing message in conv
// after a context-overflow error, so the caller can retry the same turn
// without resetting the whole conversation. It returns false when there is
// nothing left to clip — no trailing tool-result message, or every result in
// it already fits within overflowClipKeepBytes — which tells the caller to
// give up rather than loop.
//
// Only the most recent tool-result batch is a candidate: it is what just
// overflowed the request, and reaching further back would clip content the
// model has already reasoned over in a reply that followed it.
//
// A read_file result is head-sliced with a pointer back to the file it read —
// no new write is needed, because the content already exists at that path and
// read_file's own offset/limit lets the model page the rest back in. Every
// other kind collapses to a stub naming what ran and how much was discarded;
// the posture table in truncate.go's head/tail convention is a property each
// tool declares about *itself*, which this package cannot guess for a result
// it did not produce.
func clipOverflowBatch(conv *Conversation) bool {
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		msg := &conv.Messages[i]
		if msg.Role != provider.RoleUser {
			continue
		}
		hasToolResult := false
		for _, blk := range msg.Content {
			if _, ok := blk.(provider.ToolResultBlock); ok {
				hasToolResult = true
				break
			}
		}
		if !hasToolResult {
			continue
		}
		clipped := false
		for j, blk := range msg.Content {
			tr, ok := blk.(provider.ToolResultBlock)
			if !ok || len(tr.Content) <= overflowClipKeepBytes {
				continue
			}
			name, input := findToolUse(conv.Messages[:i], tr.ToolUseID)
			tr.Content = clipToolResultContent(name, input, tr.Content)
			msg.Content[j] = tr
			clipped = true
		}
		if clipped {
			conv.invalidate()
		}
		return clipped
	}
	return false
}

// findToolUse looks back through the messages preceding a tool result for the
// tool_use block it answers, returning the tool's name and raw input. Both are
// empty when no match is found (an already-repaired or orphaned result).
func findToolUse(before []provider.Message, toolUseID string) (name string, input json.RawMessage) {
	for i := len(before) - 1; i >= 0; i-- {
		for _, blk := range before[i].Content {
			if tu, ok := blk.(provider.ToolUseBlock); ok && tu.ID == toolUseID {
				return tu.Name, tu.Input
			}
		}
	}
	return "", nil
}

// clipToolResultContent shrinks one oversized tool result.
func clipToolResultContent(toolName string, input json.RawMessage, content string) string {
	if toolName == "read_file" {
		if path := readFilePathArg(input); path != "" {
			kept, _ := builtin.TruncateHead(content, overflowClipKeepBytes,
				fmt.Sprintf("re-read %s with offset/limit for the rest — it is still on disk", path))
			return kept
		}
	}
	displayName := toolName
	if displayName == "" {
		displayName = "tool"
	}
	return fmt.Sprintf("[clipped after a context-overflow retry: this %s result (%d bytes) was dropped to make room; re-issue the call if you still need it]", displayName, len(content))
}

// readFilePathArg extracts read_file's "path" argument from a tool_use's raw
// input, matching internal/tool/builtin.readTool's own field name. Empty on
// any parse failure or missing field.
func readFilePathArg(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return ""
	}
	return args.Path
}
