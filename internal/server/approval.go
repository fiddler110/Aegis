// The daemon's interactive approval seam: an engine-side Approver that turns
// a permission Ask into an SSE event and blocks on the operator's answer.
// Extracted from server.go (L4).
package server

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/fiddler110/aegis/internal/api"
)

// approvalDecision carries the client's answer to an interactive approval prompt.
type approvalDecision struct {
	Approved    bool
	AllowAlways bool
	Pattern     string // non-empty: persist "allow tool(pattern)" instead of caching per-tool (TQ6)
}

// sseApprover implements permission.Approver by sending a KindApprovalRequest
// SSE event and blocking until the client POSTs a /sessions/{id}/approve answer.
// The runID is echoed to the client so the approval reply is matched to this
// specific run, preventing a concurrent run on the same session from consuming
// the answer. AllowAlways decisions are stored in permCache so future calls to
// the same tool within the session are auto-approved without prompting.
type sseApprover struct {
	send      func(api.Event)
	ch        <-chan approvalDecision
	runID     string
	sessionID string
	permCache *sync.Map // key: sessionID+"\x00"+toolName → struct{}

	// persistRule installs a pattern-scoped "allow tool(pattern)" permission
	// rule when the client answers allow-always with a pattern (TQ6). May be
	// nil (e.g. tests), in which case the per-tool cache is used instead.
	persistRule func(toolName, pattern string)
}

func (a *sseApprover) Approve(ctx context.Context, toolName, reason string, input json.RawMessage) bool {
	// Check session-scoped allow-always cache before prompting.
	cacheKey := a.sessionID + "\x00" + toolName
	if _, ok := a.permCache.Load(cacheKey); ok {
		return true
	}
	a.send(api.Event{
		Kind:           api.KindApprovalRequest,
		Tool:           toolName,
		ToolInput:      input,
		ApprovalReason: reason,
		ApprovalID:     a.runID,
	})
	select {
	case d := <-a.ch:
		if d.AllowAlways && d.Approved {
			// A pattern-scoped rule beats the whole-tool cache: approving
			// "npm test*" must not silently approve every future shell call.
			if d.Pattern != "" && a.persistRule != nil {
				a.persistRule(toolName, d.Pattern)
			} else {
				a.permCache.Store(cacheKey, struct{}{})
			}
		}
		return d.Approved
	case <-ctx.Done():
		return false
	}
}
